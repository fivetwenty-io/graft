package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/gonvenience/ytbx"
	"github.com/homeport/dyff/pkg/dyff"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/fivetwenty-io/graft/internal/cache"
	"github.com/fivetwenty-io/graft/internal/config"
	"github.com/fivetwenty-io/graft/internal/features"
	"github.com/fivetwenty-io/graft/internal/histdiff"
	"github.com/fivetwenty-io/graft/internal/utils/ansi"

	"github.com/fivetwenty-io/graft/log"
	"github.com/fivetwenty-io/graft/pkg/graft"
	"github.com/fivetwenty-io/graft/pkg/graft/controlflow" // Register the (( if/for/while/case )) preprocessor
	_ "github.com/fivetwenty-io/graft/pkg/graft/operators" // Register operators

	"github.com/goccy/go-yaml"
)

// stdinPath is the pseudo-path recorded for a file read from standard
// input, which `-` on the command line is rewritten to.
const stdinPath = "STDIN"

// Version holds the current version of graft, overridden at build time via
// `-ldflags "-X main.Version=..."` (see Makefile). The fallback below is a
// real semver so that even an ad-hoc `go build`/`go run` without ldflags
// still satisfies genesis's check_prereqs() minimum-version gate (spruce
// compat requires >= 1.28.0, probed via `graft -v`/`--version`).
var Version = "1.32.2"

var printStdOutf = func(format string, args ...interface{}) {
	_, _ = fmt.Fprintf(os.Stdout, format, args...)
}

var exit = func(code int) {
	os.Exit(code)
}

var usage = func() {
	// Default implementation; overridden in tests and replaced in main()
	fmt.Fprintf(os.Stderr, "Usage: graft <command> [options] [files...]\n")
	exit(1)
}

func envFlag(varname string) bool {
	val := os.Getenv(varname)
	return val != "" && !strings.EqualFold(val, "false") && val != "0"
}

type YamlFile struct {
	Path   string
	Reader io.ReadCloser
}

type jsonOpts struct {
	Strict   bool
	Reverse  bool
	MultiDoc bool
	Files    []string
}

type mergeOpts struct {
	SkipEval       bool
	Prune          []string
	CherryPick     []string
	FallbackAppend bool
	EnableGoPatch  bool
	MultiDoc       bool
	DataflowOrder  string
	Files          []string
	OutputDir      string // fan only: write each result to <OutputDir>/<target-basename> instead of stdout

	// History, TracePath, ShowChanges, and ChangesOnly are merge-only
	// history/tracing flags (docs/user-guide/history-tracking.md). At most
	// one may be set (see validateHistoryFlags); when one is, handleMerge
	// prints that flag's tracking report instead of the merged document.
	History     bool
	TracePath   string // non-empty selects --trace-path <path>
	ShowChanges bool
	ChangesOnly bool
	EngineOpts  []graft.EngineOption // Programmatic engine options (not from CLI flags)

	// CacheCfg carries the resolved persistent-cache configuration
	// (internal/config's CacheConfig, after GRAFT_CACHE_* environment
	// overrides). The zero value disables the persistent cache, so
	// callers that never set it - fan, debug, vaultinfo, tests -
	// behave exactly as before it existed.
	CacheCfg config.CacheConfig
}

// hasHistoryFlag reports whether any of the merge --history/--trace-path/
// --show-changes/--changes-only flags were given.
func (o *mergeOpts) hasHistoryFlag() bool {
	return o.History || o.TracePath != "" || o.ShowChanges || o.ChangesOnly
}

// validateHistoryFlags rejects combining more than one of --history/
// --trace-path/--show-changes/--changes-only: each selects a distinct
// report format over the same underlying history data, so combining them
// has no well-defined single output (mirrors `graft diff`'s
// --side-by-side/--unified/--changes mutual exclusivity).
func (o *mergeOpts) validateHistoryFlags() error {
	selected := 0
	for _, on := range []bool{o.History, o.TracePath != "", o.ShowChanges, o.ChangesOnly} {
		if on {
			selected++
		}
	}
	if selected > 1 {
		return fmt.Errorf("--history, --trace-path, --show-changes, and --changes-only are mutually exclusive; pick one")
	}
	return nil
}

func handleColorFlag(colorOpt string) (bool, bool) {
	switch colorOpt {
	case "on":
		return true, true
	case "off":
		return false, true
	case "auto", "":
		return isatty.IsTerminal(os.Stderr.Fd()), true
	default:
		log.PrintStdErrf("Invalid --color option: %s. Must be 'on', 'off', or 'auto'.\n", colorOpt)
		return false, false
	}
}

func handleMerge(opts *mergeOpts) int {
	if err := opts.validateHistoryFlags(); err != nil {
		log.PrintStdErrf("%s\n", ansi.Sprintf("@R{%s}", err.Error()))
		return 1
	}
	if opts.hasHistoryFlag() {
		return handleMergeHistory(opts)
	}

	if store := openMergeOutputCache(opts); store != nil {
		return handleMergeCached(opts, store)
	}

	tree, _, err := cmdMergeEval(opts)
	if err != nil {
		log.PrintStdErrf("%s\n", err.Error())
		return 2
	}

	out, rc := renderMergedTree(tree)
	if rc != 0 {
		return rc
	}
	printStdOutf("%s", string(out))
	return 0
}

// renderMergedTree turns a merged document tree into the exact bytes
// `graft merge` writes to stdout (cycle check, YAML marshal, trailing
// newline), printing any error to stderr and returning its exit code.
// Shared by the plain and cache-aware merge paths so both emit
// byte-identical output.
func renderMergedTree(tree map[string]interface{}) ([]byte, int) {
	log.TRACE("Converting the following data back to YML:")
	log.TRACE("%#v", tree)

	if cycleErr := graft.CheckForCycles(tree, 4096); cycleErr != nil {
		log.PrintStdErrf("%s\n", cycleErr.Error())
		return nil, 2
	}

	merged, err := graft.MarshalYAML(tree)
	if err != nil {
		log.PrintStdErrf("Unable to convert merged result back to YAML: %s\nData:\n%#v", err.Error(), tree)
		return nil, 2
	}

	return append(merged, '\n'), 0
}

func handleFan(opts *mergeOpts) int {
	results, err := cmdFanEval(opts)
	if err != nil {
		log.PrintStdErrf("%s\n", err.Error())
		return 2
	}

	if opts.OutputDir != "" {
		return writeFanResultsToDir(results, opts.OutputDir)
	}

	for _, result := range results {
		log.TRACE("Converting the following data back to YML:")
		log.TRACE("%#v", result.Tree)

		if err := graft.CheckForCycles(result.Tree, 4096); err != nil {
			log.PrintStdErrf("%s\n", err.Error())
			return 2
		}

		merged, err := graft.MarshalYAML(result.Tree)
		if err != nil {
			log.PrintStdErrf("Unable to convert merged result back to YAML: %s\nData:\n%#v", err.Error(), result.Tree)
			return 2
		}

		printStdOutf("---\n%s\n", string(merged))
	}
	return 0
}

// writeFanResultsToDir implements `fan --output-dir`: instead of writing
// every merged result to stdout separated by `---`, each result is written
// to its own file inside outputDir, named after the target file it came
// from (see fanOutputPath). outputDir is created (including any missing
// parents) if it does not already exist.
func writeFanResultsToDir(results []fanResult, outputDir string) int {
	if err := os.MkdirAll(outputDir, 0o750); err != nil {
		log.PrintStdErrf("%s\n", ansi.Sprintf("@R{Unable to create output directory} @m{%s}: %s", outputDir, err.Error()))
		return 2
	}

	for _, result := range results {
		if err := graft.CheckForCycles(result.Tree, 4096); err != nil {
			log.PrintStdErrf("%s\n", err.Error())
			return 2
		}

		merged, err := graft.MarshalYAML(result.Tree)
		if err != nil {
			log.PrintStdErrf("Unable to convert merged result back to YAML: %s\nData:\n%#v", err.Error(), result.Tree)
			return 2
		}

		outPath := fanOutputPath(outputDir, result.Path)
		// #nosec G306 - fan output is meant to be readable configuration data, matching the permissions of merge/json's stdout-redirected output
		if err := os.WriteFile(outPath, merged, 0o644); err != nil {
			log.PrintStdErrf("%s\n", ansi.Sprintf("@R{Unable to write output file} @m{%s}: %s", outPath, err.Error()))
			return 2
		}
	}
	return 0
}

