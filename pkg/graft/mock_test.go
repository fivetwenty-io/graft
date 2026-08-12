package graft

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/fivetwenty-io/graft/internal/features"
)

func TestNewMockEngine_ImplementsEngine(t *testing.T) {
	var _ Engine = NewMockEngine()
}

func TestNewMockEngine_EnablesBackendRegistry(t *testing.T) {
	m := NewMockEngine()
	if !m.Features.IsEnabled(features.FeatureBackendRegistry) {
		t.Fatal("NewMockEngine(): FeatureBackendRegistry not enabled")
	}
}

func TestNewMockEngine_RegistersFourBackends(t *testing.T) {
	m := NewMockEngine()
	want := []string{"awsparam", "awssecret", "nats", "vault"}
	got := m.ListBackends()
	if len(got) != len(want) {
		t.Fatalf("ListBackends(): expected %v, got %v", want, got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("ListBackends(): expected %v, got %v", want, got)
		}
	}
}

// TestNewMockEngine_DoesNotLeakBackendRegistryIntoSharedFlags pins M7
// (.agents/work/20260812-wave-c/phase3-review.md): NewMockEngine used to
// enable FeatureBackendRegistry directly on the *features.FeatureFlags
// createEngineFromOptions stores by pointer, so a caller-supplied
// WithFeatureFlags value shared with a later NewEngine call observed the
// flip too. NewMockEngine must clone before enabling.
func TestNewMockEngine_DoesNotLeakBackendRegistryIntoSharedFlags(t *testing.T) {
	shared := features.DefaultFlags()
	if shared.IsEnabled(features.FeatureBackendRegistry) {
		t.Fatal("expected FeatureBackendRegistry disabled on a fresh DefaultFlags()")
	}

	m := NewMockEngine(WithFeatureFlags(shared))
	if !m.Features.IsEnabled(features.FeatureBackendRegistry) {
		t.Fatal("expected NewMockEngine's own engine to have FeatureBackendRegistry enabled")
	}

	if shared.IsEnabled(features.FeatureBackendRegistry) {
		t.Fatal("LEAK: NewMockEngine mutated the caller-supplied *features.FeatureFlags in place")
	}

	prod, err := NewEngine(WithFeatureFlags(shared))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	de, ok := prod.(*DefaultEngine)
	if !ok {
		t.Fatalf("NewEngine returned %T, want *DefaultEngine", prod)
	}
	if de.IsFeatureEnabled(features.FeatureBackendRegistry) {
		t.Fatal("LEAK: a production engine built from the same *features.FeatureFlags value has FeatureBackendRegistry enabled")
	}
}

func TestNewMockEngine_PassesThroughEngineOptions(t *testing.T) {
	m := NewMockEngine(WithMaxWorkers(7))
	if m.opts.MaxConcurrency != 7 {
		t.Fatalf("NewMockEngine(WithMaxWorkers(7)): expected MaxConcurrency 7, got %d", m.opts.MaxConcurrency)
	}
}

func TestNewMockEngine_ParsesAndEvaluatesLikeARealEngine(t *testing.T) {
	m := NewMockEngine()
	doc, err := m.ParseYAML([]byte(`foo: (( grab bar ))
bar: baz
`))
	if err != nil {
		t.Fatalf("ParseYAML() returned unexpected error: %v", err)
	}
	result, err := m.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatalf("Evaluate() returned unexpected error: %v", err)
	}
	if got, _ := result.Get("foo"); got != "baz" {
		t.Fatalf("Evaluate() result foo: expected baz, got %v", got)
	}
}

// --- Vault ---

func TestMockEngine_VaultBackend_ReturnsSeededValue(t *testing.T) {
	m := NewMockEngine()
	m.MockVault("secret/db:password", "test-password")

	backend, ok := m.GetBackend("vault")
	if !ok {
		t.Fatal("GetBackend(\"vault\"): not registered")
	}
	val, err := backend.Get(context.Background(), "secret/db:password")
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	if val != "test-password" {
		t.Fatalf("Get(): expected test-password, got %v", val)
	}
}

