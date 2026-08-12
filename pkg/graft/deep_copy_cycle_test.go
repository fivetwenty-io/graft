package graft

// deepCopyMap and deepCopyValue recurse over map[string]interface{} and
// []interface{} without cycle detection. A self-referencing value (a map
// or list that (transitively) contains itself) recurses forever and
// overflows the goroutine stack instead of returning an error.
//
// The most realistic route a caller can build such a value: the library
// API's NewDocument(map[string]interface{}) accepts an arbitrary Go map
// with no cycle validation, so a caller can construct
//
//	m := map[string]interface{}{}
//	m["self"] = m
//	doc := NewDocument(m)
//
// and merge it. Confirmed NOT reachable through plain YAML text: a
// forward-referencing anchor/alias pair that would need to alias into its
// own not-yet-finished node (`a: &x\n  b: *x`) decodes through
// goccy/go-yaml as `b: nil`, not a real Go-level cycle — go-yaml does not
// materialize self-referential structures from YAML source. The library-API
// route is exercised end to end by TestMergeCyclicDocument_ReturnsErrorNotStackOverflow
// in cyclic_document_e2e_test.go. This file unit-tests deepCopyMap and
// deepCopyValue directly, the functions named in the bug report, using a
// hand-built cyclic map (same shape the library-API route produces).

import (
	"errors"
	"strings"
	"testing"
)

func TestDeepCopyMap_SelfReferencingMapReturnsError(t *testing.T) {
	cyclic := map[string]interface{}{"x": 1}
	cyclic["self"] = cyclic

	_, err := deepCopyMap(cyclic)
	if err == nil {
		t.Fatalf("expected an error for a self-referencing map, got success")
	}
	if !strings.Contains(err.Error(), "cyclic") {
		t.Fatalf("expected a cyclic-reference error, got: %v", err)
	}
}

func TestDeepCopyValue_SelfReferencingListReturnsError(t *testing.T) {
	cyclic := []interface{}{"a", "b"}
	cyclic[1] = cyclic

	_, err := deepCopyValue(cyclic)
	if err == nil {
		t.Fatalf("expected an error for a self-referencing list, got success")
	}
	if !strings.Contains(err.Error(), "cyclic") {
		t.Fatalf("expected a cyclic-reference error, got: %v", err)
	}
}

func TestDeepCopyMap_IndirectCycleThroughNestedMapReturnsError(t *testing.T) {
	// a -> b -> a (the cycle is two levels deep, not a direct self-reference)
	a := map[string]interface{}{}
	b := map[string]interface{}{"back": a}
	a["child"] = b

	_, err := deepCopyMap(a)
	if err == nil {
		t.Fatalf("expected an error for an indirect cycle, got success")
	}
	if !strings.Contains(err.Error(), "cyclic") {
		t.Fatalf("expected a cyclic-reference error, got: %v", err)
	}
}

func TestDeepCopyMap_IndirectCycleThroughListReturnsError(t *testing.T) {
	// a -> [a] (the map's own list value contains the map itself)
	a := map[string]interface{}{}
	list := []interface{}{a}
	a["children"] = list

	_, err := deepCopyMap(a)
	if err == nil {
		t.Fatalf("expected an error for a list-mediated cycle, got success")
	}
	if !strings.Contains(err.Error(), "cyclic") {
		t.Fatalf("expected a cyclic-reference error, got: %v", err)
	}
}

// TestDeepCopyMap_DiamondStructureCopiesCorrectly pins that a
// diamond-shaped (shared but acyclic) structure — the same map instance
// reachable from two different sibling branches, never from itself — still
// copies correctly and is not mistaken for a cycle. This must keep passing
// after cycle detection lands: only an ancestor-descendant reference is a
// cycle, not a shared, already-finished node revisited from a sibling.
func TestDeepCopyMap_DiamondStructureCopiesCorrectly(t *testing.T) {
	shared := map[string]interface{}{"value": 42}
	root := map[string]interface{}{
		"left":  shared,
		"right": shared,
	}

	got, err := deepCopyMap(root)
	if err != nil {
		t.Fatalf("unexpected error copying diamond structure: %v", err)
	}

	left, ok := got["left"].(map[string]interface{})
	if !ok {
		t.Fatalf("left did not copy as a map: %T", got["left"])
	}
	right, ok := got["right"].(map[string]interface{})
	if !ok {
		t.Fatalf("right did not copy as a map: %T", got["right"])
	}
	if left["value"] != 42 || right["value"] != 42 {
		t.Fatalf("diamond values not preserved: left=%v right=%v", left, right)
	}

	// Mutating the copy's "left" must not affect "right" or the original:
	// deepCopyMap always produces independent maps, even for shared source
	// nodes (unlike the pointer identity of the source, the two copies are
	// never required to alias each other).
	left["value"] = 99
	if right["value"] != 42 {
		t.Fatalf("mutating copied left leaked into copied right: %v", right["value"])
	}
	if shared["value"] != 42 {
		t.Fatalf("mutating the copy leaked into the source: %v", shared["value"])
	}
}

// TestDeepCopyValue_AcyclicNestedStructureUnchanged pins ordinary nested
// map/list copying keeps working once cycle tracking is in place.
func TestDeepCopyValue_AcyclicNestedStructureUnchanged(t *testing.T) {
	src := map[string]interface{}{
		"list": []interface{}{1, "two", map[string]interface{}{"three": 3}},
		"nested": map[string]interface{}{
			"deeper": map[string]interface{}{"x": []interface{}{1, 2, 3}},
		},
	}

	got, err := deepCopyMap(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	list, ok := got["list"].([]interface{})
	if !ok || len(list) != 3 {
		t.Fatalf("list not copied correctly: %#v", got["list"])
	}
	innerMap, ok := list[2].(map[string]interface{})
	if !ok || innerMap["three"] != 3 {
		t.Fatalf("nested map inside list not copied correctly: %#v", list[2])
	}

	nested, ok := got["nested"].(map[string]interface{})
	if !ok {
		t.Fatalf("nested not copied as map: %#v", got["nested"])
	}
	deeper, ok := nested["deeper"].(map[string]interface{})
	if !ok {
		t.Fatalf("deeper not copied as map: %#v", nested["deeper"])
	}
	x, ok := deeper["x"].([]interface{})
	if !ok || len(x) != 3 {
		t.Fatalf("deeply nested list not copied correctly: %#v", deeper["x"])
	}
}

// TestDeepCopyMap_ErrorIsSentinelWrappable ensures the returned cycle error
// is a plain error usable with errors.New/fmt.Errorf %w chains elsewhere in
// the merge pipeline (callers wrap it with path/document context).
func TestDeepCopyMap_ErrorIsSentinelWrappable(t *testing.T) {
	cyclic := map[string]interface{}{}
	cyclic["self"] = cyclic

	_, err := deepCopyMap(cyclic)
	if err == nil {
		t.Fatalf("expected an error")
	}
	wrapped := errors.New("merge failed: " + err.Error())
	if !strings.Contains(wrapped.Error(), "cyclic") {
		t.Fatalf("wrapped error lost cycle context: %v", wrapped)
	}
}
