package operators

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/features"
	"github.com/fivetwenty-io/graft/pkg/graft"
)

// backendRegistryFlagsEnabled builds a *features.FeatureFlags with
// FeatureBackendRegistry explicitly enabled, for use with
// graft.WithFeatureFlags in these tests.
func backendRegistryFlagsEnabled() *features.FeatureFlags {
	ff := features.DefaultFlags()
	ff.Enable(features.FeatureBackendRegistry)
	return ff
}

// vaultStubBackend is a minimal graft.Backend used by these tests. It does
// not implement graft.TargetedBackend - see vaultTargetedStubBackend for
// the variant that does, letting tests exercise both the "@target
// unsupported" hard-error path and the supported path.
type vaultStubBackend struct {
	data map[string]string
}

func (b *vaultStubBackend) Name() string { return "vault" }

func (b *vaultStubBackend) Get(_ context.Context, path string) (interface{}, error) {
	v, ok := b.data[path]
	if !ok {
		return nil, graft.ErrBackendNotFound
	}
	return v, nil
}

func (b *vaultStubBackend) GetBatch(ctx context.Context, paths []string) (map[string]interface{}, error) {
	return graft.SequentialGetBatch(ctx, paths, b.Get)
}
func (b *vaultStubBackend) Health(context.Context) error { return nil }
func (b *vaultStubBackend) Close() error                 { return nil }

// vaultTargetedStubBackend embeds vaultStubBackend and adds GetWithTarget,
// so it satisfies graft.TargetedBackend.
type vaultTargetedStubBackend struct {
	vaultStubBackend
}

func (b *vaultTargetedStubBackend) GetWithTarget(ctx context.Context, target, path string) (interface{}, error) {
	return b.Get(ctx, target+"@"+path)
}

func evaluateYAML(t *testing.T, engine graft.Engine, yamlDoc string) (graft.Document, error) {
	t.Helper()
	doc, err := engine.ParseYAML([]byte(yamlDoc))
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}
	return engine.Evaluate(context.Background(), doc)
}

// TestVaultOperatorFlagOffIgnoresCustomBackend pins the "flag off" half of
// the C7 differential: a custom "vault" backend registered on an engine
// that has FeatureBackendRegistry at its default (off) must never be
// consulted - the vault operator must produce exactly the same outcome as
// an otherwise-identical engine with no custom backend at all, since
// without real Vault credentials in this test environment the built-in
// path fails deterministically with the same error either way.
func TestVaultOperatorFlagOffIgnoresCustomBackend(t *testing.T) {
	t.Setenv("VAULT_ADDR", "")
	t.Setenv("VAULT_TOKEN", "")
	t.Setenv("HOME", t.TempDir()) // no .vault-token / .svtoken fallback files

	backend := &vaultStubBackend{data: map[string]string{"secret/x:key": "custom-value"}}

	withCustom, err := graft.NewEngine(graft.WithBackend(backend))
	if err != nil {
		t.Fatalf("NewEngine (with custom backend): %v", err)
	}
	withoutCustom, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("NewEngine (no custom backend): %v", err)
	}

	yamlDoc := "value: (( vault \"secret/x:key\" ))\n"

	_, errWith := evaluateYAML(t, withCustom, yamlDoc)
	_, errWithout := evaluateYAML(t, withoutCustom, yamlDoc)

	if errWith == nil {
		t.Fatalf("expected an error (no real Vault credentials in this test environment), got a result - the custom backend was consulted despite the flag being off")
	}
	if errWith.Error() != errWithout.Error() {
		t.Fatalf("flag-off differential: error with a registered custom backend (%v) differs from error with none (%v); the registry must not be consulted when the flag is off", errWith, errWithout)
	}
	if strings.Contains(errWith.Error(), "custom-value") {
		t.Fatalf("error unexpectedly references the custom backend's value: %v", errWith)
	}
}

// TestVaultOperatorFlagOnNoCustomBackendFallsBack pins the "flag on, no
// custom backend registered" half of the C7 differential: behavior must
// match the flag-off case exactly (fallback to internal/backends/vault).
func TestVaultOperatorFlagOnNoCustomBackendFallsBack(t *testing.T) {
	flagOff, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("NewEngine (flag off): %v", err)
	}
	flagOn, err := graft.NewEngine(graft.WithFeatureFlags(backendRegistryFlagsEnabled()))
	if err != nil {
		t.Fatalf("NewEngine (flag on): %v", err)
	}

	yamlDoc := "value: (( vault \"secret/x:key\" ))\n"

	_, errOff := evaluateYAML(t, flagOff, yamlDoc)
	_, errOn := evaluateYAML(t, flagOn, yamlDoc)

	if errOff == nil || errOn == nil {
		t.Fatalf("expected both to error (no real Vault credentials in this test environment): off=%v on=%v", errOff, errOn)
	}
	if errOff.Error() != errOn.Error() {
		t.Fatalf("enabling the flag with no custom backend registered changed behavior: off=%q on=%q", errOff.Error(), errOn.Error())
	}
}

