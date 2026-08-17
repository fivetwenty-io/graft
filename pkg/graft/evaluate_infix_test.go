package graft_test

import (
	"context"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
	_ "github.com/fivetwenty-io/graft/pkg/graft/operators" // registers +, -, *, /, %, ==, !=, <, <=, >, >=, &&, !
	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

func infixLit(v interface{}) *graft.Expr {
	return &graft.Expr{Type: graft.Literal, Literal: v}
}

func infixExpr(t graft.ExprType, left, right *graft.Expr) *graft.Expr {
	return &graft.Expr{Type: t, Left: left, Right: right}
}

// TestEvaluateInfix_AllWiredTypes exercises all 13 ExprType members the spec
// (cluster A1 §1.3) marks WIRE, confirming each dispatches to its registered
// operator rather than returning "unsupported expression type".
func TestEvaluateInfix_AllWiredTypes(t *testing.T) {
	ev := &graft.Evaluator{Tree: map[string]interface{}{}}

	cases := []struct {
		name     string
		expr     *graft.Expr
		expected interface{}
	}{
		{"Negate", &graft.Expr{Type: graft.Negate, Left: infixLit(true)}, false},
		{"Addition", infixExpr(graft.Addition, infixLit(int64(1)), infixLit(int64(2))), int64(3)},
		{"Subtraction", infixExpr(graft.Subtraction, infixLit(int64(5)), infixLit(int64(2))), int64(3)},
		{"Multiplication", infixExpr(graft.Multiplication, infixLit(int64(3)), infixLit(int64(4))), int64(12)},
		{"Division", infixExpr(graft.Division, infixLit(int64(10)), infixLit(int64(4))), 2.5},
		{"Modulo", infixExpr(graft.Modulo, infixLit(int64(10)), infixLit(int64(3))), int64(1)},
		{"Equal", infixExpr(graft.Equal, infixLit(int64(1)), infixLit(1.0)), true},
		{"NotEqual", infixExpr(graft.NotEqual, infixLit("5"), infixLit(int64(5))), true},
		{"LessThan", infixExpr(graft.LessThan, infixLit(int64(1)), infixLit(int64(2))), true},
		{"LessThanOrEqual", infixExpr(graft.LessThanOrEqual, infixLit(int64(2)), infixLit(int64(2))), true},
		{"GreaterThan", infixExpr(graft.GreaterThan, infixLit(int64(3)), infixLit(int64(2))), true},
		{"GreaterThanOrEqual", infixExpr(graft.GreaterThanOrEqual, infixLit(int64(2)), infixLit(int64(2))), true},
		{"LogicalAnd", infixExpr(graft.LogicalAnd, infixLit(true), infixLit(true)), true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			val, err := graft.EvaluateInfix(ev, tc.expr)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if val != tc.expected {
				t.Fatalf("expected %v (%T), got %v (%T)", tc.expected, tc.expected, val, val)
			}
		})
	}
}

// TestEvaluateInfix_StaysUnsupported pins the six ExprType members the spec
// deliberately leaves unwired (§1.3): they must keep the byte-identical
// "unsupported expression type for evaluation: %d" message.
func TestEvaluateInfix_StaysUnsupported(t *testing.T) {
	ev := &graft.Evaluator{Tree: map[string]interface{}{}}

	cases := []struct {
		name string
		typ  graft.ExprType
	}{
		{"List", graft.List},
		{"Or", graft.Or},
		{"RegexpMatch", graft.RegexpMatch},
		{"BoshVar", graft.BoshVar},
		{"VaultGroup", graft.VaultGroup},
		{"VaultChoice", graft.VaultChoice},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			expr := &graft.Expr{Type: tc.typ, Left: infixLit(1), Right: infixLit(2)}
			_, err := graft.EvaluateInfix(ev, expr)
			if err == nil {
				t.Fatalf("expected error for unsupported type %v", tc.typ)
			}
			want := "unsupported expression type for evaluation:"
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("expected error containing %q, got %q", want, err.Error())
			}
		})
	}
}

