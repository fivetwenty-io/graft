package graft_test

import (
	"os"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

func TestExprEvaluate_EnvVar_SimpleString(t *testing.T) {
	const envName = "TEST_GRAFT_SIMPLE_STRING"
	t.Setenv(envName, "hello world")

	expr := &graft.Expr{
		Type: graft.EnvVar,
		Name: envName,
	}

	result, err := expr.Evaluate(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "hello world" {
		t.Errorf("expected %q, got %v", "hello world", result)
	}
}

func TestExprEvaluate_EnvVar_EmptyValue(t *testing.T) {
	const envName = "TEST_GRAFT_EMPTY_VALUE"
	t.Setenv(envName, "")

	expr := &graft.Expr{
		Type: graft.EnvVar,
		Name: envName,
	}

	result, err := expr.Evaluate(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty string, got %v", result)
	}
}

func TestExprEvaluate_EnvVar_UnsetVariable(t *testing.T) {
	const envName = "TEST_GRAFT_UNSET_VARIABLE"
	if err := os.Unsetenv(envName); err != nil {
		t.Fatalf("unsetenv %s: %v", envName, err)
	}

	expr := &graft.Expr{
		Type: graft.EnvVar,
		Name: envName,
	}

	result, err := expr.Evaluate(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty string for unset var, got %v", result)
	}
}

func TestExprEvaluate_EnvVar_BooleanValue(t *testing.T) {
	const envName = "TEST_GRAFT_BOOLEAN_VALUE"
	t.Setenv(envName, "true")

	expr := &graft.Expr{
		Type: graft.EnvVar,
		Name: envName,
	}

	result, err := expr.Evaluate(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	boolVal, ok := result.(bool)
	if !ok {
		t.Fatalf("expected bool, got %T (%v)", result, result)
	}
	if !boolVal {
		t.Errorf("expected true, got false")
	}
}

func TestExprEvaluate_EnvVar_NumericValue(t *testing.T) {
	const envName = "TEST_GRAFT_NUMERIC_VALUE"
	t.Setenv(envName, "42")

	expr := &graft.Expr{
		Type: graft.EnvVar,
		Name: envName,
	}

	result, err := expr.Evaluate(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	intVal, ok := result.(int)
	if !ok {
		t.Fatalf("expected int, got %T (%v)", result, result)
	}
	if intVal != 42 {
		t.Errorf("expected 42, got %d", intVal)
	}
}

func TestExprEvaluate_EnvVar_JSONValue(t *testing.T) {
	const envName = "TEST_GRAFT_JSON_VALUE"
	t.Setenv(envName, `{"key":"value"}`)

	expr := &graft.Expr{
		Type: graft.EnvVar,
		Name: envName,
	}

	result, err := expr.Evaluate(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mapVal, ok := result.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map[string]interface{}, got %T (%v)", result, result)
	}
	if mapVal["key"] != "value" {
		t.Errorf("expected map[key]=value, got %v", mapVal["key"])
	}
}
