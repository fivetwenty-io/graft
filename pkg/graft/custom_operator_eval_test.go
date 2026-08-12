package graft

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// mockOperatorReturnValue is the fixed value TestCustomOperator_TestHelper_TestWithMockOperator's
// mock operator returns; named to satisfy golangci-lint's goconst check on
// its multiple occurrences in that test.
const mockOperatorReturnValue = "mocked-value"

// upperOperator is a minimal custom operator used to prove that engine-local
// operator registration is actually consulted during evaluation (P0-1). It
// upper-cases its single string-literal argument. It intentionally does not
// support references or nested operator calls as an argument — the tests
// below only ever call it with a literal string, and a production custom
// operator would use operators.ResolveOperatorArgument for that; this test
// double keeps the graft package (no import of operators, which imports
// graft, would be a cycle) self-contained.
//
// callCount is an atomic.Int64, not a plain int: TestCustomOperator_ParallelEvaluation
// shares one *upperOperator across a wave of ops the scheduler may run
// concurrently (pool.SubmitBlocking, not the order-sensitive serial path),
// so an unsynchronized increment there would be a real data race the
// moment the worker pool's minWorkers default stops being 1.
type upperOperator struct {
	callCount atomic.Int64
}

func (o *upperOperator) Setup() error {
	return nil
}

func (o *upperOperator) Phase() OperatorPhase {
	return EvalPhase
}

func (o *upperOperator) Dependencies(_ *Evaluator, _ []*Expr, _ []*tree.Cursor, auto []*tree.Cursor) []*tree.Cursor {
	return auto
}

func (o *upperOperator) Run(_ *Evaluator, args []*Expr) (*Response, error) {
	o.callCount.Add(1)
	if len(args) != 1 {
		return nil, fmt.Errorf("upper operator requires exactly one argument, got %d", len(args))
	}
	if args[0].Type != Literal {
		return nil, fmt.Errorf("upper operator requires a literal string argument")
	}
	s, ok := args[0].Literal.(string)
	if !ok {
		return nil, fmt.Errorf("upper operator requires a string argument, got %T", args[0].Literal)
	}
	return &Response{Type: Replace, Value: strings.ToUpper(s)}, nil
}

// mustParseYAMLDoc parses YAML into a Document, failing the test on error.
// Distinct from the package's other yaml test helpers to avoid colliding
// with names already declared elsewhere in this package's test files.
func mustParseYAMLDoc(t *testing.T, engine Engine, yamlSrc string) Document {
	t.Helper()
	doc, err := engine.ParseYAML([]byte(yamlSrc))
	if err != nil {
		t.Fatalf("ParseYAML failed: %v", err)
	}
	if doc == nil {
		t.Fatalf("ParseYAML returned a nil document for non-empty input")
	}
	return doc
}

// TestCustomOperator_WithCustomOperator_ResolvesDuringEvaluation is the red
// test from the P0-1 test plan: before the fix, OperatorFor always resolved
// against the process-global DefaultRegistry, so evaluation never saw the
// engine-local registration WithCustomOperator wrote into the engine's
// registry clone, and (( upper "x" )) echoed its own source text back
// instead of evaluating to "X".
func TestCustomOperator_WithCustomOperator_ResolvesDuringEvaluation(t *testing.T) {
	engine, err := NewEngine(WithCustomOperator("upper", &upperOperator{}))
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	doc := mustParseYAMLDoc(t, engine, "greeting: (( upper \"hello\" ))\n")

	result, err := engine.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	got, err := result.GetString("greeting")
	if err != nil {
		t.Fatalf("GetString(greeting) failed: %v", err)
	}
	if got != "HELLO" {
		t.Fatalf("greeting = %q, want %q (custom operator was not consulted during evaluation)", got, "HELLO")
	}
}

// TestCustomOperator_RegisterOperator_ResolvesDuringEvaluation proves the
// same fix works when the operator is registered on an already-constructed
// engine via RegisterOperator, not only through the WithCustomOperator
// engine option.
func TestCustomOperator_RegisterOperator_ResolvesDuringEvaluation(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	if regErr := engine.RegisterOperator("upper", &upperOperator{}); regErr != nil {
		t.Fatalf("RegisterOperator failed: %v", regErr)
	}

	doc := mustParseYAMLDoc(t, engine, "greeting: (( upper \"world\" ))\n")

	result, err := engine.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	got, err := result.GetString("greeting")
	if err != nil {
		t.Fatalf("GetString(greeting) failed: %v", err)
	}
	if got != "WORLD" {
		t.Fatalf("greeting = %q, want %q", got, "WORLD")
	}
}

