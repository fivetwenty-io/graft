package graft_test

import (
	"context"
	"reflect"
	"testing"

	. "github.com/fivetwenty-io/graft/pkg/graft"
)

// mergeDocsForNullTest merges base and overlay through a fresh engine's
// simple-merge/legacy routing (whichever the overlay's markers select),
// failing the test on any error, and returns the merged root map.
func mergeDocsForNullTest(t *testing.T, base, overlay string) map[string]interface{} {
	t.Helper()
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	baseDoc := parseYAML(t, engine, base)
	overlayDoc := parseYAML(t, engine, overlay)
	merged, err := engine.Merge(context.Background(), baseDoc, overlayDoc).Execute()
	if err != nil {
		t.Fatalf("failed to merge: %v", err)
	}
	root, ok := merged.RawData().(map[string]interface{})
	if !ok {
		t.Fatalf("expected merged root to be a map, got %T", merged.RawData())
	}
	return root
}

// resolveMapPath walks nested maps by key, failing the test if any
// intermediate segment is missing or not a map. It returns the value at the
// final key and whether that key exists at all — the distinction this
// suite exists to check.
func resolveMapPath(t *testing.T, root map[string]interface{}, path []string) (interface{}, bool) {
	t.Helper()
	current := root
	for _, key := range path[:len(path)-1] {
		next, ok := current[key].(map[string]interface{})
		if !ok {
			t.Fatalf("expected a map at intermediate key %q, got %T", key, current[key])
		}
		current = next
	}
	v, ok := current[path[len(path)-1]]
	return v, ok
}

// TestSimpleMergeNullOverridePreservesKey asserts the simple-merge fast
// path (two plain documents, no array/prune/sort markers) treats an
// overlay key explicitly set to null as a null VALUE to store, not as a
// key deletion — matching spruce and the legacy merger's deliberate
// behavior (merger/merge.go mergeMap: "null is a valid value that should
// be preserved, not cause key deletion").
func TestSimpleMergeNullOverridePreservesKey(t *testing.T) {
	cases := []struct {
		name     string
		base     string
		overlay  string
		nullPath []string // key expected present with a null value
		keepPath []string // sibling expected to survive untouched
	}{
		{
			name:     "explicit null keyword",
			base:     "a: 1\nb: 2\n",
			overlay:  "b: null\n",
			nullPath: []string{"b"},
			keepPath: []string{"a"},
		},
		{
			name:     "tilde null",
			base:     "a: 1\nb: 2\n",
			overlay:  "b: ~\n",
			nullPath: []string{"b"},
			keepPath: []string{"a"},
		},
		{
			name:     "bare key null",
			base:     "a: 1\nb: 2\n",
			overlay:  "b:\n",
			nullPath: []string{"b"},
			keepPath: []string{"a"},
		},
		{
			name:     "map value overridden to null",
			base:     "a: 1\nb:\n  x: 1\n",
			overlay:  "b: ~\n",
			nullPath: []string{"b"},
			keepPath: []string{"a"},
		},
		{
			name:     "nested map key overridden to null",
			base:     "outer:\n  keep: 1\n  gone: 2\n",
			overlay:  "outer:\n  gone: ~\n",
			nullPath: []string{"outer", "gone"},
			keepPath: []string{"outer", "keep"},
		},
		{
			name:     "brand-new null key",
			base:     "a: 1\n",
			overlay:  "b: ~\n",
			nullPath: []string{"b"},
			keepPath: []string{"a"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := mergeDocsForNullTest(t, tc.base, tc.overlay)

			v, ok := resolveMapPath(t, root, tc.nullPath)
			if !ok {
				t.Fatalf("key %v was deleted; a null overlay value must preserve the key", tc.nullPath)
			}
			if v != nil {
				t.Errorf("expected key %v to hold null, got %v (%T)", tc.nullPath, v, v)
			}

			if _, ok := resolveMapPath(t, root, tc.keepPath); !ok {
				t.Errorf("sibling key %v missing from merged result", tc.keepPath)
			}
		})
	}
}

// TestSimpleMergeNullListElementPreserved pins the already-correct list
// behavior: a null overlay element in a positionally merged (inline) list
// stays null, it does not remove or shift the element.
func TestSimpleMergeNullListElementPreserved(t *testing.T) {
	root := mergeDocsForNullTest(t, "list:\n- x\n- y\n", "list:\n- ~\n")

	list, ok := root["list"].([]interface{})
	if !ok {
		t.Fatalf("expected list to be a []interface{}, got %T", root["list"])
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 elements, got %d: %v", len(list), list)
	}
	if list[0] != nil {
		t.Errorf("expected list[0] to be null, got %v (%T)", list[0], list[0])
	}
	if list[1] != "y" {
		t.Errorf("expected list[1] to be %q, got %v", "y", list[1])
	}
}

// TestNullOverrideFastAndLegacyPathsAgree asserts the fast path and the
// legacy merger produce identical output for a null override. The legacy
// variant adds a (( replace )) marker that rewrites tags to its existing
// value, forcing needsLegacyMerger routing without changing the final
// document.
func TestNullOverrideFastAndLegacyPathsAgree(t *testing.T) {
	base := "a: 1\nb: 2\ntags:\n- x\n"

	fast := mergeDocsForNullTest(t, base, "b: ~\n")
	legacy := mergeDocsForNullTest(t, base, "b: ~\ntags:\n- (( replace ))\n- x\n")

	if !reflect.DeepEqual(fast, legacy) {
		t.Errorf("fast path and legacy merger disagree on a null override:\nfast:   %#v\nlegacy: %#v", fast, legacy)
	}
	if v, ok := fast["b"]; !ok || v != nil {
		t.Errorf("expected b preserved as null on the fast path, got %v (present: %v)", v, ok)
	}
}

// TestSimpleMergeNullOverrideRecordsMergeNotDelete asserts document-memory
// history records a fast-path null override as a "merge" of an existing
// key to a null value, never as a "delete" — the recording that made
// --history render the change as <pruned> with the source file lost.
func TestSimpleMergeNullOverrideRecordsMergeNotDelete(t *testing.T) {
	engine := newTrackingEngine(t)

	base := parseYAML(t, engine, "a: 1\nb: 2\n")
	overlay := parseYAML(t, engine, "b: ~\n")
	merged, err := engine.Merge(context.Background(), base, overlay).Execute()
	if err != nil {
		t.Fatalf("failed to merge: %v", err)
	}

	root, ok := merged.RawData().(map[string]interface{})
	if !ok {
		t.Fatalf("expected merged root to be a map, got %T", merged.RawData())
	}
	if v, present := root["b"]; !present || v != nil {
		t.Fatalf("expected b preserved as null, got %v (present: %v)", v, present)
	}

	docMemory := trackerMemory(t, engine)
	found := false
	for _, event := range docMemory.GetTimeline() {
		if event.Path != "b" {
			continue
		}
		found = true
		if event.Source == "delete" {
			t.Errorf("null override recorded as a delete: %+v", event)
		}
		if event.NewValue != nil {
			t.Errorf("expected NewValue nil for the null override, got %v", event.NewValue)
		}
	}
	if !found {
		t.Fatal("expected a timeline event for path b")
	}
}
