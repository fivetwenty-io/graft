package graft_test

// parseOperatorCall (pkg/graft/parser.go) decides whether "(" right after
// a bare operator's name opens function-call syntax (op(a, b)) or is the
// first space-separated argument, itself a parenthesized group
// (op (expr) arg2), purely from whether the token immediately inside the
// parens looks like it opens its own nested operator call
// (identifierOpensOpcallAt). When the group's content is a single
// non-comma expression whose first token is a plain value or identifier
// — a ternary chain, an arithmetic grouping, anything that is not itself
// "IDENTIFIER(" or "IDENTIFIER " — that heuristic wrongly commits to
// function-call syntax, consumes just that one parenthesized value as a
// complete (and, from the parser's perspective, already-closed) call, and
// leaves whatever was meant as the next space-separated argument
// dangling: "concat (flag ? \"a\" : \"b\") \"m\"" fails with "expected
// '))' at end of operator expression, got STRING" because the trailing
// "m" is never consumed. The fix does not need to change the initial
// decision at all: after the function-call-style branch finishes, if
// something other than a terminator (")", "))", EOF, "?", ":") follows,
// it is additional space-separated arguments, and the existing
// space-separated loop (which already no-ops immediately for genuine
// "op(a, b)" calls, since the token right after them is always a
// terminator) picks them up.

import (
	"testing"

	_ "github.com/fivetwenty-io/graft/pkg/graft/operators"
)

// TestParenthesizedTernaryFirstArg is the exact repro from the bug
// report.
func TestParenthesizedTernaryFirstArg(t *testing.T) {
	doc, err := mergeYAML(t, "flag: true\nx: '(( concat (flag ? \"a\" : \"b\") \"m\" ))'\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("x")
	if err != nil {
		t.Fatalf("failed to read x: %v", err)
	}
	if got != "am" {
		t.Fatalf("expected am, got %v", got)
	}
}

// TestParenthesizedFirstArg_ThreeArguments pins more than one additional
// argument still gets picked up after the parenthesized first one, not
// just exactly one.
func TestParenthesizedFirstArg_ThreeArguments(t *testing.T) {
	doc, err := mergeYAML(t, "flag: true\nx: '(( concat (flag ? \"a\" : \"b\") \"-\" \"m\" ))'\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("x")
	if err != nil {
		t.Fatalf("failed to read x: %v", err)
	}
	if got != "a-m" {
		t.Fatalf("expected a-m, got %v", got)
	}
}

// TestFunctionCallSyntax_TwoArgs pins the genuine function-call syntax
// this heuristic exists for keeps working: "op(a, b)" with a
// comma-separated argument list and nothing following the closing paren.
func TestFunctionCallSyntax_TwoArgs(t *testing.T) {
	doc, err := mergeYAML(t, "a: x\nb: y\nx: (( concat(a, b) ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("x")
	if err != nil {
		t.Fatalf("failed to read x: %v", err)
	}
	if got != "xy" {
		t.Fatalf("expected xy, got %v", got)
	}
}

// TestFunctionCallSyntax_SingleArg pins genuine function-call syntax with
// exactly one argument and no comma — the shape most easily confused with
// "a parenthesized group used as the first space-separated argument"
// (both consume one parenthesized expression and see a terminator right
// after), which must keep resolving to a one-argument call, not silently
// gain a phantom second argument.
func TestFunctionCallSyntax_SingleArg(t *testing.T) {
	doc, err := mergeYAML(t, "a:\n  x: 1\nresult: (( keys(a) ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("result")
	if err != nil {
		t.Fatalf("failed to read result: %v", err)
	}
	list, ok := got.([]interface{})
	if !ok || len(list) != 1 || list[0] != "x" {
		t.Fatalf("expected [x], got %#v", got)
	}
}

// TestNestedCallFirstArg_Unaffected pins the already-working documented
// nested-call-as-first-argument shape (identifierOpensOpcallAt's own
// intended case: the token right after "(" IS a registered operator
// name) stays function-call-free space-separated parsing, unaffected by
// this change.
func TestNestedCallFirstArg_Unaffected(t *testing.T) {
	doc, err := mergeYAML(t, "cores: 4\nx: (( concat (grab cores) \"-cpu\" ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("x")
	if err != nil {
		t.Fatalf("failed to read x: %v", err)
	}
	if got != "4-cpu" {
		t.Fatalf("expected 4-cpu, got %v", got)
	}
}