// TestCustomOperator_TestHelper_TestWithMockOperator proves the
// TestHelper.TestWithMockOperator path (register/evaluate/unregister)
// resolves through the same fix.
func TestCustomOperator_TestHelper_TestWithMockOperator(t *testing.T) {
	h := NewTestHelper(t)

	mock := &MockOperator{
		Name:        "mockop",
		MockPhase:   EvalPhase,
		ReturnValue: mockOperatorReturnValue,
	}

	h.TestWithMockOperator("mockop", mock, func() {
		doc := mustParseYAMLDoc(t, h.engine, "value: (( mockop ))\n")

		result, err := h.engine.Evaluate(context.Background(), doc)
		if err != nil {
			t.Fatalf("Evaluate failed: %v", err)
		}

		got, err := result.GetString("value")
		if err != nil {
			t.Fatalf("GetString(value) failed: %v", err)
		}
		if got != mockOperatorReturnValue {
			t.Fatalf("value = %q, want %q", got, mockOperatorReturnValue)
		}
	})

	// After TestWithMockOperator returns, the operator has been
	// unregistered from h.engine; the same expression should now fall
	// through to the unregistered-operator passthrough, not the mock.
	doc := mustParseYAMLDoc(t, h.engine, "value: (( mockop ))\n")
	result, err := h.engine.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatalf("Evaluate after unregister failed: %v", err)
	}
	got, err := result.GetString("value")
	if err != nil {
		t.Fatalf("GetString(value) after unregister failed: %v", err)
	}
	if got == mockOperatorReturnValue {
		t.Fatalf("value = %q, want passthrough (mock operator should have been unregistered)", got)
	}
}

// TestCustomOperator_TwoEngineIsolation proves that a custom operator
// registered on one engine is invisible on a second, independently
// constructed engine: the registry clone each engine holds must not leak
// into DefaultRegistry or into any other engine's clone.
func TestCustomOperator_TwoEngineIsolation(t *testing.T) {
	engineA, err := NewEngine(WithCustomOperator("upper", &upperOperator{}))
	if err != nil {
		t.Fatalf("NewEngine (A) failed: %v", err)
	}
	engineB, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine (B) failed: %v", err)
	}

	docA := mustParseYAMLDoc(t, engineA, "greeting: (( upper \"a\" ))\n")
	resultA, err := engineA.Evaluate(context.Background(), docA)
	if err != nil {
		t.Fatalf("Evaluate (A) failed: %v", err)
	}
	gotA, err := resultA.GetString("greeting")
	if err != nil {
		t.Fatalf("GetString (A) failed: %v", err)
	}
	if gotA != "A" {
		t.Fatalf("engine A: greeting = %q, want %q", gotA, "A")
	}

	// engineB never registered "upper": the call must fall through to the
	// unregistered-operator passthrough (NullOperator), not resolve to the
	// value engineA's registration would have produced.
	docB := mustParseYAMLDoc(t, engineB, "greeting: (( upper \"a\" ))\n")
	resultB, err := engineB.Evaluate(context.Background(), docB)
	if err != nil {
		t.Fatalf("Evaluate (B) failed: %v", err)
	}
	gotB, err := resultB.GetString("greeting")
	if err != nil {
		t.Fatalf("GetString (B) failed: %v", err)
	}
	if gotB == "A" {
		t.Fatalf("engine B: greeting = %q, want passthrough — engine A's custom operator leaked into engine B", gotB)
	}
	if !strings.Contains(gotB, "upper") {
		t.Fatalf("engine B: greeting = %q, want unregistered-operator passthrough containing %q", gotB, "upper")
	}
}

