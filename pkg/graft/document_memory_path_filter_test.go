package graft_test

import (
	"context"
	"testing"

	. "github.com/fivetwenty-io/graft/pkg/graft"
)

// TestQueryPathFilter pins HistoryFilter.Path's matching grammar: a recorded
// path matches when it equals the pattern, when PathMatches accepts it
// (*, **, [0], [*], [key=value] wildcards), or when the pattern is a proper
// segment-aware path prefix of it - Path: "db" matches "db.host" but NOT
// "dbextra" (the old raw byte-prefix check wrongly matched both). An empty
// pattern disables the filter entirely.
func TestQueryPathFilter(t *testing.T) {
	newMemory := func() *DocumentMemory {
		memory := NewDocumentMemory(MemoryConfig{
			Enabled:            true,
			MaxVersionsPerNode: 10,
			MaxTotalVersions:   100,
			TrackMergePhase:    true,
			TrackEvalPhase:     true,
		})
		t.Cleanup(memory.StopBackgroundCleanup)

		record := func(path string, value interface{}) {
			t.Helper()
			if err := memory.RecordChange(path, nil, value, PhaseMerge, OpSet, "test.yaml"); err != nil {
				t.Fatalf("failed to record %s: %v", path, err)
			}
		}
		record("db.host", "localhost")
		record("db.port", 5432)
		record("db.pool.size", 10)
		record("dbextra", "surprise")
		record("app.host", "app.example.com")
		record("jobs.0.name", "web")
		return memory
	}

	paths := func(events []ChangeEvent) map[string]bool {
		got := make(map[string]bool, len(events))
		for _, e := range events {
			got[e.Path] = true
		}
		return got
	}

	cases := []struct {
		name    string
		pattern string
		want    []string
	}{
		{"exact match", "db.host", []string{"db.host"}},
		{"segment-aware prefix", "db", []string{"db.host", "db.port", "db.pool.size"}},
		{"byte-prefix false positive is fixed", "dbextra", []string{"dbextra"}},
		{"single wildcard", "*.host", []string{"db.host", "app.host"}},
		{"double wildcard", "db.**", []string{"db.host", "db.port", "db.pool.size"}},
		{"list index wildcard", "jobs.*.name", []string{"jobs.0.name"}},
		{"bracketed list index", "jobs.[0].name", []string{"jobs.0.name"}},
		{"bracketed index wildcard", "jobs.[*].name", []string{"jobs.0.name"}},
		{"empty pattern matches all", "", []string{
			"db.host", "db.port", "db.pool.size", "dbextra", "app.host", "jobs.0.name"}},
		{"no match", "cache", nil},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			memory := newMemory()
			got := paths(memory.Query(HistoryFilter{Path: tc.pattern}))
			if len(got) != len(tc.want) {
				t.Fatalf("Query(Path=%q) matched %v, want %v", tc.pattern, got, tc.want)
			}
			for _, w := range tc.want {
				if !got[w] {
					t.Errorf("Query(Path=%q) missing %q (got %v)", tc.pattern, w, got)
				}
			}
		})
	}
}

// TestHistoryQueryPathFilter exercises the same grammar through the public
// History surface a library caller sees after TrackHistory().
func TestHistoryQueryPathFilter(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	base, err := engine.ParseYAML([]byte("db:\n  host: localhost\ndbextra: x\n"))
	if err != nil {
		t.Fatalf("failed to parse base: %v", err)
	}
	overlay, err := engine.ParseYAML([]byte("db:\n  host: prod.example.com\ndbextra: y\n"))
	if err != nil {
		t.Fatalf("failed to parse overlay: %v", err)
	}

	result, err := engine.Merge(context.Background(), base, overlay).
		TrackHistory().
		Execute()
	if err != nil {
		t.Fatalf("failed to merge: %v", err)
	}

	// The merge records both the leaf ("db.host") and the changed map
	// itself ("db"); Path "db" must match both (exact + segment prefix)
	// while excluding "dbextra", the old byte-prefix false positive.
	entries := result.History().Query(HistoryFilter{Path: "db"})
	if len(entries) != 2 {
		t.Fatalf("expected the db and db.host entries for Path \"db\", got %d: %+v", len(entries), entries)
	}
	for _, e := range entries {
		if e.Path != "db" && e.Path != "db.host" {
			t.Errorf("Path \"db\" wrongly matched %q", e.Path)
		}
	}

	wild := result.History().Query(HistoryFilter{Path: "*.host"})
	if len(wild) != 1 || wild[0].Path != "db.host" {
		t.Errorf("expected wildcard *.host to match db.host only, got %+v", wild)
	}
}
