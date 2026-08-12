package graft_test

// scanParentheses (interfaces/tokenizer.go) merges any two adjacent ')'
// characters into a single TokenOperatorEnd ("))") unconditionally, with no
// awareness of paren-nesting context. When an inner "(...)" group's closing
// paren sits immediately before the outer "((...))" wrapper's own closing
// "))" — e.g. "(join "," (grab a)) ))" written without a space before the
// inner group's ")" — the inner group's ")" is swallowed into what the
// tokenizer reads as the operator-call terminator, and the parser then
// fails to find the ')' it expects to close the inner group:
// "expected ')' to close parenthesized expression". Every nested-call
// example under docs/ avoids the failing shape by always writing a space
// before the outer "))" ("... ) ))"), which is why this went undetected;
// see A-ii review F7 and A3 review P... notes in
// .agents/work/20260811-implementation/bugfix-parser-notes.md.
//
// Confirmed against the spruce reference implementation (v1.35.16 at
// /opt/homebrew/bin/spruce): spruce does not support nested parenthesized
// operator-call syntax at all — "(( (concat (grab a) (grab b)) ))" passes
// through spruce as unparsed literal text rather than evaluating. This
// grammar is a graft-specific extension (cluster A2), so there is no
// spruce behavior to match; this file's fix and tests are graft-only.

import (
	"testing"

	_ "github.com/fivetwenty-io/graft/pkg/graft/operators"
)

// TestAdjacentClosingParens_InnerGroupThenOuterWrapper is the exact failing
// shape from the bug report: an inner "(...)" group's ')' immediately
// followed by the outer "((...))" wrapper's "))", with no space between.
func TestAdjacentClosingParens_InnerGroupThenOuterWrapper(t *testing.T) {
	doc, err := mergeYAML(t, "a: 1\nx: (( (join \",\" (grab a)) ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("x")
	if err != nil {
		t.Fatalf("failed to read x: %v", err)
	}
	if got != "1" {
		t.Fatalf("expected \"1\", got %v", got)
	}
}

// TestAdjacentClosingParens_DeeperNesting covers two sibling inner groups
// each closing immediately before the outer wrapper, ending in the
// "...b)) ))" shape called out in the bug report.
func TestAdjacentClosingParens_DeeperNesting(t *testing.T) {
	doc, err := mergeYAML(t, "a: 1\nb: 2\nx: (( (concat (grab a) (grab b)) ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("x")
	if err != nil {
		t.Fatalf("failed to read x: %v", err)
	}
	if got != "12" {
		t.Fatalf("expected \"12\", got %v", got)
	}
}

// TestAdjacentClosingParens_SpacedFormStillWorks pins the workaround every
// doc example already uses (a space before the outer "))") so the fix does
// not regress it.
func TestAdjacentClosingParens_SpacedFormStillWorks(t *testing.T) {
	doc, err := mergeYAML(t, "a: 1\nx: (( (join \",\" (grab a) ) ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("x")
	if err != nil {
		t.Fatalf("failed to read x: %v", err)
	}
	if got != "1" {
		t.Fatalf("expected \"1\", got %v", got)
	}
}

// TestAdjacentClosingParens_PlainGrabUnchanged pins the ordinary, unnested
// "))" wrapper close — the overwhelmingly common case — is untouched.
func TestAdjacentClosingParens_PlainGrabUnchanged(t *testing.T) {
	doc, err := mergeYAML(t, "a: hello\nx: (( grab a ))\n")
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

// TestAdjacentClosingParens_ThreeLevelsDeepEndingInFourCloseParens pushes
// past two adjacent ')' to three in a row closing three right-nested
// groups ("(grab a)))"), followed by the outer wrapper's own "))".
func TestAdjacentClosingParens_ThreeLevelsDeepEndingInFourCloseParens(t *testing.T) {
	doc, err := mergeYAML(t, "a: 1\nx: (( (concat \"x\" (concat \"y\" (grab a))) ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("x")
	if err != nil {
		t.Fatalf("failed to read x: %v", err)
	}
	if got != "xy1" {
		t.Fatalf("expected \"xy1\", got %v", got)
	}
}

// TestAdjacentClosingParens_ArithmeticGroupingUnaffected pins that a plain
// (non-opcall) parenthesized arithmetic grouping immediately followed by
// the outer "))" — no space, three ')' in a row — still tokenizes
// correctly (single ')' for the grouping, then "))" for the wrapper),
// matching the "grouping parens around infix arithmetic" case in
// parser_nested_e2e_test.go but without the space that test relies on.
func TestAdjacentClosingParens_ArithmeticGroupingUnaffected(t *testing.T) {
	doc, err := mergeYAML(t, "x: (( (1 + 2)))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("x")
	if err != nil {
		t.Fatalf("failed to read x: %v", err)
	}
	if got != int64(3) {
		t.Fatalf("expected 3, got %v (%T)", got, got)
	}
}

// TestAdjacentClosingParens_MultiArgBareCallWithNestedGroupArgument covers
// a shape found live in the examples corpus:
// `(( concat "Basic " (base64 (grab x)) ))`. Same root cause as the other
// cases in this file — "x))" is two adjacent group closes (grab's, then
// base64's) immediately before a space and the outer wrapper's own "))" —
// but this one is a multi-argument bare call (concat takes a literal
// string argument *and* a nested-call argument) rather than a single
// parenthesized group wrapping the whole expression, so it exercises a
// different argument-list parse path than the other cases here.
func TestAdjacentClosingParens_MultiArgBareCallWithNestedGroupArgument(t *testing.T) {
	doc, err := mergeYAML(t, "x: hello\ny: (( concat \"Basic \" (base64 (grab x)) ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("y")
	if err != nil {
		t.Fatalf("failed to read y: %v", err)
	}
	if got != "Basic aGVsbG8=" {
		t.Fatalf("expected \"Basic aGVsbG8=\", got %v", got)
	}
}
