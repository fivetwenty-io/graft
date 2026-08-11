package graft

import (
	"bytes"
	"context"
	"os"
	"testing"
)

// parallelDeterminismIterations is the repeated-run count used to gain
// statistical confidence that parallel (COW/scheduler) evaluation produces
// byte-identical output across runs. Parallel evaluation is the CLI's
// default, so nondeterminism here would directly threaten genesis's
// byte-sensitive stderr/JSON parsing contracts - a single pass is not
// sufficient evidence.
const parallelDeterminismIterations = 40

// newDeterminismTestEngine builds a fresh engine per iteration (mirrors
// mergeAllDocs' per-invocation engine construction in cmd/graft/main.go)
// with parallel evaluation enabled and a worker count higher than 1 so the
// scheduler actually dispatches concurrent waves rather than degenerating
// to single-goroutine execution.
func newDeterminismTestEngine(t *testing.T) Engine {
	t.Helper()
	engine, err := NewEngine(
		WithParallel(true),
		WithConcurrency(8),
		WithCache(true, 1000),
	)
	if err != nil {
		t.Fatalf("NewEngine() error = %v", err)
	}
	return engine
}

// readFixture loads a fixture file relative to the repo root (pkg/graft is
// two directories below it).
func readFixture(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile("../../" + path)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", path, err)
	}
	return data
}

// TestParallelMergeDeterministicOutput repeatedly runs a full merge (the
// same engine.Merge(...).Execute() path the CLI's mergeAllDocs uses) over
// representative multi-doc, operator-heavy fixture sets and asserts every
// run's marshaled YAML output is byte-identical to the first. Covers array
// merge markers (assets/merge), the sort operator merged in on top of raw
// data (assets/sort), and a grab/calc dependency chain (assets/calc).
func TestParallelMergeDeterministicOutput(t *testing.T) {
	SilenceWarnings(true)

	cases := []struct {
		name  string
		files []string
	}{
		{
			name:  "array_merge_markers",
			files: []string{"assets/merge/first.yml", "assets/merge/second.yml"},
		},
		{
			name:  "sort_operator",
			files: []string{"assets/sort/base.yml", "assets/sort/op.yml"},
		},
		{
			name:  "calc_dependency_chain",
			files: []string{"assets/calc/dependencies.yml"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			raw := make([][]byte, 0, len(tc.files))
			for _, f := range tc.files {
				raw = append(raw, readFixture(t, f))
			}

			var baseline []byte
			for i := 0; i < parallelDeterminismIterations; i++ {
				engine := newDeterminismTestEngine(t)

				docs := make([]Document, 0, len(raw))
				for _, data := range raw {
					doc, err := engine.ParseYAML(data)
					if err != nil {
						t.Fatalf("iteration %d: ParseYAML() error = %v", i, err)
					}
					docs = append(docs, doc)
				}

				merged, err := engine.Merge(context.Background(), docs...).Execute()
				if err != nil {
					t.Fatalf("iteration %d: Merge().Execute() error = %v", i, err)
				}

				out, err := MarshalYAML(merged.GetData())
				if err != nil {
					t.Fatalf("iteration %d: MarshalYAML() error = %v", i, err)
				}

				if i == 0 {
					baseline = out
					continue
				}
				if !bytes.Equal(out, baseline) {
					t.Fatalf("iteration %d produced output different from iteration 0 under parallel evaluation.\niteration 0:\n%s\niteration %d:\n%s",
						i, baseline, i, out)
				}
			}
		})
	}
}

// TestParallelEvaluateDeterministicOutput repeatedly calls Engine.Evaluate
// directly (bypassing the merge builder) on a freshly parsed document with
// both independent operators (safe to run concurrently within a phase) and
// a dependency chain (must respect scheduler wave ordering), asserting
// byte-identical marshaled output across every run.
func TestParallelEvaluateDeterministicOutput(t *testing.T) {
	SilenceWarnings(true)

	yamlDoc := []byte(`
meta:
  base: 10
  name: app
  enabled: true

independent:
  i1: (( grab meta.base ))
  i2: (( grab meta.name ))
  i3: (( concat meta.name "-v1" ))
  i4: (( calc "meta.base * 2" ))
  i5: (( calc "meta.base + 5" ))

chain:
  step1: (( calc "meta.base + 1" ))
  step2: (( calc "chain.step1 * 2" ))
  step3: (( calc "chain.step2 + chain.step1" ))
`)

	var baseline []byte
	for i := 0; i < parallelDeterminismIterations; i++ {
		engine := newDeterminismTestEngine(t)

		doc, err := engine.ParseYAML(yamlDoc)
		if err != nil {
			t.Fatalf("iteration %d: ParseYAML() error = %v", i, err)
		}

		result, err := engine.Evaluate(context.Background(), doc)
		if err != nil {
			t.Fatalf("iteration %d: Evaluate() error = %v", i, err)
		}

		out, err := MarshalYAML(result.GetData())
		if err != nil {
			t.Fatalf("iteration %d: MarshalYAML() error = %v", i, err)
		}

		if i == 0 {
			baseline = out
			continue
		}
		if !bytes.Equal(out, baseline) {
			t.Fatalf("iteration %d produced output different from iteration 0 under parallel evaluation.\niteration 0:\n%s\niteration %d:\n%s",
				i, baseline, i, out)
		}
	}
}
