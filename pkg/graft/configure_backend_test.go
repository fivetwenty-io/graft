package graft

import (
	"context"
	"testing"

	"github.com/fivetwenty-io/graft/internal/features"
)

// TestConfigure_WiresBackendRegistryOptions is the H2 regression guard
// (.agents/work/20260812-wave-c/phase3-review.md). DefaultEngine.Configure
// used to copy WithBackend/WithBackendRegistry/WithBackendRetry/
// WithBackendCache/WithAuditLogger into e.opts without ever reading them
// back out - a silent no-op indistinguishable from success. Configure must
// actually register the backend and flip the feature flag.
func TestConfigure_WiresBackendRegistryOptions(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	de, ok := engine.(*DefaultEngine)
	if !ok {
		t.Fatalf("NewEngine returned %T, want *DefaultEngine", engine)
	}

	if de.IsFeatureEnabled(features.FeatureBackendRegistry) {
		t.Fatal("expected FeatureBackendRegistry disabled before Configure")
	}
	if _, ok := de.GetBackend("vault"); ok {
		t.Fatal("expected no backend registered under \"vault\" before Configure")
	}

	backend := newStubBackend("vault")
	backend.data["secret/x:key"] = "s3cr3t"

	if err := de.Configure(WithBackend(backend), WithBackendRegistry(true)); err != nil {
		t.Fatalf("Configure returned an error: %v", err)
	}

	if !de.IsFeatureEnabled(features.FeatureBackendRegistry) {
		t.Fatal("Configure(WithBackendRegistry(true)) did not enable the flag - CONFIRMED regression from phase3-review.md H2")
	}
	got, ok := de.GetBackend("vault")
	if !ok {
		t.Fatal("Configure(WithBackend(...)) did not register the backend - CONFIRMED regression from phase3-review.md H2")
	}
	// GetBackend returns the registry's wrapper (registerBackendLocked),
	// not the *stubBackend pointer itself, so identity comparison is not
	// meaningful here - confirm the wrapper delegates to it instead.
	val, err := got.Get(context.Background(), "secret/x:key")
	if err != nil {
		t.Fatalf("GetBackend(\"vault\").Get failed: %v", err)
	}
	if val != "s3cr3t" {
		t.Fatalf("GetBackend(\"vault\").Get = %v, want %q", val, "s3cr3t")
	}
	if got := backend.callCount(); got != 1 {
		t.Fatalf("backend.callCount() = %d, want 1", got)
	}
}

// TestConfigure_WithVaultResolvesThroughRegistryAfterFlagOn exercises the
// full consumer path H2's remediation guidance calls for: Configure with a
// WithVault-registered backend, then a flag-on evaluation actually
// consults it (rather than falling through to internal/backends, which
// would fail without real Vault credentials).
func TestConfigure_WithVaultResolvesThroughRegistryAfterFlagOn(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	de, ok := engine.(*DefaultEngine)
	if !ok {
		t.Fatalf("NewEngine returned %T, want *DefaultEngine", engine)
	}

	backend := newStubBackend("vault")
	backend.data["secret/x:key"] = "s3cr3t"

	if err := de.Configure(WithBackend(backend), WithBackendRegistry(true)); err != nil {
		t.Fatalf("Configure returned an error: %v", err)
	}

	doc, err := de.ParseYAML([]byte("value: (( vault \"secret/x:key\" ))\n"))
	if err != nil {
		t.Fatalf("ParseYAML failed: %v", err)
	}
	result, err := de.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatalf("Evaluate returned an error: %v", err)
	}

	value, err := result.GetString("value")
	if err != nil {
		t.Fatalf("GetString(\"value\") failed: %v", err)
	}
	if value != "s3cr3t" {
		t.Fatalf("value = %q, want %q (Configure-registered backend was not consulted)", value, "s3cr3t")
	}
	if got := backend.callCount(); got != 1 {
		t.Fatalf("backend.callCount() = %d, want 1", got)
	}
}