func TestMockEngine_VaultBackend_MockVaultPathBuildsCombinedKey(t *testing.T) {
	m := NewMockEngine()
	m.MockVaultPath("secret/db", "password", "from-path")

	backend, _ := m.GetBackend("vault")
	val, err := backend.Get(context.Background(), "secret/db:password")
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	if val != "from-path" {
		t.Fatalf("Get(): expected from-path, got %v", val)
	}
}

func TestMockEngine_VaultBackend_UnseededPathReturnsErrBackendNotFound(t *testing.T) {
	m := NewMockEngine()

	backend, _ := m.GetBackend("vault")
	_, err := backend.Get(context.Background(), "secret/missing:pass")
	if !errors.Is(err, ErrBackendNotFound) {
		t.Fatalf("Get() error: expected ErrBackendNotFound, got %v", err)
	}
}

func TestMockEngine_VaultBackend_MockVaultErrorInjectsError(t *testing.T) {
	m := NewMockEngine()
	injected := errors.New("vault unreachable")
	m.MockVaultError("secret/down:pass", injected)

	backend, _ := m.GetBackend("vault")
	_, err := backend.Get(context.Background(), "secret/down:pass")
	if !errors.Is(err, injected) {
		t.Fatalf("Get() error: expected to wrap %v, got %v", injected, err)
	}
}

func TestMockEngine_VaultBackend_MockVaultOverwritesPriorError(t *testing.T) {
	m := NewMockEngine()
	m.MockVaultError("secret/x:y", errors.New("boom"))
	m.MockVault("secret/x:y", "recovered")

	backend, _ := m.GetBackend("vault")
	val, err := backend.Get(context.Background(), "secret/x:y")
	if err != nil {
		t.Fatalf("Get() returned unexpected error after re-seeding: %v", err)
	}
	if val != "recovered" {
		t.Fatalf("Get(): expected recovered, got %v", val)
	}
}

func TestMockEngine_VaultCalls_RecordsEachGet(t *testing.T) {
	m := NewMockEngine()
	m.MockVault("secret/a:k", "1")
	m.MockVault("secret/b:k", "2")

	backend, _ := m.GetBackend("vault")
	if _, err := backend.Get(context.Background(), "secret/a:k"); err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	if _, err := backend.Get(context.Background(), "secret/b:k"); err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}

	calls := m.VaultCalls()
	if len(calls) != 2 {
		t.Fatalf("VaultCalls(): expected 2 calls, got %d (%v)", len(calls), calls)
	}
	if calls[0].Path != "secret/a:k" || calls[1].Path != "secret/b:k" {
		t.Fatalf("VaultCalls(): unexpected paths: %v", calls)
	}
	if calls[0].Backend != "vault" {
		t.Fatalf("VaultCalls()[0].Backend: expected vault, got %q", calls[0].Backend)
	}
}

func TestMockEngine_WasVaultCalled(t *testing.T) {
	m := NewMockEngine()
	m.MockVault("secret/a:k", "1")

	backend, _ := m.GetBackend("vault")

	if m.WasVaultCalled("secret/a:k") {
		t.Fatal("WasVaultCalled(): true before any Get")
	}
	if _, err := backend.Get(context.Background(), "secret/a:k"); err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	if !m.WasVaultCalled("secret/a:k") {
		t.Fatal("WasVaultCalled(): false after Get")
	}
	if m.WasVaultCalled("secret/never:k") {
		t.Fatal("WasVaultCalled(): true for a path never fetched")
	}
}

// --- AWS ---

func TestMockEngine_AWSParamBackend_ReturnsSeededValue(t *testing.T) {
	m := NewMockEngine()
	m.MockAWSParam("/app/config", "plain-value")

	backend, ok := m.GetBackend("awsparam")
	if !ok {
		t.Fatal("GetBackend(\"awsparam\"): not registered")
	}
	val, err := backend.Get(context.Background(), "/app/config")
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	if val != "plain-value" {
		t.Fatalf("Get(): expected plain-value, got %v", val)
	}
}

