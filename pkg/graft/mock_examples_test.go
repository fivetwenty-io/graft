package graft_test

// Testable examples backing docs/developer-guide/testing.md's Mock Engine
// section and docs/developer-guide/custom-operators.md's OperatorFunc/
// NewTestEvaluator sections (go test ./pkg/graft/ -run Example).

import (
	"context"
	"fmt"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// ExampleNewMockEngine seeds a vault value and evaluates a document that
// references it, with no real Vault reachable - the pattern
// docs/developer-guide/testing.md's "Mock Engine" section documents.
func ExampleNewMockEngine() {
	engine := graft.NewMockEngine()
	engine.MockVault("secret/db:password", "test-password")

	doc, err := engine.ParseYAML([]byte(`
database:
  password: (( vault "secret/db:password" ))
`))
	if err != nil {
		fmt.Println("parse error:", err)
		return
	}

	result, err := engine.Evaluate(context.Background(), doc)
	if err != nil {
		fmt.Println("evaluate error:", err)
		return
	}

	fmt.Println(result.String("database.password"))
	// Output:
	// test-password
}

// ExampleMockEngine_vaultDefault shows the "|| default" fallback the vault
// operator supports working unchanged against a MockEngine: an unseeded
// path is a "not found" backend error, which the vault operator's
// existing default-value handling catches exactly as it does against a
// real Vault instance.
func ExampleMockEngine_vaultDefault() {
	engine := graft.NewMockEngine()

	doc, err := engine.ParseYAML([]byte(`password: (( vault "secret/missing:pass" || "default" ))`))
	if err != nil {
		fmt.Println("parse error:", err)
		return
	}

	result, err := engine.Evaluate(context.Background(), doc)
	if err != nil {
		fmt.Println("evaluate error:", err)
		return
	}

	fmt.Println(result.String("password"))
	// Output:
	// default
}

// ExampleMockEngine_verifyingCalls shows VaultCalls/WasVaultCalled -
// docs/developer-guide/testing.md's "Verifying Calls" section - confirming
// which paths a test's document actually looked up.
func ExampleMockEngine_verifyingCalls() {
	engine := graft.NewMockEngine()
	engine.MockVault("secret/db:password", "test-password")
	engine.MockVault("secret/db:username", "admin")

	doc, err := engine.ParseYAML([]byte(`
database:
  password: (( vault "secret/db:password" ))
  username: (( vault "secret/db:username" ))
`))
	if err != nil {
		fmt.Println("parse error:", err)
		return
	}

	if _, err := engine.Evaluate(context.Background(), doc); err != nil {
		fmt.Println("evaluate error:", err)
		return
	}

	fmt.Println("vault calls:", len(engine.VaultCalls()))
	fmt.Println("password path called:", engine.WasVaultCalled("secret/db:password"))
	fmt.Println("unrelated path called:", engine.WasVaultCalled("secret/other:key"))
	// Output:
	// vault calls: 2
	// password path called: true
	// unrelated path called: false
}

// ExampleOperatorFunc shows the custom-operators.md "Using OperatorFunc"
// pattern: adapting a plain function into an Operator and registering it.
func ExampleOperatorFunc() {
	engine, err := graft.NewEngine(
		graft.WithCustomOperator("shout", &graft.OperatorFunc{
			OpPhase: graft.EvalPhase,
			Fn: func(ev *graft.Evaluator, args []*graft.Expr) (*graft.Response, error) {
				resolved, err := graft.EvaluateOperatorArgs(ev, args)
				if err != nil {
					return nil, err
				}
				return &graft.Response{Type: graft.Replace, Value: fmt.Sprintf("%v!", resolved[0])}, nil
			},
		}),
	)
	if err != nil {
		fmt.Println("new engine error:", err)
		return
	}

	doc, err := engine.ParseYAML([]byte(`greeting: (( shout "hello" ))`))
	if err != nil {
		fmt.Println("parse error:", err)
		return
	}

	result, err := engine.Evaluate(context.Background(), doc)
	if err != nil {
		fmt.Println("evaluate error:", err)
		return
	}

	fmt.Println(result.String("greeting"))
	// Output:
	// hello!
}
