package graft_test

// GrabOperator.Run dispatches its per-argument post-processing (a bare
// reference resolved directly, a nested-call's computed string
// re-interpreted as a grab path, a literal/env-var used as-is) on the
// argument expression's own Type. When that argument is wrapped in a `||`
// fallback, ResolveOperatorArgument's LogicalOr case already picks and
// resolves whichever side succeeds and returns its value — but grab.go's
// dispatch switch never looks past the outer LogicalOr node, so it takes
// the "use the resolved value as-is" branch instead of the "successful
// side was a nested call, re-resolve its result as a path" branch: the
// nested call's computed path string is used as the final answer instead
// of being grabbed. `(( grab (concat "a." "b") ))` (no fallback) works;
// `(( grab (concat "a." "b") || ~ ))` (fallback present, primary side
// still succeeds) returns the literal string "a.b" instead of the value
// at $.a.b.

import (
	"testing"

	_ "github.com/fivetwenty-io/graft/pkg/graft/operators"
)

// TestGrabNestedCallComputedPath_NoFallback pins the already-working
// baseline: no `||` present, nested-call argument re-resolved as a path.
func TestGrabNestedCallComputedPath_NoFallback(t *testing.T) {
	doc, err := mergeYAML(t, "a:\n  b: hello\nx: (( grab (concat \"a.\" \"b\") ))\n")
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

// TestGrabNestedCallComputedPath_WithFallbackStillResolves is the exact
// failing shape from the bug report: same expression, `|| ~` appended.
// The primary (left) side still succeeds, so the result must be identical
// to the no-fallback case above, not the unresolved literal "a.b".
func TestGrabNestedCallComputedPath_WithFallbackStillResolves(t *testing.T) {
	doc, err := mergeYAML(t, "a:\n  b: hello\nx: (( grab (concat \"a.\" \"b\") || ~ ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("x")
	if err != nil {
		t.Fatalf("failed to read x: %v", err)
	}
	if got != "hello" {
		t.Fatalf("expected hello (identical to the no-fallback case), got %v", got)
	}
}

// TestGrabNestedCallComputedPath_FallbackUsedWhenPrimaryPathMissing pins
// the fallback still fires when the nested call's computed path does not
// resolve to anything: grab.Run's own path-resolution attempt for the
// primary side must fail (not silently succeed with an unresolved string),
// so LogicalOr's fallback logic moves on to the right side.
func TestGrabNestedCallComputedPath_FallbackUsedWhenPrimaryPathMissing(t *testing.T) {
	doc, err := mergeYAML(t, "a:\n  b: hello\nx: (( grab (concat \"a.\" \"missing\") || \"bye\" ))\n")
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

// TestGrabBareReferenceWithFallback_Unaffected pins the already-working
// bare-reference + `||` case (named in the bug report as working) stays
// working: no nested call, no computed-path re-resolution needed, just
// the reference's own value.
func TestGrabBareReferenceWithFallback_Unaffected(t *testing.T) {
	doc, err := mergeYAML(t, "a:\n  b: hello\nx: (( grab a.b || ~ ))\n")
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
