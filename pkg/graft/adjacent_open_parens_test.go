package graft_test

// scanParentheses (interfaces/tokenizer.go) merges any two adjacent '('
// characters into a single TokenOperatorStart ("((") unconditionally —
// the mirror-image of the adjacent-')' bug fixed for closing parens
// (parser_adjacent_close_parens_test.go). When a genuine grouping '(' is
// immediately followed by another '(' that opens a nested group, with no
// space between them — e.g. "((A && B) || C)" as the content of a "((
// ... ))" marker, which tokenizes the outer group's own '(' adjacent to
// the inner "(A && B)" group's '(' — the pair wrongly tokenizes as one
// TokenOperatorStart. The parser then routes it through
// parseNestedOperator (a genuine nested "(( ... ))" marker, which graft
// does not actually support mid-expression — see item 16 in
// .agents/work/20260811-implementation/examples-repair-notes.md, a
// separate, out-of-scope finding) instead of two ordinary TokenLeftParen
// opens, and fails with "expected '))' to close nested operator".

import (
	"testing"

	_ "github.com/fivetwenty-io/graft/pkg/graft/operators"
)

// TestAdjacentOpenParens_OuterGroupWrappingInnerGroup is the exact repro:
// the whole marker content is one outer group directly wrapping an inner
// group, with no space between the two opening parens.
func TestAdjacentOpenParens_OuterGroupWrappingInnerGroup(t *testing.T) {
	doc, err := mergeYAML(t, "A: true\nB: true\nC: false\nx: (( ((A && B) || C) ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("x")
	if err != nil {
		t.Fatalf("failed to read x: %v", err)
	}
	if got != true {
		t.Fatalf("expected true, got %v", got)
	}
}

// TestAdjacentOpenParens_MultiArgBareCallWithGroupedArgument covers a
// multi-arg bare call (concat) whose argument is a group directly
// wrapping a further nested call, with no space between the two opening
// parens — the second shape named in the bug report.
func TestAdjacentOpenParens_MultiArgBareCallWithGroupedArgument(t *testing.T) {
	doc, err := mergeYAML(t, "a: 1\nb: 2\nx: (( concat ((grab a) + (grab b)) \"!\" ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("x")
	if err != nil {
		t.Fatalf("failed to read x: %v", err)
	}
	if got != "3!" {
		t.Fatalf("expected 3!, got %v", got)
	}
}

// TestAdjacentOpenParens_SpacedFormStillWorks pins the already-working
// workaround (a space between the two opening parens) keeps working.
func TestAdjacentOpenParens_SpacedFormStillWorks(t *testing.T) {
	doc, err := mergeYAML(t, "A: true\nB: true\nC: false\nx: (( ( (A && B) || C) ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("x")
	if err != nil {
		t.Fatalf("failed to read x: %v", err)
	}
	if got != true {
		t.Fatalf("expected true, got %v", got)
	}
}

// TestAdjacentOpenParens_PlainMarkerOpenUnchanged pins the ordinary,
// single "((" marker-open — the overwhelmingly common case — is
// untouched: it must stay one TokenOperatorStart even when the very next
// character is itself '(', which is how every "(( (grab a) ... ))"-style
// marker already starts.
func TestAdjacentOpenParens_PlainMarkerOpenUnchanged(t *testing.T) {
	doc, err := mergeYAML(t, "a: 1\nx: (( (grab a) ))\n")
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

// TestAdjacentOpenParens_PlainGrabUnchanged pins the ordinary,
// no-parens-at-all case is untouched.
func TestAdjacentOpenParens_PlainGrabUnchanged(t *testing.T) {
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

// TestAdjacentOpenParens_ThreeLevelsDeep pushes past two adjacent '(' to
// three in a row opening three left-nested groups, mirroring the
// adjacent-close-parens test of the same shape.
func TestAdjacentOpenParens_ThreeLevelsDeep(t *testing.T) {
	doc, err := mergeYAML(t, "a: 1\nx: (( (((grab a))) ))\n")
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
