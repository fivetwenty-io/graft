package graft_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
	_ "github.com/fivetwenty-io/graft/pkg/graft/operators" // registers vault/awsparam/awssecret/nats operators
)

// TestWithAWS_ObservableEffect_Param proves WithAWS's AWSConfig actually
// reaches a live SSM GetParameter request: an httptest server standing in
// for SSM serves a fixed response, and Endpoint/Region/SkipAuth are the
// only way the AWS SDK is directed at it, so a correct read proves the
// configured AWSConfig was used, not ignored.
func TestWithAWS_ObservableEffect_Param(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		_, _ = w.Write([]byte(`{"Parameter":{"Name":"/c1b/demo","Value":"value-from-configured-backend"}}`))
	}))
	defer srv.Close()

	engine, err := graft.NewEngine(
		graft.WithBackendRegistry(true),
		graft.WithAWS(graft.AWSConfig{Region: "us-east-1", Endpoint: srv.URL, SkipAuth: true}),
	)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	doc, err := engine.ParseYAML([]byte(`value: (( awsparam "/c1b/demo" ))` + "\n"))
	if err != nil {
		t.Fatalf("ParseYAML failed: %v", err)
	}

	evaluated, err := engine.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	got, err := evaluated.Get("value")
	if err != nil {
		t.Fatalf("Get(value) failed: %v", err)
	}
	if got != "value-from-configured-backend" {
		t.Fatalf("expected value read through the WithAWS-configured backend, got %v", got)
	}
}

// TestWithAWS_ObservableEffect_Secret mirrors the Param test above for
// awssecret / Secrets Manager, proving the same WithAWS call configures
// both operators' backends.
func TestWithAWS_ObservableEffect_Secret(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		_, _ = w.Write([]byte(`{"Name":"db","SecretString":"value-from-configured-backend"}`))
	}))
	defer srv.Close()

	engine, err := graft.NewEngine(
		graft.WithBackendRegistry(true),
		graft.WithAWS(graft.AWSConfig{Region: "us-east-1", Endpoint: srv.URL, SkipAuth: true}),
	)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	doc, err := engine.ParseYAML([]byte(`value: (( awssecret "db" ))` + "\n"))
	if err != nil {
		t.Fatalf("ParseYAML failed: %v", err)
	}

	evaluated, err := engine.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	got, err := evaluated.Get("value")
	if err != nil {
		t.Fatalf("Get(value) failed: %v", err)
	}
	if got != "value-from-configured-backend" {
		t.Fatalf("expected value read through the WithAWS-configured backend, got %v", got)
	}
}

// TestWithAWS_FlagOffFallsBackToBuiltIn mirrors
// TestWithVault_FlagOffFallsBackToBuiltIn: WithAWS alone (without
// WithBackendRegistry(true)) must not change evaluation.
func TestWithAWS_FlagOffFallsBackToBuiltIn(t *testing.T) {
	contacted := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		contacted = true
		w.Header().Set("Content-Type", "application/x-amz-json-1.1")
		_, _ = w.Write([]byte(`{"Parameter":{"Name":"/c1b/demo","Value":"should-not-be-read"}}`))
	}))
	defer srv.Close()

	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_ACCESS_KEY_ID", "")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "")
	t.Setenv("HOME", t.TempDir())

	engine, err := graft.NewEngine(graft.WithAWS(graft.AWSConfig{Region: "us-east-1", Endpoint: srv.URL, SkipAuth: true}))
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	doc, err := engine.ParseYAML([]byte(`value: (( awsparam "/c1b/demo" ))` + "\n"))
	if err != nil {
		t.Fatalf("ParseYAML failed: %v", err)
	}

	if _, err := engine.Evaluate(context.Background(), doc); err == nil {
		t.Fatal("expected an error from the built-in (unconfigured) AWS path, got nil")
	}
	if contacted {
		t.Fatal("the WithAWS-configured httptest server was contacted despite FeatureBackendRegistry being off")
	}
}

// TestWithAWSTarget_SelectsPerTargetConfig mirrors
// TestWithVaultTarget_SelectsPerTargetConfig for AWS.
func TestWithAWSTarget_SelectsPerTargetConfig(t *testing.T) {
	newSrv := func(value string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/x-amz-json-1.1")
			_, _ = w.Write([]byte(`{"Parameter":{"Name":"/c1b/target-demo","Value":"` + value + `"}}`))
		}))
	}

	defaultSrv := newSrv("default-value")
	defer defaultSrv.Close()
	targetSrv := newSrv("target-specific-value")
	defer targetSrv.Close()

	engine, err := graft.NewEngine(
		graft.WithBackendRegistry(true),
		graft.WithAWS(graft.AWSConfig{Region: "us-east-1", Endpoint: defaultSrv.URL, SkipAuth: true}),
		graft.WithAWSTarget("prod", graft.AWSConfig{Region: "us-east-1", Endpoint: targetSrv.URL, SkipAuth: true}),
	)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	doc, err := engine.ParseYAML([]byte(`value: (( awsparam@prod "/c1b/target-demo" ))` + "\n"))
	if err != nil {
		t.Fatalf("ParseYAML failed: %v", err)
	}

	evaluated, err := engine.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	got, err := evaluated.Get("value")
	if err != nil {
		t.Fatalf("Get(value) failed: %v", err)
	}
	if got != "target-specific-value" {
		t.Fatalf("expected the WithAWSTarget-configured value, got %v", got)
	}
}
