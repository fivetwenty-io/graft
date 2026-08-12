package graft_test

import (
	"context"
	"strings"
	"testing"

	. "github.com/fivetwenty-io/graft/pkg/graft"
)

// This file is the P0-2 regression suite: DocumentMemory's three producers
// (merger/merge.go, merge_builder_impl.go, evaluator.go) must all record
// history under the same canonical no-"$" dotted path vocabulary
// (pkg/graft/tree.Cursor.String()), so a merge-phase entry and an eval-phase
// entry for the same node land in one NodeHistory bucket instead of two
// disjoint ones keyed by different literal strings.

// newTrackingEngine returns an Engine with document-memory tracking enabled,
// failing the test immediately if construction fails.
func newTrackingEngine(t *testing.T) Engine {
	t.Helper()
	engine, err := NewEngine(WithMemoryConfig(MemoryConfig{Enabled: true}))
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	return engine
}

// trackerMemory extracts the concrete *DocumentMemory behind an engine's
// MemoryTracker, failing the test if tracking was not wired up.
func trackerMemory(t *testing.T, engine Engine) *DocumentMemory {
	t.Helper()
	tracker := engine.GetMemoryTracker()
	if tracker == nil {
		t.Fatal("expected a memory tracker on the engine")
	}
	docMemory, ok := tracker.(*DocumentMemory)
	if !ok {
		t.Fatalf("expected memory tracker to be *DocumentMemory, got %T", tracker)
	}
	return docMemory
}

// parseYAML parses src through engine, failing the test on error.
func parseYAML(t *testing.T, engine Engine, src string) Document {
	t.Helper()
	doc, err := engine.ParseYAML([]byte(src))
	if err != nil {
		t.Fatalf("failed to parse document: %v", err)
	}
	return doc
}

// assertCanonicalPath fails the test if path carries the merger's internal
// "$" root marker. tree.ParseCursor silently discards a leading "$" node,
// so a resolvability check alone (merged.Get(path)) cannot distinguish
// "database.host" from "$.database.host" — both resolve. Recorded history
// paths must match the canonical form exactly, since a bucket keyed
// "$.database.host" and one keyed "database.host" are different map
// entries in DocumentMemory.histories and will not merge.
func assertCanonicalPath(t *testing.T, path string) {
	t.Helper()
	if strings.HasPrefix(path, "$") {
		t.Errorf("recorded path %q carries the merger's internal \"$\" root marker; history paths must be canonical (no \"$\" prefix)", path)
	}
}

func mapKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// TestHistoryPathVocabularyResolvesAllRecordedPaths exercises the
// merge_builder_impl.go performSimpleMergeAtPath route (two documents, no
// array-merge markers) and asserts every path DocumentMemory records is
// both canonical and resolvable against the merged result.
func TestHistoryPathVocabularyResolvesAllRecordedPaths(t *testing.T) {
	engine := newTrackingEngine(t)

	base := parseYAML(t, engine, `
database:
  host: primary
  port: 5432
network:
  name: default
`)
	overlay := parseYAML(t, engine, `
database:
  host: (( grab replica.host ))
  extra: added
replica:
  host: secondary
`)

	merged, err := engine.Merge(context.Background(), base, overlay).Execute()
	if err != nil {
		t.Fatalf("failed to merge: %v", err)
	}

	docMemory := trackerMemory(t, engine)
	timeline := docMemory.GetTimeline()
	if len(timeline) == 0 {
		t.Fatal("expected a non-empty timeline")
	}

	seen := make(map[string]bool)
	for _, event := range timeline {
		seen[event.Path] = true
		assertCanonicalPath(t, event.Path)
		if _, getErr := merged.Get(event.Path); getErr != nil {
			t.Errorf("recorded path %q does not resolve via Document.Get: %v", event.Path, getErr)
		}
	}

	// Nested merge-phase changes (database.host, database.extra) must be
	// recorded under their full dotted path, not the bare leaf key
	// (merge_builder_impl.go's earlier defect: performSimpleMergeAtPath
	// recursed into nested maps but recorded only the immediate key, e.g.
	// "host" instead of "database.host").
	for _, want := range []string{"database.host", "database.extra"} {
		if !seen[want] {
			t.Errorf("expected timeline to contain path %q; got paths %v", want, mapKeys(seen))
		}
	}

	// The pre-fix bare-key form must not reappear as its own bucket.
	if _, histErr := docMemory.GetHistory("host"); histErr == nil {
		t.Error("did not expect a bare-key \"host\" history bucket; nested paths must carry their full prefix")
	}
}