// fanOutputPath derives the output filename for one fan result from the
// YamlFile.Path recorded for it. Target files are always processed through
// splitLoadYamlFile (see cmdFanEval), so docPath always carries a trailing
// "[N]" document-index suffix (e.g. "targets/dev.yml[0]", or
// "STDIN[0]" when the target came from stdin); that suffix is stripped
// before deriving the basename, and re-added as a "-N" filename suffix only
// when N > 0, so a target file that only produced one document (the common
// case) gets a clean "dev.yml" output name, while a multi-document target
// file produces "multi.yml", "multi-1.yml", "multi-2.yml", etc.
func fanOutputPath(outputDir, docPath string) string {
	base := docPath
	index := 0
	if open := strings.LastIndex(base, "["); open >= 0 && strings.HasSuffix(base, "]") {
		if n, err := strconv.Atoi(base[open+1 : len(base)-1]); err == nil {
			index = n
			base = base[:open]
		}
	}

	if base == stdinPath {
		base = "stdin.yml"
	}

	name := filepath.Base(base)
	ext := filepath.Ext(name)
	if ext == "" {
		ext = ".yml"
		name += ext
	}
	if index > 0 {
		name = strings.TrimSuffix(name, ext) + fmt.Sprintf("-%d", index) + ext
	}

	return filepath.Join(outputDir, name)
}

// expandFanTargets replaces any directory entry in paths with the sorted
// list of its immediate .yml/.yaml/.json files (dotfiles and
// subdirectories are skipped; expansion is not recursive), matching
// docs/user-guide/cli/fan.md's "target directory" usage
// (`graft fan template.yml targets/`). Non-directory paths (including the
// stdin sentinel "-") and paths that fail to stat are passed through
// unchanged so the existing file-open error path in loadYamlFile reports
// the failure consistently.
func expandFanTargets(paths []string) ([]string, error) {
	expanded := make([]string, 0, len(paths))
	for _, p := range paths {
		if p == "-" {
			expanded = append(expanded, p)
			continue
		}

		info, statErr := os.Stat(p)
		if statErr != nil || !info.IsDir() {
			expanded = append(expanded, p)
			continue
		}

		entries, readErr := os.ReadDir(p)
		if readErr != nil {
			return nil, ansi.Errorf("@R{Error reading directory} @m{%s}: %s", p, readErr.Error())
		}

		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if strings.HasPrefix(name, ".") {
				continue
			}
			switch strings.ToLower(filepath.Ext(name)) {
			case ".yml", ".yaml", ".json":
				names = append(names, name)
			}
		}
		if len(names) == 0 {
			return nil, ansi.Errorf("@R{Empty target directory}: %s contains no .yml/.yaml/.json files", p)
		}
		sort.Strings(names)

		for _, name := range names {
			expanded = append(expanded, filepath.Join(p, name))
		}
	}
	return expanded, nil
}

// configEngineOpts converts a loaded internal/config.Config and a resolved
// internal/features.FeatureFlags set into engine construction options. cfg
// nil (--config not specified) omits WithConfigInstance and the
// concurrency/parallel options derived from it (see resolveConcurrency); ff
// nil omits WithFeatureFlags, in which case engine construction falls back
// to its own features.DefaultFlags() default (see pkg/graft/engine.go
// createEngineFromOptions), so passing ff == features.DefaultFlags() (no
// GRAFT_FEATURE_* overrides) is behaviorally identical to omitting it.
//
// When cfg is non-nil, its Parallel section drives the engine's
// parallel-evaluation default (parallel evaluation is enabled by default,
// replacing the previously hardcoded WithConcurrency(10)), and
// resolveConcurrency derives the worker count from cfg.Parallel.MaxWorkers
// (an explicit file/env override) or runtime.NumCPU() floored at 1.
//
// Parallel evaluation has two documented kill switches -
// GRAFT_PARALLEL_ENABLED (config) and GRAFT_FEATURE_PARALLEL (feature
// flag) - and either one set to false disables it: the effective value
// is the AND of both gates. graft.WithParallel then writes that one
// value to both EnableParallel and the FeatureParallelEvaluation flag on
// the *FeatureFlags instance passed via WithFeatureFlags, keeping the
// engine's two parallel gates in sync.
func configEngineOpts(cfg *config.Config, ff *features.FeatureFlags) []graft.EngineOption {
	var opts []graft.EngineOption
	if cfg != nil {
		opts = append(opts, graft.WithConfigInstance(cfg))
	}
	if ff != nil {
		opts = append(opts, graft.WithFeatureFlags(ff))
	}
	if cfg != nil {
		enabled := cfg.Parallel.Enabled
		// FeatureParallelEvaluation defaults to false while the config
		// tier defaults to true, so the merged flag value cannot say
		// whether the operator asked for anything - only an explicit
		// GRAFT_FEATURE_PARALLEL in the environment can, and an explicit
		// false must win.
		if v, explicit := features.EnvOverride(features.EnvFeatureParallel); explicit {
			enabled = enabled && v
		}
		opts = append(opts,
			graft.WithParallel(enabled),
			graft.WithConcurrency(resolveConcurrency(cfg.Parallel)),
		)
	}
	return opts
}

// resolveConcurrency derives graft's default worker count from a resolved
// Parallel config section. An explicit cfg.Parallel.MaxWorkers (set via
// config file or GRAFT_PARALLEL_MAX_WORKERS) takes precedence; otherwise
// runtime.NumCPU() is used, floored at 1 so a single-core host still gets a
// usable pool. This replaces the CLI's previous unconditional
// WithConcurrency(10).
func resolveConcurrency(p config.ParallelConfig) int {
	if p.MaxWorkers > 0 {
		return p.MaxWorkers
	}
	n := runtime.NumCPU()
	if n < 1 {
		n = 1
	}
	return n
}

// resolveStartupFeatureFlags computes the CLI's effective feature-flag set
// for one invocation: internal/features.DefaultFlags() overridden by any
// recognized GRAFT_FEATURE_* environment variables (internal/features/env.go
// documents the full env-var-to-flag mapping: GRAFT_FEATURE_PARALLEL,
// GRAFT_FEATURE_CACHE, GRAFT_FEATURE_METRICS, GRAFT_FEATURE_DEBUG,
// GRAFT_FEATURE_STRICT_TYPES, GRAFT_FEATURE_POOLS). An unset or
// unrecognized-value env var leaves the corresponding flag at its default,
// per FeatureFlags.LoadFromEnv's own documented behavior - so no error path
// is needed here (mirrors resolveStartupConfig's env>default precedence,
// without the file tier since feature flags are env/default only).
func resolveStartupFeatureFlags() *features.FeatureFlags {
	ff := features.DefaultFlags()
	ff.LoadFromEnv()
	return ff
}

// resolveStartupConfig computes the CLI's effective configuration for one
// invocation: the --config file's contents if configPath is non-empty
// (otherwise config.DefaultConfig()), with GRAFT_* environment variable
// overrides (internal/config.ApplyEnv) applied on top. This gives the
// precedence order env > file > default. The result is re-validated after
// the environment overrides are applied, since an override (e.g. an
// out-of-range GRAFT_ENGINE_MAX_RECURSION) can make an otherwise-valid
// base configuration invalid.
func resolveStartupConfig(configPath string) (*config.Config, error) {
	cfg := config.DefaultConfig()
	if configPath != "" {
		loaded, err := config.Load(configPath)
		if err != nil {
			return nil, fmt.Errorf("loading config file %s: %w", configPath, err)
		}
		cfg = loaded
	}

	config.ApplyEnv(cfg)
	if err := config.Validate(cfg); err != nil {
		return nil, fmt.Errorf("applying environment overrides: %w", err)
	}

	return cfg, nil
}

