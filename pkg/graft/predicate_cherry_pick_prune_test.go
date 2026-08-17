package graft

import (
	"context"
	"testing"
)

// These tests pin the fix for a gap carried over from Stage A-i's own
// review: --cherry-pick and --prune paths with a "field=value" predicate
// segment (e.g. "servers.name=primary") used to fail or silently no-op,
// because findNamedArrayEntry/findNamedArrayEntryWithIndex (the merge
// builder's own hand-rolled array navigator) only ever matched a bare name
// against name/id/key fields — it never recognized predicate syntax at
// all. They now dispatch a predicate segment to tree.ParsePredicateSegment
// / tree.PredicateFind, the exact same first-match, list-containers-only
// functions the (( grab ... )) predicate resolver uses.

func predicateTestServers() map[string]interface{} {
	return map[string]interface{}{
		"servers": []interface{}{
			map[string]interface{}{"name": "primary", "host": "10.0.0.1"},
			map[string]interface{}{"name": "secondary", "host": "10.0.0.2"},
		},
	}
}

func TestFindNamedArrayEntryWithIndex_PredicateSegment(t *testing.T) {
	servers := predicateTestServers()["servers"].([]interface{})

	idx, entry, found := findNamedArrayEntryWithIndex(servers, "name=secondary")
	if !found {
		t.Fatalf("predicate segment name=secondary not found")
	}
	if idx != 1 {
		t.Errorf("got index %d, want 1", idx)
	}
	m, ok := entry.(map[string]interface{})
	if !ok || m["host"] != "10.0.0.2" {
		t.Errorf("got entry %#v, want the secondary server", entry)
	}
}

func TestFindNamedArrayEntryWithIndex_PredicateSegmentNotFound(t *testing.T) {
	servers := predicateTestServers()["servers"].([]interface{})

	_, _, found := findNamedArrayEntryWithIndex(servers, "name=missing")
	if found {
		t.Errorf("expected name=missing to not be found")
	}
}

func TestFindNamedArrayEntryWithIndex_BareNameStillWorks(t *testing.T) {
	// Non-regression: the pre-existing bare name/id/key auto-match must
	// keep working unchanged alongside the new predicate branch.
	servers := predicateTestServers()["servers"].([]interface{})

	idx, entry, found := findNamedArrayEntryWithIndex(servers, "primary")
	if !found {
		t.Fatalf("bare name lookup for 'primary' not found")
	}
	if idx != 0 {
		t.Errorf("got index %d, want 0", idx)
	}
	m, ok := entry.(map[string]interface{})
	if !ok || m["host"] != "10.0.0.1" {
		t.Errorf("got entry %#v, want the primary server", entry)
	}
}

func TestRemoveKey_PredicateFinalSegmentDeletesMatchingEntry(t *testing.T) {
	m := &mergeBuilderImpl{}
	data := predicateTestServers()

	m.removeKey(data, "servers.name=secondary")

	servers, ok := data["servers"].([]interface{})
	if !ok {
		t.Fatalf("servers is not a list: %#v", data["servers"])
	}
	if len(servers) != 1 {
		t.Fatalf("got %d servers, want 1: %#v", len(servers), servers)
	}
	entry, ok := servers[0].(map[string]interface{})
	if !ok || entry["name"] != "primary" {
		t.Errorf("got %#v, want only the primary server remaining", servers[0])
	}
}

func TestRemoveKey_PredicateFinalSegmentNotFoundIsNoOp(t *testing.T) {
	m := &mergeBuilderImpl{}
	data := predicateTestServers()

	m.removeKey(data, "servers.name=missing")

	servers := data["servers"].([]interface{})
	if len(servers) != 2 {
		t.Fatalf("servers mutated: got %d entries, want 2 unchanged", len(servers))
	}
}

func TestRemoveKey_BareNamedFinalArraySegmentStillNoOp(t *testing.T) {
	// Non-regression (prune_named_entry_test.go's own contract): a bare
	// (non-predicate) named final segment on an array stays a spruce-parity
	// no-op — only the new "field=value" predicate shape resolves to a
	// delete index.
	m := &mergeBuilderImpl{}
	data := predicateTestServers()

	m.removeKey(data, "servers.secondary")

	servers := data["servers"].([]interface{})
	if len(servers) != 2 {
		t.Fatalf("servers mutated: got %d entries, want 2 unchanged (bare named final segment must stay a no-op)", len(servers))
	}
}

func TestRemoveKey_PredicateMidPathSegmentNavigatesThrough(t *testing.T) {
	// A predicate segment that isn't the final one still needs to resolve
	// so a deeper key can be pruned (e.g. --prune
	// "servers.name=primary.host").
	m := &mergeBuilderImpl{}
	data := predicateTestServers()

	m.removeKey(data, "servers.name=primary.host")

	servers := data["servers"].([]interface{})
	primary, ok := servers[0].(map[string]interface{})
	if !ok {
		t.Fatalf("servers[0] is not a map: %#v", servers[0])
	}
	if _, stillPresent := primary["host"]; stillPresent {
		t.Errorf("expected host to be pruned from the primary entry, got %#v", primary)
	}
	if primary["name"] != "primary" {
		t.Errorf("expected the rest of the primary entry to survive, got %#v", primary)
	}
}

// --- End-to-end, through the public MergeBuilder API ---

// assertOnlyPrimaryServer checks that result's servers list holds exactly
// the primary entry, which is what both a cherry-pick of the primary and a
// prune of the secondary must leave behind.
func assertOnlyPrimaryServer(t *testing.T, result Document) {
	t.Helper()

	data, ok := result.RawData().(map[string]interface{})
	if !ok {
		t.Fatalf("result is not a map: %#v", result.RawData())
	}
	servers, ok := data["servers"].([]interface{})
	if !ok {
		t.Fatalf("servers is not a list: %#v", data["servers"])
	}
	if len(servers) != 1 {
		t.Fatalf("got %d servers, want 1: %#v", len(servers), servers)
	}
	entry, ok := servers[0].(map[string]interface{})
	if !ok || entry["name"] != "primary" {
		t.Errorf("got %#v, want only the primary server", servers[0])
	}
}

func TestCherryPick_PredicateSegmentEndToEnd(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	doc := NewDocument(predicateTestServers())

	result, err := engine.Merge(context.Background(), doc).
		WithCherryPick("servers.name=primary").
		Execute()
	if err != nil {
		t.Fatalf("cherry-pick failed: %v", err)
	}

	assertOnlyPrimaryServer(t, result)
}

func TestPrune_PredicateSegmentEndToEnd(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	doc := NewDocument(predicateTestServers())

	result, err := engine.Merge(context.Background(), doc).
		WithPrune("servers.name=secondary").
		Execute()
	if err != nil {
		t.Fatalf("prune failed: %v", err)
	}

	assertOnlyPrimaryServer(t, result)
}