// TestConfigure_FailedBackendRegistrationLeavesEngineUnchanged mirrors
// TestConfigure_FailedOperatorRegistrationLeavesConfigUnchanged for the
// backend registry: a Configure call combining a valid option with an
// invalid pending backend registration (empty Name()) must not apply the
// valid part, matching Configure's all-or-nothing doc comment.
func TestConfigure_FailedBackendRegistrationLeavesEngineUnchanged(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	de, ok := engine.(*DefaultEngine)
	if !ok {
		t.Fatalf("NewEngine returned %T, want *DefaultEngine", engine)
	}

	if de.IsVaultSkipped() {
		t.Fatal("expected vault not skipped before Configure")
	}

	badBackend := newStubBackend("") // empty Name() is invalid

	err = de.Configure(WithSkipVault(true), WithBackendRegistry(true), WithBackend(badBackend))
	if err == nil {
		t.Fatal("Configure with an empty-name backend succeeded, want an error")
	}

	if de.IsVaultSkipped() {
		t.Fatal("Configure applied SkipVault despite the failing backend registration in the same call - Configure is not atomic")
	}
	if de.IsFeatureEnabled(features.FeatureBackendRegistry) {
		t.Fatal("Configure applied WithBackendRegistry(true) despite the failing backend registration in the same call - Configure is not atomic")
	}
	if _, ok := de.GetBackend(""); ok {
		t.Fatal("Configure registered the invalid backend despite returning an error")
	}
}

// TestConfigure_BackendNameKeyMismatchIsRejected pins
// validatePendingBackends' third check: EngineOptions.Backends is an
// exported field, so a hand-built EngineOption can store a Backend under a
// map key that disagrees with its own Name() - Configure must reject that
// rather than silently registering it under the Name() it reports (which
// would differ from the key a caller might expect to look it up under).
func TestConfigure_BackendNameKeyMismatchIsRejected(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	de, ok := engine.(*DefaultEngine)
	if !ok {
		t.Fatalf("NewEngine returned %T, want *DefaultEngine", engine)
	}

	mismatched := newStubBackend("vault")
	injectMismatchedBackend := EngineOption(func(o *EngineOptions) {
		if o.Backends == nil {
			o.Backends = make(map[string]Backend)
		}
		o.Backends["awsparam"] = mismatched // key disagrees with mismatched.Name() == "vault"
	})

	err = de.Configure(injectMismatchedBackend)
	if err == nil {
		t.Fatal("Configure with a mismatched backend key/Name() succeeded, want an error")
	}
	if _, ok := de.GetBackend("awsparam"); ok {
		t.Fatal("Configure registered the mismatched backend despite returning an error")
	}
	if _, ok := de.GetBackend("vault"); ok {
		t.Fatal("Configure registered the mismatched backend despite returning an error")
	}
}

// TestConfigure_BackendValidationErrorIsDeterministic mirrors
// TestConfigure_OperatorValidationErrorIsDeterministic: Configure must
// validate (and register) pending backends in sorted-name order, so which
// of several invalid registrations is reported does not vary run to run
// with Go's randomized map iteration.
func TestConfigure_BackendValidationErrorIsDeterministic(t *testing.T) {
	const runs = 30

	for i := 0; i < runs; i++ {
		engine, err := NewEngine()
		if err != nil {
			t.Fatalf("run %d: NewEngine failed: %v", i, err)
		}
		de, ok := engine.(*DefaultEngine)
		if !ok {
			t.Fatalf("run %d: NewEngine returned %T, want *DefaultEngine", i, engine)
		}

		emptyNamed := newStubBackend("")
		injectTwoBad := EngineOption(func(o *EngineOptions) {
			o.Backends = map[string]Backend{
				"":            emptyNamed,
				"zzz-backend": newStubBackend("mismatched-name"),
			}
		})

		err = de.Configure(injectTwoBad)
		if err == nil {
			t.Fatalf("run %d: Configure with invalid pending backends succeeded, want an error", i)
		}
		const wantErr = "backend Name() must not be empty"
		if got := err.Error(); got != wantErr {
			t.Fatalf("run %d: Configure error = %q, want %q (deterministic sorted-name order requires the empty name to be validated first)", i, got, wantErr)
		}
	}
}
