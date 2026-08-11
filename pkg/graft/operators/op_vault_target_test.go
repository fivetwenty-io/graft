package operators

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// TestVaultTargetSelectsPooledClient pins the spec cluster A7 §7 fix: a
// non-empty "@target" must select the pooled, target-specific Vault client
// (internal/backends/vault.DefaultPool.GetClient) rather than being parsed
// and silently discarded. An httptest server stands in for a second Vault
// instance, reachable only via VAULT_<TARGET>_ADDR/TOKEN, so a correct read
// through it — rather than an error or a read from the (unconfigured)
// default instance — proves the target was actually used.
func TestVaultTargetSelectsPooledClient(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/secret/a7-target-demo" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data": {"password": "target-specific-value"}}`))
	}))
	defer srv.Close()

	const target = "a7targetdemo"
	t.Setenv("VAULT_A7TARGETDEMO_ADDR", srv.URL)
	t.Setenv("VAULT_A7TARGETDEMO_TOKEN", "test-token")

	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	doc, err := engine.ParseYAML([]byte(`secret: (( vault@` + target + ` "secret/a7-target-demo:password" ))` + "\n"))
	if err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}

	evaluated, evalErr := engine.Evaluate(context.Background(), doc)
	if evalErr != nil {
		t.Fatalf("unexpected error: %v", evalErr)
	}

	got, getErr := evaluated.Get("secret")
	if getErr != nil {
		t.Fatalf("failed to read secret: %v", getErr)
	}
	if got != "target-specific-value" {
		t.Fatalf("expected the value read through the target-specific client, got %v", got)
	}
}

// TestVaultTargetCacheKeysAreNamespaced pins the second half of the spec
// cluster A7 §7 fix, which the target-selection test above cannot reach: the
// same secret path read through two different targets must produce two
// different values, not one cached value serving both.
//
// vault.SecretCache is a process-global keyed by the secret path. Before the
// cache key was namespaced by target, the first lookup of "secret/<path>"
// populated that key and every later lookup of the same path — against any
// instance — hit the cache and returned the first instance's secret. Two
// httptest servers standing in for two Vault instances, serving the same
// path with different values, make that collision observable: without
// namespacing, both keys below come back with whichever server was read
// first.
func TestVaultTargetCacheKeysAreNamespaced(t *testing.T) {
	const secretPath = "/v1/secret/a7-cache-namespace"

	newInstance := func(value string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != secretPath {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data": {"password": "` + value + `"}}`))
		}))
	}

	alpha := newInstance("value-from-alpha")
	defer alpha.Close()
	beta := newInstance("value-from-beta")
	defer beta.Close()

	t.Setenv("VAULT_A7CACHEALPHA_ADDR", alpha.URL)
	t.Setenv("VAULT_A7CACHEALPHA_TOKEN", "test-token")
	t.Setenv("VAULT_A7CACHEBETA_ADDR", beta.URL)
	t.Setenv("VAULT_A7CACHEBETA_TOKEN", "test-token")

	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	doc, err := engine.ParseYAML([]byte(
		`fromAlpha: (( vault@a7cachealpha "secret/a7-cache-namespace:password" ))` + "\n" +
			`fromBeta: (( vault@a7cachebeta "secret/a7-cache-namespace:password" ))` + "\n"))
	if err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}

	evaluated, evalErr := engine.Evaluate(context.Background(), doc)
	if evalErr != nil {
		t.Fatalf("unexpected error: %v", evalErr)
	}

	gotAlpha, err := evaluated.Get("fromAlpha")
	if err != nil {
		t.Fatalf("failed to read fromAlpha: %v", err)
	}
	gotBeta, err := evaluated.Get("fromBeta")
	if err != nil {
		t.Fatalf("failed to read fromBeta: %v", err)
	}

	if gotAlpha != "value-from-alpha" {
		t.Fatalf("expected the alpha instance's value, got %v", gotAlpha)
	}
	if gotBeta != "value-from-beta" {
		t.Fatalf("expected the beta instance's value, got %v (a cross-target cache hit)", gotBeta)
	}
}