func handleVaultInfo(vaultFiles []string, enableGoPatch bool, cfg *config.Config, ff *features.FeatureFlags, jsonOutput, pathsOnly bool) int {
	engineOpts := append([]graft.EngineOption{graft.WithSkipVault(true)}, configEngineOpts(cfg, ff)...)
	opts := &mergeOpts{
		Files:         vaultFiles,
		EnableGoPatch: enableGoPatch,
		EngineOpts:    engineOpts,
	}
	_, engine, err := cmdMergeEval(opts)
	if err != nil {
		log.PrintStdErrf("%s\n", err.Error())
		return 2
	}

	// Neither new flag given: byte-identical to the pre-existing behavior
	// genesis scrapes (docs/spruce/genesis-compat-contract.md).
	if !jsonOutput && !pathsOnly {
		printStdOutf("%s\n", formatVaultRefs(engine))
		return 0
	}

	output, err := formatVaultInfoExtended(buildVaultRefs(engine), jsonOutput, pathsOnly)
	if err != nil {
		log.PrintStdErrf("%s\n", err.Error())
		return 2
	}
	printStdOutf("%s\n", output)
	return 0
}

func handleJSON(opts jsonOpts) int {
	if opts.Reverse {
		yamls, err := cmdJSONReverseEval(opts)
		if err != nil {
			log.PrintStdErrf("%s\n", err)
			return 2
		}
		if len(yamls) == 1 {
			printStdOutf("%s\n", yamls[0])
			return 0
		}
		for _, y := range yamls {
			printStdOutf("---\n%s\n", y)
		}
		return 0
	}

	jsons, err := cmdJSONEval(opts)
	if err != nil {
		log.PrintStdErrf("%s\n", err)
		return 2
	}

	if opts.MultiDoc {
		combined, combineErr := graft.CombineJSONLines(jsons)
		if combineErr != nil {
			log.PrintStdErrf("%s\n", combineErr)
			return 2
		}
		printStdOutf("%s\n", combined)
		return 0
	}

	for _, output := range jsons {
		printStdOutf("%s\n", output)
	}
	return 0
}

// diffOpts holds `graft diff`'s subcommand-specific flags (see
// docs/user-guide/cli/diff.md): at most one of SideBySide/Unified/Changes
// selects an alternate rendering of the same underlying semantic diff;
// leaving all three false keeps the pre-existing dyff HumanReport default,
// byte-for-byte.
type diffOpts struct {
	NoColor    bool
	SideBySide bool
	Unified    bool
	Changes    bool
	Context    int // negative means "use renderUnifiedDiff's default"
	Width      int // <= 0 means "use renderSideBySide's default"
	Quiet      bool
}

func handleDiff(files []string, colorOpt string, opts diffOpts) int {
	if colorOpt == "auto" || colorOpt == "" {
		ansi.Color(isatty.IsTerminal(os.Stdout.Fd()))
	}
	if opts.NoColor {
		ansi.Color(false)
	}
	if len(files) != 2 {
		usage()
		return 1
	}

	selected := 0
	for _, on := range []bool{opts.SideBySide, opts.Unified, opts.Changes} {
		if on {
			selected++
		}
	}
	if selected > 1 {
		log.PrintStdErrf("%s\n", ansi.Sprintf("@R{--side-by-side, --unified, and --changes are mutually exclusive; pick one}"))
		return 1
	}

	if selected == 0 {
		output, differences, err := diffFiles(files)
		if err != nil {
			log.PrintStdErrf("%s\n", err)
			return 2
		}
		if !opts.Quiet {
			printStdOutf("%s\n", output)
		}
		if differences {
			return 1
		}
		return 0
	}

	return handleDiffRender(files, opts)
}

// handleDiffRender implements the `--side-by-side`/`--unified`/`--changes`
// alternate diff renderings, all built from the same
// internal/histdiff.Compare semantic diff (itself built on dyff, matching
// the default diffFiles path) rather than a second diff algorithm.
func handleDiffRender(files []string, opts diffOpts) int {
	fromLabel, fromDoc, toLabel, toDoc, err := loadDiffDocuments(files)
	if err != nil {
		log.PrintStdErrf("%s\n", err)
		return 2
	}

	changes, err := histdiff.Compare(fromLabel, fromDoc, toLabel, toDoc)
	if err != nil {
		log.PrintStdErrf("%s\n", ansi.Sprintf("@R{Error comparing} @m{%s} @R{and} @m{%s}: %s", fromLabel, toLabel, err.Error()))
		return 2
	}

	var output string
	switch {
	case opts.Changes:
		output = renderChangeList(changes)
	case opts.Unified:
		output, err = renderUnifiedDiff(fromLabel, fromDoc, toLabel, toDoc, opts.Context)
	case opts.SideBySide:
		output, err = renderSideBySide(fromLabel, fromDoc, toLabel, toDoc, opts.Width)
	}
	if err != nil {
		log.PrintStdErrf("%s\n", err.Error())
		return 2
	}

	if !opts.Quiet {
		printStdOutf("%s", output)
		if !strings.HasSuffix(output, "\n") {
			printStdOutf("\n")
		}
	}

	if len(changes) > 0 {
		return 1
	}
	return 0
}

// loadDiffDocuments loads exactly two YAML/JSON files via ytbx (the same
// loader diffFiles uses for the default dyff report) and decodes each to a
// plain Go value, for renderers that need the actual document content
// (--unified, --side-by-side) rather than just a change list.
func loadDiffDocuments(paths []string) (fromLabel string, fromDoc interface{}, toLabel string, toDoc interface{}, err error) {
	if len(paths) != 2 {
		return "", nil, "", nil, ansi.Errorf("incorrect number of files given to loadDiffDocuments(); please file a bug report")
	}

	from, to, err := ytbx.LoadFiles(paths[0], paths[1])
	if err != nil {
		return "", nil, "", nil, err
	}

	fromVal, err := decodeInputFileDocument(from)
	if err != nil {
		return "", nil, "", nil, ansi.Errorf("@m{%s}: @R{%s}", paths[0], err.Error())
	}
	toVal, err := decodeInputFileDocument(to)
	if err != nil {
		return "", nil, "", nil, ansi.Errorf("@m{%s}: @R{%s}", paths[1], err.Error())
	}

	return paths[0], fromVal, paths[1], toVal, nil
}

// decodeInputFileDocument decodes the first document of a loaded
// ytbx.InputFile into a plain Go value. An input file with no documents
// (an empty file) decodes to an empty map, matching graft merge/json's own
// empty-document handling.
func decodeInputFileDocument(f ytbx.InputFile) (interface{}, error) {
	if len(f.Documents) == 0 {
		return map[string]interface{}{}, nil
	}
	var v interface{}
	if err := f.Documents[0].Decode(&v); err != nil {
		return nil, err
	}
	return v, nil
}

// versionFlagPrecedesVerb reports whether the -v/--version token the user
// typed sits before the subcommand name on the command line. When no
// subcommand was invoked (cmd is the root), any position counts as
// preceding. args is os.Args[1:]. Combined shorthand groups containing 'v'
// (e.g. -Dv) count as version tokens, matching pflag's parse.
func versionFlagPrecedesVerb(cmd *cobra.Command, args []string) bool {
	if !cmd.HasParent() {
		return true
	}
	verb := cmd.CalledAs()
	if verb == "" {
		verb = cmd.Name()
	}
	for _, arg := range args {
		if arg == verb {
			return false
		}
		if arg == "--version" || strings.HasPrefix(arg, "--version=") ||
			(strings.HasPrefix(arg, "-") && !strings.HasPrefix(arg, "--") && strings.ContainsRune(arg[1:], 'v')) {
			return true
		}
	}
	// Neither the verb nor a version token found (unreachable in a normal
	// parse); be conservative and let the subcommand run.
	return false
}

