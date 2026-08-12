package graft

import (
	"context"
	"testing"
)

// These tests exercise MockEngine through a full Engine.Evaluate call
// against real "(( vault ... ))"/"(( awsparam ... ))"/"(( awssecret ... ))"/
// "(( nats ... ))" documents - the end-to-end path testing.md documents,
// as opposed to mock_test.go's direct Backend.Get calls. They depend on
// the vault/awsparam/awssecret/nats operators consulting the
// FeatureBackendRegistry-gated backend registry (C7); see
// .agents/work/20260812-wave-c/c6-notes.md for status if these are
// failing.

func TestMockEngine_Evaluate_VaultLookupResolvesToSeededValue(t *testing.T) {
	m := NewMockEngine()
	m.MockVault("secret/db:password", "test-password")

	doc, err := m.ParseYAML([]byte(`database:
  password: (( vault "secret/db:password" ))
`))
	if err != nil {
		t.Fatalf("ParseYAML() returned unexpected error: %v", err)
	}

	result, err := m.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatalf("Evaluate() returned unexpected error: %v", err)
	}
	if got := result.String("database.password"); got != "test-password" {
		t.Fatalf("database.password: expected test-password, got %q", got)
	}
	if !m.WasVaultCalled("secret/db:password") {
		t.Fatal("WasVaultCalled(\"secret/db:password\"): expected true after Evaluate")
	}
}

func TestMockEngine_Evaluate_VaultMissingWithDefaultFallsBack(t *testing.T) {
	m := NewMockEngine()

	doc, err := m.ParseYAML([]byte(`password: (( vault "secret/missing:pass" || "default" ))`))
	if err != nil {
		t.Fatalf("ParseYAML() returned unexpected error: %v", err)
	}

	result, err := m.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatalf("Evaluate() returned unexpected error: %v", err)
	}
	if got := result.String("password"); got != "default" {
		t.Fatalf("password: expected default, got %q", got)
	}
}

func TestMockEngine_Evaluate_VaultMissingRequiredIsAnError(t *testing.T) {
	m := NewMockEngine()

	doc, err := m.ParseYAML([]byte(`password: (( vault "secret/required:pass" ))`))
	if err != nil {
		t.Fatalf("ParseYAML() returned unexpected error: %v", err)
	}

	if _, err := m.Evaluate(context.Background(), doc); err == nil {
		t.Fatal("Evaluate(): expected an error for a missing required vault path, got nil")
	}
}

func TestMockEngine_Evaluate_AWSParamResolvesToSeededValue(t *testing.T) {
	m := NewMockEngine()
	m.MockAWSParam("/app/host", "localhost")

	doc, err := m.ParseYAML([]byte(`host: (( awsparam "/app/host" ))`))
	if err != nil {
		t.Fatalf("ParseYAML() returned unexpected error: %v", err)
	}

	result, err := m.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatalf("Evaluate() returned unexpected error: %v", err)
	}
	if got := result.String("host"); got != "localhost" {
		t.Fatalf("host: expected localhost, got %q", got)
	}
}

func TestMockEngine_Evaluate_AWSSecretResolvesToSeededValue(t *testing.T) {
	m := NewMockEngine()
	m.MockAWSSecret("prod/api-key", "sk-test-123")

	doc, err := m.ParseYAML([]byte(`api_key: (( awssecret "prod/api-key" ))`))
	if err != nil {
		t.Fatalf("ParseYAML() returned unexpected error: %v", err)
	}

	result, err := m.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatalf("Evaluate() returned unexpected error: %v", err)
	}
	if got := result.String("api_key"); got != "sk-test-123" {
		t.Fatalf("api_key: expected sk-test-123, got %q", got)
	}
}

func TestMockEngine_Evaluate_NATSResolvesToSeededValue(t *testing.T) {
	m := NewMockEngine()
	m.MockNATS("kv:config/level", "info")

	doc, err := m.ParseYAML([]byte(`level: (( nats "kv:config/level" ))`))
	if err != nil {
		t.Fatalf("ParseYAML() returned unexpected error: %v", err)
	}

	result, err := m.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatalf("Evaluate() returned unexpected error: %v", err)
	}
	if got := result.String("level"); got != "info" {
		t.Fatalf("level: expected info, got %q", got)
	}
}
