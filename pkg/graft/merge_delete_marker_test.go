package graft_test

import (
	"context"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
	. "github.com/fivetwenty-io/graft/pkg/graft"
)

// mergeDocsErr merges base and overlay through a fresh engine and returns
// the merge error (nil on success), failing the test only on setup errors.
func mergeDocsErr(t *testing.T, base, overlay string) error {
	t.Helper()
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	baseDoc := parseYAML(t, engine, base)
	overlayDoc := parseYAML(t, engine, overlay)
	_, err = engine.Merge(context.Background(), baseDoc, overlayDoc).Execute()
	return err
}

const deleteGuardMsg = `$.scalarkey: inappropriate use of (( delete )) operator outside of a list`

// forceLegacyTail is appended to an overlay to force needsLegacyMerger
// routing without changing the merged result: (( replace )) rewrites tags
// to the same value the base already holds.
const forceLegacyTail = "tags:\n- (( replace ))\n- x\n"

// TestDeleteMarkerOutsideListErrorsOnBothPaths asserts an argument-bearing
// (( delete ... )) in map-value position fails the merge with the same
// error on the simple-merge fast path and the legacy merger path, instead
// of leaking the marker into the output as literal data. spruce also
// rejects these inputs (from its eval phase, as "(( delete )) operator not
// defined"); graft rejects them at merge time with an explicit wording.
func TestDeleteMarkerOutsideListErrorsOnBothPaths(t *testing.T) {
	ansi.Color(false)

	forms := []struct {
		name   string
		marker string
	}{
		{"string argument", `(( delete "hello" ))`},
		{"keyed argument", `(( delete name "hello" ))`},
		{"integer argument", `(( delete 0 ))`},
		{"reference argument", `(( delete meta.key ))`},
	}

	bases := []struct {
		name string
		base string
	}{
		{"scalar base", "scalarkey: hello\n"},
		{"map base", "scalarkey:\n  nested: 1\n"},
		{"list base", "scalarkey:\n- hello\n"},
		{"absent base", "other: 1\n"},
	}

	for _, form := range forms {
		for _, base := range bases {
			t.Run(form.name+" onto "+base.name, func(t *testing.T) {
				baseWithTags := base.base + "tags:\n- x\n"
				overlay := "scalarkey: " + form.marker + "\n"

				fastErr := mergeDocsErr(t, baseWithTags, overlay)
				if fastErr == nil {
					t.Fatal("fast path: expected a merge error, got success")
				}
				if !strings.Contains(fastErr.Error(), deleteGuardMsg) {
					t.Errorf("fast path error %q does not contain %q", fastErr.Error(), deleteGuardMsg)
				}

				legacyErr := mergeDocsErr(t, baseWithTags, overlay+forceLegacyTail)
				if legacyErr == nil {
					t.Fatal("legacy path: expected a merge error, got success")
				}
				if fastErr.Error() != legacyErr.Error() {
					t.Errorf("paths disagree:\nfast:   %q\nlegacy: %q", fastErr.Error(), legacyErr.Error())
				}
			})
		}
	}
}

// TestDeleteMarkerInFirstDocumentErrors asserts the guard also covers a
// marker arriving in the first (base) document of a merge, which the fast
// path prepares without the legacy merger unless routed.
func TestDeleteMarkerInFirstDocumentErrors(t *testing.T) {
	ansi.Color(false)

	err := mergeDocsErr(t, "scalarkey: (( delete \"hello\" ))\n", "other: 1\n")
	if err == nil {
		t.Fatal("expected a merge error for a marker in the first document, got success")
	}
	if !strings.Contains(err.Error(), deleteGuardMsg) {
		t.Errorf("error %q does not contain %q", err.Error(), deleteGuardMsg)
	}
}

// TestBareDeleteMarkerPassthroughPinned pins the bare, argument-less
// (( delete )) form's passthrough behavior for spruce parity: spruce
// leaves the literal marker text in the output in both map-value and
// list-entry position, and so does graft. This is deliberate parity, not
// an accident — only the argument-bearing forms are guarded.
func TestBareDeleteMarkerPassthroughPinned(t *testing.T) {
	t.Run("map value", func(t *testing.T) {
		engine, err := NewEngine()
		if err != nil {
			t.Fatalf("failed to create engine: %v", err)
		}
		base := parseYAML(t, engine, "scalarkey: hello\n")
		overlay := parseYAML(t, engine, "scalarkey: (( delete ))\n")
		merged, err := engine.Merge(context.Background(), base, overlay).Execute()
		if err != nil {
			t.Fatalf("expected passthrough, got error: %v", err)
		}
		root := merged.RawData().(map[string]interface{})
		if root["scalarkey"] != "(( delete ))" {
			t.Errorf("expected literal marker passthrough, got %v", root["scalarkey"])
		}
	})

	t.Run("list entry", func(t *testing.T) {
		engine, err := NewEngine()
		if err != nil {
			t.Fatalf("failed to create engine: %v", err)
		}
		base := parseYAML(t, engine, "list:\n- a\n- b\n")
		overlay := parseYAML(t, engine, "list:\n- (( delete ))\n")
		merged, err := engine.Merge(context.Background(), base, overlay).Execute()
		if err != nil {
			t.Fatalf("expected passthrough, got error: %v", err)
		}
		root := merged.RawData().(map[string]interface{})
		list, ok := root["list"].([]interface{})
		if !ok || len(list) != 2 {
			t.Fatalf("expected a 2-element list, got %v", root["list"])
		}
		if list[0] != "(( delete ))" || list[1] != "b" {
			t.Errorf("expected [(( delete )), b], got %v", list)
		}
	})
}

// TestDeleteMarkerListPositionStillWorks is the regression guard for the
// legal list-entry forms alongside the new map-value guard.
func TestDeleteMarkerListPositionStillWorks(t *testing.T) {
	cases := []struct {
		name    string
		overlay string
		want    []interface{}
	}{
		{"delete by value", "list:\n- (( delete \"a\" ))\n", []interface{}{"b"}},
		{"delete by index", "list:\n- (( delete 0 ))\n", []interface{}{"b"}},
		{"delete-if-present no-op", "list:\n- (( delete \"not-there\" ))\n", []interface{}{"a", "b"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			engine, err := NewEngine()
			if err != nil {
				t.Fatalf("failed to create engine: %v", err)
			}
			base := parseYAML(t, engine, "list:\n- a\n- b\n")
			overlay := parseYAML(t, engine, tc.overlay)
			merged, err := engine.Merge(context.Background(), base, overlay).Execute()
			if err != nil {
				t.Fatalf("merge failed: %v", err)
			}
			root := merged.RawData().(map[string]interface{})
			list, ok := root["list"].([]interface{})
			if !ok {
				t.Fatalf("expected a list, got %T", root["list"])
			}
			if len(list) != len(tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, list)
			}
			for i := range tc.want {
				if list[i] != tc.want[i] {
					t.Errorf("element %d: expected %v, got %v", i, tc.want[i], list[i])
				}
			}
		})
	}
}