// newRootCmd creates a fresh Cobra command tree. Called each time main() runs
// so that tests calling main() multiple times get clean flag state.
func newRootCmd() (*cobra.Command, *bool) {
	var debug, trace, version bool
	var colorOpt string
	var configPath string

	// Track whether PersistentPreRunE signaled an abort (e.g., invalid color)
	var aborted bool

	// loadedConfig holds the configuration used for engine construction:
	// the --config file if specified (else config.DefaultConfig()), with
	// GRAFT_* environment overrides applied on top. Set once in
	// PersistentPreRunE before any subcommand's RunE runs.
	var loadedConfig *config.Config

	// loadedFeatureFlags holds the feature-flag set used for engine
	// construction: internal/features.DefaultFlags() with GRAFT_FEATURE_*
	// environment overrides applied (see resolveStartupFeatureFlags). Set
	// once in PersistentPreRunE before any subcommand's RunE runs, matching
	// loadedConfig's env>default resolution pattern.
	var loadedFeatureFlags *features.FeatureFlags

	// maxLoopIterations is the --max-loop-iterations override for (( while ))
	// loops (pkg/graft/controlflow). 0 means "not set on the command line";
	// GRAFT_MAX_LOOP_ITERATIONS or the package default (1000) applies then.
	var maxLoopIterations int

	rootCmd := &cobra.Command{
		Use:           "graft",
		Short:         "graft - YAML merging and operator evaluation",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			// Handle debug/trace flags
			if envFlag("DEBUG") || debug {
				log.DebugOn = true
			}
			if envFlag("TRACE") || trace {
				log.TraceOn = true
				log.DebugOn = true
			}

			// A pre-verb version flag wins over any subcommand, matching
			// spruce: its Version flag is checked before verb dispatch,
			// so `spruce -v merge ...` prints the version and exits 0
			// without reading any input. Handled here (not in the root
			// RunE) so subcommands never run, and before config loading
			// so `-v` cannot fail on a bad --config path.
			//
			// A post-verb `-v` (e.g. `graft merge -v file`) is ignored
			// and the verb runs; spruce instead treats the token as a
			// filename and exits 2. Honoring it here would be worse than
			// either: a stray `-v` in a scripted merge would write a
			// version string into the captured manifest with exit 0.
			// The divergence is documented in
			// docs/spruce/genesis-compat-contract.md.
			if version && versionFlagPrecedesVerb(cmd, os.Args[1:]) {
				printStdOutf("%s - Version %s\n", os.Args[0], Version)
				aborted = true
				exit(0)
				return fmt.Errorf("version requested")
			}

			// Handle color flag
			colorEnabled, colorValid := handleColorFlag(colorOpt)
			if !colorValid {
				aborted = true
				exit(1)
				return fmt.Errorf("invalid color option")
			}
			ansi.Color(colorEnabled)

			// (( while )) loops are capped to prevent runaway/non-terminating
			// expansion (docs/user-guide/operators/control-flow.md's
			// documented default is 1000). --max-loop-iterations overrides
			// both that default and GRAFT_MAX_LOOP_ITERATIONS.
			if maxLoopIterations > 0 {
				controlflow.SetMaxLoopIterations(maxLoopIterations)
			}

			// Load the --config file (or defaults) and apply GRAFT_*
			// environment overrides up front so every subcommand fails
			// fast on a bad path/value, rather than partway through
			// merge/fan/vaultinfo execution.
			cfg, err := resolveStartupConfig(configPath)
			if err != nil {
				aborted = true
				log.PrintStdErrf("%s\n", err.Error())
				exit(1)
				return err
			}
			loadedConfig = cfg

			// Resolve GRAFT_FEATURE_* environment overrides once per
			// invocation, alongside loadedConfig above.
			loadedFeatureFlags = resolveStartupFeatureFlags()
			return nil
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			if aborted {
				return nil
			}
			// No subcommand given: call usage. (-v/--version never
			// reaches here; PersistentPreRunE handles it.)
			usage()
			return nil
		},
	}

	rootCmd.PersistentFlags().BoolVarP(&debug, "debug", "D", false, "Enable debugging")
	rootCmd.PersistentFlags().BoolVarP(&trace, "trace", "T", false, "Enable trace mode debugging (very verbose)")
	rootCmd.PersistentFlags().BoolVarP(&version, "version", "v", false, "Display version information")
	rootCmd.PersistentFlags().StringVar(&colorOpt, "color", "", "Control color output (on/off/auto, default: auto)")
	rootCmd.PersistentFlags().StringVar(&configPath, "config", "", "Path to a YAML configuration file (see internal/config); absent means unchanged default behavior")
	rootCmd.PersistentFlags().IntVar(&maxLoopIterations, "max-loop-iterations", 0, "Maximum (( while )) loop iterations before erroring (default: 1000, or GRAFT_MAX_LOOP_ITERATIONS)")

	// merge command
	var mergeSkipEval, mergeFallbackAppend, mergeGoPatch, mergeMultiDoc bool
	var mergePrune, mergeCherryPick []string
	var mergeDataflowOrder string
	var mergeHistory, mergeShowChanges, mergeChangesOnly, mergeInteractive bool
	var mergeTracePath string

	mergeCmd := &cobra.Command{
		Use:   "merge [files...]",
		Short: "Merge multiple YAML/JSON files",
		RunE: func(_ *cobra.Command, args []string) error {
			opts := &mergeOpts{
				SkipEval:       mergeSkipEval,
				Prune:          mergePrune,
				CherryPick:     mergeCherryPick,
				FallbackAppend: mergeFallbackAppend,
				EnableGoPatch:  mergeGoPatch,
				MultiDoc:       mergeMultiDoc,
				DataflowOrder:  mergeDataflowOrder,
				Files:          args,
				History:        mergeHistory,
				TracePath:      mergeTracePath,
				ShowChanges:    mergeShowChanges,
				ChangesOnly:    mergeChangesOnly,
				EngineOpts:     configEngineOpts(loadedConfig, loadedFeatureFlags),
				CacheCfg:       loadedConfig.Cache,
			}
			if mergeInteractive {
				exit(handleDebug(args, opts, os.Stdin, os.Stdout))
				return nil
			}
			exit(handleMerge(opts))
			return nil
		},
	}
	mergeCmd.Flags().BoolVar(&mergeSkipEval, "skip-eval", false, "Do not evaluate graft logic after merging docs")
	mergeCmd.Flags().StringArrayVar(&mergePrune, "prune", nil, "Specify keys to prune from final output (may be specified more than once)")
	mergeCmd.Flags().StringArrayVar(&mergeCherryPick, "cherry-pick", nil, "The opposite of prune, specify keys to cherry-pick from final output (may be specified more than once)")
	mergeCmd.Flags().BoolVar(&mergeFallbackAppend, "fallback-append", false, "Default merge normally tries to key merge, then inline. This flag says do an append instead of an inline.")
	mergeCmd.Flags().BoolVar(&mergeGoPatch, "go-patch", false, "Enable the use of go-patch when parsing files to be merged")
	mergeCmd.Flags().BoolVarP(&mergeMultiDoc, "multi-doc", "m", false, "Treat multi-doc yaml as multiple files.")
	mergeCmd.Flags().StringVar(&mergeDataflowOrder, "dataflow-order", "", "Order of operations in dataflow output: alphabetical (default) or insertion")
	mergeCmd.Flags().BoolVar(&mergeHistory, "history", false, "Print per-path merge history instead of the merged document")
	mergeCmd.Flags().StringVar(&mergeTracePath, "trace-path", "", "Print detailed history for a single path instead of the merged document")
	mergeCmd.Flags().BoolVar(&mergeShowChanges, "show-changes", false, "Print a merge/evaluation change summary instead of the merged document")
	mergeCmd.Flags().BoolVar(&mergeChangesOnly, "changes-only", false, "Print only the paths that changed during merge/evaluation instead of the merged document")
	mergeCmd.Flags().BoolVar(&mergeInteractive, "interactive", false, "Launch the interactive debug REPL instead of merging directly (equivalent to 'graft debug')")

	// fan command
	var fanSkipEval, fanFallbackAppend, fanGoPatch, fanMultiDoc bool
	var fanPrune, fanCherryPick []string
	var fanDataflowOrder, fanOutputDir string

	fanCmd := &cobra.Command{
		Use:   "fan [files...]",
		Short: "Fan out source document across target documents",
		RunE: func(_ *cobra.Command, args []string) error {
			opts := &mergeOpts{
				SkipEval:       fanSkipEval,
				Prune:          fanPrune,
				CherryPick:     fanCherryPick,
				FallbackAppend: fanFallbackAppend,
				EnableGoPatch:  fanGoPatch,
				MultiDoc:       fanMultiDoc,
				DataflowOrder:  fanDataflowOrder,
				Files:          args,
				OutputDir:      fanOutputDir,
				EngineOpts:     configEngineOpts(loadedConfig, loadedFeatureFlags),
			}
			exit(handleFan(opts))
			return nil
		},
	}
	fanCmd.Flags().BoolVar(&fanSkipEval, "skip-eval", false, "Do not evaluate graft logic after merging docs")
	fanCmd.Flags().StringArrayVar(&fanPrune, "prune", nil, "Specify keys to prune from final output (may be specified more than once)")
	fanCmd.Flags().StringArrayVar(&fanCherryPick, "cherry-pick", nil, "The opposite of prune, specify keys to cherry-pick from final output (may be specified more than once)")
	fanCmd.Flags().BoolVar(&fanFallbackAppend, "fallback-append", false, "Default merge normally tries to key merge, then inline. This flag says do an append instead of an inline.")
	fanCmd.Flags().BoolVar(&fanGoPatch, "go-patch", false, "Enable the use of go-patch when parsing files to be merged")
	fanCmd.Flags().BoolVarP(&fanMultiDoc, "multi-doc", "m", false, "Treat multi-doc yaml as multiple files.")
	fanCmd.Flags().StringVar(&fanDataflowOrder, "dataflow-order", "", "Order of operations in dataflow output: alphabetical (default) or insertion")
	fanCmd.Flags().StringVarP(&fanOutputDir, "output-dir", "o", "", "Write each result to <output-dir>/<target-basename> instead of stdout")

	// json command
	var jsonStrict, jsonReverse, jsonMultiDoc bool

	jsonCmd := &cobra.Command{
		Use:   "json [files...]",
		Short: "Convert YAML to JSON",
		RunE: func(_ *cobra.Command, args []string) error {
			opts := jsonOpts{
				Strict:   jsonStrict,
				Reverse:  jsonReverse,
				MultiDoc: jsonMultiDoc,
				Files:    args,
			}
			exit(handleJSON(opts))
			return nil
		},
	}
	jsonCmd.Flags().BoolVar(&jsonStrict, "strict", false, "Refuse to convert non-string keys to strings")
	jsonCmd.Flags().BoolVarP(&jsonReverse, "reverse", "r", false, "Convert JSON to YAML instead of YAML to JSON")
	jsonCmd.Flags().BoolVar(&jsonMultiDoc, "multi-doc", false, "Wrap multiple JSON documents into a single JSON array instead of one object per line")

	// diff command
	var diffNoColor, diffSideBySide, diffUnified, diffChanges, diffQuiet bool
	var diffContext, diffWidth int

	diffCmd := &cobra.Command{
		Use:   "diff [file1] [file2]",
		Short: "Show the semantic differences between two YAML files",
		RunE: func(_ *cobra.Command, args []string) error {
			exit(handleDiff(args, colorOpt, diffOpts{
				NoColor:    diffNoColor,
				SideBySide: diffSideBySide,
				Unified:    diffUnified,
				Changes:    diffChanges,
				Context:    diffContext,
				Width:      diffWidth,
				Quiet:      diffQuiet,
			}))
			return nil
		},
	}
	diffCmd.Flags().BoolVarP(&diffSideBySide, "side-by-side", "y", false, "Side-by-side diff view")
	diffCmd.Flags().BoolVarP(&diffUnified, "unified", "u", false, "Unified diff format (git-style), grouped by top-level key")
	diffCmd.Flags().BoolVar(&diffChanges, "changes", false, "List all changes (original -> new) grouped by change type")
	diffCmd.Flags().IntVar(&diffContext, "context", -1, "Lines of context around each change in --unified output (default: 3)")
	diffCmd.Flags().IntVar(&diffWidth, "width", 0, "Total output width for --side-by-side (default: 80)")
	diffCmd.Flags().BoolVar(&diffNoColor, "no-color", false, "Disable colorized output for this command, overriding --color")
	diffCmd.Flags().BoolVarP(&diffQuiet, "quiet", "q", false, "Exit with status only, no output")

	// vaultinfo command
	var vaultInfoGoPatch, vaultInfoJSON, vaultInfoPathsOnly bool

	vaultinfoCmd := &cobra.Command{
		Use:   "vaultinfo [files...]",
		Short: "List vault references in the given files",
		RunE: func(_ *cobra.Command, args []string) error {
			exit(handleVaultInfo(args, vaultInfoGoPatch, loadedConfig, loadedFeatureFlags, vaultInfoJSON, vaultInfoPathsOnly))
			return nil
		},
	}
	vaultinfoCmd.Flags().BoolVar(&vaultInfoGoPatch, "go-patch", false, "Enable the use of go-patch when parsing files to be merged")
	vaultinfoCmd.Flags().BoolVar(&vaultInfoJSON, "json", false, "Output as JSON instead of YAML")
	vaultinfoCmd.Flags().BoolVar(&vaultInfoPathsOnly, "paths-only", false, "Output only the Vault secret paths (one per line, or a JSON array with --json), not their referring locations")

	// debug command
	var debugGoPatch, debugFallbackAppend bool

	debugCmd := &cobra.Command{
		Use:   "debug [files...]",
		Short: "Interactive debugging REPL for step-through merge analysis",
		RunE: func(_ *cobra.Command, args []string) error {
			opts := &mergeOpts{
				EnableGoPatch:  debugGoPatch,
				FallbackAppend: debugFallbackAppend,
				EngineOpts:     configEngineOpts(loadedConfig, loadedFeatureFlags),
			}
			exit(handleDebug(args, opts, os.Stdin, os.Stdout))
			return nil
		},
	}
	debugCmd.Flags().BoolVar(&debugGoPatch, "go-patch", false, "Enable the use of go-patch when parsing files to be merged (same meaning as merge --go-patch)")
	debugCmd.Flags().BoolVar(&debugFallbackAppend, "fallback-append", false, "Use append semantics instead of inline for the default array-merge fallback (same meaning as merge --fallback-append)")

	rootCmd.AddCommand(mergeCmd, fanCmd, jsonCmd, diffCmd, vaultinfoCmd, debugCmd)

	return rootCmd, &aborted
}

