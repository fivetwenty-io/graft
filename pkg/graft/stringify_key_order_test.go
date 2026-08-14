package graft_test

import (
	"strings"
	"testing"

	_ "github.com/fivetwenty-io/graft/pkg/graft/operators"
)

// TestStringifyKeyOrder locks in that (( stringify )) serializes map
// keys in the same spruce-compatible order as every other YAML emit
// (digit runs numeric: item2 < item9 < item10), by routing through the
// shared graft.MarshalYAML instead of a private goccy encoder that
// sorted keys lexicographically. Scope is ordering only — spruce
// renders the containing multi-line string as a literal block while
// goccy emits a quoted scalar, a documented style divergence
// (docs/spruce/known-gaps.md, stringify-block-scalar-style).
func TestStringifyKeyOrder(t *testing.T) {
	doc, err := mergeYAML(t, "meta:\n  item10: c\n  item2: a\n  item9: b\nout: (( stringify meta ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("out")
	if err != nil {
		t.Fatalf("failed to read out: %v", err)
	}
	s, ok := got.(string)
	if !ok {
		t.Fatalf("expected out to be a string, got %T: %v", got, got)
	}

	lastIdx := -1
	for _, key := range []string{"item2:", "item9:", "item10:"} {
		idx := strings.Index(s, key)
		if idx == -1 {
			t.Fatalf("stringified output missing key %q: %q", key, s)
		}
		if idx < lastIdx {
			t.Fatalf("key %q out of spruce order in stringified output: %q", key, s)
		}
		lastIdx = idx
	}
}
