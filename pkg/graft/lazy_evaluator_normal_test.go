package graft

import (
	"testing"
)

func TestEvaluateExpressionNormally_Literal(t *testing.T) {
	expr := &Expr{
		Type:    Literal,
		Literal: "hello",
	}
	evaluator := &Evaluator{Tree: map[string]interface{}{}}

	result, err := evaluateExpressionNormally(expr, evaluator)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if result != "hello" {
		t.Errorf("expected %q, got %v", "hello", result)
	}
}

func TestEvaluateExpressionNormally_IntLiteral(t *testing.T) {
	expr := &Expr{
		Type:    Literal,
		Literal: 42,
	}
	evaluator := &Evaluator{Tree: map[string]interface{}{}}

	result, err := evaluateExpressionNormally(expr, evaluator)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if result != 42 {
		t.Errorf("expected 42, got %v", result)
	}
}

func TestEvaluateExpressionNormally_DelegatesToEvaluateExpr(t *testing.T) {
	// Use a literal as the simplest delegation case; EvaluateExpr wraps it in a Response
	expr := &Expr{
		Type:    Literal,
		Literal: "delegated",
	}
	evaluator := &Evaluator{Tree: map[string]interface{}{}}

	// EvaluateExpr returns resp.Value for a literal; evaluateExpressionNormally must unwrap it
	result, err := evaluateExpressionNormally(expr, evaluator)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if result != "delegated" {
		t.Errorf("expected %q, got %v", "delegated", result)
	}
}

func TestEvaluateExpressionNormally_NilExpr(t *testing.T) {
	evaluator := &Evaluator{Tree: map[string]interface{}{}}

	_, err := evaluateExpressionNormally(nil, evaluator)
	if err == nil {
		t.Fatal("expected an error for nil expr, got nil")
	}
}
