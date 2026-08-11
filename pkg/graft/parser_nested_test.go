package graft

import (
	"strings"
	"testing"
)

// TestParseParenthesized_OpcallVsGrouping pins the §2.4 disambiguation table:
// a "(" opens an operator-call group only when the first token is a
// registered operator name AND the token after it is not ")", ",", "?",
// ":", or an infix operator. Otherwise "(" is ordinary arithmetic grouping,
// exactly as it worked before A2.
//
// This lives in package graft (not graft_test) so it can inspect parsed
// *Expr shape directly. It cannot import pkg/graft/operators itself (which
// imports this package — an import cycle; see opcall_target_test.go's
// identical note), but relies on it being registered anyway: this test
// binary also links the external package graft_test test files (e.g.
// a6_backward_compat_test.go), which blank-import pkg/graft/operators, and
// Go runs every transitively-imported package's init() before any test
// function executes — so "grab", "sort", and "concat" are registered by
// the time these tests run.
func TestParseParenthesized_OpcallVsGrouping(t *testing.T) {
	cases := []struct {
		name  string
		src   string
		check func(t *testing.T, e *Expr)
	}{
		{
			name: "(grab a) is an opcall",
			src:  "(( (grab a) ))",
			check: func(t *testing.T, e *Expr) {
				if e.Type != OperatorCall || e.Operator != "grab" {
					t.Fatalf("expected OperatorCall(grab), got %#v", e)
				}
			},
		},
		{
			name: "(a + b) is grouping",
			src:  "(( (a + b) ))",
			check: func(t *testing.T, e *Expr) {
				if e.Type != Addition {
					t.Fatalf("expected Addition, got %#v", e)
				}
				if e.Left.Type != Reference {
					t.Fatalf("expected left operand to be a Reference, got %#v", e.Left)
				}
			},
		},
		{
			name: "(sort + 1) is grouping because next token after sort is infix",
			src:  "(( (sort + 1) ))",
			check: func(t *testing.T, e *Expr) {
				if e.Type != Addition {
					t.Fatalf("expected Addition, got %#v", e)
				}
				if e.Left.Type != Reference || e.Left.Reference == nil {
					t.Fatalf("expected left operand to be a Reference(sort), got %#v", e.Left)
				}
			},
		},
		{
			name: "(grab) alone is grouping -> Reference(grab), next token is )",
			src:  "(( (grab) ))",
			check: func(t *testing.T, e *Expr) {
				if e.Type != Reference {
					t.Fatalf("expected Reference(grab), got %#v", e)
				}
			},
		},
		{
			name: "(sort by name) is an opcall, next token after sort is an identifier",
			src:  "(( (sort by name) ))",
			check: func(t *testing.T, e *Expr) {
				if e.Type != OperatorCall || e.Operator != "sort" {
					t.Fatalf("expected OperatorCall(sort), got %#v", e)
				}
			},
		},
		{
			name: "two opcalls under addition, each group independent",
			src:  "(( (grab a) + (grab b) ))",
			check: func(t *testing.T, e *Expr) {
				if e.Type != Addition {
					t.Fatalf("expected Addition, got %#v", e)
				}
				if e.Left.Type != OperatorCall || e.Left.Operator != "grab" {
					t.Fatalf("expected left OperatorCall(grab), got %#v", e.Left)
				}
				if e.Right.Type != OperatorCall || e.Right.Operator != "grab" {
					t.Fatalf("expected right OperatorCall(grab), got %#v", e.Right)
				}
			},
		},
		{
			name: "grouping parens around infix arithmetic, unchanged behavior",
			src:  "(( (1 + 2) * 3 ))",
			check: func(t *testing.T, e *Expr) {
				if e.Type != Multiplication {
					t.Fatalf("expected Multiplication, got %#v", e)
				}
				if e.Left.Type != Addition {
					t.Fatalf("expected left operand to be Addition (grouping preserved), got %#v", e.Left)
				}
			},
		},
		{
			name: "concat with a nested grab call as an operand",
			src:  `(( concat "a" (grab b) ))`,
			check: func(t *testing.T, e *Expr) {
				if e.Type != OperatorCall || e.Operator != "concat" {
					t.Fatalf("expected OperatorCall(concat), got %#v", e)
				}
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := NewParser(tc.src, EvalPhase)
			opcall, err := p.ParseOpcall()
			if err != nil {
				t.Fatalf("unexpected parse error: %v", err)
			}
			expr := opcallExprForTest(opcall)
			tc.check(t, expr)
		})
	}
}

// opcallExprForTest extracts the underlying *Expr wrapped by exprToOpcall so
// nested-parse tests can assert on parse shape directly, without going
// through a full merge/evaluate pass.
func opcallExprForTest(o *Opcall) *Expr {
	if op, ok := o.op.(*exprOperator); ok {
		return op.expr
	}
	// A bare Reference is wrapped as a single-arg "grab" opcall with no
	// name (exprToOpcall's Reference branch). A genuine registered-operator
	// call already carries its own name/args; reconstruct an equivalent
	// Expr node for it.
	if len(o.args) == 1 && o.name == "" {
		return o.args[0]
	}
	return &Expr{
		Type:     OperatorCall,
		Operator: o.name,
		Call:     o,
	}
}

// deepGrouping builds n levels of space-separated single-paren grouping
// around a literal, e.g. n=3 -> "( ( ( 1 ) ) )". The spaces force each "("
// to tokenize as a standalone TokenLeftParen rather than pairing up with
// its neighbor into a TokenOperatorStart "((" (the tokenizer greedily
// merges two adjacent parens — interfaces/tokenizer.go:473-479), so this
// exercises parseParenthesized's own nesting counter exactly n times.
func deepGrouping(n int) string {
	open := strings.Repeat("( ", n)
	closeParens := strings.Repeat(" )", n)
	return open + "1" + closeParens
}

// TestNestedParenDepthLimit pins §2.5: nesting beyond 64 "(" / "((" levels
// is a hard parse error, not a stack overflow.
func TestNestedParenDepthLimit(t *testing.T) {
	src := "(( " + deepGrouping(65) + " ))"
	p := NewParser(src, EvalPhase)
	_, err := p.ParseOpcall()
	if err == nil {
		t.Fatalf("expected nesting-too-deep error, got none")
	}
	if !strings.Contains(err.Error(), "nesting too deep") {
		t.Fatalf("expected 'nesting too deep' error, got: %v", err)
	}
}

// TestNestedParenDepthLimit_JustUnderCap pins that 64 levels of nested
// grouping parens still parses successfully — the limit is exclusive.
func TestNestedParenDepthLimit_JustUnderCap(t *testing.T) {
	src := "(( " + deepGrouping(64) + " ))"
	p := NewParser(src, EvalPhase)
	_, err := p.ParseOpcall()
	if err != nil {
		t.Fatalf("unexpected error at the boundary: %v", err)
	}
}