func main() {
	rootCmd, aborted := newRootCmd()

	err := rootCmd.Execute()

	// If already aborted (e.g., invalid --color), don't call usage
	if *aborted {
		return
	}

	// If Cobra returned an error (unknown command, bad flags, etc.), call usage
	if err != nil {
		usage()
		return
	}
}

func parseYAML(data []byte) (map[string]interface{}, error) {
	// Handle empty document
	if len(bytes.TrimSpace(data)) == 0 {
		log.DEBUG("YAML doc is empty, creating empty hash/map")
		return make(map[string]interface{}), nil
	}

	// First, unmarshal into a generic interface to detect root type
	var raw interface{}
	if err := yaml.Unmarshal(graft.QuoteInjectKeys(data), &raw); err != nil {
		return nil, ansi.Errorf("@R{Root of YAML document is not a hash/map}: %s\n", err.Error())
	}

	switch v := raw.(type) {
	case map[string]interface{}:
		if len(v) == 0 {
			log.DEBUG("YAML doc is empty, creating empty hash/map")
		}
		return graft.NormalizeMap(v), nil
	case nil:
		log.DEBUG("YAML doc is null/empty, creating empty hash/map")
		return make(map[string]interface{}), nil
	case []interface{}:
		return nil, graft.NewRootIsArrayError(ansi.Sprintf("@R{Root of YAML document is not a hash/map}: root is an array\n"))
	default:
		return nil, ansi.Errorf("@R{Root of YAML document is not a hash/map}: found %T\n", raw)
	}
}

