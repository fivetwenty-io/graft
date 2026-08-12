package graft_test

// The round-4 Candidate E fix (evaluator.go, dataFlowContext.buildDependencyGraph)
// only reaches the sequential evaluation path (RunPhase), which is what
// mergeYAML's plain graft.NewEngine() uses by default
// (FeatureParallelEvaluation's own library default is false — see
// cmd/graft/config_precedence_test.go). RunPhaseParallel
// (evaluator_parallel.go) — which the graft CLI enables BY DEFAULT (see
// cmd/graft/main.go's configEngineOpts doc comment: "parallel evaluation
// is enabled by default") — computes its own, separate dependency list
// inside runOpsWithScheduler via a second, independent call to
// taskOp.Dependencies(ev, allLocs), which had the identical missing-
// ev.Here bug the round-4 fix addressed for buildDependencyGraph, just at
// a different call site the round-4 fix never touched. The practical
// effect: the round-4 fix appeared to work when checked directly against
// the library's serial default, but the CLI's actual default execution
// path — what every documented example and real user actually runs —
// was unaffected and still broken, for calc-to-calc siblings too, not
// only the infix-arithmetic-sibling shape the examples-repairer's direct
// verification surfaced.
//
// These tests explicitly enable parallel evaluation (graft.WithParallel)
// to exercise the CLI's real default path, rather than relying on
// mergeYAML's serial-only default.

import (
	"context"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
	_ "github.com/fivetwenty-io/graft/pkg/graft/operators"
)

// mergeYAMLParallel runs a merge through an engine with parallel
// evaluation explicitly enabled, matching the graft CLI's default
// (cmd/graft/main.go's configEngineOpts): "parallel evaluation is enabled
// by default".
func mergeYAMLParallel(t *testing.T, yamlSrc string) (graft.Document, error) {
	t.Helper()
	engine, err := graft.NewEngine(graft.WithParallel(true))
	if err != nil {
		t.Fatalf("failed to create parallel engine: %v", err)
	}
	doc, err := engine.ParseYAML([]byte(yamlSrc))
	if err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}
	return engine.Evaluate(context.Background(), doc)
}

// TestCalcBareSiblingVariable_ParallelEngine_CalcSibling is the round-4
// Candidate E repro (a bare named-variable sibling that is itself another
// calc call), re-run against the CLI's real default (parallel) execution
// path rather than the library's serial default.
func TestCalcBareSiblingVariable_ParallelEngine_CalcSibling(t *testing.T) {
	doc, err := mergeYAMLParallel(t, "sizing:\n  width: (( calc \"10 * 2\" ))\n  area: (( calc \"floor(width)\" ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("sizing.area")
	if err != nil {
		t.Fatalf("failed to read sizing.area: %v", err)
	}
	if got != int64(20) {
		t.Fatalf("expected 20, got %v (%T)", got, got)
	}
}

// TestCalcBareSiblingVariable_ParallelEngine_InfixSibling is the exact
// shape the examples-repairer's direct verification surfaced: a bare
// named-variable sibling computed by plain infix subtraction, not a
// nested calc call — expression-operators/arithmetic.yml's
// final_price/final_price_int pattern, reduced to a minimal repro.
func TestCalcBareSiblingVariable_ParallelEngine_InfixSibling(t *testing.T) {
	doc, err := mergeYAMLParallel(t, "calculations:\n  total_with_tax: 108\n  discount_amount: 15\n  final_price: (( calculations.total_with_tax - calculations.discount_amount ))\n  final_price_int: (( calc \"floor(final_price)\" ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("calculations.final_price_int")
	if err != nil {
		t.Fatalf("failed to read calculations.final_price_int: %v", err)
	}
	if got != int64(93) {
		t.Fatalf("expected 93, got %v (%T)", got, got)
	}
}

// TestCalcBareSiblingVariable_ParallelEngine_Repeated repeats the exact
// arithmetic.yml shape 25 times: the parallel scheduler's own wave
// ordering (and Go's randomized map iteration feeding it) means a fix
// that only wins a race would not necessarily reproduce as passing on
// every run.
func TestCalcBareSiblingVariable_ParallelEngine_Repeated(t *testing.T) {
	src := "calculations:\n  total_with_tax: 108\n  discount_amount: 15\n  final_price: (( calculations.total_with_tax - calculations.discount_amount ))\n  final_price_int: (( calc \"floor(final_price)\" ))\n"
	for i := 0; i < 25; i++ {
		doc, err := mergeYAMLParallel(t, src)
		if err != nil {
			t.Fatalf("iteration %d: unexpected error: %v", i, err)
		}
		got, err := doc.Get("calculations.final_price_int")
		if err != nil {
			t.Fatalf("iteration %d: failed to read calculations.final_price_int: %v", i, err)
		}
		if got != int64(93) {
			t.Fatalf("iteration %d: expected 93, got %v (%T)", i, got, got)
		}
	}
}

// TestCalcFullyQualifiedSiblingPath_ParallelEngine pins the already-
// working full-qualified-path workaround stays working under the
// parallel engine too.
func TestCalcFullyQualifiedSiblingPath_ParallelEngine(t *testing.T) {
	doc, err := mergeYAMLParallel(t, "sizing:\n  width: (( calc \"10 * 2\" ))\n  area: (( calc \"floor(sizing.width)\" ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("sizing.area")
	if err != nil {
		t.Fatalf("failed to read sizing.area: %v", err)
	}
	if got != int64(20) {
		t.Fatalf("expected 20, got %v (%T)", got, got)
	}
}