// TestVaultOperatorFlagOnCustomBackendConsulted proves the "flag on +
// custom backend" path actually resolves through the custom backend
// instead of internal/backends/vault.
func TestVaultOperatorFlagOnCustomBackendConsulted(t *testing.T) {
	backend := &vaultStubBackend{data: map[string]string{"secret/x:key": "custom-value"}}
	engine, err := graft.NewEngine(
		graft.WithFeatureFlags(backendRegistryFlagsEnabled()),
		graft.WithBackend(backend),
	)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	doc, err := evaluateYAML(t, engine, "value: (( vault \"secret/x:key\" ))\n")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}

	got, err := doc.GetString("value")
	if err != nil {
		t.Fatalf("GetString(\"value\"): %v", err)
	}
	if got != "custom-value" {
		t.Fatalf("value = %q, want %q", got, "custom-value")
	}
}

// TestVaultOperatorCustomBackendNotFoundMatchesContractShape pins the
// Genesis compatibility contract's not-found detection ("starts with
// `secret `, ends with ` not found`") for a custom backend's
// ErrBackendNotFound, exactly matching the built-in path's error text.
func TestVaultOperatorCustomBackendNotFoundMatchesContractShape(t *testing.T) {
	backend := &vaultStubBackend{data: map[string]string{}} // empty: every Get is not-found
	engine, err := graft.NewEngine(
		graft.WithFeatureFlags(backendRegistryFlagsEnabled()),
		graft.WithBackend(backend),
	)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	_, err = evaluateYAML(t, engine, "value: (( vault \"secret/x:key\" ))\n")
	if err == nil {
		t.Fatalf("expected an error for a not-found custom backend lookup")
	}
	if !strings.Contains(err.Error(), "secret secret/x:key not found") {
		t.Fatalf("error = %q, want it to contain the contract-pinned \"secret secret/x:key not found\" shape", err.Error())
	}
}

// TestVaultOperatorCustomBackendGenericErrorWrapped pins that a non-not-
// found custom backend failure surfaces as a *graft.GraftError{Type:
// ExternalError} wrapping a *graft.BackendError, reachable via errors.As -
// never *graft.BackendError as the outermost error.
func TestVaultOperatorCustomBackendGenericErrorWrapped(t *testing.T) {
	boom := errors.New("connection refused")
	backend := &erroringBackend{name: "vault", err: boom}
	engine, err := graft.NewEngine(
		graft.WithFeatureFlags(backendRegistryFlagsEnabled()),
		graft.WithBackend(backend),
	)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	_, evalErr := evaluateYAML(t, engine, "value: (( vault \"secret/x:key\" ))\n")
	if evalErr == nil {
		t.Fatalf("expected an error")
	}

	// engine.Evaluate reports per-path failures wrapped in a
	// graft.MultiError; op_vault_notfound_test.go's asMultiError helper
	// reaches the first per-path error with a plain type assertion (still
	// useful when a test wants that error specifically). This test instead
	// digs in with the same helper for the intermediate assertions, then
	// separately confirms (below) that errors.As also works directly
	// against evalErr, since MultiError.Unwrap makes the two equivalent.
	var multi graft.MultiError
	if !asMultiError(evalErr, &multi) || len(multi.Errors) == 0 {
		t.Fatalf("expected a graft.MultiError with at least one error, got %T: %v", evalErr, evalErr)
	}

	var graftErr *graft.GraftError
	if !errors.As(multi.Errors[0], &graftErr) {
		t.Fatalf("errors.As(multi.Errors[0], &graft.GraftError{}) = false; got %v", multi.Errors[0])
	}
	if graftErr.Type != graft.ExternalError {
		t.Fatalf("GraftError.Type = %v, want ExternalError", graftErr.Type)
	}
	var backendErr *graft.BackendError
	if !errors.As(multi.Errors[0], &backendErr) {
		t.Fatalf("errors.As(multi.Errors[0], &graft.BackendError{}) = false; got %v", multi.Errors[0])
	}
	if backendErr.Backend != "vault" {
		t.Fatalf("BackendError.Backend = %q, want \"vault\"", backendErr.Backend)
	}
}

// TestVaultOperatorCustomBackendErrorReachableDirectlyViaErrorsAs pins the
// exact consumer pattern docs/developer-guide/custom-backends.md's "Errors"
// section documents: errors.As(err, &backendErr) applied directly to the
// error engine.Evaluate returns, with no manual MultiError unwrapping. This
// only works because graft.MultiError implements Unwrap() []error; without
// it, errors.As stops at the MultiError and never reaches the
// *graft.BackendError nested inside its first element (see H1 in
// .agents/work/20260812-wave-c/phase3-review.md).
func TestVaultOperatorCustomBackendErrorReachableDirectlyViaErrorsAs(t *testing.T) {
	boom := errors.New("connection refused")
	backend := &erroringBackend{name: "vault", err: boom}
	engine, err := graft.NewEngine(
		graft.WithFeatureFlags(backendRegistryFlagsEnabled()),
		graft.WithBackend(backend),
	)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	_, evalErr := evaluateYAML(t, engine, "value: (( vault \"secret/x:key\" ))\n")
	if evalErr == nil {
		t.Fatalf("expected an error")
	}

	var backendErr *graft.BackendError
	if !errors.As(evalErr, &backendErr) {
		t.Fatalf("errors.As(evalErr, &backendErr) = false against %T: %v; docs/developer-guide/custom-backends.md's documented pattern requires this to work directly", evalErr, evalErr)
	}
	if backendErr.Backend != "vault" {
		t.Fatalf("BackendError.Backend = %q, want \"vault\"", backendErr.Backend)
	}
	if backendErr.Path != "secret/x:key" {
		t.Fatalf("BackendError.Path = %q, want \"secret/x:key\"", backendErr.Path)
	}
	if !errors.Is(evalErr, boom) {
		t.Fatalf("errors.Is(evalErr, boom) = false; BackendError.Unwrap should reach the original cause")
	}
}