func loadYamlFile(file string) (YamlFile, error) {
	var target YamlFile
	if file == "-" {
		target = YamlFile{Reader: os.Stdin, Path: "-"}
	} else {
		// #nosec G304 - File path is from user-provided command line arguments which is expected behavior for the CLI tool
		f, err := os.Open(file)
		if err != nil {
			return YamlFile{}, ansi.Errorf("@R{Error reading file} @m{%s}: %s", file, err.Error())
		}
		target = YamlFile{Path: file, Reader: f}
	}
	return target, nil
}

func splitLoadYamlFile(file string) ([]YamlFile, error) {
	docs := []YamlFile{}

	yamlFile, err := loadYamlFile(file)
	if err != nil {
		return nil, err
	}

	fileData, err := readFile(&yamlFile)
	if err != nil {
		return nil, err
	}

	rawDocs := bytes.Split(fileData, []byte("\n---\n"))
	// strip off empty document created if the first three bytes of the file are the doc separator
	// keeps the indexing correct for when used with error messages
	if len(rawDocs[0]) == 0 {
		rawDocs = rawDocs[1:]
	}

	for i, docBytes := range rawDocs {
		buf := bytes.NewBuffer(docBytes)
		doc := YamlFile{Path: fmt.Sprintf("%s[%d]", yamlFile.Path, i), Reader: io.NopCloser(buf)}
		docs = append(docs, doc)
	}
	return docs, nil
}

// resolveMergeInputFiles resolves options.Files to the actual list of
// YamlFiles to merge: falling back to stdin ("-") when no files were given
// (erroring if stdin has no data piped in), and splitting each file on the
// multi-doc "\n---\n" separator when options.MultiDoc is set. Shared by
// cmdMergeEval and buildMergeHistorySteps (merge --history/--trace-path/
// --show-changes/--changes-only), which both need this exact resolution
// but the latter also needs each resolved file's raw bytes cached for
// repeated replay (see buildMergeHistorySteps).
func resolveMergeInputFiles(options *mergeOpts) ([]YamlFile, error) {
	files := []YamlFile{}

	if len(options.Files) < 1 {
		stdinInfo, err := os.Stdin.Stat()
		if err != nil {
			return nil, ansi.Errorf("@R{Error statting STDIN} - Bailing out: %s\n", err.Error())
		}

		if stdinInfo.Mode()&os.ModeCharDevice != 0 {
			return nil, ansi.Errorf("@R{Error reading STDIN}: no data found. Did you forget to pipe data to STDIN, or specify yaml files to merge?")
		}

		options.Files = append(options.Files, "-")
	}

	for _, file := range options.Files {
		if options.MultiDoc {
			docs, err := splitLoadYamlFile(file)
			if err != nil {
				return nil, err
			}
			files = append(files, docs...)
		} else {
			yamlFile, err := loadYamlFile(file)
			if err != nil {
				return nil, err
			}
			files = append(files, yamlFile)
		}
	}

	return files, nil
}

func cmdMergeEval(options *mergeOpts) (map[string]interface{}, graft.Engine, error) {
	files, err := resolveMergeInputFiles(options)
	if err != nil {
		return nil, nil, err
	}

	result, engine, err := mergeAllDocs(files, options)
	if err != nil {
		return nil, nil, err
	}

	return result, engine, nil
}

// fanResult pairs one fan target's merged document tree with the YamlFile
// path it was merged against, so callers (handleFan / writeFanResultsToDir)
// can name output files after their target when --output-dir is given.
type fanResult struct {
	Path string
	Tree map[string]interface{}
}

func cmdFanEval(options *mergeOpts) ([]fanResult, error) {
	// Only fall back to stdin when fewer than 2 file arguments were given -
	// not 0, since fan's first positional argument is always the source,
	// not a target: a source-only invocation (1 argument) has no target at
	// all yet, and stdin is meant to supply it (`cat targets.yml | graft
	// fan src.yml`, matching `cat x | graft merge`'s own stdin-as-input
	// convention). Guarding on 0 instead breaks exactly that case (F20).
	//
	// Appending "-" unconditionally whenever stdin isn't a terminal
	// (spruce's own cmdFanEval does this too, bug-for-bug:
	// ../spruce/cmd/spruce/main.go's cmdFanEval) is still wrong: once a
	// source AND at least one explicit target are both given, there is
	// nothing left for stdin to usefully supply, so silently turning any
	// non-terminal stdin into a further extra target - and potentially
	// hanging forever reading an open pipe that's never closed - has no
	// legitimate use case. An explicit "-" argument (anywhere, as source
	// or target) still works either way, since it's already present in
	// options.Files without needing this fallback to add it.
	if len(options.Files) < 2 {
		stdinInfo, err := os.Stdin.Stat()
		if err != nil {
			return nil, ansi.Errorf("@R{Error statting STDIN} - Bailing out: %s\n", err.Error())
		}
		if stdinInfo.Mode()&os.ModeCharDevice == 0 {
			options.Files = append(options.Files, "-")
		}
	}

	if len(options.Files) == 0 {
		return nil, ansi.Errorf("@R{Missing Input:} You must specify at least a source document to graft fan. If no files are specified, STDIN is used. Using STDIN for source and target docs only works with -m.")
	}

	roots := []fanResult{}
	sourcePath := options.Files[0]
	options.Files = options.Files[1:]

	var expandErr error
	options.Files, expandErr = expandFanTargets(options.Files)
	if expandErr != nil {
		return nil, expandErr
	}

	docs := []YamlFile{}
	source := YamlFile{}
	if options.MultiDoc {
		sourceDocs, sourceErr := splitLoadYamlFile(sourcePath)
		if sourceErr != nil {
			return nil, sourceErr
		}
		// only the first yaml document of the source will be treated as actual source, all others
		// will be treated as target documents
		source = sourceDocs[0]
		docs = append(sourceDocs[1:], docs...)
	} else {
		var sourceLoadErr error
		source, sourceLoadErr = loadYamlFile(sourcePath)
		if sourceLoadErr != nil {
			return nil, sourceLoadErr
		}
	}

	for _, file := range options.Files {
		yamlDocs, yamlErr := splitLoadYamlFile(file)
		if yamlErr != nil {
			return nil, yamlErr
		}
		docs = append(docs, yamlDocs...)
	}

	sourceBytes, err := readFile(&source)
	if err != nil {
		return nil, err
	}

	if len(docs) < 1 {
		return nil, ansi.Errorf("@R{Missing Input:} You must specify at least one target document to graft fan. If no files are specified, STDIN is used. Using STDIN for source and target docs only works with -m.")
	}

	for _, doc := range docs {
		sourceBuffer := bytes.NewBuffer(sourceBytes)
		source = YamlFile{Path: source.Path, Reader: io.NopCloser(sourceBuffer)}
		result, _, err := mergeAllDocs([]YamlFile{source, doc}, options)
		if err != nil {
			return nil, err
		}
		roots = append(roots, fanResult{Path: doc.Path, Tree: result})
	}

	return roots, nil
}

