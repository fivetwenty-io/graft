package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/fivetwenty-io/graft/internal/config"
	"github.com/fivetwenty-io/graft/internal/features"
	"github.com/fivetwenty-io/graft/pkg/graft"
)

// This file proves the precedence order CLI flag > env var > config file >
// default end-to-end for every setting that actually overlaps across those
// tiers today.
//
// No CLI flag currently exists for any of internal/config's or
// internal/features' settings (grep the flag definitions in newRootCmd: no
// --cache/--parallel/--concurrency/--metrics/--strict-mode flag exists, and
// none is invented here). So every case below tests the chain that
// actually exists: env > file > default for internal/config fields (3
// tiers - config.Load + config.ApplyEnv via resolveStartupConfig), and
// env > default for internal/features flags (2 tiers -
// FeatureFlags.LoadFromEnv via resolveStartupFeatureFlags; feature flags
// have no --config file section). If a CLI flag is added for one of these
// settings in the future, its test case belongs here too, one tier higher.

// configPrecedenceCase describes one internal/config field's expected value
// at each precedence tier: default (nothing set), file (only the config
// file sets it), and env (both file and env set it - env must win).
type configPrecedenceCase struct {
	name     string
	fileYAML string
	envVar   string
	envValue string
	get      func(cfg *config.Config) interface{}
	wantDef  interface{}
	wantFile interface{}
	wantEnv  interface{}
}

func configPrecedenceCases() []configPrecedenceCase {
	return []configPrecedenceCase{
		{
			name:     "engine.strict_mode",
			fileYAML: "engine:\n  strict_mode: true\n",
			envVar:   config.EnvEngineStrictMode, envValue: "false",
			get:     func(c *config.Config) interface{} { return c.Engine.StrictMode },
			wantDef: false, wantFile: true, wantEnv: false,
		},
		{
			name:     "engine.max_recursion",
			fileYAML: "engine:\n  max_recursion: 50\n",
			envVar:   config.EnvEngineMaxRecursion, envValue: "25",
			get:     func(c *config.Config) interface{} { return c.Engine.MaxRecursion },
			wantDef: 100, wantFile: 50, wantEnv: 25,
		},
		{
			name:     "engine.timeout",
			fileYAML: "engine:\n  timeout: 10s\n",
			envVar:   config.EnvEngineTimeout, envValue: "5s",
			get:     func(c *config.Config) interface{} { return c.Engine.Timeout },
			wantDef: 30 * time.Second, wantFile: 10 * time.Second, wantEnv: 5 * time.Second,
		},
		{
			name:     "cache.enabled",
			fileYAML: "cache:\n  enabled: false\n",
			envVar:   config.EnvCacheEnabled, envValue: "true",
			get:     func(c *config.Config) interface{} { return c.Cache.Enabled },
			wantDef: true, wantFile: false, wantEnv: true,
		},
		{
			name:     "cache.max_size",
			fileYAML: "cache:\n  max_size: 500\n",
			envVar:   config.EnvCacheMaxSize, envValue: "250",
			get:     func(c *config.Config) interface{} { return c.Cache.MaxSize },
			wantDef: 10000, wantFile: 500, wantEnv: 250,
		},
		{
			name:     "cache.ttl",
			fileYAML: "cache:\n  ttl: 2m\n",
			envVar:   config.EnvCacheTTL, envValue: "1m",
			get:     func(c *config.Config) interface{} { return c.Cache.TTL },
			wantDef: 5 * time.Minute, wantFile: 2 * time.Minute, wantEnv: 1 * time.Minute,
		},
		{
			name:     "cache.l2_enabled",
			fileYAML: "cache:\n  l2_enabled: true\n  l2_path: /tmp/graft-l2\n",
			envVar:   config.EnvCacheL2Enabled, envValue: "false",
			get:     func(c *config.Config) interface{} { return c.Cache.L2Enabled },
			wantDef: false, wantFile: true, wantEnv: false,
		},
		{
			name:     "cache.l2_path",
			fileYAML: "cache:\n  l2_path: /tmp/l2-file\n",
			envVar:   config.EnvCacheL2Path, envValue: "/tmp/l2-env",
			get:     func(c *config.Config) interface{} { return c.Cache.L2Path },
			wantDef: "", wantFile: "/tmp/l2-file", wantEnv: "/tmp/l2-env",
		},
		{
			name:     "parallel.enabled",
			fileYAML: "parallel:\n  enabled: false\n",
			envVar:   config.EnvParallelEnabled, envValue: "true",
			get:     func(c *config.Config) interface{} { return c.Parallel.Enabled },
			wantDef: true, wantFile: false, wantEnv: true, // default is parallel-on
		},
		{
			name:     "parallel.min_workers",
			fileYAML: "parallel:\n  min_workers: 2\n",
			envVar:   config.EnvParallelMinWorkers, envValue: "3",
			get:     func(c *config.Config) interface{} { return c.Parallel.MinWorkers },
			wantDef: 1, wantFile: 2, wantEnv: 3,
		},
		{
			name:     "parallel.max_workers",
			fileYAML: "parallel:\n  max_workers: 4\n",
			envVar:   config.EnvParallelMaxWorkers, envValue: "8",
			get:     func(c *config.Config) interface{} { return c.Parallel.MaxWorkers },
			wantDef: 0, wantFile: 4, wantEnv: 8, // 0 = auto (runtime.NumCPU, see resolveConcurrency)
		},
		{
			name:     "metrics.enabled",
			fileYAML: "metrics:\n  enabled: true\n",
			envVar:   config.EnvMetricsEnabled, envValue: "false",
			get:     func(c *config.Config) interface{} { return c.Metrics.Enabled },
			wantDef: false, wantFile: true, wantEnv: false,
		},
		{
			name:     "metrics.format",
			fileYAML: "metrics:\n  format: json\n",
			envVar:   config.EnvMetricsFormat, envValue: "text",
			get:     func(c *config.Config) interface{} { return c.Metrics.Format },
			wantDef: "prometheus", wantFile: "json", wantEnv: "text",
		},
		{
			name:     "metrics.endpoint",
			fileYAML: "metrics:\n  endpoint: /custom-metrics\n",
			envVar:   config.EnvMetricsEndpoint, envValue: "/env-metrics",
			get:     func(c *config.Config) interface{} { return c.Metrics.Endpoint },
			wantDef: "/metrics", wantFile: "/custom-metrics", wantEnv: "/env-metrics",
		},
		{
			name:     "logging.level",
			fileYAML: "logging:\n  level: debug\n",
			envVar:   config.EnvLoggingLevel, envValue: "error",
			get:     func(c *config.Config) interface{} { return c.Logging.Level },
			wantDef: "info", wantFile: "debug", wantEnv: "error",
		},
		{
			name:     "logging.format",
			fileYAML: "logging:\n  format: json\n",
			envVar:   config.EnvLoggingFormat, envValue: "text",
			get:     func(c *config.Config) interface{} { return c.Logging.Format },
			wantDef: "text", wantFile: "json", wantEnv: "text",
		},
	}
}