// TestEvaluateInfix_NestedArithmetic pins §0.2: nested infix nodes (an
// Addition whose Left is itself an Addition) must resolve through both
// Expr.Evaluate and operators.ResolveOperatorArgument without an
// "unsupported expression type" error on the inner node.
func TestEvaluateInfix_NestedArithmetic(t *testing.T) {
	ev := &graft.Evaluator{Tree: map[string]interface{}{}}

	// (1 + 2) + 3 == 6
	inner := infixExpr(graft.Addition, infixLit(int64(1)), infixLit(int64(2)))
	outer := infixExpr(graft.Addition, inner, infixLit(int64(3)))

	val, err := graft.EvaluateInfix(ev, outer)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != int64(6) {
		t.Fatalf("expected int64(6), got %v (%T)", val, val)
	}
}

// TestEvaluateInfix_ArithmeticCoercion pins the §1.5 coercion table for
// performArithmetic: the live legacy function, not the dormant type
// handlers (§0.1).
func TestEvaluateInfix_ArithmeticCoercion(t *testing.T) {
	ev := &graft.Evaluator{Tree: map[string]interface{}{}}

	t.Run("int op int stays int64", func(t *testing.T) {
		val, err := graft.EvaluateInfix(ev, infixExpr(graft.Addition, infixLit(int64(2)), infixLit(int64(3))))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := val.(int64); !ok {
			t.Fatalf("expected int64 result, got %T (%v)", val, val)
		}
	})

	t.Run("int add overflow promotes to float64", func(t *testing.T) {
		val, err := graft.EvaluateInfix(ev, infixExpr(graft.Addition,
			infixLit(int64(9223372036854775807)), infixLit(int64(1))))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := val.(float64); !ok {
			t.Fatalf("expected overflow to promote to float64, got %T (%v)", val, val)
		}
	})

	t.Run("any float operand yields float64", func(t *testing.T) {
		val, err := graft.EvaluateInfix(ev, infixExpr(graft.Addition, infixLit(int64(2)), infixLit(1.5)))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val != 3.5 {
			t.Fatalf("expected 3.5, got %v", val)
		}
	})

	t.Run("division always float64 even for exact int result", func(t *testing.T) {
		val, err := graft.EvaluateInfix(ev, infixExpr(graft.Division, infixLit(int64(10)), infixLit(int64(2))))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if _, ok := val.(float64); !ok {
			t.Fatalf("expected float64 result from division, got %T (%v)", val, val)
		}
		if val != 5.0 {
			t.Fatalf("expected 5.0, got %v", val)
		}
	})

	t.Run("nil operand treated as int64(0)", func(t *testing.T) {
		val, err := graft.EvaluateInfix(ev, infixExpr(graft.Addition, infixLit(nil), infixLit(int64(5))))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val != int64(5) {
			t.Fatalf("expected int64(5), got %v (%T)", val, val)
		}
	})

	t.Run("string operand on left errors", func(t *testing.T) {
		_, err := graft.EvaluateInfix(ev, infixExpr(graft.Addition, infixLit("abc"), infixLit(int64(1))))
		if err == nil {
			t.Fatalf("expected error")
		}
		want := "left operand: cannot use string 'abc' in arithmetic operation"
		if err.Error() != want {
			t.Fatalf("expected %q, got %q", want, err.Error())
		}
	})

	t.Run("string operand on right errors", func(t *testing.T) {
		_, err := graft.EvaluateInfix(ev, infixExpr(graft.Addition, infixLit(int64(1)), infixLit("abc")))
		if err == nil {
			t.Fatalf("expected error")
		}
		want := "right operand: cannot use string 'abc' in arithmetic operation"
		if err.Error() != want {
			t.Fatalf("expected %q, got %q", want, err.Error())
		}
	})

	t.Run("bool operand errors", func(t *testing.T) {
		_, err := graft.EvaluateInfix(ev, infixExpr(graft.Addition, infixLit(true), infixLit(int64(1))))
		if err == nil {
			t.Fatalf("expected error")
		}
		want := "left operand: cannot use boolean 'true' in arithmetic operation"
		if err.Error() != want {
			t.Fatalf("expected %q, got %q", want, err.Error())
		}
	})
}

