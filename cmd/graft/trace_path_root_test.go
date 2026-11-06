package main

import (
	"testing"

	"github.com/fivetwenty-io/graft/internal/history"
)

// TestFindPathHistoryAcceptsRootAnchor pins that --trace-path takes the
// "$." root anchor graft accepts everywhere else, and bracket indexes,
// and finds the same history as the bare dotted path.
func TestFindPathHistoryAcceptsRootAnchor(t *testing.T) {
	all := []history.PathHistory{{Path: "x"}, {Path: "m.a"}, {Path: "l.0"}}
	for _, path := range []string{"x", "$.x", "m.a", "$.m.a", "l[0]", "$.l.0"} {
		if _, found := findPathHistory(all, path); !found {
			t.Errorf("findPathHistory(%q) found nothing", path)
		}
	}
	for _, path := range []string{"$", "$.", "y", "$.y"} {
		if _, found := findPathHistory(all, path); found {
			t.Errorf("findPathHistory(%q) found a history", path)
		}
	}
}

// TestFindPathHistoryPrefersExactPath pins that a real key named "$"
// is still reachable as "$.x", because the exact path is tried before
// the root anchor is stripped.
func TestFindPathHistoryPrefersExactPath(t *testing.T) {
	all := []history.PathHistory{{Path: "$.x"}, {Path: "x"}}
	ph, found := findPathHistory(all, "$.x")
	if !found || ph.Path != "$.x" {
		t.Fatalf("got %q, %v; want the exact $.x history", ph.Path, found)
	}
	ph, found = findPathHistory([]history.PathHistory{{Path: "x"}}, "$.x")
	if !found || ph.Path != "x" {
		t.Fatalf("fallback got %q, %v; want x", ph.Path, found)
	}
}
