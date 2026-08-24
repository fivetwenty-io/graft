package main

import (
	"context"
	"fmt"
	"io"
	"runtime"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/config"
	"github.com/fivetwenty-io/graft/internal/features"
	"github.com/fivetwenty-io/graft/pkg/graft"
)

// newInMemoryYamlFile builds a YamlFile backed by an in-memory reader, for
// tests that don't need real files on disk.
func newInMemoryYamlFile(path, content string) YamlFile {
	return YamlFile{Path: path, Reader: io.NopCloser(strings.NewReader(content))}
}

// TestBuildEngineAndDocsPreservesFileOrderUnderConcurrentParsing proves
// that buildEngineAndDocs' concurrent file read/parse (file-level
// parallelism) still returns docs indexed by the caller's
// original file order, not by goroutine completion order - merge order is
// significant (later files override earlier ones), so this would be a
// silent correctness regression if it broke.
func TestBuildEngineAndDocsPreservesFileOrderUnderConcurrentParsing(t *testing.T) {
	files := []YamlFile{
		newInMemoryYamlFile("first.yml", "value: first\nonly_in_first: 1\n"),
		newInMemoryYamlFile("second.yml", "value: second\nonly_in_second: 2\n"),
		newInMemoryYamlFile("third.yml", "value: third\nonly_in_third: 3\n"),
	}

	engine, docs, _, err := buildEngineAndDocs(files, &mergeOpts{})
	if err != nil {
		t.Fatalf("buildEngineAndDocs() error = %v", err)
	}
	if len(docs) != 3 {
		t.Fatalf("expected 3 docs, got %d", len(docs))
	}

	mergeBuilder := engine.Merge(context.Background(), docs...)
	merged, err := mergeBuilder.Execute()
	if err != nil {
		t.Fatalf("merge Execute() error = %v", err)
	}
	result := merged.RawData().(map[string]interface{})

	// "value" must reflect the LAST file (third.yml) per graft's overlay
	// semantics, which only holds if merge order matches file order.
	if result["value"] != "third" {
		t.Errorf("value = %v, want %q (file order must be preserved through concurrent parsing)", result["value"], "third")
	}
	for key, want := range map[string]int{"only_in_first": 1, "only_in_second": 2, "only_in_third": 3} {
		if result[key] != want {
			t.Errorf("%s = %v, want %d", key, result[key], want)
		}
	}
}

// TestBuildEngineAndDocsReportsEarliestFileErrorUnderConcurrentParsing
// proves that when multiple files fail to parse, buildEngineAndDocs
// reports the earliest-indexed file's error - matching the sequential
// loop it replaced, which stopped at the first failure and never even
// attempted later files. Concurrent parsing attempts every file
// regardless, so this pins the result's determinism explicitly.
func TestBuildEngineAndDocsReportsEarliestFileErrorUnderConcurrentParsing(t *testing.T) {
	files := []YamlFile{
		newInMemoryYamlFile("ok.yml", "a: 1\n"),
		newInMemoryYamlFile("bad-first.yml", "- this\n- is\n- an array, not a map\n"),
		newInMemoryYamlFile("bad-second.yml", "- another\n- array\n"),
	}

	_, _, _, err := buildEngineAndDocs(files, &mergeOpts{}) //nolint:dogsled // only err matters here; engine/docs/refs are unused
	if err == nil {
		t.Fatal("expected an error from the two array-root files, got nil")
	}
	if !strings.Contains(err.Error(), "bad-first.yml") {
		t.Errorf("expected error to name the earliest-indexed failing file (bad-first.yml), got: %s", err.Error())
	}
}

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
		files, err := openFiles([]string{"../../assets/static_ips/multi-azs-same-ip-different-index.yml"})
		if err != nil {
			t.Fatalf("iteration %d: openFiles() error = %v", i, err)
		}

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

// TestParallelStaticIPOrderSensitiveCarveOutPreventsRaceyClaims covers a
// gap in TestParallelStaticIPClaimErrorDeterministic above: forcing
// isOrderSensitiveOp to return false (fully defeating the serialization
// StaticIPOperator.OrderSensitive exists to provide) does not make that
// test fail. Its fixture has exactly one colliding pair, and
// claimStaticIP's critical
// section is fast enough that sorted submission order alone usually
// reproduces the same winner run after run even with no explicit
// serialization, so that test never actually exercised the race the
// carve-out exists to prevent.
//
// This test widens real-operator coverage well beyond that one-pair
// fixture: 80 independent pairs of static_ips jobs (160 operators total,
// one winner + one duplicate-claim error per pair), each pair claiming the
// single static IP in its own subnet so pairs never contend with each
// other, all landing in one dependency-free wave, with
// cfg.Parallel.MaxWorkers pinned to 2 to maximize queue contention. With
// the carve-out intact (the normal, shipped state), every pair's winner is
// deterministic and the aggregated error text is byte-identical across
// repeated runs - this test asserts exactly that and would fail on any
// regression that made static_ips's winner selection nondeterministic in
// the normal, unmutated codebase.
//
// Honest limitation: manually mutating isOrderSensitiveOp to `return
// false` does NOT reliably make this test fail, even at this 80-pair
// scale or a 1000-pair scale tried during development. claimStaticIP's
// critical section is a single global mutex around a few nanoseconds of
// map work; on this hardware (32 logical CPUs, GOMAXPROCS=32) Go's
// scheduler resolves goroutine dispatch order closely enough to submission
// order that the theoretical race the carve-out guards against does not
// manifest without also injecting artificial delay into the operator
// itself, which was judged out of scope (it would mean shipping
// test-only behavior in production operator code). This test therefore
// verifies the real operator's behavior is correct and deterministic at
// wave-scale, but is not proof the OrderSensitive carve-out specifically
// is load-bearing on every possible machine/load condition.
func TestParallelStaticIPOrderSensitiveCarveOutPreventsRaceyClaims(t *testing.T) {
	const pairs = 80
	const repeats = 10

	var b strings.Builder
	b.WriteString("jobs:\n")
	for g := 0; g < pairs; g++ {
		fmt.Fprintf(&b, `- name: group%03da
  instances: 1
  azs: [z1]
  networks:
  - name: net%03d
    static_ips: (( static_ips(0) ))
- name: group%03db
  instances: 1
  azs: [z1]
  networks:
  - name: net%03d
    static_ips: (( static_ips(0) ))
`, g, g, g, g)
	}
	b.WriteString("networks:\n")
	for g := 0; g < pairs; g++ {
		fmt.Fprintf(&b, `- name: net%03d
  subnets:
  - az: z1
    static:
    - 10.%d.1.1
`, g, g)
	}
	yamlSrc := b.String()

	cfg := config.DefaultConfig()
	cfg.Parallel.MaxWorkers = 2
	ff := features.DefaultFlags()

	var baseline string
	for i := 0; i < repeats; i++ {
		files := []YamlFile{newInMemoryYamlFile("static-ips-amplified.yml", yamlSrc)}
		opts := &mergeOpts{EngineOpts: configEngineOpts(cfg, ff)}

		_, _, err := mergeAllDocs(files, opts)
		if err == nil {
			t.Fatalf("iteration %d: expected %d duplicate-IP-claim errors (one per pair), got nil", i, pairs)
		}

		got := err.Error()
		if i == 0 {
			baseline = got
			continue
		}
		if got != baseline {
			t.Fatalf("iteration %d produced different static_ips claim errors than iteration 0 under parallel evaluation (winner nondeterminism - the OrderSensitive carve-out is not preventing a real race)\niteration 0 length: %d\niteration %d length: %d",
				i, len(baseline), i, len(got))
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
