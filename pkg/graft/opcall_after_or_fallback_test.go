package graft_test

// parseOperatorCall's own space-separated-argument loop has a second,
// separate "||" handler (distinct from parseLogicalOr / parseOperand,
// used by the general expression grammar) for fallbacks written inside a
// bare operator call's own argument list, e.g. "(( grab v || fallback ))".
// Its right-hand side is parsed with a bare p.parsePrimary() call, which
// never runs identifierOpensOpcallAt's two-token lookahead or the
// temporary p.opcallPos override parseOperand applies elsewhere — so a
// bare operator identifier (no "@target", no explicit "(") on the right
// of this specific "||" is never recognized as opening a call and
// degrades to a plain reference instead. Confirmed NOT limited to nested
// contexts: "(( grab missing || grab fallback ))" fails identically at
// the top level, because the top-level operator call ("grab") itself
// parses its own single argument through this exact same loop. The
// @target form already works regardless of nesting (commit 700dbbc,
// pkg/graft/target_fallback_test.go) because it is checked unconditionally,
// with no dependency on opcallPos at all.

import (
	"testing"

	_ "github.com/fivetwenty-io/graft/pkg/graft/operators"
)

// TestBareOpAfterOr_TopLevel is the top-level shape (not nested inside
// another call's argument) that turned out to already be broken, found
// while investigating the nested repro below: the same code path handles
// both.
func TestBareOpAfterOr_TopLevel(t *testing.T) {
	doc, err := mergeYAML(t, "fallback: bye\nx: (( grab missing || grab fallback ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("x")
	if err != nil {
		t.Fatalf("failed to read x: %v", err)
	}
	if got != "bye" {
		t.Fatalf("expected bye, got %v", got)
	}
}

// TestBareOpAfterOr_NestedInAnotherCallsArgument is the exact repro from
// the bug report: the "||" sits inside another operator call's own
// argument (grab's argument here is "(concat "a." "missing") || grab
// fallback" as a single expression), one level deeper than the top-level
// case above.
func TestBareOpAfterOr_NestedInAnotherCallsArgument(t *testing.T) {
	doc, err := mergeYAML(t, "a:\n  b: hello\nfallback: bye\nx: (( grab (concat \"a.\" \"missing\") || grab fallback ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("x")
	if err != nil {
		t.Fatalf("failed to read x: %v", err)
	}
	if got != "bye" {
		t.Fatalf("expected bye, got %v", got)
	}
}

// TestBareOpAfterOr_NestedNoFallbackNeeded pins that the primary side
// still wins (fallback's bare op is never invoked, no side effect from
// its argument) when the primary side succeeds, matching the "identical
// with and without ||" expectation used elsewhere in this bug set.
func TestBareOpAfterOr_NestedNoFallbackNeeded(t *testing.T) {
	doc, err := mergeYAML(t, "a:\n  b: hello\nfallback: bye\nx: (( grab (concat \"a.\" \"b\") || grab fallback ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("x")
	if err != nil {
		t.Fatalf("failed to read x: %v", err)
	}
	if got != "hello" {
		t.Fatalf("expected hello, got %v", got)
	}
}

// TestBareOpAfterOr_MultiArgFallback pins a fallback operator that itself
// takes more than one space-separated argument (not just a single bare
// reference) still parses and runs as one call, not degrading into loose
// extra arguments for the outer call. concat's computed result ("ab") is
// deliberately given a matching top-level key so this exercises grab's
// own established computed-path behavior (pinned by
// TestGrabNestedCallComputedPath_NoFallback) rather than asserting a
// different, inconsistent semantic for a fallback position specifically:
// the point under test here is argument-count correctness (concat must
// consume both "a" and "b" itself, not leak "b" as a second positional
// argument to the outer grab), not path-vs-literal semantics.
func TestBareOpAfterOr_MultiArgFallback(t *testing.T) {
	doc, err := mergeYAML(t, "ab: found-it\nx: (( grab missing || concat \"a\" \"b\" ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("x")
	if err != nil {
		t.Fatalf("failed to read x: %v", err)
	}
	if got != "found-it" {
		t.Fatalf("expected found-it, got %v", got)
	}
}

// TestTargetedCallAfterOr_TopLevelStillWorks pins the already-working
// "@target" form (pkg/graft/target_fallback_test.go covers the crash-
// safety side of this; this pins the successful-fallback-value side)
// stays unaffected by this fix.
func TestTargetedCallAfterOr_TopLevelStillWorks(t *testing.T) {
	doc, err := mergeYAML(t, "v: 1\nx: (( grab v || awsparam@staging \"/a\" ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("x")
	if err != nil {
		t.Fatalf("failed to read x: %v", err)
	}
	if got != int64(1) && got != 1 {
		t.Fatalf("expected 1, got %v (%T)", got, got)
	}
}

// TestBareReferenceAfterOr_StillWorks pins the plain-reference fallback
// case (no operator identifier at all on the right of ||) stays working.
func TestBareReferenceAfterOr_StillWorks(t *testing.T) {
	doc, err := mergeYAML(t, "fallback: bye\nx: (( grab missing || fallback ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("x")
	if err != nil {
		t.Fatalf("failed to read x: %v", err)
	}
	if got != "bye" {
		t.Fatalf("expected bye, got %v", got)
	}
}

// TestDocumentedGrabOrGrabPattern pins the exact pattern documented in
// docs/user-guide/operators/external-sources.md ("Apply environment
// overrides" section): `grab overrides.path || grab defaults.path`. Both
// sides here are bare references, not nested calls — this is the shape
// that motivated this fix and must produce the override value when
// present and fall through to the default when it is not.
func TestDocumentedGrabOrGrabPattern(t *testing.T) {
	t.Run("override present", func(t *testing.T) {
		doc, err := mergeYAML(t, "overrides:\n  database:\n    host: override-host\ndatabase:\n  host: default-host\ndb_host: (( grab overrides.database.host || grab database.host ))\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, err := doc.Get("db_host")
		if err != nil {
			t.Fatalf("failed to read db_host: %v", err)
		}
		if got != "override-host" {
			t.Fatalf("expected override-host, got %v", got)
		}
	})

	t.Run("override absent, falls through to default", func(t *testing.T) {
		doc, err := mergeYAML(t, "overrides: {}\ndatabase:\n  host: default-host\ndb_host: (( grab overrides.database.host || grab database.host ))\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, err := doc.Get("db_host")
		if err != nil {
			t.Fatalf("failed to read db_host: %v", err)
		}
		if got != "default-host" {
			t.Fatalf("expected default-host, got %v", got)
		}
	})
}
