package graft

import (
	"errors"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// TestMockOperator_ImplementsOperatorInterface is a compile-time check that
// MockOperator implements the Operator interface.
func TestMockOperator_ImplementsOperatorInterface(t *testing.T) {
	var _ Operator = &MockOperator{}
}

func TestMockOperator_Setup(t *testing.T) {
	mock := &MockOperator{}
	if err := mock.Setup(); err != nil {
		t.Fatalf("Setup() returned unexpected error: %v", err)
	}
}

func TestMockOperator_Run_ReturnsValue(t *testing.T) {
	mock := &MockOperator{
		ReturnValue: "hello",
	}
	resp, err := mock.Run(nil, nil)
	if err != nil {
		t.Fatalf("Run() returned unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("Run() returned nil response")
	}
	if resp.Type != Replace {
		t.Fatalf("Run() response type: expected Replace, got %v", resp.Type)
	}
	if resp.Value != "hello" {
		t.Fatalf("Run() response value: expected %q, got %v", "hello", resp.Value)
	}
}

func TestMockOperator_Run_ReturnsError(t *testing.T) {
	expectedErr := errors.New("mock error")
	mock := &MockOperator{
		ReturnError: expectedErr,
	}
	resp, err := mock.Run(nil, nil)
	if err == nil {
		t.Fatal("Run() expected error but got nil")
	}
	if !errors.Is(err, expectedErr) {
		t.Fatalf("Run() error: expected %v, got %v", expectedErr, err)
	}
	if resp != nil {
		t.Fatalf("Run() expected nil response on error, got %v", resp)
	}
}

func TestMockOperator_Run_TracksCallCount(t *testing.T) {
	mock := &MockOperator{}
	for i := 0; i < 3; i++ {
		if _, err := mock.Run(nil, nil); err != nil {
			t.Fatalf("Run() iteration %d returned unexpected error: %v", i, err)
		}
	}
	if mock.CallCount != 3 {
		t.Fatalf("CallCount: expected 3, got %d", mock.CallCount)
	}
}

func TestMockOperator_Run_TracksLastArgs(t *testing.T) {
	mock := &MockOperator{}
	args := []*Expr{
		{Type: Literal, Literal: "arg1"},
		{Type: Literal, Literal: "arg2"},
	}
	if _, err := mock.Run(nil, args); err != nil {
		t.Fatalf("Run() returned unexpected error: %v", err)
	}
	if len(mock.LastArgs) != 2 {
		t.Fatalf("LastArgs length: expected 2, got %d", len(mock.LastArgs))
	}
	if mock.LastArgs[0] != args[0] {
		t.Fatalf("LastArgs[0]: expected %v, got %v", args[0], mock.LastArgs[0])
	}
	if mock.LastArgs[1] != args[1] {
		t.Fatalf("LastArgs[1]: expected %v, got %v", args[1], mock.LastArgs[1])
	}
}

func TestMockOperator_Phase(t *testing.T) {
	mock := &MockOperator{MockPhase: MergePhase}
	if got := mock.Phase(); got != MergePhase {
		t.Fatalf("Phase(): expected MergePhase, got %v", got)
	}
}

func TestMockOperator_Phase_Default(t *testing.T) {
	mock := &MockOperator{}
	// Zero value of OperatorPhase is MergePhase (iota = 0)
	if got := mock.Phase(); got != MergePhase {
		t.Fatalf("Phase() default: expected MergePhase (zero value), got %v", got)
	}
}

func TestMockOperator_Dependencies(t *testing.T) {
	mock := &MockOperator{}
	autoCursors := []*tree.Cursor{
		{},
		{},
	}
	result := mock.Dependencies(nil, nil, nil, autoCursors)
	if len(result) != len(autoCursors) {
		t.Fatalf("Dependencies() length: expected %d, got %d", len(autoCursors), len(result))
	}
	for i, c := range autoCursors {
		if result[i] != c {
			t.Fatalf("Dependencies()[%d]: expected %v, got %v", i, c, result[i])
		}
	}
}

func TestTestHelper_TestWithMockOperator(t *testing.T) {
	h := NewTestHelper(t)
	mock := &MockOperator{
		Name:        "test-mock",
		ReturnValue: "test-value",
	}

	var registeredDuring bool
	h.TestWithMockOperator("test-mock", mock, func() {
		op, ok := h.engine.GetOperator("test-mock")
		if ok && op == mock {
			registeredDuring = true
		}
	})

	if !registeredDuring {
		t.Fatal("MockOperator was not registered during testFunc execution")
	}

	// After testFunc, operator should be unregistered
	_, ok := h.engine.GetOperator("test-mock")
	if ok {
		t.Fatal("MockOperator should be unregistered after testFunc completes")
	}
}