// TestCustomOperator_NestedInsideConcat proves the operators-package nested
// resolution path (operators/types.go's OperatorForEngine wrapper, used by
// operator_helpers.go's evaluateNestedOperator and by op_concat.go's
// Dependencies) also honors the engine-local registry: without it, a custom
// operator nested inside another operator's call — (( concat "a" (upper
// "b") )) — degrades to NullOperator even though the same operator resolves
// correctly at top level.
func TestCustomOperator_NestedInsideConcat(t *testing.T) {
	engine, err := NewEngine(WithCustomOperator("upper", &upperOperator{}))
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	doc := mustParseYAMLDoc(t, engine, "combined: (( concat \"a\" (upper \"b\") ))\n")

	result, err := engine.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	got, err := result.GetString("combined")
	if err != nil {
		t.Fatalf("GetString(combined) failed: %v", err)
	}
	if got != "aB" {
		t.Fatalf("combined = %q, want %q (nested custom operator not resolved via engine registry)", got, "aB")
	}
}

// depAwareOperator is a custom operator that takes a single reference
// argument, resolves it, and returns its string value. Its Dependencies()
// method reports that reference as a real dependency — unlike upperOperator
// (whose only argument is a literal, so it never exercises the four
// engine-aware Dependencies() lookups added alongside evaluateNestedOperator
// in op_concat.go, op_cartesian_product.go, op_inject.go, op_ips.go: F2).
type depAwareOperator struct{}

func (depAwareOperator) Setup() error {
	return nil
}

func (depAwareOperator) Phase() OperatorPhase {
	return EvalPhase
}

func (depAwareOperator) Dependencies(_ *Evaluator, args []*Expr, _, auto []*tree.Cursor) []*tree.Cursor {
	deps := auto
	for _, a := range args {
		if a.Type == Reference && a.Reference != nil {
			deps = append(deps, a.Reference)
		}
	}
	return deps
}

func (depAwareOperator) Run(ev *Evaluator, args []*Expr) (*Response, error) {
	if len(args) != 1 || args[0].Type != Reference || args[0].Reference == nil {
		return nil, fmt.Errorf("depaware operator requires exactly one reference argument")
	}
	val, err := args[0].Reference.Resolve(ev.Tree)
	if err != nil {
		return nil, fmt.Errorf("depaware: %w", err)
	}
	s, ok := val.(string)
	if !ok {
		return nil, fmt.Errorf("depaware operator requires a string value at %s, got %T", args[0].Reference, val)
	}
	return &Response{Type: Replace, Value: s}, nil
}

// TestCustomOperator_NestedDependencyOrdering is the F2 regression test:
// a custom operator nested inside (( concat ... )) whose Dependencies()
// reports a real reference must have that reference resolved before the
// custom operator runs, including under parallel evaluation.
//
// document_key is named "a_consumer" (alphabetically before "zzz_producer")
// specifically so that a missing dependency edge is observable: with no
// edge between the two ops, evaluator.go's kahnSort/findFreeNodes treats
// both as free in the same wave and breaks the tie alphabetically —
// "a_consumer" would run first, reading zzz_producer's still-unevaluated
// source text "(( grab source ))" instead of waiting for it to resolve to
// "hello". Naming the producer key alphabetically after the consumer is
// deliberate: it is what makes a missing edge produce a wrong *value*
// (not just a coincidentally-correct accidental ordering) that this test
// can assert against.
//
// Reverting the four Dependencies() call sites in op_concat.go,
// op_cartesian_product.go, op_inject.go, op_ips.go back to bare
// OperatorFor(arg.Op()) makes depAwareOperator (a *custom* operator, so it
// is never present in DefaultRegistry) resolve to NullOperator inside
// ConcatOperator.Dependencies, whose Dependencies() always returns nil —
// silently dropping the edge and reproducing the "a_consumer" = "got:((
// grab source ))" wrong-value failure this test asserts against.
func TestCustomOperator_NestedDependencyOrdering(t *testing.T) {
	engine, err := NewEngine(WithCustomOperator("depaware", depAwareOperator{}), WithParallel(true))
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	de, ok := engine.(*DefaultEngine)
	if !ok {
		t.Fatalf("NewEngine returned %T, want *DefaultEngine", engine)
	}
	if de.GetWorkerPool() == nil {
		t.Fatal("expected WithParallel(true) to construct a worker pool; got nil (test would silently exercise the serial path)")
	}

	doc := mustParseYAMLDoc(t, engine, ""+
		"a_consumer: (( concat \"got:\" (depaware zzz_producer) ))\n"+
		"zzz_producer: (( grab source ))\n"+
		"source: hello\n")

	result, err := engine.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	got, err := result.GetString("a_consumer")
	if err != nil {
		t.Fatalf("GetString(a_consumer) failed: %v", err)
	}
	if got != "got:hello" {
		t.Fatalf("a_consumer = %q, want %q (nested custom operator's Dependencies() edge was not honored — it ran before its dependency was resolved)", got, "got:hello")
	}
}