// resolveStdinDefaultFiles appends the stdin sentinel "-" to files when no
// files were given and stdin is actually piped in (not a character
// device/terminal). Mirrors cmdMergeEval's `if len(options.Files) < 1`
// guard: only falling back to stdin when no file arguments were given at
// all, rather than unconditionally appending "-" whenever stdin isn't a
// terminal. Without the length guard, `graft json file.yml` run with
// non-terminal stdin (any pipe or redirect, e.g. under a CI runner or a
// harness like this one) would also try to read stdin - returning
// unexpected extra output if stdin has data, or blocking forever if stdin
// is an open pipe that is never closed.
func resolveStdinDefaultFiles(files []string) ([]string, error) {
	if len(files) > 0 {
		return files, nil
	}
	stdinInfo, err := os.Stdin.Stat()
	if err != nil {
		return nil, ansi.Errorf("@R{Error statting STDIN} - Bailing out: %s\n", err.Error())
	}
	if stdinInfo.Mode()&os.ModeCharDevice == 0 {
		return append(files, "-"), nil
	}
	return files, nil
}

func cmdJSONEval(options jsonOpts) ([]string, error) {
	files, err := resolveStdinDefaultFiles(options.Files)
	if err != nil {
		return nil, err
	}

	output, err := graft.JSONifyFiles(files, options.Strict)
	if err != nil {
		return nil, err
	}

	return output, nil
}

// cmdJSONReverseEval implements `graft json --reverse`: JSON input (from
// files or stdin) converted to YAML documents.
func cmdJSONReverseEval(options jsonOpts) ([]string, error) {
	files, err := resolveStdinDefaultFiles(options.Files)
	if err != nil {
		return nil, err
	}

	return graft.YAMLifyFiles(files)
}

type yamlVaultSecret struct {
	Key        string   `json:"key"`
	References []string `json:"references"`
}

type byKey []yamlVaultSecret

type yamlVaultRefs struct {
	Secrets []yamlVaultSecret `json:"secrets"`
}

func (refs byKey) Len() int           { return len(refs) }
func (refs byKey) Swap(i, j int)      { refs[i], refs[j] = refs[j], refs[i] }
func (refs byKey) Less(i, j int) bool { return refs[i].Key < refs[j].Key }

// buildVaultRefs collects and sorts engine's discovered Vault references
// into the shape both the default YAML output (formatVaultRefs) and the
// --json/--paths-only output (formatVaultInfoExtended) render from, so all
// three share one sort/collection implementation. Secrets is always a
// non-nil (possibly empty) slice so JSON marshaling emits `[]` rather than
// `null` when no Vault references are found; graft.MarshalYAML already
// renders a nil slice as `[]` (see formatVaultRefs's existing byte-for-byte
// test coverage), so this is not a behavior change for the YAML path.
func buildVaultRefs(engine graft.Engine) yamlVaultRefs {
	refs := yamlVaultRefs{Secrets: []yamlVaultSecret{}}
	vaultRefs := engine.GetOperatorState().GetVaultRefs()
	for secret, srcs := range vaultRefs {
		refs.Secrets = append(refs.Secrets, yamlVaultSecret{secret, srcs})
	}

	sort.Sort(byKey(refs.Secrets))
	for _, secret := range refs.Secrets {
		sort.Strings(secret.References)
	}

	return refs
}

func formatVaultRefs(engine graft.Engine) string {
	refs := buildVaultRefs(engine)

	output, err := graft.MarshalYAML(refs)
	if err != nil {
		panic(fmt.Sprintf("Could not marshal YAML for vault references: %+v", refs))
	}

	return string(output)
}

// formatVaultInfoExtended renders `vaultinfo --json` and/or
// `vaultinfo --paths-only` output from an already-built, already-sorted
// yamlVaultRefs. Combining both flags yields a JSON array of just the
// secret key strings; --paths-only alone yields one key per line (plain
// text, easy to feed into a shell `while read` loop); --json alone yields
// the full key+references structure as JSON.
func formatVaultInfoExtended(refs yamlVaultRefs, jsonOutput, pathsOnly bool) (string, error) {
	if pathsOnly {
		paths := make([]string, len(refs.Secrets))
		for i, s := range refs.Secrets {
			paths[i] = s.Key
		}
		if jsonOutput {
			b, err := json.MarshalIndent(paths, "", "  ")
			if err != nil {
				return "", ansi.Errorf("@R{Could not marshal JSON for vault paths}: %s", err.Error())
			}
			return string(b), nil
		}
		return strings.Join(paths, "\n"), nil
	}

	b, err := json.MarshalIndent(refs, "", "  ")
	if err != nil {
		return "", ansi.Errorf("@R{Could not marshal JSON for vault references}: %s", err.Error())
	}
	return string(b), nil
}

func readFile(file *YamlFile) ([]byte, error) {
	var data []byte
	var err error

	if file.Path == "-" {
		file.Path = stdinPath
		stat, statErr := os.Stdin.Stat()
		if statErr != nil {
			return nil, ansi.Errorf("@R{Error statting STDIN} - Bailing out: %s\n", statErr.Error())
		}
		if stat.Mode()&os.ModeCharDevice == 0 {
			data, err = io.ReadAll(os.Stdin)
			if err != nil {
				return nil, ansi.Errorf("@R{Error reading file} @m{%s}: %s\n", file.Path, err.Error())
			}
		}
	} else {
		data, err = io.ReadAll(file.Reader)
		if err != nil {
			return nil, ansi.Errorf("@R{Error reading file} @m{%s}: %s\n", file.Path, err.Error())
		}
	}
	if len(data) == 0 && file.Path == stdinPath {
		return nil, ansi.Errorf("@R{Error reading STDIN}: no data found. Did you forget to pipe data to STDIN, or specify yaml files to merge?")
	}

	return data, nil
}

// buildEngineAndDocs constructs the graft.Engine and parses every input
// file into a graft.Document, exactly as mergeAllDocs' single-shot merge
// path always has. It is also reused by the history-tracking path
// (cmdMergeHistoryEval) so `graft merge --history`/--trace-path/
// --show-changes/--changes-only parse files identically to a plain
// `graft merge` - same engine construction, same go-patch detection, same
// blank/null-document-as-empty-map handling - and only differ in how the
// resulting documents are merged (incrementally, to capture per-file raw
// snapshots, rather than in one Execute() call).
func buildEngineAndDocs(files []YamlFile, options *mergeOpts) (graft.Engine, []graft.Document, error) {
	// Create engine with the cache default applied first, then
	// caller-provided options (options.EngineOpts) layered on top, so a
	// cache-related entry in options.EngineOpts overrides this default
	// instead of being silently discarded. Concurrency/parallel-evaluation
	// defaults come from options.EngineOpts (see
	// configEngineOpts/resolveConcurrency) when a resolved config was
	// supplied by the CLI. No caller today sets a cache option via
	// options.EngineOpts (see configEngineOpts), so this ordering is
	// behaviorally identical to before for every current invocation; the
	// feature-flag gate on caching (see the comment in
	// pkg/graft/engine.go createEngineFromOptions) is unaffected by this
	// change either way.
	engineOpts := make([]graft.EngineOption, 0, len(options.EngineOpts)+2)
	engineOpts = append(engineOpts, graft.WithCache(true, 1000))
	engineOpts = append(engineOpts, options.EngineOpts...)

	// Set dataflow order if specified (default to alphabetical if not set)
	dataflowOrder := options.DataflowOrder
	if dataflowOrder == "" {
		dataflowOrder = "alphabetical"
	}
	engineOpts = append(engineOpts, graft.WithDataflowOrder(dataflowOrder))

	engine, err := graft.NewEngine(engineOpts...)
	if err != nil {
		return nil, nil, ansi.Errorf("@R{Failed to create graft engine}: %s", err.Error())
	}

	// Read and parse every file concurrently: reading bytes off disk (or
	// STDIN) and parsing them into a Document has no side effects and
	// doesn't depend on any other file, so it is safe to fan out - only
	// the eventual merge order (docs stays indexed by the caller's
	// original file order below) is significant, and that is untouched.
	// engine.ParseYAML performs no writes to engine state (verified: it
	// only calls package-level parse helpers), so sharing one *DefaultEngine
	// across these goroutines is safe.
	// The fan-out is bounded to NumCPU workers rather than one goroutine
	// per file: peak memory grows with the number of documents held
	// mid-parse at once, so an unbounded launch makes RSS proportional
	// to file count for no throughput gain once every core is busy.
	// The per-document parse cache (Layer 2) is shared by all workers;
	// FileStore.Get/Put are safe for concurrent use. nil (the default -
	// cache disabled, debug/trace run, unusable directory) restores the
	// exact pre-cache behavior.
	parseStore := openMergeParseCache(options)

	results := make([]fileParseResult, len(files))
	workers := runtime.NumCPU()
	if workers > len(files) {
		workers = len(files)
	}
	indices := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range indices {
				// Each parse gets its own copy of the YamlFile: readFile
				// mutates its Path field for the "-"/STDIN case, and
				// sharing that mutation across goroutines (even
				// harmlessly, since each only touches its own index)
				// would be unnecessarily fragile.
				fileCopy := files[idx]
				results[idx] = parseOneYamlFile(engine, fileCopy, options, parseStore)
			}
		}()
	}
	for i := range files {
		indices <- i
	}
	close(indices)
	wg.Wait()

	// Preserve the sequential loop's behavior exactly: it stopped at the
	// first file that failed to read/parse, in file order, so any later
	// file's (now also attempted, since reads ran concurrently) error is
	// discarded in favor of the earliest-indexed one.
	docs := make([]graft.Document, 0, len(files))
	for i, r := range results {
		if r.err != nil {
			return nil, nil, r.err
		}
		log.DEBUG("Processed file '%s'", files[i].Path)
		docs = append(docs, r.doc)
	}

	return engine, docs, nil
}

