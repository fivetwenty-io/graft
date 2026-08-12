package graft

import (
	"errors"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// TestOperatorFunc_ImplementsOperatorInterface is a compile-time check that
// *OperatorFunc implements the Operator interface.
func TestOperatorFunc_ImplementsOperatorInterface(t *testing.T) {
	var _ Operator = &OperatorFunc{}
}

func TestOperatorFunc_Setup_AlwaysNil(t *testing.T) {
	f := &OperatorFunc{}
	if err := f.Setup(); err != nil {
		t.Fatalf("Setup() returned unexpected error: %v", err)
	}
}

func TestOperatorFunc_Run_InvokesFn(t *testing.T) {
	called := false
	var gotArgs []*Expr
	args := []*Expr{{Type: Literal, Literal: "x"}}

	f := &OperatorFunc{
		Fn: func(ev *Evaluator, a []*Expr) (*Response, error) {
			called = true
			gotArgs = a
			return &Response{Type: Replace, Value: "y"}, nil
		},
	}

	resp, err := f.Run(nil, args)
	if err != nil {
		t.Fatalf("Run() returned unexpected error: %v", err)
	}
	if !called {
		t.Fatal("Run() did not invoke Fn")
	}
	if len(gotArgs) != 1 || gotArgs[0] != args[0] {
		t.Fatalf("Run() passed wrong args to Fn: %v", gotArgs)
	}
	if resp == nil || resp.Type != Replace || resp.Value != "y" {
		t.Fatalf("Run() returned unexpected response: %+v", resp)
	}
}

func TestOperatorFunc_Run_PropagatesError(t *testing.T) {
	wantErr := errors.New("boom")
	f := &OperatorFunc{
		Fn: func(ev *Evaluator, a []*Expr) (*Response, error) {
			return nil, wantErr
		},
	}

	resp, err := f.Run(nil, nil)
	if !errors.Is(err, wantErr) {
		t.Fatalf("Run() error: expected %v, got %v", wantErr, err)
	}
	if resp != nil {
		t.Fatalf("Run() expected nil response on error, got %v", resp)
	}
}

func TestOperatorFunc_Run_NilFnReturnsError(t *testing.T) {
	f := &OperatorFunc{}

	resp, err := f.Run(nil, nil)
	if err == nil {
		t.Fatal("Run() with nil Fn expected an error, got nil")
	}
	if resp != nil {
		t.Fatalf("Run() with nil Fn expected nil response, got %v", resp)
	}
}

func TestOperatorFunc_Phase_DefaultsToZeroValue(t *testing.T) {
	f := &OperatorFunc{}
	if got := f.Phase(); got != MergePhase {
		t.Fatalf("Phase() default: expected MergePhase (zero value), got %v", got)
	}
}

func TestOperatorFunc_Phase_ReturnsConfiguredPhase(t *testing.T) {
	f := &OperatorFunc{OpPhase: EvalPhase}
	if got := f.Phase(); got != EvalPhase {
		t.Fatalf("Phase(): expected EvalPhase, got %v", got)
	}
}

func TestOperatorFunc_Dependencies_DefaultsToAuto(t *testing.T) {
	f := &OperatorFunc{}
	auto := []*tree.Cursor{{}, {}}
	got := f.Dependencies(nil, nil, nil, auto)
	if len(got) != len(auto) {
		t.Fatalf("Dependencies() length: expected %d, got %d", len(auto), len(got))
	}
	for i, c := range auto {
		if got[i] != c {
			t.Fatalf("Dependencies()[%d]: expected %v, got %v", i, c, got[i])
		}
	}
}

func TestOperatorFunc_Dependencies_UsesDependsOnWhenSet(t *testing.T) {
	extra := &tree.Cursor{}
	f := &OperatorFunc{
		DependsOn: func(ev *Evaluator, args []*Expr, locs, auto []*tree.Cursor) []*tree.Cursor {
			return append(append([]*tree.Cursor{}, auto...), extra)
		},
	}
	auto := []*tree.Cursor{{}}
	got := f.Dependencies(nil, nil, nil, auto)
	if len(got) != 2 {
		t.Fatalf("Dependencies() length: expected 2, got %d", len(got))
	}
	if got[1] != extra {
		t.Fatalf("Dependencies()[1]: expected the DependsOn-added cursor, got %v", got[1])
	}
}