// TestConfigPrecedenceEnvOverFileOverDefault proves, for every
// internal/config field (all 16), that resolveStartupConfig resolves it as
// default when nothing is set, the config file's value when only a file is
// given, and the GRAFT_* env var's value when both a file and the env var
// are set (env wins over file).
func TestConfigPrecedenceEnvOverFileOverDefault(t *testing.T) {
	for _, tc := range configPrecedenceCases() {
		t.Run(tc.name, func(t *testing.T) {
			// Tier 1: default (nothing set).
			cfg, err := resolveStartupConfig("")
			if err != nil {
				t.Fatalf("default tier: resolveStartupConfig() error = %v", err)
			}
			if got := tc.get(cfg); !reflect.DeepEqual(got, tc.wantDef) {
				t.Errorf("default tier: %s = %v, want %v", tc.name, got, tc.wantDef)
			}

			// Tier 2: config file only.
			path := filepath.Join(t.TempDir(), "config.yaml")
			if err := os.WriteFile(path, []byte(tc.fileYAML), 0o600); err != nil {
				t.Fatalf("writing config file: %v", err)
			}
			cfg, err = resolveStartupConfig(path)
			if err != nil {
				t.Fatalf("file tier: resolveStartupConfig() error = %v", err)
			}
			if got := tc.get(cfg); !reflect.DeepEqual(got, tc.wantFile) {
				t.Errorf("file tier: %s = %v, want %v (file must override default)", tc.name, got, tc.wantFile)
			}

			// Tier 3: config file + env var (env must win).
			t.Setenv(tc.envVar, tc.envValue)
			cfg, err = resolveStartupConfig(path)
			if err != nil {
				t.Fatalf("env tier: resolveStartupConfig() error = %v", err)
			}
			if got := tc.get(cfg); !reflect.DeepEqual(got, tc.wantEnv) {
				t.Errorf("env tier: %s = %v, want %v (env must override file)", tc.name, got, tc.wantEnv)
			}
		})
	}
}

// featurePrecedenceCase describes one internal/features flag's expected
// value at each precedence tier. Feature flags have no --config file
// section, so there are only 2 tiers: default and env.
type featurePrecedenceCase struct {
	name     string
	flag     string
	envVar   string
	envValue string
	wantDef  bool
	wantEnv  bool
}

