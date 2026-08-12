package graft

import "testing"

func TestNewTestEvaluator_ReturnsNonNilEvaluator(t *testing.T) {
	ev := NewTestEvaluator(t, `foo: bar`)
	if ev == nil {
		t.Fatal("NewTestEvaluator() returned nil")
	}
}

func TestNewTestEvaluator_TreeReflectsFixture(t *testing.T) {
	ev := NewTestEvaluator(t, "database:\n  host: localhost\n  port: 5432\n")

	db, ok := ev.Tree["database"].(map[string]interface{})
	if !ok {
		t.Fatalf("Tree[\"database\"] not a map: %T", ev.Tree["database"])
	}
	if db["host"] != "localhost" {
		t.Fatalf("Tree database.host: expected localhost, got %v", db["host"])
	}
	if db["port"] != 5432 {
		t.Fatalf("Tree database.port: expected 5432, got %v (%T)", db["port"], db["port"])
	}
}

// TestNewTestEvaluator_UsableByOperatorRun proves the returned *Evaluator is
// a real evaluator usable to unit-test an Operator's Run method directly,
// without going through Engine.Evaluate — the replacement pattern for the
// fabricated NewMockEvalContext docs promised.
func TestNewTestEvaluator_UsableByOperatorRun(t *testing.T) {
	ev := NewTestEvaluator(t, `name: world`)

	greet := &OperatorFunc{
		OpPhase: EvalPhase,
		Fn: func(ev *Evaluator, args []*Expr) (*Response, error) {
			resolved, err := EvaluateOperatorArgs(ev, args)
			if err != nil {
				return nil, err
			}
			return &Response{Type: Replace, Value: "hello, " + resolved[0].(string)}, nil
		},
	}

	args := []*Expr{{Type: Literal, Literal: "there"}}
	resp, err := greet.Run(ev, args)
	if err != nil {
		t.Fatalf("Run() returned unexpected error: %v", err)
	}
	if resp.Value != "hello, there" {
		t.Fatalf("Run() value: expected %q, got %v", "hello, there", resp.Value)
	}
}

// TestNewTestEvaluator_EngineIsSet proves the *Evaluator returned is wired
// to a real engine, so operator resolution through OperatorForEngine and
// evaluator helpers that read ev.engine (e.g. nested operator dispatch)
// behave as they would inside a full Engine.Evaluate call.
func TestNewTestEvaluator_EngineIsSet(t *testing.T) {
	ev := NewTestEvaluator(t, `foo: bar`)

	custom := &MockOperator{Name: "probe", ReturnValue: "ok"}
	if got := OperatorForEngine(ev.engine, "probe"); got == custom {
		t.Fatal("OperatorForEngine before registration: unexpectedly resolved to custom")
	}

	de, ok := ev.engine.(*DefaultEngine)
	if !ok {
		t.Fatalf("ev.engine is not *DefaultEngine: %T", ev.engine)
	}
	if err := de.RegisterOperator("probe", custom); err != nil {
		t.Fatalf("RegisterOperator() returned unexpected error: %v", err)
	}
	if got := OperatorForEngine(ev.engine, "probe"); got != custom {
		t.Fatalf("OperatorForEngine after registration: expected %v, got %v", custom, got)
	}
}
