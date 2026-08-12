package graft_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
	_ "github.com/fivetwenty-io/graft/pkg/graft/operators" // registers vault/awsparam/awssecret/nats operators
)

// TestWithVault_ObservableEffect proves WithVault's VaultConfig actually
// reaches a live request: an httptest server standing in for Vault serves
// a KV v1 response only at the exact path/token this test configures, so a
// correct read (rather than a connection error or a request to the wrong
// address) proves the configured Address/Token were used, not ignored.
func TestWithVault_ObservableEffect(t *testing.T) {
	var gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Vault-Token")
		if r.URL.Path != "/v1/secret/c1b-demo" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data": {"password": "value-from-configured-backend"}}`))
	}))
	defer srv.Close()

	engine, err := graft.NewEngine(
		graft.WithBackendRegistry(true),
		graft.WithVault(graft.VaultConfig{Address: srv.URL, Token: "c1b-test-token"}),
	)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	doc, err := engine.ParseYAML([]byte(`secret: (( vault "secret/c1b-demo:password" ))` + "\n"))
	if err != nil {
		t.Fatalf("ParseYAML failed: %v", err)
	}

	evaluated, err := engine.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	got, err := evaluated.Get("secret")
	if err != nil {
		t.Fatalf("Get(secret) failed: %v", err)
	}
	if got != "value-from-configured-backend" {
		t.Fatalf("expected value read through the WithVault-configured backend, got %v", got)
	}
	if gotToken != "c1b-test-token" {
		t.Fatalf("expected the configured Token to reach the request, got X-Vault-Token=%q", gotToken)
	}
}

// TestWithVault_FlagOffFallsBackToBuiltIn proves WithVault alone (without
// WithBackendRegistry(true)) does not change evaluation: FeatureBackendRegistry
// stays off by default, so the vault operator never consults the registry
// and the httptest server here is never contacted - the missing-env-var
// error from the built-in path proves that.
func TestWithVault_FlagOffFallsBackToBuiltIn(t *testing.T) {
	contacted := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contacted = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data": {"password": "should-not-be-read"}}`))
	}))
	defer srv.Close()

	t.Setenv("VAULT_ADDR", "")
	t.Setenv("VAULT_TOKEN", "")
	t.Setenv("HOME", t.TempDir()) // no .vault-token / .svtoken fallback files

	engine, err := graft.NewEngine(graft.WithVault(graft.VaultConfig{Address: srv.URL, Token: "unused"}))
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	doc, err := engine.ParseYAML([]byte(`secret: (( vault "secret/c1b-demo:password" ))` + "\n"))
	if err != nil {
		t.Fatalf("ParseYAML failed: %v", err)
	}

	if _, err := engine.Evaluate(context.Background(), doc); err == nil {
		t.Fatal("expected an error from the built-in (unconfigured) Vault path, got nil")
	}
	if contacted {
		t.Fatal("the WithVault-configured httptest server was contacted despite FeatureBackendRegistry being off")
	}
}

// TestWithVault_NotFoundMapsToContractShape proves a WithVault-backed miss
// still produces the Genesis-compatibility-pinned "secret <path> not
// found" shape (docs/spruce/genesis-compat-contract.md), not
// vaultOptionBackend's own internal error text.
func TestWithVault_NotFoundMapsToContractShape(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Real Vault returns 404 with an empty body for a missing KV v1
		// path; the vault SDK's ParseRawResponseAndCloseBody treats an
		// empty 404 body as io.EOF and returns (nil, nil), which is what
		// this test needs to exercise vaultOptionBackend's own nil-secret
		// -> ErrBackendNotFound mapping. A non-empty, non-JSON 404 body
		// (e.g. net/http's default "404 page not found" text) makes the
		// SDK attempt - and fail - a JSON decode instead, which is a
		// different failure mode than the one under test here.
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	engine, err := graft.NewEngine(
		graft.WithBackendRegistry(true),
		graft.WithVault(graft.VaultConfig{Address: srv.URL, Token: "t"}),
	)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	doc, err := engine.ParseYAML([]byte(`secret: (( vault "secret/missing:password" ))` + "\n"))
	if err != nil {
		t.Fatalf("ParseYAML failed: %v", err)
	}

	_, evalErr := engine.Evaluate(context.Background(), doc)
	if evalErr == nil {
		t.Fatal("expected an error for a missing secret, got nil")
	}
	msg := evalErr.Error()
	if !strings.Contains(msg, "secret ") || !strings.Contains(msg, " not found") {
		t.Fatalf("expected the contract-pinned \"secret ... not found\" shape, got: %s", msg)
	}
}

// TestWithVaultTarget_SelectsPerTargetConfig proves WithVaultTarget's
// configuration is used for "@target" lookups, independent of - and
// distinct from - WithVault's default configuration: two httptest servers
// serve different values at the same store path, and a target-qualified
// vault@ call must read the WithVaultTarget one.
func TestWithVaultTarget_SelectsPerTargetConfig(t *testing.T) {
	newSrv := func(value string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/secret/c1b-target-demo" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data": {"password": "` + value + `"}}`))
		}))
	}

	defaultSrv := newSrv("default-value")
	defer defaultSrv.Close()
	targetSrv := newSrv("target-specific-value")
	defer targetSrv.Close()

	engine, err := graft.NewEngine(
		graft.WithBackendRegistry(true),
		graft.WithVault(graft.VaultConfig{Address: defaultSrv.URL, Token: "default-token"}),
		graft.WithVaultTarget("prod", graft.VaultConfig{Address: targetSrv.URL, Token: "prod-token"}),
	)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	doc, err := engine.ParseYAML([]byte(`secret: (( vault@prod "secret/c1b-target-demo:password" ))` + "\n"))
	if err != nil {
		t.Fatalf("ParseYAML failed: %v", err)
	}

	evaluated, err := engine.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	got, err := evaluated.Get("secret")
	if err != nil {
		t.Fatalf("Get(secret) failed: %v", err)
	}
	if got != "target-specific-value" {
		t.Fatalf("expected the WithVaultTarget-configured value, got %v", got)
	}
}

// TestWithVault_LastCallWins proves a second WithVault call for the engine
// replaces the first's configuration rather than being ignored or merged.
func TestWithVault_LastCallWins(t *testing.T) {
	newSrv := func(value string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data": {"k": "` + value + `"}}`))
		}))
	}
	first := newSrv("first")
	defer first.Close()
	second := newSrv("second")
	defer second.Close()

	engine, err := graft.NewEngine(
		graft.WithBackendRegistry(true),
		graft.WithVault(graft.VaultConfig{Address: first.URL, Token: "t1"}),
		graft.WithVault(graft.VaultConfig{Address: second.URL, Token: "t2"}),
	)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	doc, err := engine.ParseYAML([]byte(`secret: (( vault "path:k" ))` + "\n"))
	if err != nil {
		t.Fatalf("ParseYAML failed: %v", err)
	}
	evaluated, err := engine.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	got, _ := evaluated.Get("secret")
	if got != "second" {
		t.Fatalf("expected the second WithVault call to win, got %v", got)
	}
}