func featurePrecedenceCases() []featurePrecedenceCase {
	return []featurePrecedenceCase{
		{
			name: "caching", flag: features.FeatureCaching,
			envVar: features.EnvFeatureCache, envValue: "false",
			wantDef: true, wantEnv: false,
		},
		{
			name: "memory_pools", flag: features.FeatureMemoryPools,
			envVar: features.EnvFeaturePools, envValue: "false",
			wantDef: true, wantEnv: false,
		},
		{
			name: "metrics", flag: features.FeatureMetrics,
			envVar: features.EnvFeatureMetrics, envValue: "true",
			wantDef: false, wantEnv: true,
		},
		{
			name: "debug_logging", flag: features.FeatureDebugLogging,
			envVar: features.EnvFeatureDebug, envValue: "true",
			wantDef: false, wantEnv: true,
		},
		{
			name: "strict_type_checking", flag: features.FeatureStrictTypeChecking,
			envVar: features.EnvFeatureStrictTypes, envValue: "true",
			wantDef: false, wantEnv: true,
		},
		{
			// FeatureParallelEvaluation's own library default is false
			// (internal/features.DefaultFlags' own conservative default,
			// intentionally left untouched). It is exercised here at the
			// resolveStartupFeatureFlags level only; the CLI's actual "is
			// parallel on" decision ANDs internal/config's Parallel.Enabled
			// with any explicit GRAFT_FEATURE_PARALLEL setting (see
			// TestParallelPrecedenceEitherGateDisables below), which is
			// where the parallel-on-by-default guarantee lives.
			name: "parallel_evaluation", flag: features.FeatureParallelEvaluation,
			envVar: features.EnvFeatureParallel, envValue: "true",
			wantDef: false, wantEnv: true,
		},
	}
}

// TestFeatureFlagPrecedenceEnvOverDefault proves, for every
// internal/features flag (all 6), that resolveStartupFeatureFlags resolves
// it as its library default when no GRAFT_FEATURE_* var is set, and the
// env var's value when it is set.
func TestFeatureFlagPrecedenceEnvOverDefault(t *testing.T) {
	for _, tc := range featurePrecedenceCases() {
		t.Run(tc.name, func(t *testing.T) {
			ff := resolveStartupFeatureFlags()
			if got := ff.IsEnabled(tc.flag); got != tc.wantDef {
				t.Errorf("default tier: %s = %v, want %v", tc.name, got, tc.wantDef)
			}

			t.Setenv(tc.envVar, tc.envValue)
			ff = resolveStartupFeatureFlags()
			if got := ff.IsEnabled(tc.flag); got != tc.wantEnv {
				t.Errorf("env tier: %s = %v, want %v (env must override default)", tc.name, got, tc.wantEnv)
			}
		})
	}
}

// TestParallelPrecedenceEitherGateDisables locks in the contract for the
// one setting with two independent env-var paths:
// internal/config.Parallel.Enabled (GRAFT_PARALLEL_ENABLED) and
// internal/features.FeatureParallelEvaluation (GRAFT_FEATURE_PARALLEL).
// Parallel evaluation runs only when both agree it should: an operator
// who sets either variable to false gets what they asked for. The
// previous wiring applied graft.WithParallel(cfg.Parallel.Enabled) after
// graft.WithFeatureFlags(ff), silently clobbering an explicit
// GRAFT_FEATURE_PARALLEL=false with the config default of true; that
// left one documented kill switch inert, which is asserted away here.
func TestParallelPrecedenceEitherGateDisables(t *testing.T) {
	newEngine := func(t *testing.T, cfg *config.Config, ff *features.FeatureFlags) *graft.DefaultEngine {
		t.Helper()
		engine, err := graft.NewEngine(configEngineOpts(cfg, ff)...)
		if err != nil {
			t.Fatalf("graft.NewEngine() error = %v", err)
		}
		de, ok := engine.(*graft.DefaultEngine)
		if !ok {
			t.Fatalf("graft.NewEngine() returned %T, want *graft.DefaultEngine", engine)
		}
		return de
	}

	t.Run("both gates default true keeps parallel on", func(t *testing.T) {
		de := newEngine(t, config.DefaultConfig(), features.DefaultFlags())
		if de.GetWorkerPool() == nil {
			t.Error("expected a worker pool: both gates default to enabled")
		}
	})

	t.Run("GRAFT_FEATURE_PARALLEL=false disables despite config default", func(t *testing.T) {
		t.Setenv(features.EnvFeatureParallel, "false")
		cfg := config.DefaultConfig() // Parallel.Enabled == true

		de := newEngine(t, cfg, resolveStartupFeatureFlags())
		if de.GetWorkerPool() != nil {
			t.Error("expected no worker pool: an explicit feature-flag disable must stick")
		}
	})

	t.Run("config Parallel.Enabled=false disables despite feature flag", func(t *testing.T) {
		t.Setenv(features.EnvFeatureParallel, "true")
		cfg := config.DefaultConfig()
		cfg.Parallel.Enabled = false

		de := newEngine(t, cfg, resolveStartupFeatureFlags())
		if de.GetWorkerPool() != nil {
			t.Error("expected no worker pool: cfg.Parallel.Enabled=false must win over an enabled feature flag")
		}
	})
}