// TestCustomOperator_ParallelEvaluation proves the fix also covers the
// parallel evaluation path (evaluator_parallel.go's RunPhaseParallel),
// which the CLI uses by default. RunPhaseParallel shares DataFlow and RunOp
// with the serial path, so this is primarily a regression guard against a
// future change that bypasses those shared entry points, but it is run
// explicitly because a fix that only works in serial library tests would be
// dead code on the real CLI path.
func TestCustomOperator_ParallelEvaluation(t *testing.T) {
	op := &upperOperator{}
	engine, err := NewEngine(WithCustomOperator("upper", op), WithParallel(true))
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	de, ok := engine.(*DefaultEngine)
	if !ok {
		t.Fatalf("NewEngine returned %T, want *DefaultEngine", engine)
	}
	if de.GetWorkerPool() == nil {
		t.Fatal("expected WithParallel(true) to construct a worker pool; got nil (test would silently exercise the serial path)")
	}

	// Five independent keys, each its own (( upper ... )) call with no
	// cross-references, so the parallel scheduler is free to place all
	// five in one concurrent wave — generated rather than five near-
	// identical literal lines/map entries.
	const opCount = 5
	var src strings.Builder
	want := make(map[string]string, opCount)
	for i := 0; i < opCount; i++ {
		key := fmt.Sprintf("op%d", i)
		val := fmt.Sprintf("val%d", i)
		fmt.Fprintf(&src, "%s: (( upper %q ))\n", key, val)
		want[key] = strings.ToUpper(val)
	}

	doc := mustParseYAMLDoc(t, engine, src.String())

	result, err := engine.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	for key, wantVal := range want {
		got, err := result.GetString(key)
		if err != nil {
			t.Fatalf("GetString(%s) failed: %v", key, err)
		}
		if got != wantVal {
			t.Fatalf("%s = %q, want %q (parallel evaluation path did not consult engine-local registry)", key, got, wantVal)
		}
	}

	if got := op.callCount.Load(); got != opCount {
		t.Fatalf("upper operator ran %d times, want %d (some ops in the parallel wave were skipped or double-run)", got, opCount)
	}
}

// TestCustomOperator_UnregisteredOperatorPassthroughUnchanged is a
// regression guard: an unregistered operator (( foo )) must still echo its
// own source text back unchanged, exactly as it did before this fix.
// Genesis's multi-pass templating depends on this passthrough to leave
// not-yet-resolvable markers in place across passes.
func TestCustomOperator_UnregisteredOperatorPassthroughUnchanged(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	doc := mustParseYAMLDoc(t, engine, "value: (( foo \"bar\" ))\n")

	result, err := engine.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	got, err := result.GetString("value")
	if err != nil {
		t.Fatalf("GetString(value) failed: %v", err)
	}
	if got != `(( foo "bar" ))` {
		t.Fatalf("value = %q, want unchanged source text %q", got, `(( foo "bar" ))`)
	}
}

// TestCustomOperator_UnregisteredOperatorLogicalOrFallbackUnchanged is a
// regression guard for the one prior behavior change already shipped ahead
// of P0-1: (( foo || "default" )) with an unregistered foo must still
// resolve to the fallback "default", not to a NullOperator passthrough of
// the whole expression.
func TestCustomOperator_UnregisteredOperatorLogicalOrFallbackUnchanged(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	doc := mustParseYAMLDoc(t, engine, "value: (( foo || \"default\" ))\n")

	result, err := engine.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	got, err := result.GetString("value")
	if err != nil {
		t.Fatalf("GetString(value) failed: %v", err)
	}
	if got != "default" {
		t.Fatalf("value = %q, want %q", got, "default")
	}
}
