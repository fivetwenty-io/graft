package graft_test

// The space-separated-argument loop in parseOperatorCall
// (pkg/graft/parser.go) treats any binary-operator token as "the
// argument list is finished" (isBinaryOperator), including TokenMinus.
// Each argument here is parsed as one standalone primary (parsePrimary),
// never through the full precedence-climbing expression parser, so there
// is no "previous operand" a bare '-' at this position could validly be
// subtracting from — but the blanket check does not know that, and
// breaks out of the loop the moment it sees '-', leaving a trailing
// negative-number argument like "-5" in "(( ips net -5 ))" never
// consumed: "ips requires at least two arguments" even though a second
// one was written. "(( ips net (-5) ))" works because the parenthesized
// form routes through parsePrimary's own TokenLeftParen case instead of
// this loop's isBinaryOperator check. Confirmed against the spruce
// reference implementation (v1.35.16, /opt/homebrew/bin/spruce): spruce
// supports the bare "(( ips net -5 ))" form and produces the identical
// result to the parenthesized workaround, so this is a parity gap, not a
// graft-only extension.

import (
	"testing"

	_ "github.com/fivetwenty-io/graft/pkg/graft/operators"
)

// TestNegativeLiteralArgument_BareForm is the exact repro from the bug
// report.
func TestNegativeLiteralArgument_BareForm(t *testing.T) {
	doc, err := mergeYAML(t, "net:\n  x: 10.0.0.0/24\nresult: (( ips net.x -5 ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("result")
	if err != nil {
		t.Fatalf("failed to read result: %v", err)
	}
	if got != "10.0.0.251" {
		t.Fatalf("expected 10.0.0.251, got %v", got)
	}
}

// TestNegativeLiteralArgument_MatchesParenthesizedForm pins the bare form
// produces the identical result to the already-working parenthesized
// workaround.
func TestNegativeLiteralArgument_MatchesParenthesizedForm(t *testing.T) {
	bare, err := mergeYAML(t, "net:\n  x: 10.0.0.0/24\nresult: (( ips net.x -5 ))\n")
	if err != nil {
		t.Fatalf("unexpected error (bare form): %v", err)
	}
	parenthesized, err := mergeYAML(t, "net:\n  x: 10.0.0.0/24\nresult: (( ips net.x (-5) ))\n")
	if err != nil {
		t.Fatalf("unexpected error (parenthesized form): %v", err)
	}
	bareVal, err := bare.Get("result")
	if err != nil {
		t.Fatalf("failed to read bare result: %v", err)
	}
	parenVal, err := parenthesized.Get("result")
	if err != nil {
		t.Fatalf("failed to read parenthesized result: %v", err)
	}
	if bareVal != parenVal {
		t.Fatalf("bare form %v does not match parenthesized form %v", bareVal, parenVal)
	}
}

// TestNegativeLiteralArgument_PositiveFormUnaffected pins the existing,
// already-working positive-offset form stays working.
func TestNegativeLiteralArgument_PositiveFormUnaffected(t *testing.T) {
	doc, err := mergeYAML(t, "net:\n  x: 10.0.0.0/24\nresult: (( ips net.x 5 ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("result")
	if err != nil {
		t.Fatalf("failed to read result: %v", err)
	}
	if got != "10.0.0.5" {
		t.Fatalf("expected 10.0.0.5, got %v", got)
	}
}

// TestInfixSubtraction_BareFormUnaffected pins bare infix subtraction
// through the full expression parser (not inside a bare call's own
// space-separated argument list) is unaffected: "A - B" must still mean
// subtraction, not "A" followed by a dropped/misparsed "-B".
func TestInfixSubtraction_BareFormUnaffected(t *testing.T) {
	doc, err := mergeYAML(t, "a: 10\nb: 3\nx: (( a - b ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("x")
	if err != nil {
		t.Fatalf("failed to read x: %v", err)
	}
	if got != int64(7) {
		t.Fatalf("expected 7, got %v (%T)", got, got)
	}
}

// TestInfixSubtraction_CalcStringUnaffected pins the calc-string form of
// subtraction (op_calc.go's own raw-substring handling, a completely
// separate code path from the bare space-separated-argument loop) is
// unaffected.
func TestInfixSubtraction_CalcStringUnaffected(t *testing.T) {
	doc, err := mergeYAML(t, "x: (( calc \"10 - 3\" ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("x")
	if err != nil {
		t.Fatalf("failed to read x: %v", err)
	}
	if got != int64(7) {
		t.Fatalf("expected 7, got %v (%T)", got, got)
	}
}

// TestNegativeLiteralArgument_ConcatStillJoinsCorrectly pins a negative
// literal as a non-first bare-call argument to a different multi-arg
// operator (concat, which stringifies its arguments) still produces the
// expected joined value, not silently dropping the negative argument or
// misinterpreting the call boundary.
func TestNegativeLiteralArgument_ConcatStillJoinsCorrectly(t *testing.T) {
	doc, err := mergeYAML(t, "x: (( concat \"offset:\" -5 ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("x")
	if err != nil {
		t.Fatalf("failed to read x: %v", err)
	}
	if got != "offset:-5" {
		t.Fatalf("expected offset:-5, got %v", got)
	}
}