func TestMockEngine_AWSParamBackend_JSONEncodesValue(t *testing.T) {
	m := NewMockEngine()
	m.MockAWSParamJSON("/app/config.json", map[string]interface{}{"host": "localhost"})

	backend, _ := m.GetBackend("awsparam")
	val, err := backend.Get(context.Background(), "/app/config.json")
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	if val != `{"host":"localhost"}` {
		t.Fatalf("Get(): expected JSON-encoded string, got %v", val)
	}
}

func TestMockEngine_AWSParamBackend_UnseededReturnsErrBackendNotFound(t *testing.T) {
	m := NewMockEngine()

	backend, _ := m.GetBackend("awsparam")
	_, err := backend.Get(context.Background(), "/missing")
	if !errors.Is(err, ErrBackendNotFound) {
		t.Fatalf("Get() error: expected ErrBackendNotFound, got %v", err)
	}
}

func TestMockEngine_AWSParamBackend_MockAWSParamErrorInjectsError(t *testing.T) {
	m := NewMockEngine()
	injected := errors.New("access denied")
	m.MockAWSParamError("/secret-param", injected)

	backend, _ := m.GetBackend("awsparam")
	_, err := backend.Get(context.Background(), "/secret-param")
	if !errors.Is(err, injected) {
		t.Fatalf("Get() error: expected to wrap %v, got %v", injected, err)
	}
}

func TestMockEngine_AWSSecretBackend_ReturnsSeededStringValue(t *testing.T) {
	m := NewMockEngine()
	m.MockAWSSecret("prod/api-key", "sk-test-123")

	backend, ok := m.GetBackend("awssecret")
	if !ok {
		t.Fatal("GetBackend(\"awssecret\"): not registered")
	}
	val, err := backend.Get(context.Background(), "prod/api-key")
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	if val != "sk-test-123" {
		t.Fatalf("Get(): expected sk-test-123, got %v", val)
	}
}

func TestMockEngine_AWSSecretBackend_ReturnsSeededStructuredValue(t *testing.T) {
	m := NewMockEngine()
	seeded := map[string]string{"username": "testuser", "password": "testpass"}
	m.MockAWSSecret("prod/db", seeded)

	backend, _ := m.GetBackend("awssecret")
	val, err := backend.Get(context.Background(), "prod/db")
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	got, ok := val.(map[string]string)
	if !ok || got["username"] != "testuser" || got["password"] != "testpass" {
		t.Fatalf("Get(): expected %v, got %v", seeded, val)
	}
}

func TestMockEngine_AWSCalls_CombinesParamAndSecret(t *testing.T) {
	m := NewMockEngine()
	m.MockAWSParam("/a", "1")
	m.MockAWSSecret("b", "2")

	paramBackend, _ := m.GetBackend("awsparam")
	secretBackend, _ := m.GetBackend("awssecret")
	if _, err := paramBackend.Get(context.Background(), "/a"); err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	if _, err := secretBackend.Get(context.Background(), "b"); err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}

	calls := m.AWSCalls()
	if len(calls) != 2 {
		t.Fatalf("AWSCalls(): expected 2 calls, got %d (%v)", len(calls), calls)
	}
	backends := map[string]bool{calls[0].Backend: true, calls[1].Backend: true}
	if !backends["awsparam"] || !backends["awssecret"] {
		t.Fatalf("AWSCalls(): expected one awsparam and one awssecret call, got %v", calls)
	}
}

// --- NATS ---

func TestMockEngine_NATSBackend_ReturnsSeededValue(t *testing.T) {
	m := NewMockEngine()
	m.MockNATS("kv:config/settings", map[string]interface{}{"debug": true})

	backend, ok := m.GetBackend("nats")
	if !ok {
		t.Fatal("GetBackend(\"nats\"): not registered")
	}
	val, err := backend.Get(context.Background(), "kv:config/settings")
	if err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}
	got, ok := val.(map[string]interface{})
	if !ok || got["debug"] != true {
		t.Fatalf("Get(): unexpected value %v", val)
	}
}

