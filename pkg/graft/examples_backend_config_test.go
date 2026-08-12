package graft_test

// Testable examples backing docs/developer-guide/library-api/options.md's
// "Backend Configuration Options" section: WithVault and WithAWS. Both
// need something to actually connect to, so each spins up a small
// httptest.Server standing in for the real Vault/SSM endpoint - not
// something a real caller does (a real caller points VaultConfig.Address/
// AWSConfig.Endpoint at their real Vault/AWS endpoint instead), but
// necessary for this example to compile, run, and produce a deterministic
// // Output: like every other example in this file.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"

	"github.com/fivetwenty-io/graft/pkg/graft"
	_ "github.com/fivetwenty-io/graft/pkg/graft/operators" // registers vault/awsparam/awssecret/nats operators
)

// ExampleWithVault configures a real (here, httptest-backed) Vault
// connection per-engine, rather than through VAULT_ADDR/VAULT_TOKEN
// environment variables.
func ExampleWithVault() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data": {"password": "s3cr3t"}}`))
	}))
	defer srv.Close()

	engine, err := graft.NewEngine(
		graft.WithBackendRegistry(true),
		graft.WithVault(graft.VaultConfig{Address: srv.URL, Token: "s.example"}),
	)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	doc, err := engine.ParseYAML([]byte("password: (( vault \"secret/db:password\" ))\n"))
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	result, err := engine.Evaluate(context.Background(), doc)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	password, _ := result.GetString("password")
	fmt.Println(password)
	// Output:
	// s3cr3t
}

// ExampleWithAWS configures a real (here, httptest-backed) AWS SSM
// Parameter Store connection per-engine, rather than through AWS_PROFILE/
// AWS_REGION environment variables. One WithAWS call configures both the
// awsparam and awssecret operators; this example only exercises awsparam.
func ExampleWithAWS() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		_, _ = w.Write([]byte(`{"Parameter":{"Name":"/app/password","Value":"s3cr3t"}}`))
	}))
	defer srv.Close()

	engine, err := graft.NewEngine(
		graft.WithBackendRegistry(true),
		graft.WithAWS(graft.AWSConfig{Region: "us-east-1", Endpoint: srv.URL, SkipAuth: true}),
	)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	doc, err := engine.ParseYAML([]byte("password: (( awsparam \"/app/password\" ))\n"))
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	result, err := engine.Evaluate(context.Background(), doc)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	password, _ := result.GetString("password")
	fmt.Println(password)
	// Output:
	// s3cr3t
}
