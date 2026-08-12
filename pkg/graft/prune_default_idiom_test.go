package graft_test

// `(( grab X || (( prune )) ))` is a graft-only extension that lets a
// missing/falsy grab fall through to pruning the key entirely, instead of
// a literal fallback value. This worked at HEAD. The Candidate C tokenizer
// fix (interfaces/tokenizer.go: a '(' only combines with a following '('
// into TokenOperatorStart when no group is currently open) and the Bug 4
// resolveGrabArgValue LogicalOr rewrite (op_grab.go) each independently
// broke it: the inner "((" of "|| (( prune ))" is nested inside the outer
// marker's own already-open group, so Candidate C's guard (groupStack
// non-empty at that point) stops it from combining into a genuine nested
// marker, degrading "prune" to a bare, unresolvable reference.
//
// The deeper problem the fix addresses: "((A && B) || C)" (Candidate C's
// own repro) and "(( prune ))" used as an operand (Bug report's repro) are
// LEXICALLY IDENTICAL at the point the tokenizer must decide whether two
// adjacent '(' characters combine — both are "two adjacent '(' at a
// position where a primary expression is expected" — yet need opposite
// resolutions: the first is two ordinary, unrelated grouping opens; the
// second is a genuine, self-contained nested marker. No purely local,
// stateful tokenizer heuristic (including groupStack-emptiness) can
// distinguish them, because the distinguishing signal (does the content
// end with a matching "))" immediately, or with a single ')' followed by
// more content belonging to an enclosing group) only becomes available
// after parsing the content — a parse-time, not lex-time, decision.

import (
	"testing"

	_ "github.com/fivetwenty-io/graft/pkg/graft/operators"
)

// TestPruneDefaultIdiom_GrabHitKeepsValue is the reviewer's exact repro,
// hit case: the grab succeeds, so the "|| (( prune ))" fallback never
// fires and the key keeps its value.
func TestPruneDefaultIdiom_GrabHitKeepsValue(t *testing.T) {
	doc, err := mergeYAML(t, "meta:\n  keep: true\nconfig:\n  enabled: (( grab meta.keep || (( prune )) ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("config.enabled")
	if err != nil {
		t.Fatalf("failed to read config.enabled: %v", err)
	}
	if got != true {
		t.Fatalf("expected true, got %v", got)
	}
}

// TestPruneDefaultIdiom_GrabMissPrunesKey is the reviewer's exact repro,
// miss case: the grab fails, so the "|| (( prune ))" fallback fires and
// the key is pruned from the final output entirely (not present, not
// null — absent).
func TestPruneDefaultIdiom_GrabMissPrunesKey(t *testing.T) {
	doc, err := mergeYAML(t, "meta:\n  keep: true\nconfig:\n  enabled: (( grab meta.keep || (( prune )) ))\n  optional: (( grab meta.missing || (( prune )) ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := doc.Get("config.optional"); err == nil {
		t.Fatalf("expected config.optional to be pruned (absent), but it resolved")
	}
	// The hit case on the same document must still resolve correctly
	// alongside the miss case.
	got, err := doc.Get("config.enabled")
	if err != nil {
		t.Fatalf("failed to read config.enabled: %v", err)
	}
	if got != true {
		t.Fatalf("expected true, got %v", got)
	}
}

// TestPruneDefaultIdiom_BothCasesExitZero pins the reviewer's full
// repro end to end via the merge path used elsewhere in this file,
// asserting a clean (no-error) merge in one call — the closest
// equivalent to the CLI's "exit 0" the library API exposes.
func TestPruneDefaultIdiom_BothCasesExitZero(t *testing.T) {
	doc, err := mergeYAML(t, "meta:\n  keep: true\nconfig:\n  enabled: (( grab meta.keep || (( prune )) ))\n  optional: (( grab meta.missing || (( prune )) ))\n")
	if err != nil {
		t.Fatalf("unexpected error (expected exit-0-equivalent success): %v", err)
	}
	raw, ok := doc.RawData().(map[string]interface{})
	if !ok {
		t.Fatalf("expected document RawData to be a map, got %T", doc.RawData())
	}
	config, ok := raw["config"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected config to be a map, got %T", raw["config"])
	}
	if _, present := config["optional"]; present {
		t.Fatalf("expected config.optional to be absent after pruning, found: %v", config["optional"])
	}
	if config["enabled"] != true {
		t.Fatalf("expected config.enabled=true, got %v", config["enabled"])
	}
}
