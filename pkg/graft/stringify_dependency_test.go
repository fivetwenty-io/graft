package graft_test

// StringifyOperator.Dependencies (op_stringify.go) returns only `auto` —
// it never declares a dependency on opcalls nested inside the subtree its
// argument references. The evaluator's dependency graph (both the
// sequential path, evaluator.go's buildDependencyGraph, and the parallel
// path, evaluator_parallel.go's runOpsWithScheduler) therefore has no
// edge forcing a nested opcall under stringify's target to run before
// stringify captures that subtree, so stringify can (and, per the
// examples-repair-notes.md minimal repro, reliably does) run first and
// serialize the subtree's still-unevaluated marker text instead of its
// resolved value. InjectOperator.Dependencies (op_inject.go) already
// solves the identical problem for its own reference argument by walking
// `locs` (every other opcall's location in the document) for entries
// under the reference's own canonical path.

import (
	"strings"
	"testing"

	_ "github.com/fivetwenty-io/graft/pkg/graft/operators"
)

// mergeYAMLParallel is defined in calc_sibling_dependency_parallel_test.go
// (same package). Reused here per the round-5 lesson: a dependency-graph
// fix must be pinned on both the sequential and parallel evaluation
// paths, since RunPhaseParallel computes its own, separate dependency
// list rather than reusing the sequential path's.

// TestStringifySubtreeDependency_Serial is the examples-repair-notes.md
// minimal repro, run through the library's serial default (RunPhase).
func TestStringifySubtreeDependency_Serial(t *testing.T) {
	doc, err := mergeYAML(t, "services:\n  a: 1\n  b: 2\nenv:\n  config:\n    services: (( grab services ))\n    other: hello\n  final: (( stringify env.config ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("env.final")
	if err != nil {
		t.Fatalf("failed to read env.final: %v", err)
	}
	s, ok := got.(string)
	if !ok {
		t.Fatalf("expected env.final to be a string, got %T: %v", got, got)
	}
	if strings.Contains(s, "((") {
		t.Fatalf("env.final still contains an unresolved marker: %q", s)
	}
	for _, want := range []string{"other: hello", "a: 1", "b: 2"} {
		if !strings.Contains(s, want) {
			t.Fatalf("env.final missing expected resolved content %q: %q", want, s)
		}
	}
}

// TestStringifySubtreeDependency_Parallel is the same repro, run through
// the CLI's real default (parallel evaluation) — the round-5 lesson.
func TestStringifySubtreeDependency_Parallel(t *testing.T) {
	doc, err := mergeYAMLParallel(t, "services:\n  a: 1\n  b: 2\nenv:\n  config:\n    services: (( grab services ))\n    other: hello\n  final: (( stringify env.config ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("env.final")
	if err != nil {
		t.Fatalf("failed to read env.final: %v", err)
	}
	s, ok := got.(string)
	if !ok {
		t.Fatalf("expected env.final to be a string, got %T: %v", got, got)
	}
	if strings.Contains(s, "((") {
		t.Fatalf("env.final still contains an unresolved marker: %q", s)
	}
	for _, want := range []string{"other: hello", "a: 1", "b: 2"} {
		if !strings.Contains(s, want) {
			t.Fatalf("env.final missing expected resolved content %q: %q", want, s)
		}
	}
}

// TestStringifySubtreeDependency_Repeated repeats the parallel-path case
// many times: wave scheduling depends on Go's randomized map iteration in
// more than one place, so a fix that only wins a race would not
// necessarily reproduce as passing on every run.
func TestStringifySubtreeDependency_Repeated(t *testing.T) {
	src := "services:\n  a: 1\n  b: 2\nenv:\n  config:\n    services: (( grab services ))\n    other: hello\n  final: (( stringify env.config ))\n"
	for i := 0; i < 25; i++ {
		doc, err := mergeYAMLParallel(t, src)
		if err != nil {
			t.Fatalf("iteration %d: unexpected error: %v", i, err)
		}
		got, err := doc.Get("env.final")
		if err != nil {
			t.Fatalf("iteration %d: failed to read env.final: %v", i, err)
		}
		s, ok := got.(string)
		if !ok {
			t.Fatalf("iteration %d: expected env.final to be a string, got %T: %v", i, got, got)
		}
		if strings.Contains(s, "((") {
			t.Fatalf("iteration %d: env.final still contains an unresolved marker: %q", i, s)
		}
	}
}

// TestStringifyScalarTarget_Unaffected pins the already-working scalar
// case (stringify's own dispatch for string/int/float/bool arguments,
// which never reaches the YAML-marshal-a-subtree branch this bug is
// about) stays unaffected.
func TestStringifyScalarTarget_Unaffected(t *testing.T) {
	doc, err := mergeYAML(t, "count: 5\nresult: (( stringify count ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("result")
	if err != nil {
		t.Fatalf("failed to read result: %v", err)
	}
	if got != "5" {
		t.Fatalf("expected \"5\", got %v", got)
	}
}

// TestStringifyNoNestedOpcalls_Unaffected pins a subtree with no nested
// opcalls at all (the common case) stays unaffected — the fix must not
// require every stringify target to contain an opcall to work correctly.
func TestStringifyNoNestedOpcalls_Unaffected(t *testing.T) {
	doc, err := mergeYAML(t, "plain:\n  first: 1\n  second: two\nresult: (( stringify plain ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("result")
	if err != nil {
		t.Fatalf("failed to read result: %v", err)
	}
	s, ok := got.(string)
	if !ok {
		t.Fatalf("expected result to be a string, got %T: %v", got, got)
	}
	for _, want := range []string{"first: 1", "second: two"} {
		if !strings.Contains(s, want) {
			t.Fatalf("result missing expected content %q: %q", want, s)
		}
	}
}
