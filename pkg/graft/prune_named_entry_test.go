package graft

import (
	"context"
	"testing"
)

// These tests cover --prune path resolution parity with spruce, verified
// against the actual spruce binary (github.com/geofffranks/spruce):
//
//   - A named final path segment on an array (e.g. "items.beta") is a
//     silent no-op in spruce: spruce.Evaluator.Prune only removes array
//     entries addressed by numeric index at the final segment; named
//     lookup ("name"/"key"/"id") is only applied by the underlying
//     tree.Cursor when navigating *through* an array to reach a deeper
//     key (e.g. "items.beta.port"), never as the final delete target.
//   - A prune path that fails to resolve (missing key, out-of-range
//     index, wrong type) is silently ignored in spruce (Resolve returns
//     an error and the caller does `continue`), matching exit 0 with no
//     stderr output.
//
// removeKey previously modeled the "named final segment on an array"
// case as an *error* (invalid array index), which applyPruning then
// swallowed unconditionally via a bare `continue`. That produced the
// right external result by accident while conflating "nothing to prune
// here" with "something is internally wrong" and hiding genuine errors
// behind the same catch-all. These tests pin the corrected semantics:
// every "path doesn't resolve" case leaves the document untouched, which
// is why removeKey has no error to report at all.

func TestRemoveKeyNamedFinalArraySegmentIsNoOp(t *testing.T) {
	m := &mergeBuilderImpl{}
	data := map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{"name": "alpha", "port": 1},
			map[string]interface{}{"name": "beta", "port": 2},
		},
	}

	m.removeKey(data, "items.beta")

	items := data["items"].([]interface{})
	if len(items) != 2 {
		t.Fatalf("items mutated: got %d entries, want 2 (spruce leaves the array untouched for a named final segment)", len(items))
	}
}

func TestRemoveKeyScalarMidPathIsNoOp(t *testing.T) {
	m := &mergeBuilderImpl{}
	data := map[string]interface{}{
		"a": 1,
	}

	m.removeKey(data, "a.b")
	if data["a"] != 1 {
		t.Fatalf("data mutated: a = %v, want unchanged 1", data["a"])
	}
}

func TestRemoveKeyFinalSegmentOnScalarIsNoOp(t *testing.T) {
	m := &mergeBuilderImpl{}
	data := map[string]interface{}{
		"outer": map[string]interface{}{
			"inner": 42,
		},
	}

	m.removeKey(data, "outer.inner.deeper")
	inner := data["outer"].(map[string]interface{})["inner"]
	if inner != 42 {
		t.Fatalf("data mutated: inner = %v, want unchanged 42", inner)
	}
}

func TestApplyPruningPropagatesErrorInsteadOfSwallowing(t *testing.T) {
	m := &mergeBuilderImpl{pruneKeys: []string{"a"}}
	doc := NewDocument(map[string]interface{}{"a": 1})

	result, err := m.applyPruning(doc)
	if err != nil {
		t.Fatalf("applyPruning returned unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("applyPruning returned nil document with nil error")
	}
	data, ok := result.RawData().(map[string]interface{})
	if !ok {
		t.Fatalf("result.RawData() is not a map: %T", result.RawData())
	}
	if _, exists := data["a"]; exists {
		t.Fatal("expected key 'a' to be pruned")
	}
}

// End-to-end parity checks against verified spruce binary behavior,
// exercised through the public MergeBuilder API.

func TestPruneNamedFinalArraySegmentNoOpMatchesSpruce(t *testing.T) {
	doc := NewDocument(map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{"name": "alpha", "port": 1},
			map[string]interface{}{"name": "beta", "port": 2},
		},
	})
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	result, err := engine.Merge(context.Background(), doc).WithPrune("items.beta").Execute()
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	data := result.RawData().(map[string]interface{})
	items := data["items"].([]interface{})
	if len(items) != 2 {
		t.Fatalf("got %d items, want 2 (spruce --prune items.beta with a named final segment is a no-op)", len(items))
	}
}

func TestPruneNamedIntermediateArraySegmentDeletesFinalKey(t *testing.T) {
	doc := NewDocument(map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{"name": "alpha", "port": 1},
			map[string]interface{}{"name": "beta", "port": 2},
		},
	})
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	result, err := engine.Merge(context.Background(), doc).WithPrune("items.beta.port").Execute()
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	data := result.RawData().(map[string]interface{})
	items := data["items"].([]interface{})
	beta := items[1].(map[string]interface{})
	if _, exists := beta["port"]; exists {
		t.Fatal("expected 'port' to be pruned from the named entry 'beta'")
	}
	if beta["name"] != "beta" {
		t.Fatalf("beta entry corrupted: %+v", beta)
	}
	alpha := items[0].(map[string]interface{})
	if alpha["port"] != 1 {
		t.Fatalf("alpha entry should be untouched: %+v", alpha)
	}
}

func TestPruneNonexistentPathNoOpMatchesSpruce(t *testing.T) {
	doc := NewDocument(map[string]interface{}{"a": 1})
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	result, err := engine.Merge(context.Background(), doc).WithPrune("nonexistent.path").Execute()
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	data := result.RawData().(map[string]interface{})
	if data["a"] != 1 {
		t.Fatalf("data mutated: %+v", data)
	}
}

func TestPruneNumericFinalArraySegmentStillDeletes(t *testing.T) {
	doc := NewDocument(map[string]interface{}{
		"items": []interface{}{
			map[string]interface{}{"name": "alpha"},
			map[string]interface{}{"name": "beta"},
		},
	})
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	result, err := engine.Merge(context.Background(), doc).WithPrune("items.1").Execute()
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	data := result.RawData().(map[string]interface{})
	items := data["items"].([]interface{})
	if len(items) != 1 {
		t.Fatalf("got %d items, want 1", len(items))
	}
	if items[0].(map[string]interface{})["name"] != "alpha" {
		t.Fatalf("wrong item remained: %+v", items[0])
	}
}