// TestEvaluateInfix_DivModByZero pins §1.6.
func TestEvaluateInfix_DivModByZero(t *testing.T) {
	ev := &graft.Evaluator{Tree: map[string]interface{}{}}

	t.Run("division by zero", func(t *testing.T) {
		_, err := graft.EvaluateInfix(ev, infixExpr(graft.Division, infixLit(int64(5)), infixLit(int64(0))))
		if err == nil || err.Error() != "division by zero" {
			t.Fatalf("expected 'division by zero', got %v", err)
		}
	})

	t.Run("modulo by zero", func(t *testing.T) {
		_, err := graft.EvaluateInfix(ev, infixExpr(graft.Modulo, infixLit(int64(5)), infixLit(int64(0))))
		if err == nil || err.Error() != "modulo by zero" {
			t.Fatalf("expected 'modulo by zero', got %v", err)
		}
	})

	t.Run("modulo with float operand errors", func(t *testing.T) {
		_, err := graft.EvaluateInfix(ev, infixExpr(graft.Modulo, infixLit(5.0), infixLit(int64(2))))
		if err == nil || err.Error() != "modulo operation requires integer operands" {
			t.Fatalf("expected 'modulo operation requires integer operands', got %v", err)
		}
	})

	t.Run("5 / nil divides by coerced zero", func(t *testing.T) {
		_, err := graft.EvaluateInfix(ev, infixExpr(graft.Division, infixLit(int64(5)), infixLit(nil)))
		if err == nil || err.Error() != "division by zero" {
			t.Fatalf("expected 'division by zero', got %v", err)
		}
	})

	t.Run("nil / 5 is 0 with no error", func(t *testing.T) {
		val, err := graft.EvaluateInfix(ev, infixExpr(graft.Division, infixLit(nil), infixLit(int64(5))))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val != 0.0 {
			t.Fatalf("expected 0.0, got %v", val)
		}
	})
}

// TestEvaluateInfix_Equality pins §1.5's legacyEqual rules.
func TestEvaluateInfix_Equality(t *testing.T) {
	ev := &graft.Evaluator{Tree: map[string]interface{}{}}

	cases := []struct {
		name     string
		left     interface{}
		right    interface{}
		expected bool
	}{
		{"nil == nil is true", nil, nil, true},
		{"one nil is false", nil, int64(1), false},
		{"numeric cross-type 1 == 1.0 is true", int64(1), 1.0, true},
		{"string vs number is false", "5", int64(5), false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			val, err := graft.EvaluateInfix(ev, infixExpr(graft.Equal, infixLit(tc.left), infixLit(tc.right)))
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if val != tc.expected {
				t.Fatalf("expected %v, got %v", tc.expected, val)
			}
		})
	}
}

