package graft

// The default array merge strategy (InlineArrays, the zero value) must merge
// scalar arrays positionally the way spruce does: overlay elements replace
// base elements index by index, base elements beyond the overlay's length
// are kept, and overlay elements beyond the base's length are appended.
// Verified against spruce v1.35.16:
//
//	f: [a, b, c]  +  f: [x9]          ->  f: [x9, b, c]
//	f: [a]        +  f: [x1, y1, z1]  ->  f: [x1, y1, z1]
//
// The simple merge path previously treated InlineArrays as ReplaceArrays,
// so a shorter overlay silently truncated the base array.

import (
	"context"
	"testing"
)

func mergeTwo(t *testing.T, base, overlay string, strategy *ArrayMergeStrategy) Document {
	t.Helper()
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("engine: %v", err)
	}
	baseDoc, err := engine.ParseYAML([]byte(base))
	if err != nil {
		t.Fatalf("parse base: %v", err)
	}
	overlayDoc, err := engine.ParseYAML([]byte(overlay))
	if err != nil {
		t.Fatalf("parse overlay: %v", err)
	}
	builder := engine.Merge(context.Background(), baseDoc, overlayDoc)
	if strategy != nil {
		builder = builder.WithArrayMergeStrategy(*strategy)
	}
	result, err := builder.Execute()
	if err != nil {
		t.Fatalf("merge: %v", err)
	}
	return result
}

func assertSlice(t *testing.T, doc Document, path string, want []interface{}) {
	t.Helper()
	got, err := doc.GetSlice(path)
	if err != nil {
		t.Fatalf("GetSlice(%s): %v", path, err)
	}
	if len(got) != len(want) {
		t.Fatalf("%s: expected %v, got %v", path, want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s[%d]: expected %v, got %v (full: %v)", path, i, want[i], got[i], got)
		}
	}
}

func TestDefaultInlineMergeKeepsBaseTail(t *testing.T) {
	result := mergeTwo(t, "f: [a, b, c]\n", "f: [x9]\n", nil)
	assertSlice(t, result, "f", []interface{}{"x9", "b", "c"})
}

func TestDefaultInlineMergeAppendsOverlayTail(t *testing.T) {
	result := mergeTwo(t, "f: [a]\n", "f: [x1, y1, z1]\n", nil)
	assertSlice(t, result, "f", []interface{}{"x1", "y1", "z1"})
}

func TestDefaultInlineMergeNestedScalarArray(t *testing.T) {
	result := mergeTwo(t, "meta:\n  azs: [z1, z2, z3]\n", "meta:\n  azs: [z9]\n", nil)
	assertSlice(t, result, "meta.azs", []interface{}{"z9", "z2", "z3"})
}

func TestDefaultInlineMergeElementWise(t *testing.T) {
	result := mergeTwo(t, "n: [1, 2, 3]\n", "n: [9, 8]\n", nil)
	got, err := result.GetSlice("n")
	if err != nil {
		t.Fatalf("GetSlice(n): %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 elements, got %v", got)
	}
}

func TestExplicitReplaceStillTruncates(t *testing.T) {
	strategy := ReplaceArrays
	result := mergeTwo(t, "f: [a, b, c]\n", "f: [x9]\n", &strategy)
	assertSlice(t, result, "f", []interface{}{"x9"})
}

func TestFallbackAppendUnaffected(t *testing.T) {
	strategy := AppendArrays
	result := mergeTwo(t, "f: [a, b, c]\n", "f: [x9]\n", &strategy)
	assertSlice(t, result, "f", []interface{}{"a", "b", "c", "x9"})
}