// TestDefaultTierReproducesCompatBaseline proves the DEFAULT tier (no
// --config flag, no GRAFT_* / GRAFT_FEATURE_* env vars, no config file)
// reproduces the required spruce-compat baseline: parallel evaluation on
// (a runtime.NumCPU()-derived default), cache on (unchanged CLI behavior -
// mergeAllDocs' unconditional WithCache(true, 1000), gated only by
// FeatureCaching which defaults to true), and metrics off
// (FeatureMetrics/EnableMetrics both default false/unset, and nothing in
// the CLI ever requests WithMetrics(true)).
func TestDefaultTierReproducesCompatBaseline(t *testing.T) {
	cfg, err := resolveStartupConfig("")
	if err != nil {
		t.Fatalf("resolveStartupConfig(\"\") error = %v", err)
	}
	ff := resolveStartupFeatureFlags()

	opts := append(configEngineOpts(cfg, ff), graft.WithCache(true, 1000))
	engine, err := graft.NewEngine(opts...)
	if err != nil {
		t.Fatalf("graft.NewEngine() error = %v", err)
	}
	de, ok := engine.(*graft.DefaultEngine)
	if !ok {
		t.Fatalf("graft.NewEngine() returned %T, want *graft.DefaultEngine", engine)
	}

	if de.GetWorkerPool() == nil {
		t.Error("default tier: expected parallel evaluation on (a non-nil worker pool)")
	}
	if de.GetCache() == nil {
		t.Error("default tier: expected caching on (a non-nil cache), matching today's CLI behavior")
	}
	if de.GetMetricsRegistry() != nil {
		t.Error("default tier: expected metrics off (a nil metrics registry)")
	}
}

// --theme is the first setting with a working CLI flag tier (see this
// file's header note above): flag > env > default, resolved by
// resolveThemeTier (cmd/graft/main.go), exercised here directly rather
// than through resolveStartupConfig since --theme has no internal/config
// or config-file tier at all this release.

// TestThemePrecedenceFlagOverEnvOverDefault proves resolveThemeTier's
// three tiers: default ("auto", nothing set), env only (GRAFT_THEME
// wins over the default), and flag plus env together (the flag must
// win over GRAFT_THEME).
func TestThemePrecedenceFlagOverEnvOverDefault(t *testing.T) {
	t.Run("default tier: neither flag nor env set", func(t *testing.T) {
		theme, valid, warnings := resolveThemeTier(false, "auto", "", false)
		if !valid {
			t.Fatalf("resolveThemeTier() flagValid = false, want true")
		}
		if theme != "auto" {
			t.Errorf("resolveThemeTier() theme = %q, want %q", theme, "auto")
		}
		if len(warnings) != 0 {
			t.Errorf("resolveThemeTier() warnings = %v, want none", warnings)
		}
	})

	t.Run("env tier: GRAFT_THEME wins over the default", func(t *testing.T) {
		theme, valid, warnings := resolveThemeTier(false, "auto", "light", false)
		if !valid {
			t.Fatalf("resolveThemeTier() flagValid = false, want true")
		}
		if theme != "light" {
			t.Errorf("resolveThemeTier() theme = %q, want %q (env must override default)", theme, "light")
		}
		if len(warnings) != 0 {
			t.Errorf("resolveThemeTier() warnings = %v, want none", warnings)
		}
	})

	t.Run("flag tier: --theme wins over GRAFT_THEME", func(t *testing.T) {
		theme, valid, warnings := resolveThemeTier(true, "mono", "light", false)
		if !valid {
			t.Fatalf("resolveThemeTier() flagValid = false, want true")
		}
		if theme != "mono" {
			t.Errorf("resolveThemeTier() theme = %q, want %q (flag must override env)", theme, "mono")
		}
		if len(warnings) != 0 {
			t.Errorf("resolveThemeTier() warnings = %v, want none", warnings)
		}
	})
}