// TestEvaluateInfix_Ordering pins §1.5's legacyCompare rules, including the
// number-vs-string stringify-then-lexicographic oddity.
func TestEvaluateInfix_Ordering(t *testing.T) {
	ev := &graft.Evaluator{Tree: map[string]interface{}{}}

	t.Run("nil sorts lowest", func(t *testing.T) {
		val, err := graft.EvaluateInfix(ev, infixExpr(graft.LessThan, infixLit(nil), infixLit(int64(1))))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val != true {
			t.Fatalf("expected true, got %v", val)
		}
	})

	t.Run("string comparison is lexicographic byte order", func(t *testing.T) {
		val, err := graft.EvaluateInfix(ev, infixExpr(graft.LessThan, infixLit("Z"), infixLit("a")))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val != true {
			t.Fatalf("expected true ('Z' < 'a' in byte order), got %v", val)
		}
	})

	t.Run("number vs string stringified and compared lexicographically", func(t *testing.T) {
		// "9" < "10" numerically is false, but lexicographically "10" < "9".
		val, err := graft.EvaluateInfix(ev, infixExpr(graft.LessThan, infixLit(int64(10)), infixLit("9")))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val != true {
			t.Fatalf("expected true (stringified '10' < '9'), got %v", val)
		}
	})

	t.Run("uncomparable types error", func(t *testing.T) {
		_, err := graft.EvaluateInfix(ev, infixExpr(graft.LessThan,
			infixLit([]interface{}{1}), infixLit(map[string]interface{}{"a": 1})))
		if err == nil {
			t.Fatalf("expected error")
		}
		if !strings.Contains(err.Error(), "cannot compare") {
			t.Fatalf("expected 'cannot compare' error, got %v", err)
		}
	})
}

// TestEvaluateInfix_LogicalAndShortCircuits pins §1.4: && evaluates the
// right operand only if the left is truthy, and never touches the right
// operand's value even then — it returns IsTruthy(right), a bool.
func TestEvaluateInfix_LogicalAndShortCircuits(t *testing.T) {
	ev := &graft.Evaluator{Tree: map[string]interface{}{}}

	t.Run("left falsy short-circuits without evaluating right", func(t *testing.T) {
		// Right side references a path that does not exist; if evaluated it
		// would error. A correct short-circuit never reaches it.
		cursor, err := tree.ParseCursor("does.not.exist")
		if err != nil {
			t.Fatalf("failed to build cursor: %v", err)
		}
		right := &graft.Expr{Type: graft.Reference, Reference: cursor}

		val, err := graft.EvaluateInfix(ev, infixExpr(graft.LogicalAnd, infixLit(false), right))
		if err != nil {
			t.Fatalf("expected short-circuit to avoid the right-side error, got: %v", err)
		}
		if val != false {
			t.Fatalf("expected false, got %v", val)
		}
	})

	t.Run("returns truthiness of right operand, not its value", func(t *testing.T) {
		val, err := graft.EvaluateInfix(ev, infixExpr(graft.LogicalAnd, infixLit(true), infixLit("non-empty")))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if val != true {
			t.Fatalf("expected bool true (truthiness), got %v (%T)", val, val)
		}
	})
}

// TestEvaluateInfix_SinglePrefix pins B-14: evaluating an infix node through
// the full merge pipeline (the exprOperator wrapper set by parser.go) must
// produce exactly one "$.<path>: " prefix on the error, not a doubled one
// from both Opcall.Run and an inner wrapping.
func TestEvaluateInfix_SinglePrefix(t *testing.T) {
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	doc, err := engine.ParseYAML([]byte("x: (( 1 / 0 ))\n"))
	if err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}

	_, err = engine.Evaluate(context.Background(), doc)
	if err == nil {
		t.Fatalf("expected division-by-zero error")
	}

	msg := err.Error()
	if strings.Count(msg, "$.") != 1 {
		t.Fatalf("expected exactly one '$.' prefix, got %d in: %s", strings.Count(msg, "$."), msg)
	}
	if !strings.Contains(msg, "$.x: division by zero") {
		t.Fatalf("expected '$.x: division by zero', got: %s", msg)
	}
}

// TestEvaluateInfix_EndToEndMerge exercises the worked example from the
// spec's "Done means" line for Stage A-i: (( 1 + 2 )) and
// (( env == "production" )) evaluate through a real merge.
func TestEvaluateInfix_EndToEndMerge(t *testing.T) {
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	doc, err := engine.ParseYAML([]byte("x: (( 1 + 2 ))\n"))
	if err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}

	evaluated, err := engine.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	got, err := evaluated.Get("x")
	if err != nil {
		t.Fatalf("failed to read x: %v", err)
	}
	if got != int64(3) {
		t.Fatalf("expected int64(3), got %v (%T)", got, got)
	}
}
