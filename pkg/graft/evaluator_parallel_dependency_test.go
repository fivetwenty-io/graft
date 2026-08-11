package graft

import (
	"bytes"
	"context"
	"testing"
)

// parallelDependencyFixtures pins the parallel-scheduler dependency-edge
// bug: a dependent operator referencing a path deeper than a producing
// operator's own path (e.g. `grab meta.vm.small` where `meta.vm` is itself
// `(( grab ... ))`). Two alphabetical directions are covered because the
// scheduler's same-wave fallback sort (tasks with no computed dependency
// edge run in alphabetical task-ID order within their wave) makes one
// direction pass even when the edge itself is missing - only the direction
// where the dependent's path sorts before the producer's path exposes a
// dropped edge as a hard failure.
var parallelDependencyFixtures = []struct {
	name string
	yaml string
}{
	{
		// Dependent ("instance_groups...") sorts BEFORE producer
		// ("meta.vm") alphabetically. A missing dependency edge here runs
		// the dependent first and fails: the producer's op hasn't run yet,
		// so `meta.vm` is still the literal `(( grab defaults.vms ))`
		// scalar rather than the map `grab meta.vm.small` needs to descend
		// into.
		name: "dependent_before_producer_alphabetically",
		yaml: `
instance_groups:
- name: web
  vm_type: (( grab meta.vm.small ))
meta:
  vm: (( grab defaults.vms ))
defaults:
  vms: {small: minimal}
`,
	},
	{
		// Dependent ("zzz.deep.value") sorts AFTER producer
		// ("aaa.producer") alphabetically - a missing edge does not
		// surface here because the scheduler's alphabetical same-wave sort
		// happens to place the producer first anyway. Pinned as a
		// regression guard for both directions.
		name: "dependent_after_producer_alphabetically",
		yaml: `
aaa:
  producer: (( grab defaults.vms ))
zzz:
  deep:
    value: (( grab aaa.producer.small ))
defaults:
  vms: {small: minimal}
`,
	},
	{
		// Multi-hop chain, each dependent's path sorting before its
		// producer's op path: aaa depends on a subtree of mid, mid depends
		// on a subtree of zzz.
		name: "multi_hop_chain_before_producer",
		yaml: `
aaa:
  c: (( grab mid.b.inner ))
mid:
  b: (( grab zzz.a.value ))
zzz:
  a: {value: {inner: leaf}}
`,
	},
	{
		// Producer op sits inside a list element (index-addressed path);
		// dependent references a subtree of that op's own path and sorts
		// before it alphabetically.
		name: "producer_inside_list_element",
		yaml: `
apps:
  api_url: (( grab groups.0.settings.host ))
groups:
- name: web
  settings: (( grab defaults.settings ))
defaults:
  settings: {host: example.com, port: 80}
`,
	},
	{
		// Control: reference to a genuinely undefined path, unrelated to
		// any operator's subtree. Both modes must report the same error.
		name: "undefined_reference_error",
		yaml: `
thing: (( grab does.not.exist ))
`,
	},
}

// newParallelDependencyTestEngine builds an engine with the given parallel
// setting, worker concurrency, and caching enabled, matching the shape used
// by TestParallelMergeDeterministicOutput.
func newParallelDependencyTestEngine(t *testing.T, parallel bool) Engine {
	t.Helper()
	engine, err := NewEngine(
		WithParallel(parallel),
		WithConcurrency(8),
		WithCache(true, 1000),
	)
	if err != nil {
		t.Fatalf("NewEngine(parallel=%v) error = %v", parallel, err)
	}
	return engine
}

// TestParallelDependencyEdge_DependentBeforeProducerAlphabetically is the
// exact repro for the parallel dependency-mapping bug: with parallel
// evaluation (the CLI default), a `grab` reaching into a subtree of another
// operator's own result path must still resolve correctly even when the
// dependent operator's path sorts alphabetically before the producer's.
func TestParallelDependencyEdge_DependentBeforeProducerAlphabetically(t *testing.T) {
	SilenceWarnings(true)

	engine := newParallelDependencyTestEngine(t, true)
	doc, err := engine.ParseYAML([]byte(parallelDependencyFixtures[0].yaml))
	if err != nil {
		t.Fatalf("ParseYAML() error = %v", err)
	}

	merged, err := engine.Merge(context.Background(), doc).Execute()
	if err != nil {
		t.Fatalf("Merge().Execute() error = %v (parallel evaluator dropped the meta.vm -> meta.vm.small dependency edge)", err)
	}

	data, ok := merged.GetData().(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected merged data shape: %#v", merged.GetData())
	}
	groups, ok := data["instance_groups"].([]interface{})
	if !ok || len(groups) != 1 {
		t.Fatalf("unexpected instance_groups shape: %#v", data["instance_groups"])
	}
	web, ok := groups[0].(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected instance_groups[0] shape: %#v", groups[0])
	}
	if got := web["vm_type"]; got != "minimal" {
		t.Fatalf("vm_type = %v, want %q", got, "minimal")
	}
}