// TestThemeInvalidValuePerTier proves decision 14's asymmetric
// invalid-value policy: an invalid --theme flag value is rejected
// (flagValid = false, so the caller exits 1), while an invalid
// GRAFT_THEME value warns once and falls through to the default tier
// instead of failing.
func TestThemeInvalidValuePerTier(t *testing.T) {
	t.Run("invalid flag value is rejected, not silently substituted", func(t *testing.T) {
		theme, valid, warnings := resolveThemeTier(true, "bogus", "", false)
		if valid {
			t.Fatalf("resolveThemeTier() flagValid = true for %q, want false", "bogus")
		}
		if theme != "" {
			t.Errorf("resolveThemeTier() theme = %q on an invalid flag, want empty (caller must not use it)", theme)
		}
		if len(warnings) != 0 {
			t.Errorf("resolveThemeTier() warnings = %v, want none (the caller prints its own exit-1 message)", warnings)
		}
	})

	t.Run("invalid env value warns and falls through to the default tier", func(t *testing.T) {
		theme, valid, warnings := resolveThemeTier(false, "auto", "bogus", false)
		if !valid {
			t.Fatalf("resolveThemeTier() flagValid = false, want true (an invalid env value must never abort)")
		}
		if theme != "auto" {
			t.Errorf("resolveThemeTier() theme = %q, want %q (falls through to the flag's own default)", theme, "auto")
		}
		if len(warnings) != 1 {
			t.Fatalf("resolveThemeTier() warnings = %v, want exactly one", warnings)
		}
		if !strings.Contains(warnings[0], "bogus") || !strings.Contains(warnings[0], themeEnvVar) {
			t.Errorf("resolveThemeTier() warning = %q, want it to name %q and the bad value", warnings[0], themeEnvVar)
		}
	})

	t.Run("an explicit --theme flag still validated even when GRAFT_THEME is also set", func(t *testing.T) {
		theme, valid, warnings := resolveThemeTier(true, "bogus", "dark", false)
		if valid {
			t.Fatalf("resolveThemeTier() flagValid = true for %q, want false", "bogus")
		}
		if theme != "" || len(warnings) != 0 {
			t.Errorf("resolveThemeTier() = (%q, %v), want (\"\", nil) on an invalid flag", theme, warnings)
		}
	})
}

// TestThemeEnvVarMisspellingWarning proves GRAFT_UI_THEME - the
// mechanical GRAFT_<SECTION>_<FIELD> name a user might reach for instead
// of GRAFT_THEME - triggers a one-line warning only when GRAFT_THEME
// itself is unset; the two warnings never both appear, since a set
// GRAFT_THEME (valid or not) already tells the resolver which var the
// user meant.
func TestThemeEnvVarMisspellingWarning(t *testing.T) {
	t.Run("GRAFT_UI_THEME alone warns and resolves to the default", func(t *testing.T) {
		theme, valid, warnings := resolveThemeTier(false, "auto", "", true)
		if !valid || theme != "auto" {
			t.Fatalf("resolveThemeTier() = (%q, %v), want (\"auto\", true)", theme, valid)
		}
		if len(warnings) != 1 {
			t.Fatalf("resolveThemeTier() warnings = %v, want exactly one", warnings)
		}
		if !strings.Contains(warnings[0], themeEnvVarMisspelling) || !strings.Contains(warnings[0], themeEnvVar) {
			t.Errorf("resolveThemeTier() warning = %q, want it to name both %q and %q", warnings[0], themeEnvVarMisspelling, themeEnvVar)
		}
	})

	t.Run("GRAFT_THEME set alongside GRAFT_UI_THEME suppresses the misspelling warning", func(t *testing.T) {
		theme, valid, warnings := resolveThemeTier(false, "auto", "light", true)
		if !valid || theme != "light" {
			t.Fatalf("resolveThemeTier() = (%q, %v), want (\"light\", true)", theme, valid)
		}
		if len(warnings) != 0 {
			t.Errorf("resolveThemeTier() warnings = %v, want none: GRAFT_THEME is set, so GRAFT_UI_THEME is not the mistake here", warnings)
		}
	})

	t.Run("an explicit --theme flag also suppresses the misspelling warning", func(t *testing.T) {
		theme, valid, warnings := resolveThemeTier(true, "dark", "", true)
		if !valid || theme != "dark" {
			t.Fatalf("resolveThemeTier() = (%q, %v), want (\"dark\", true)", theme, valid)
		}
		if len(warnings) != 0 {
			t.Errorf("resolveThemeTier() warnings = %v, want none: an explicit flag wins outright", warnings)
		}
	})
}
