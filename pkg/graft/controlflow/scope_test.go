package controlflow

import (
	"fmt"
	"testing"

	_ "github.com/fivetwenty-io/graft/pkg/graft/operators" // registers grab, concat, ==, &&, etc.
)

func TestEvalExpr_BareIdentifierResolvesAsReference(t *testing.T) {
	e := newEnv(map[string]interface{}{
		"services": []interface{}{"api", "worker"},
	})
	val, err := evalExpr("services", e, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	list, ok := val.([]interface{})
	if !ok || len(list) != 2 || list[0] != "api" {
		t.Fatalf("got %#v, want [api worker]", val)
	}
}

func TestEvalExpr_DottedReference(t *testing.T) {
	e := newEnv(map[string]interface{}{
		"features": map[string]interface{}{"debug": true},
	})
	val, err := evalExpr("features.debug", e, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != true {
		t.Fatalf("got %#v, want true", val)
	}
}

func TestEvalExpr_InfixComparison(t *testing.T) {
	e := newEnv(map[string]interface{}{"environment": "production"})
	val, err := evalExpr(`environment == "production"`, e, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != true {
		t.Fatalf("got %#v, want true", val)
	}
}

func TestEvalExpr_ExplicitGrab(t *testing.T) {
	e := newEnv(map[string]interface{}{"environment": "production"})
	val, err := evalExpr(`grab environment == "production"`, e, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != true {
		t.Fatalf("got %#v, want true", val)
	}
}

func TestEvalExpr_MissingReferenceErrors(t *testing.T) {
	e := newEnv(map[string]interface{}{})
	_, err := evalExpr("nonexistent_key", e, "controlflow.if.L1")
	if err == nil {
		t.Fatal("expected an error for a missing reference")
	}
}

func TestEvalTruthy(t *testing.T) {
	cases := []struct {
		expr string
		want bool
	}{
		{"a", true},  // non-empty string "true-ish" scalar? see below: a=1 (nonzero int)
		{"b", false}, // b=0
		{"c", false}, // c=false
		{"d", true},  // d="false" (non-empty string is truthy, even the string "false")
		{"e", false}, // e=[] (empty list)
		{"e2", true}, // e2=[1] (non-empty list)
	}
	scope := map[string]interface{}{
		"a": int64(1), "b": int64(0), "c": false, "d": "false",
		"e": []interface{}{}, "e2": []interface{}{1},
	}
	e := newEnv(scope)
	for _, c := range cases {
		got, err := evalTruthy(c.expr, e, "test")
		if err != nil {
			t.Fatalf("evalTruthy(%q): unexpected error: %v", c.expr, err)
		}
		if got != c.want {
			t.Errorf("evalTruthy(%q) = %v, want %v", c.expr, got, c.want)
		}
	}
}

func TestEnv_WithBindingShadowsScopeAndOuterBindings(t *testing.T) {
	base := newEnv(map[string]interface{}{"svc": "from-scope"})
	inner := base.withBinding("svc", "from-loop")

	val, err := evalExpr("svc", inner, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val != "from-loop" {
		t.Fatalf("got %v, want from-loop (binding should shadow scope)", val)
	}

	// The original env (and scope) must be unmodified.
	val2, err := evalExpr("svc", base, "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if val2 != "from-scope" {
		t.Fatalf("base env mutated: got %v, want from-scope", val2)
	}
}

func TestBuildPrescanScope_StripsBlocksAndEvaluatesRemainder(t *testing.T) {
	src := "environment: production\n" +
		"greeting: (( concat \"hello \" environment ))\n" +
		"(( if environment == \"production\" ))\n" +
		"replicas: 5\n" +
		"(( fi ))\n"
	lines := classifyLines(src)
	top, err := parseDocument(lines)
	if err != nil {
		t.Fatalf("parseDocument: %v", err)
	}
	scope, err := buildPrescanScope(lines, top)
	if err != nil {
		t.Fatalf("buildPrescanScope: %v", err)
	}
	if scope["environment"] != "production" {
		t.Errorf("scope[environment] = %#v, want production", scope["environment"])
	}
	if scope["greeting"] != "hello production" {
		t.Errorf("scope[greeting] = %#v, want %q (operators inside the static remainder should be evaluated)", scope["greeting"], "hello production")
	}
	if _, present := scope["replicas"]; present {
		t.Errorf("scope should not contain replicas: it was only inside the stripped if-block")
	}
}

func TestBuildPrescanScope_UnresolvableOperatorLeavesKeyAbsent(t *testing.T) {
	src := "a: (( grab does.not.exist ))\n" +
		"b: 2\n" +
		"(( if b == 2 ))\n" +
		"c: 3\n" +
		"(( fi ))\n"
	lines := classifyLines(src)
	top, err := parseDocument(lines)
	if err != nil {
		t.Fatalf("parseDocument: %v", err)
	}
	scope, err := buildPrescanScope(lines, top)
	if err != nil {
		t.Fatalf("buildPrescanScope should not itself fail when an operator is unresolvable: %v", err)
	}
	if _, present := scope["a"]; present {
		t.Errorf("scope[a] should be absent (its grab could not resolve), got %#v", scope["a"])
	}
	if fmt.Sprintf("%v", scope["b"]) != "2" {
		t.Errorf("scope[b] = %#v, want 2", scope["b"])
	}
}