// TestParallelDependencyEdge_DependentAfterProducerAlphabetically pins the
// direction that already passed before the fix (by luck of the same-wave
// alphabetical sort), so a regression that only breaks the after-direction
// is also caught.
func TestParallelDependencyEdge_DependentAfterProducerAlphabetically(t *testing.T) {
	SilenceWarnings(true)

	engine := newParallelDependencyTestEngine(t, true)
	doc, err := engine.ParseYAML([]byte(parallelDependencyFixtures[1].yaml))
	if err != nil {
		t.Fatalf("ParseYAML() error = %v", err)
	}

	merged, err := engine.Merge(context.Background(), doc).Execute()
	if err != nil {
		t.Fatalf("Merge().Execute() error = %v", err)
	}

	data, ok := merged.GetData().(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected merged data shape: %#v", merged.GetData())
	}
	zzz, ok := data["zzz"].(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected zzz shape: %#v", data["zzz"])
	}
	deep, ok := zzz["deep"].(map[string]interface{})
	if !ok {
		t.Fatalf("unexpected zzz.deep shape: %#v", zzz["deep"])
	}
	if got := deep["value"]; got != "minimal" {
		t.Fatalf("zzz.deep.value = %v, want %q", got, "minimal")
	}
}

// runParallelDependencyMerge parses and merges rawDocs under the given
// parallel setting, returning the marshaled YAML output (nil on error) and
// any merge error.
func runParallelDependencyMerge(t *testing.T, parallel bool, rawDocs [][]byte) ([]byte, error) {
	t.Helper()

	engine := newParallelDependencyTestEngine(t, parallel)

	docs := make([]Document, 0, len(rawDocs))
	for _, raw := range rawDocs {
		doc, err := engine.ParseYAML(raw)
		if err != nil {
			t.Fatalf("ParseYAML() error = %v", err)
		}
		docs = append(docs, doc)
	}

	merged, err := engine.Merge(context.Background(), docs...).Execute()
	if err != nil {
		return nil, err
	}

	out, err := MarshalYAML(merged.GetData())
	if err != nil {
		t.Fatalf("MarshalYAML() error = %v", err)
	}
	return out, nil
}

// assertSequentialParallelEquivalent runs rawDocs through both the
// sequential and parallel evaluators and asserts they agree: either both
// fail with byte-identical error text, or both succeed with byte-identical
// marshaled output. The sequential path is the trusted reference (it
// already has the parent-path dependency fallback); parallel must never
// diverge from it.
func assertSequentialParallelEquivalent(t *testing.T, name string, rawDocs [][]byte) {
	t.Helper()

	seqOut, seqErr := runParallelDependencyMerge(t, false, rawDocs)
	parOut, parErr := runParallelDependencyMerge(t, true, rawDocs)

	if (seqErr == nil) != (parErr == nil) {
		t.Fatalf("%s: sequential/parallel error presence diverges: sequential err=%v parallel err=%v", name, seqErr, parErr)
	}
	if seqErr != nil {
		if seqErr.Error() != parErr.Error() {
			t.Fatalf("%s: sequential/parallel error text diverges:\nsequential: %s\nparallel:   %s", name, seqErr.Error(), parErr.Error())
		}
		return
	}
	if !bytes.Equal(seqOut, parOut) {
		t.Fatalf("%s: sequential/parallel output diverges:\nsequential:\n%s\nparallel:\n%s", name, seqOut, parOut)
	}
}

// TestParallelSequentialDependencyEquivalence runs a battery of
// dependency-shaped fixtures (the deep-reference repro directions above,
// plus the existing array-merge/sort/calc asset fixtures already used for
// determinism coverage) through both the sequential and parallel
// evaluators, asserting identical output or identical errors. Sequential
// and parallel must always produce the same dependency graph.
func TestParallelSequentialDependencyEquivalence(t *testing.T) {
	SilenceWarnings(true)

	for _, tc := range parallelDependencyFixtures {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			assertSequentialParallelEquivalent(t, tc.name, [][]byte{[]byte(tc.yaml)})
		})
	}

	assetCases := []struct {
		name  string
		files []string
	}{
		{name: "array_merge_markers", files: []string{"assets/merge/first.yml", "assets/merge/second.yml"}},
		{name: "sort_operator", files: []string{"assets/sort/base.yml", "assets/sort/op.yml"}},
		{name: "calc_dependency_chain", files: []string{"assets/calc/dependencies.yml"}},
	}

	for _, tc := range assetCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			raw := make([][]byte, 0, len(tc.files))
			for _, f := range tc.files {
				raw = append(raw, readFixture(t, f))
			}
			assertSequentialParallelEquivalent(t, tc.name, raw)
		})
	}
}
