package pending

import (
	"errors"
	"fmt"
	"testing"

	"github.com/fivetwenty-io/graft/internal/literal"
)

func TestRecordKeepsPathsApart(t *testing.T) {
	r := NewRecord()
	r.Add([]string{"a.b"}, "a.b", nil)
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
	r.Add([]string{"a"}, "a", nil)
	r.Done([]string{"a"})
	if _, ok := r.At([]string{"a"}); ok {
		t.Fatal("a nil record reported a pending path")
	}
	if _, ok := r.In(map[string]interface{}{"a": 1}, "a"); ok {
		t.Fatal("a nil record reported a pending slot")
	}
	if _, ok := r.Within(map[string]interface{}{"a": 1}); ok {
		t.Fatal("a nil record reported a pending slot within a value")
	}
}

func TestRecordKnowsSlotsByIdentity(t *testing.T) {
	list := []interface{}{"x", "(( op ))"}
	meta := map[string]interface{}{"l": list, "v": "(( op ))"}
	r := NewRecord()
	r.Add([]string{"meta", "l", "1"}, "meta.l.1", list)
	r.Add([]string{"meta", "v"}, "meta.v", meta)

	if where, ok := r.In(list, "1"); !ok || where != "meta.l.1" {
		t.Fatalf("In(list, 1) = %q, %v", where, ok)
	}
	alias := list[0:2]
	if where, ok := r.In(alias, "1"); !ok || where != "meta.l.1" {
		t.Fatalf("a slice of the same list misses the slot, got %q, %v", where, ok)
	}
	if _, ok := r.In(append([]interface{}(nil), list...), "1"); ok {
		t.Fatal("a copy of the list shares its slots")
	}
	if _, ok := r.In(list, "0"); ok {
		t.Fatal("entry 0 holds no operator")
	}
	if _, ok := r.In(map[string]interface{}{"v": 1}, "v"); ok {
		t.Fatal("another map with the same key shares the slot")
	}

	root := map[string]interface{}{"meta": meta, "self": nil}
	root["self"] = root
	if where, ok := r.Within(root); !ok || where != "meta.l.1" {
		t.Fatalf("Within(root) = %q, %v, want meta.l.1 first by key order", where, ok)
	}
	r.Done([]string{"meta", "l", "1"})
	if where, ok := r.Within(root); !ok || where != "meta.v" {
		t.Fatalf("Within(root) after meta.l.1 ran = %q, %v", where, ok)
	}
	r.Done([]string{"meta", "v"})
	if _, ok := r.Within(root); ok {
		t.Fatal("Within reports a slot after every operator ran")
	}
	if _, ok := r.In(meta, "v"); ok {
		t.Fatal("meta.v is still pending after Done")
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