// TestVaultOperatorTargetedCustomBackend proves "@target" reaches a
// TargetedBackend's GetWithTarget with the parsed target name.
func TestVaultOperatorTargetedCustomBackend(t *testing.T) {
	backend := &vaultTargetedStubBackend{
		vaultStubBackend: vaultStubBackend{data: map[string]string{"prod@secret/x:key": "prod-value"}},
	}
	engine, err := graft.NewEngine(
		graft.WithFeatureFlags(backendRegistryFlagsEnabled()),
		graft.WithBackend(backend),
	)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	doc, err := evaluateYAML(t, engine, "value: (( vault@prod \"secret/x:key\" ))\n")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	got, err := doc.GetString("value")
	if err != nil {
		t.Fatalf("GetString(\"value\"): %v", err)
	}
	if got != "prod-value" {
		t.Fatalf("value = %q, want %q", got, "prod-value")
	}
}

// TestVaultOperatorTargetUnsupportedByCustomBackend proves that "@target"
// against a custom backend which does not implement graft.TargetedBackend
// is a hard configuration error, not a silent fallback to the default
// (untargeted) lookup.
func TestVaultOperatorTargetUnsupportedByCustomBackend(t *testing.T) {
	backend := &vaultStubBackend{data: map[string]string{"secret/x:key": "custom-value"}}
	engine, err := graft.NewEngine(
		graft.WithFeatureFlags(backendRegistryFlagsEnabled()),
		graft.WithBackend(backend),
	)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	_, evalErr := evaluateYAML(t, engine, "value: (( vault@prod \"secret/x:key\" ))\n")
	if evalErr == nil {
		t.Fatalf("expected an error for @target against a non-TargetedBackend")
	}
	if !strings.Contains(evalErr.Error(), "does not support @target selection") {
		t.Fatalf("error = %q, want it to mention unsupported @target selection", evalErr.Error())
	}
}

// TestVaultOperatorNilEngineNeverConsultsRegistry directly exercises
// performVaultLookup with an Evaluator that has no engine wired
// (graft.EngineOf returns nil), the scenario the plan's "Hazard in
// GetEngine" note is about. It must behave exactly like the built-in path
// always has - not panic, and not silently treat "no engine" as "no
// custom backend found" via a throwaway registry.
//
// What this test does NOT cover (phase3-review.md M8): it does not pin
// resolveCustomBackend's use of graft.EngineOf over graft.GetEngine.
// Swapping that call to graft.GetEngine(ev) kills zero tests in this
// package - CreateDefaultEngine's throwaway engine uses
// features.DefaultFlags() with no env loading, so both routes reach the
// same "registry off" outcome today. See
// TestResolveCustomBackend_EngineOfDiffersFromGetEngine
// (backend_resolve_test.go) for a test against a seam that does
// distinguish the two.
func TestVaultOperatorNilEngineNeverConsultsRegistry(t *testing.T) {
	ev := &graft.Evaluator{Tree: map[string]interface{}{}}
	if graft.EngineOf(ev) != nil {
		t.Fatalf("test precondition failed: ev.engine must be nil")
	}

	fallbackEngine := graft.GetEngine(ev) // constructs a throwaway default engine
	op := VaultOperator{}

	_, err := op.performVaultLookup(ev, fallbackEngine, "", "secret/x:key")
	if err == nil {
		t.Fatalf("expected an error (no real Vault credentials in this test environment)")
	}
	// The error must come from the built-in internal/backends/vault path
	// (a Vault URL/token configuration error), not a nil-pointer panic or
	// registry-related error - proving the nil-engine case fell straight
	// through to the pre-C7 behavior.
	if strings.Contains(err.Error(), "does not support @target") {
		t.Fatalf("unexpected registry-path error on a nil-engine Evaluator: %v", err)
	}
}

// erroringBackend always fails Get/GetWithTarget with a fixed error, for
// error-wrapping tests.
type erroringBackend struct {
	name string
	err  error
}

func (b *erroringBackend) Name() string                                     { return b.name }
func (b *erroringBackend) Get(context.Context, string) (interface{}, error) { return nil, b.err }
func (b *erroringBackend) GetBatch(context.Context, []string) (map[string]interface{}, error) {
	return nil, b.err
}
func (b *erroringBackend) Health(context.Context) error { return nil }
func (b *erroringBackend) Close() error                 { return nil }
