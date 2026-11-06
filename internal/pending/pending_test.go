package pending

import (
	"errors"
	"fmt"
	"testing"

	"github.com/fivetwenty-io/graft/internal/literal"
)

func TestRecordKeepsPathsApart(t *testing.T) {
	r := NewRecord()
	r.Add([]string{"a.b"}, "a.b")
	if _, ok := r.At([]string{"a", "b"}); ok {
		t.Fatal("the path a, b matched the key a.b")
	}
	if where, ok := r.At([]string{"a.b"}); !ok || where != "a.b" {
		t.Fatalf("At(a.b) = %q, %v", where, ok)
	}
	r.Done([]string{"a.b"})
	if _, ok := r.At([]string{"a.b"}); ok {
		t.Fatal("a.b is still pending after Done")
	}
}

func TestNilRecordHoldsNothing(t *testing.T) {
	var r *Record
	r.Add([]string{"a"}, "a")
	r.Done([]string{"a"})
	if _, ok := r.At([]string{"a"}); ok {
		t.Fatal("a nil record reported a pending path")
	}
}

func TestStopsFallback(t *testing.T) {
	nerr := fmt.Errorf("wrapped: %w", &NotEvaluatedError{Grab: "g", Target: "t"})
	if !errors.Is(nerr, ErrNotEvaluated) || !StopsFallback(nerr) {
		t.Fatal("a wrapped NotEvaluatedError does not stop a fallback")
	}
	if !StopsFallback(literal.Refuse("grab", "a path")) {
		t.Fatal("a refused literal does not stop a fallback")
	}
	if StopsFallback(errors.New("not found")) {
		t.Fatal("an ordinary error stops a fallback")
	}
}