// TestVaultTargetNotConfiguredErrorsDistinctly proves the target is not
// silently ignored: a target with no VAULT_<TARGET>_ADDR/TOKEN environment
// variables produces a target-specific "not found" error, not the generic
// default-client-initialization error the no-target path would produce.
func TestVaultTargetNotConfiguredErrorsDistinctly(t *testing.T) {
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	doc, err := engine.ParseYAML([]byte(`secret: (( vault@a7nonexistenttarget "secret/x:y" ))` + "\n"))
	if err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}

	_, evalErr := engine.Evaluate(context.Background(), doc)
	if evalErr == nil {
		t.Fatalf("expected an error for an unconfigured target")
	}

	msg := extractOperatorErrorMessage(evalErr)
	if !strings.Contains(msg, "a7nonexistenttarget") {
		t.Fatalf("expected the error to name the target, got: %s", msg)
	}
	if strings.Contains(msg, "Error during Vault client initialization") {
		t.Fatalf("target lookup must not fall back to the default-client error path, got: %s", msg)
	}
}

// TestGrabTargetErrors pins §7.2: once Opcall carries a target, an operator
// that does not support one must error rather than silently ignoring it —
// the exact regression the spec calls out for "(( grab@prod meta ))".
func TestGrabTargetErrors(t *testing.T) {
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	doc, err := engine.ParseYAML([]byte("meta: hello\nx: (( grab@prod meta ))\n"))
	if err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}

	_, evalErr := engine.Evaluate(context.Background(), doc)
	if evalErr == nil {
		t.Fatalf("expected an error: grab does not support @target")
	}

	msg := extractOperatorErrorMessage(evalErr)
	want := "grab operator does not support an @target"
	if msg != want {
		t.Fatalf("expected %q, got %q", want, msg)
	}
}

// TestVaultTargetFormARejected pins the end-to-end path for form (a),
// complementing the parser-level pin in pkg/graft/opcall_target_test.go.
func TestVaultTargetFormARejected(t *testing.T) {
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	doc, err := engine.ParseYAML([]byte(`secret: (( vault prod@"secret/foo:bar" ))` + "\n"))
	if err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}

	_, evalErr := engine.Evaluate(context.Background(), doc)
	if evalErr == nil {
		t.Fatalf("expected form (a) to be rejected")
	}
	want := `vault target must be written as (( vault@<target> "path:key" )), not (( vault <target>@"path:key" ))`
	if !strings.Contains(evalErr.Error(), want) {
		t.Fatalf("expected the redirecting message, got: %v", evalErr)
	}
}

// TestAwsTargetNotConfiguredErrorsDistinctly and
// TestNatsTargetNotConfiguredErrorsDistinctly mirror the vault proof for
// the other two operators the spec brings into scope (§7.2): the target
// must reach the operator (a target-specific error) rather than being
// silently dropped.
func TestAwsTargetNotConfiguredErrorsDistinctly(t *testing.T) {
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	doc, err := engine.ParseYAML([]byte(`secret: (( awsparam@a7nonexistenttarget "/some/path" ))` + "\n"))
	if err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}

	_, evalErr := engine.Evaluate(context.Background(), doc)
	if evalErr == nil {
		t.Fatalf("expected an error for an unconfigured AWS target")
	}
	msg := extractOperatorErrorMessage(evalErr)
	if !strings.Contains(msg, "a7nonexistenttarget") {
		t.Fatalf("expected the error to name the target, got: %s", msg)
	}
}

func TestNatsTargetNotConfiguredErrorsDistinctly(t *testing.T) {
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}

	doc, err := engine.ParseYAML([]byte(`secret: (( nats@a7nonexistenttarget "kv:bucket/key" ))` + "\n"))
	if err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}

	_, evalErr := engine.Evaluate(context.Background(), doc)
	if evalErr == nil {
		t.Fatalf("expected an error for an unconfigured NATS target")
	}
	msg := extractOperatorErrorMessage(evalErr)
	if !strings.Contains(msg, "a7nonexistenttarget") {
		t.Fatalf("expected the error to name the target, got: %s", msg)
	}
}