// fileParseResult holds the outcome of parsing one input file into a
// graft.Document, or the error that occurred while doing so.
type fileParseResult struct {
	doc graft.Document
	err error
}

// parseOneYamlFile reads and parses a single input file (or STDIN),
// handling go-patch detection and blank/null-document normalization
// identically to the sequential loop it replaces in buildEngineAndDocs.
// Safe to call concurrently for different files sharing the same engine.
// A non-nil parseStore serves and stores per-document parse results
// (Layer 2 of the persistent cache, see merge_parse_cache.go); marker
// documents and go-patch array documents are never cached.
func parseOneYamlFile(engine graft.Engine, file YamlFile, options *mergeOpts, parseStore *cache.FileStore) fileParseResult {
	log.DEBUG("Processing file '%s'", file.Path)

	data, readErr := readFile(&file)
	if readErr != nil {
		return fileParseResult{err: readErr}
	}

	// Check if it's a go-patch document. DetectArrayRoot's byte probe
	// answers "not an array" for the common map-rooted file without a
	// throwaway classification parse; syntax errors surface from the
	// real parse below either way.
	if options.EnableGoPatch {
		if graft.IsArrayError(graft.DetectArrayRoot(data)) {
			log.DEBUG("Detected root of document as an array. Attempting go-patch parsing")
			ops, patchErr := graft.ParseGoPatch(data)
			if patchErr != nil {
				return fileParseResult{err: ansi.Errorf("@m{%s}: @R{%s}\n", file.Path, patchErr.Error())}
			}
			return fileParseResult{doc: graft.NewGoPatchDocument(ops)}
		}
	}

	// Layer 2 lookup. Parsing a marker-free document is a pure function
	// of its bytes, so a stored tree can stand in for a real parse. A
	// document with control-flow markers evaluates operators during
	// parse and must never be served from or stored into the cache;
	// cacheKey doubles as that gate ("" = do not cache).
	var cacheKey string
	if parseStore != nil && !controlflow.HasMarkers(data) {
		cacheKey = parseCacheKey(data)
		if raw, ok := parseStore.Get(cacheKey); ok {
			if tree, valid := decodeCachedTree(raw); valid {
				// Each hit decodes a fresh tree, so the merge is free to
				// mutate it in place without touching the stored entry.
				return fileParseResult{doc: graft.NewDocument(tree)}
			}
			// Corrupt entry: fall through to a real parse, which will
			// overwrite it with a good one.
		}
	}

	// Parse as YAML
	doc, parseDocErr := engine.ParseYAML(data)
	if parseDocErr != nil {
		return fileParseResult{err: ansi.Errorf("@m{%s}: @R{%s}\n", file.Path, parseDocErr.Error())}
	}
	if doc == nil {
		// engine.ParseYAML returns (nil, nil) for blank, comment-only,
		// or null ("---") documents. Treat as an empty map, matching
		// spruce's behavior of merging such documents as {} no-ops.
		log.DEBUG("YAML doc '%s' is null/empty, creating empty hash/map", file.Path)
		doc = graft.NewDocument(make(map[string]interface{}))
	}
	if cacheKey != "" {
		// Store immediately, before any merge can mutate the tree. An
		// unencodable tree is simply not cached.
		if m, ok := doc.RawData().(map[string]interface{}); ok {
			if encoded, encErr := encodeCachedTree(m); encErr == nil {
				_ = parseStore.Put(cacheKey, encoded)
			}
		}
	}
	return fileParseResult{doc: doc}
}

func mergeAllDocs(files []YamlFile, options *mergeOpts) (map[string]interface{}, graft.Engine, error) {
	engine, docs, err := buildEngineAndDocs(files, options)
	if err != nil {
		return nil, nil, err
	}

	// Merge all documents
	mergeBuilder := engine.Merge(context.TODO(), docs...)

	// Apply merge options
	if options.FallbackAppend {
		mergeBuilder = mergeBuilder.WithArrayMergeStrategy(graft.AppendArrays)
	}

	if options.SkipEval {
		mergeBuilder = mergeBuilder.SkipEvaluation()
	}

	// Apply cherry-pick keys at the builder level
	if len(options.CherryPick) > 0 {
		mergeBuilder = mergeBuilder.WithCherryPick(options.CherryPick...)
	}

	// Apply prune keys at the builder level
	if len(options.Prune) > 0 {
		mergeBuilder = mergeBuilder.WithPrune(options.Prune...)
	}

	// Execute merge
	merged, err := mergeBuilder.Execute()
	if err != nil {
		// Check if this is a MultiError from the merger (Issue #172)
		if strings.Contains(err.Error(), "error(s) detected:") {
			return nil, nil, err
		}
		return nil, nil, ansi.Errorf("@R{Merge failed}: %s", err.Error())
	}

	// Get the raw data for backward compatibility
	// The CLI expects a map[string]interface{}
	data, ok := merged.GetData().(map[string]interface{})
	if !ok {
		return nil, nil, ansi.Errorf("@R{Merge result is not a map}")
	}
	return data, engine, nil
}

func diffFiles(paths []string) (output string, hasDifferences bool, err error) {
	if len(paths) != 2 {
		return "", false, ansi.Errorf("incorrect number of files given to diffFiles(); please file a bug report")
	}

	from, to, err := ytbx.LoadFiles(paths[0], paths[1])
	if err != nil {
		return "", false, err
	}

	report, err := dyff.CompareInputFiles(from, to)
	if err != nil {
		return "", false, err
	}

	reportWriter := &dyff.HumanReport{
		Report:            report,
		DoNotInspectCerts: false,
		NoTableStyle:      false,
		OmitHeader:        true,
	}

	var buf bytes.Buffer
	out := bufio.NewWriter(&buf)
	if err := reportWriter.WriteReport(out); err != nil {
		return "", false, fmt.Errorf("failed to write report: %w", err)
	}
	if err := out.Flush(); err != nil {
		return "", false, fmt.Errorf("failed to flush report: %w", err)
	}

	return buf.String(), len(report.Diffs) > 0, nil
}
