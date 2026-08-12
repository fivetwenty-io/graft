package graft_test

// Compiling examples for graft.Walk, graft.Accept, and graft.Visitor
// (pkg/graft/expr_walk.go). Each example parses a real operator
// expression through the public parser entry point, graft.ParseOpcallWithParser,
// rather than constructing an *graft.Expr by hand, so it doubles as a check
// that Walk/Accept behave as documented against the actual grammar.

import (
	"fmt"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// exprOf parses "(( src ))" and returns the *graft.Expr it evaluates to —
// the same extraction mustParseInner uses internally in the package's own
// tests, reimplemented here against only exported API since this file
// lives in the external graft_test package.
func exprOf(src string) (*graft.Expr, error) {
	opcall, err := graft.ParseOpcallWithParser(graft.EvalPhase, "(( "+src+" ))")
	if err != nil {
		return nil, err
	}
	args := opcall.Args()
	if len(args) == 0 {
		return nil, fmt.Errorf("parsed %q to an operator call with no arguments", src)
	}
	return args[0], nil
}

// ExampleWalk visits every node of "1 + 2 * 3" — Addition(1,
// Multiplication(2, 3)) — and counts them.
func ExampleWalk() {
	root, err := exprOf("1 + 2 * 3")
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	count := 0
	graft.Walk(root, func(*graft.Expr) bool {
		count++
		return true
	})
	fmt.Println("nodes visited:", count)
	// Output:
	// nodes visited: 5
}

// ExampleWalk_pruning shows fn returning false to skip a node's children:
// "!flag && other" is LogicalAnd(Negate(Reference(flag)),
// Reference(other)). Pruning at the Negate node skips the Reference nested
// under it but still visits the LogicalAnd's other child.
func ExampleWalk_pruning() {
	root, err := exprOf("!flag && other")
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	full := 0
	graft.Walk(root, func(*graft.Expr) bool {
		full++
		return true
	})

	pruned := 0
	graft.Walk(root, func(e *graft.Expr) bool {
		pruned++
		return e.Type != graft.Negate
	})

	fmt.Println("full walk:", full)
	fmt.Println("pruned walk:", pruned)
	// Output:
	// full walk: 4
	// pruned walk: 3
}

// describeVisitor renders an Expr tree as a parenthesized string. It
// implements every graft.Visitor method, including VisitOther, which here
// stands in for any ExprType with no dedicated method (List, Or,
// RegexpMatch, BoshVar, VaultGroup, VaultChoice today, and whatever
// ExprType graft adds later) — the whole point of the catch-all is that
// this Visitor implementation does not need a new method to keep
// compiling and running when that happens.
type describeVisitor struct{}

func (describeVisitor) VisitLiteral(e *graft.Expr) interface{} {
	return fmt.Sprintf("%v", e.Literal)
}

func (describeVisitor) VisitReference(e *graft.Expr) interface{} {
	if e.Reference == nil {
		return "<target>"
	}
	return e.Reference.String()
}

func (describeVisitor) VisitOperatorCall(e *graft.Expr) interface{} {
	return e.Operator + "(...)"
}

func (describeVisitor) VisitBinaryOp(e *graft.Expr) interface{} {
	return fmt.Sprintf("(%v %v)",
		graft.Accept(e.Left, describeVisitor{}),
		graft.Accept(e.Right, describeVisitor{}))
}

func (describeVisitor) VisitUnaryOp(e *graft.Expr) interface{} {
	return fmt.Sprintf("!%v", graft.Accept(e.Left, describeVisitor{}))
}

func (describeVisitor) VisitEnvVar(e *graft.Expr) interface{} {
	return "$" + e.Name
}

func (describeVisitor) VisitOther(*graft.Expr) interface{} {
	return "<other>"
}

// ExampleAccept dispatches "1 + 2" (Addition(Literal(1), Literal(2))) to a
// Visitor. Accept itself does not recurse; describeVisitor.VisitBinaryOp
// recurses explicitly by calling Accept again on e.Left and e.Right.
func ExampleAccept() {
	root, err := exprOf("1 + 2")
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(graft.Accept(root, describeVisitor{}))
	// Output:
	// (1 2)
}
