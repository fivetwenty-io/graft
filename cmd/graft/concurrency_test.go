package main

import (
	"runtime"
	"testing"

	"github.com/fivetwenty-io/graft/internal/config"
	"github.com/fivetwenty-io/graft/internal/features"
	"github.com/fivetwenty-io/graft/pkg/graft"
)

// staticIPRaceDeterminismIterations mirrors
// parallelDeterminismIterations in pkg/graft/parallel_determinism_test.go:
// N >= 30 repeated runs for statistical confidence that parallel
// evaluation is deterministic.
const staticIPRaceDeterminismIterations = 30

// TestParallelStaticIPClaimErrorDeterministic is a regression test for a
// nondeterminism found and fixed while making parallel evaluation the
// default: with parallel evaluation enabled by default, two independent
// static_ips claims in different, unrelated jobs (no dataflow dependency
// between them) land in the same scheduler wave. Both mutate the engine's
// shared usedIPs map (see op_static_ips.go's claimStaticIPMu), so which
// job's claim "wins" - and therefore which job's name appears in the
// "already allocated to" error - depended on which goroutine reached the
// claim first. Before the evaluator_parallel.go fix (deterministic
// per-wave task ordering + sequential pool.SubmitWaitContext dispatch),
// this flipped between "static_z1"/"static_z2" across repeated runs of the
// exact same input.
func TestParallelStaticIPClaimErrorDeterministic(t *testing.T) {
	cfg := config.DefaultConfig() // Parallel.Enabled == true (default)
	ff := features.DefaultFlags()

	var baseline string
	for i := 0; i < staticIPRaceDeterminismIterations; i++ {
		files := []YamlFile{}
		f, err := openFiles([]string{"../../assets/static_ips/multi-azs-same-ip-different-index.yml"})
		if err != nil {
			t.Fatalf("iteration %d: openFiles() error = %v", i, err)
		}
		files = f

		opts := &mergeOpts{
			Files:      []string{"../../assets/static_ips/multi-azs-same-ip-different-index.yml"},
			EngineOpts: configEngineOpts(cfg, ff),
		}

		_, _, err = mergeAllDocs(files, opts)
		if err == nil {
			t.Fatalf("iteration %d: expected a duplicate-IP-claim error, got nil", i)
		}

		got := err.Error()
		if i == 0 {
			baseline = got
			continue
		}
		if got != baseline {
			t.Fatalf("iteration %d produced a different error message than iteration 0 under parallel evaluation.\niteration 0: %s\niteration %d: %s",
				i, baseline, i, got)
		}
	}
}

// TestResolveConcurrencyUsesNumCPUWhenMaxWorkersUnset proves the
// previously-hardcoded WithConcurrency(10) is now derived from
// runtime.NumCPU() when the config's Parallel.MaxWorkers is left at its
// "auto-detect" zero value.
func TestResolveConcurrencyUsesNumCPUWhenMaxWorkersUnset(t *testing.T) {
	got := resolveConcurrency(config.ParallelConfig{MaxWorkers: 0})
	want := runtime.NumCPU()
	if want < 1 {
		want = 1
	}
	if got != want {
		t.Errorf("resolveConcurrency(MaxWorkers=0) = %d, want %d (runtime.NumCPU floored at 1)", got, want)
	}
}

// TestResolveConcurrencyHonorsExplicitMaxWorkers proves an explicit
// config/env-supplied MaxWorkers value takes precedence over the
// NumCPU-derived default.
func TestResolveConcurrencyHonorsExplicitMaxWorkers(t *testing.T) {
	got := resolveConcurrency(config.ParallelConfig{MaxWorkers: 3})
	if got != 3 {
		t.Errorf("resolveConcurrency(MaxWorkers=3) = %d, want 3", got)
	}
}

// TestResolveConcurrencyNeverBelowOne proves the floor applies even for a
// pathological runtime.NumCPU() report (defensive; NumCPU is documented to
// always return >= 1, but resolveConcurrency must not propagate 0 or a
// negative worker count into WithConcurrency either way).
func TestResolveConcurrencyNeverBelowOne(t *testing.T) {
	got := resolveConcurrency(config.ParallelConfig{MaxWorkers: 0})
	if got < 1 {
		t.Errorf("resolveConcurrency(MaxWorkers=0) = %d, want >= 1", got)
	}
}

// TestConfigEngineOptsDefaultConfigEnablesParallel proves that, with the
// CLI's default resolved config (config.DefaultConfig(): Parallel.Enabled
// == true), configEngineOpts produces engine options that actually
// construct a worker pool - not just a config value that's stored and
// never consumed (the previous behavior).
func TestConfigEngineOptsDefaultConfigEnablesParallel(t *testing.T) {
	cfg := config.DefaultConfig()
	ff := features.DefaultFlags()

	opts := configEngineOpts(cfg, ff)
	engine, err := graft.NewEngine(opts...)
	if err != nil {
		t.Fatalf("graft.NewEngine() error = %v", err)
	}

	de, ok := engine.(*graft.DefaultEngine)
	if !ok {
		t.Fatalf("graft.NewEngine() returned %T, want *graft.DefaultEngine", engine)
	}

	if de.GetWorkerPool() == nil {
		t.Error("Expected a non-nil worker pool with default config (Parallel.Enabled == true) and default feature flags")
	}
}

// TestConfigEngineOptsParallelDisabledByConfig proves cfg.Parallel.Enabled
// == false (as a file/env override would produce) results in no worker
// pool, i.e. the config value is genuinely load-bearing in both
// directions, not just a one-way "always on" shortcut.
func TestConfigEngineOptsParallelDisabledByConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Parallel.Enabled = false
	ff := features.DefaultFlags()

	opts := configEngineOpts(cfg, ff)
	engine, err := graft.NewEngine(opts...)
	if err != nil {
		t.Fatalf("graft.NewEngine() error = %v", err)
	}

	de, ok := engine.(*graft.DefaultEngine)
	if !ok {
		t.Fatalf("graft.NewEngine() returned %T, want *graft.DefaultEngine", engine)
	}

	if de.GetWorkerPool() != nil {
		t.Error("Expected a nil worker pool when cfg.Parallel.Enabled == false")
	}
}

// TestConfigEngineOptsHonorsConfiguredMaxWorkers proves an explicit
// cfg.Parallel.MaxWorkers value (as a file/env override would produce)
// reaches the engine's MaxConcurrency, replacing the previous hardcoded 10.
func TestConfigEngineOptsHonorsConfiguredMaxWorkers(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Parallel.MaxWorkers = 2
	ff := features.DefaultFlags()

	opts := configEngineOpts(cfg, ff)
	engine, err := graft.NewEngine(opts...)
	if err != nil {
		t.Fatalf("graft.NewEngine() error = %v", err)
	}

	de, ok := engine.(*graft.DefaultEngine)
	if !ok {
		t.Fatalf("graft.NewEngine() returned %T, want *graft.DefaultEngine", engine)
	}

	pool := de.GetWorkerPool()
	if pool == nil {
		t.Fatal("Expected a non-nil worker pool with cfg.Parallel.MaxWorkers == 2")
	}
}