func TestMockEngine_NATSBackend_UnseededReturnsErrBackendNotFound(t *testing.T) {
	m := NewMockEngine()

	backend, _ := m.GetBackend("nats")
	_, err := backend.Get(context.Background(), "kv:missing/key")
	if !errors.Is(err, ErrBackendNotFound) {
		t.Fatalf("Get() error: expected ErrBackendNotFound, got %v", err)
	}
}

func TestMockEngine_NATSCalls_RecordsGets(t *testing.T) {
	m := NewMockEngine()
	m.MockNATS("kv:a/b", "v")

	backend, _ := m.GetBackend("nats")
	if _, err := backend.Get(context.Background(), "kv:a/b"); err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}

	calls := m.NATSCalls()
	if len(calls) != 1 || calls[0].Path != "kv:a/b" || calls[0].Backend != "nats" {
		t.Fatalf("NATSCalls(): unexpected %v", calls)
	}
}

// --- GetBatch / Health / Close ---

func TestMockEngine_BackendGetBatch_SkipsNotFound(t *testing.T) {
	m := NewMockEngine()
	m.MockVault("secret/a:k", "1")

	backend, _ := m.GetBackend("vault")
	results, err := backend.GetBatch(context.Background(), []string{"secret/a:k", "secret/missing:k"})
	if err != nil {
		t.Fatalf("GetBatch() returned unexpected error: %v", err)
	}
	if len(results) != 1 || results["secret/a:k"] != "1" {
		t.Fatalf("GetBatch(): expected only secret/a:k present, got %v", results)
	}
}

func TestMockEngine_BackendHealthAndClose_AlwaysSucceed(t *testing.T) {
	m := NewMockEngine()
	for _, name := range []string{"vault", "awsparam", "awssecret", "nats"} {
		backend, ok := m.GetBackend(name)
		if !ok {
			t.Fatalf("GetBackend(%q): not registered", name)
		}
		if err := backend.Health(context.Background()); err != nil {
			t.Fatalf("Health() for %q returned unexpected error: %v", name, err)
		}
		if err := backend.Close(); err != nil {
			t.Fatalf("Close() for %q returned unexpected error: %v", name, err)
		}
	}
}

// --- Reset ---

func TestMockEngine_Reset_ClearsSeededValuesErrorsAndCalls(t *testing.T) {
	m := NewMockEngine()
	m.MockVault("secret/a:k", "1")
	m.MockVaultError("secret/b:k", errors.New("x"))
	m.MockAWSParam("/p", "v")
	m.MockAWSSecret("s", "v")
	m.MockNATS("kv:a/b", "v")

	vaultBackend, _ := m.GetBackend("vault")
	if _, err := vaultBackend.Get(context.Background(), "secret/a:k"); err != nil {
		t.Fatalf("Get() returned unexpected error: %v", err)
	}

	m.Reset()

	if len(m.VaultCalls()) != 0 {
		t.Fatalf("Reset(): expected VaultCalls() empty, got %v", m.VaultCalls())
	}
	if _, err := vaultBackend.Get(context.Background(), "secret/a:k"); !errors.Is(err, ErrBackendNotFound) {
		t.Fatalf("Reset(): expected secret/a:k to be unseeded, got err=%v", err)
	}
	if _, err := vaultBackend.Get(context.Background(), "secret/b:k"); !errors.Is(err, ErrBackendNotFound) {
		t.Fatalf("Reset(): expected secret/b:k error to be cleared, got err=%v", err)
	}
}

// --- Concurrency ---

func TestMockEngine_ConcurrentGets_NoDataRace(t *testing.T) {
	m := NewMockEngine()
	for i := 0; i < 10; i++ {
		m.MockVault(fmt.Sprintf("secret/%d:k", i), i)
	}

	backend, _ := m.GetBackend("vault")
	done := make(chan struct{})
	for i := 0; i < 10; i++ {
		go func(i int) {
			defer func() { done <- struct{}{} }()
			_, _ = backend.Get(context.Background(), fmt.Sprintf("secret/%d:k", i))
		}(i)
	}
	for i := 0; i < 10; i++ {
		<-done
	}

	if len(m.VaultCalls()) != 10 {
		t.Fatalf("VaultCalls(): expected 10 calls, got %d", len(m.VaultCalls()))
	}
}
