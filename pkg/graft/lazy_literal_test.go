package graft_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
	_ "github.com/fivetwenty-io/graft/pkg/graft/operators"
	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// lazyLiteralTree holds a literal and the value its expression text
// names, so a lazy expression that ran that text as code would read it.
func lazyLiteralTree() map[string]interface{} {
	return map[string]interface{}{
		"params": graft.NewLiteral(map[string]interface{}{
			"p": "meta.zq7secret",
			"e": "meta.zq7secret * 2",
		}),
		"meta": map[string]interface{}{"zq7secret": 21},
	}
}

// lazyCall parses src as one operator call and returns it as the
// expression a LazyExpression evaluates.
func lazyCall(t *testing.T, src string) *graft.Expr {
	t.Helper()
	op, err := graft.ParseOpcall(graft.EvalPhase, src)
	if err != nil || op == nil {
		t.Fatalf("ParseOpcall(%q): %v", src, err)
	}
	return &graft.Expr{Type: graft.OperatorCall, Operator: strings.Fields(strings.Trim(src, "() "))[0], Call: op}
}

// TestLazyExpressionKeepsLiterals checks that a LazyExpression treats a
// literal the way the engine does. A reference into one comes back as a
// literal, an operator result built from one comes back as a literal,
// and an operator that would read one as code still refuses.
func TestLazyExpressionKeepsLiterals(t *testing.T) {
	t.Run("reference", func(t *testing.T) {
		cursor, err := tree.ParseCursor("params.p")
		if err != nil {
			t.Fatal(err)
		}
		ev := &graft.Evaluator{Tree: lazyLiteralTree()}
		got, err := graft.NewLazyExpression(&graft.Expr{Type: graft.Reference, Reference: cursor}, ev).Evaluate()
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := got.(graft.LiteralValue); !ok {
			t.Fatalf("the reference gave %T, want a graft.LiteralValue", got)
		}
	})
	t.Run("builder", func(t *testing.T) {
		ev := &graft.Evaluator{Tree: lazyLiteralTree()}
		got, err := graft.NewLazyExpression(lazyCall(t, `(( concat params.p "" ))`), ev).Evaluate()
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := got.(graft.LiteralValue); !ok {
			t.Fatalf("concat gave %T, want a graft.LiteralValue", got)
		}
	})
	t.Run("plain builder", func(t *testing.T) {
		ev := &graft.Evaluator{Tree: lazyLiteralTree()}
		got, err := graft.NewLazyExpression(lazyCall(t, `(( concat "a" "b" ))`), ev).Evaluate()
		if err != nil {
			t.Fatal(err)
		}
		if got != "ab" {
			t.Fatalf("concat gave %#v, want \"ab\"", got)
		}
	})
	t.Run("refusal", func(t *testing.T) {
		ev := &graft.Evaluator{Tree: lazyLiteralTree()}
		_, err := graft.NewLazyExpression(lazyCall(t, `(( calc params.e ))`), ev).Evaluate()
		if !errors.Is(err, graft.ErrLiteralRefused) {
			t.Fatalf("calc gave %v, want a refusal", err)
		}
	})
	t.Run("refusal of a lazily built path", func(t *testing.T) {
		ev := &graft.Evaluator{Tree: lazyLiteralTree()}
		built, err := graft.NewLazyExpression(lazyCall(t, `(( concat params.e "" ))`), ev).Evaluate()
		if err != nil {
			t.Fatal(err)
		}
		ev.Tree["t"] = built
		_, err = graft.NewLazyExpression(lazyCall(t, `(( calc t ))`), ev).Evaluate()
		if !errors.Is(err, graft.ErrLiteralRefused) {
			t.Fatalf("calc over the built text gave %v, want a refusal", err)
		}
	})
}