// TestHistoryPathVocabularyCrossPhaseSinglePath asserts a path touched
// during both the merge phase (merge_builder_impl.go) and the eval phase
// (evaluator.go) produces a single NodeHistory carrying entries from both,
// rather than two disjoint buckets keyed by different literal strings.
func TestHistoryPathVocabularyCrossPhaseSinglePath(t *testing.T) {
	engine := newTrackingEngine(t)

	base := parseYAML(t, engine, `
database:
  host: primary
`)
	overlay := parseYAML(t, engine, `
database:
  host: (( grab replica.host ))
replica:
  host: secondary
`)

	if _, mergeErr := engine.Merge(context.Background(), base, overlay).Execute(); mergeErr != nil {
		t.Fatalf("failed to merge: %v", mergeErr)
	}

	docMemory := trackerMemory(t, engine)

	history, err := docMemory.GetHistory("database.host")
	if err != nil {
		t.Fatalf("expected a single NodeHistory at \"database.host\", got error: %v", err)
	}
	if len(history.Versions) != 2 {
		t.Fatalf("expected 2 versions at \"database.host\" (merge + eval), got %d: %+v", len(history.Versions), history.Versions)
	}

	mergeVersion, evalVersion := history.Versions[0], history.Versions[1]

	if mergeVersion.Phase != PhaseMerge || mergeVersion.Operation != OpMerge {
		t.Errorf("expected version 1 Phase=PhaseMerge/Operation=OpMerge, got Phase=%v Operation=%v", mergeVersion.Phase, mergeVersion.Operation)
	}
	if mergeVersion.Value != "(( grab replica.host ))" {
		t.Errorf("expected version 1 value to be the unresolved operator text, got %v", mergeVersion.Value)
	}

	if evalVersion.Phase != PhaseEval || evalVersion.Operation != OpTransform {
		t.Errorf("expected version 2 Phase=PhaseEval/Operation=OpTransform, got Phase=%v Operation=%v", evalVersion.Phase, evalVersion.Operation)
	}
	if evalVersion.Value != "secondary" {
		t.Errorf("expected version 2 value \"secondary\", got %v", evalVersion.Value)
	}
}

// TestHistoryPathVocabularyMergerRouteCanonicalPaths drives the
// merger/merge.go RecordMergeChange route directly (adversarial review
// finding F1): a single document containing an array-merge marker forces
// mergeBuilderImpl.Execute's single-document fast path through
// merger.Merger.Merge instead of performSimpleMergeAtPath
// (merge_builder_impl.go's hasArrayOperators/hasArraysWithMaps/
// hasPruneOperators check routes here). The sibling resolvability tests
// above cannot detect a "$"-prefixed regression on this route:
// tree.ParseCursor discards a leading "$" node, so Get("$.meta.k")
// resolves exactly like Get("meta.k") does — a poisoned
// canonicalHistoryPath that leaves the "$" prefix in place (or replaces it
// with any other unstripped decoration) passes a resolvability-only check.
// This test instead asserts exact string equality against the canonical
// path set.
func TestHistoryPathVocabularyMergerRouteCanonicalPaths(t *testing.T) {
	engine := newTrackingEngine(t)

	doc := parseYAML(t, engine, `
list:
  - (( append ))
  - c
meta:
  k: v
`)

	if _, err := engine.Merge(context.Background(), doc).Execute(); err != nil {
		t.Fatalf("failed to merge: %v", err)
	}

	docMemory := trackerMemory(t, engine)

	// The whole document is new (merged onto an empty base internally, per
	// mergeBuilderImpl.Execute's single-document branch), so every
	// top-level and nested key is recorded via mergeMap's "add" branch:
	// "list" (the array itself, as one blob - list-element mutations are
	// not recorded, a documented gap, not this test's concern), "meta"
	// (the whole added subtree), and "meta.k" (recorded again as MergeObj
	// recurses into the newly-added map via its own mergeMap call).
	want := map[string]bool{"list": true, "meta": true, "meta.k": true}
	got := make(map[string]bool)
	for _, event := range docMemory.GetTimeline() {
		got[event.Path] = true
		assertCanonicalPath(t, event.Path)
	}

	for path := range want {
		if !got[path] {
			t.Errorf("expected the merger route to record canonical path %q; got %v", path, mapKeys(got))
		}
	}
	for path := range got {
		if !want[path] {
			t.Errorf("merger route recorded unexpected path %q; got %v", path, mapKeys(got))
		}
	}
}
