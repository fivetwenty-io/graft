package operators

import (
	"context"
	"net"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// hermeticizeNATSEnv points internal/backends/nats's built-in connection
// path at a guaranteed-refused local address (an ephemeral TCP port bound
// then immediately closed) and disables its connection retry/backoff, so a
// test exercising the built-in (unconfigured) NATS path fails fast and
// deterministically regardless of the machine it runs on - it no longer
// depends on the absence of a real NATS server on the default :4222, nor
// waits through the default 4-attempt (1s/2s/4s backoff, ~7s total) retry
// sequence (see phase3-review.md M9).
func hermeticizeNATSEnv(t *testing.T) {
	t.Helper()

	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen: %v", err)
	}
	addr := ln.Addr().String()
	if err := ln.Close(); err != nil {
		t.Fatalf("ln.Close: %v", err)
	}

	t.Setenv("NATS_URL", "nats://"+addr)
	t.Setenv("NATS_RETRIES", "0")
	t.Setenv("NATS_TIMEOUT", "200ms")
	t.Setenv("NATS_RETRY_INTERVAL", "1ms")
}

// natsStubBackend is a minimal graft.Backend registered under "nats". Get
// returns whatever interface{} was seeded, unstringified - proving the
// NATS operator preserves arbitrary value types from a custom backend the
// same way it does from internal/backends/nats's KV/Object fetches.
type natsStubBackend struct {
	data map[string]interface{}
}

func (b *natsStubBackend) Name() string { return "nats" }

func (b *natsStubBackend) Get(_ context.Context, path string) (interface{}, error) {
	v, ok := b.data[path]
	if !ok {
		return nil, graft.ErrBackendNotFound
	}
	return v, nil
}

func (b *natsStubBackend) GetBatch(ctx context.Context, paths []string) (map[string]interface{}, error) {
	return graft.SequentialGetBatch(ctx, paths, b.Get)
}
func (b *natsStubBackend) Health(context.Context) error { return nil }
func (b *natsStubBackend) Close() error                 { return nil }

// natsTargetedStubBackend adds GetWithTarget.
type natsTargetedStubBackend struct {
	natsStubBackend
}

func (b *natsTargetedStubBackend) GetWithTarget(ctx context.Context, target, path string) (interface{}, error) {
	return b.Get(ctx, target+"@"+path)
}

func TestNatsOperatorFlagOffIgnoresCustomBackend(t *testing.T) {
	hermeticizeNATSEnv(t)

	backend := &natsStubBackend{data: map[string]interface{}{"kv:mykey": "custom-value"}}

	withCustom, err := graft.NewEngine(graft.WithBackend(backend))
	if err != nil {
		t.Fatalf("NewEngine (with custom backend): %v", err)
	}

	_, evalErr := evaluateYAML(t, withCustom, "value: (( nats \"kv:mykey\" ))\n")
	if evalErr == nil {
		t.Fatalf("expected an error (no real NATS server in this test environment); the custom backend was consulted despite the flag being off")
	}
	if strings.Contains(evalErr.Error(), "custom-value") {
		t.Fatalf("error unexpectedly references the custom backend's value: %v", evalErr)
	}
}

func TestNatsOperatorFlagOnCustomBackendConsulted(t *testing.T) {
	backend := &natsStubBackend{data: map[string]interface{}{"kv:mykey": "custom-value"}}
	engine, err := graft.NewEngine(
		graft.WithFeatureFlags(backendRegistryFlagsEnabled()),
		graft.WithBackend(backend),
	)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	doc, err := evaluateYAML(t, engine, "value: (( nats \"kv:mykey\" ))\n")
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

// TestNatsOperatorCustomBackendPreservesNonStringValues proves the nats
// operator does not stringify a custom backend's result (unlike
// vault/awsparam/awssecret), matching the built-in NATS path's own
// interface{}-preserving behavior.
func TestNatsOperatorCustomBackendPreservesNonStringValues(t *testing.T) {
	backend := &natsStubBackend{data: map[string]interface{}{
		"kv:mykey": map[string]interface{}{"nested": "map-value"},
	}}
	engine, err := graft.NewEngine(
		graft.WithFeatureFlags(backendRegistryFlagsEnabled()),
		graft.WithBackend(backend),
	)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	doc, err := evaluateYAML(t, engine, "value: (( nats \"kv:mykey\" ))\n")
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	got, err := doc.GetMap("value")
	if err != nil {
		t.Fatalf("GetMap(\"value\"): %v", err)
	}
	if got["nested"] != "map-value" {
		t.Fatalf("value.nested = %v, want map-value", got["nested"])
	}
}

func TestNatsOperatorTargetedCustomBackend(t *testing.T) {
	backend := &natsTargetedStubBackend{
		natsStubBackend: natsStubBackend{data: map[string]interface{}{"prod@kv:mykey": "prod-value"}},
	}
	engine, err := graft.NewEngine(
		graft.WithFeatureFlags(backendRegistryFlagsEnabled()),
		graft.WithBackend(backend),
	)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	doc, err := evaluateYAML(t, engine, "value: (( nats@prod \"kv:mykey\" ))\n")
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

func TestNatsOperatorTargetUnsupportedByCustomBackend(t *testing.T) {
	backend := &natsStubBackend{data: map[string]interface{}{"kv:mykey": "custom-value"}}
	engine, err := graft.NewEngine(
		graft.WithFeatureFlags(backendRegistryFlagsEnabled()),
		graft.WithBackend(backend),
	)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	_, evalErr := evaluateYAML(t, engine, "value: (( nats@prod \"kv:mykey\" ))\n")
	if evalErr == nil {
		t.Fatalf("expected an error for @target against a non-TargetedBackend")
	}
	if !strings.Contains(evalErr.Error(), "does not support @target selection") {
		t.Fatalf("error = %q, want it to mention unsupported @target selection", evalErr.Error())
	}
}

// TestNatsOperatorCustomBackendRejectsConfigArgument pins M3
// (.agents/work/20260812-wave-c/phase3-review.md): a second argument
// (internal/backends/nats connection config, e.g. a URL string) has no
// Backend-interface equivalent for a custom "nats" backend to receive, so
// the operator must reject the call rather than silently discard the
// argument and its validation.
func TestNatsOperatorCustomBackendRejectsConfigArgument(t *testing.T) {
	backend := &natsStubBackend{data: map[string]interface{}{"kv:mykey": "custom-value"}}
	engine, err := graft.NewEngine(
		graft.WithFeatureFlags(backendRegistryFlagsEnabled()),
		graft.WithBackend(backend),
	)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	_, evalErr := evaluateYAML(t, engine, "value: (( nats \"kv:mykey\" \"nats://localhost:4222\" ))\n")
	if evalErr == nil {
		t.Fatalf("expected an error for a config argument against a registered custom \"nats\" backend")
	}
	if !strings.Contains(evalErr.Error(), "config argument is not supported against a custom") {
		t.Fatalf("error = %q, want it to mention the unsupported config argument", evalErr.Error())
	}
}

// TestNatsOperatorCustomBackendNotFoundWrapped proves a not-found result
// from a custom NATS backend surfaces as a *graft.GraftError{Type:
// ExternalError} wrapping *graft.BackendError - unlike vault, NATS has no
// Genesis-contract-pinned not-found string to match (see
// docs/spruce/genesis-compat-contract.md), so it uses the same generic
// wrapping as any other backend failure.
func TestNatsOperatorCustomBackendNotFoundWrapped(t *testing.T) {
	backend := &natsStubBackend{data: map[string]interface{}{}} // empty: every Get is not-found
	engine, err := graft.NewEngine(
		graft.WithFeatureFlags(backendRegistryFlagsEnabled()),
		graft.WithBackend(backend),
	)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	_, evalErr := evaluateYAML(t, engine, "value: (( nats \"kv:mykey\" ))\n")
	if evalErr == nil {
		t.Fatalf("expected an error")
	}
	if !strings.Contains(evalErr.Error(), "external_error") {
		t.Fatalf("error = %q, want it to contain \"external_error\" (GraftError.Type: ExternalError)", evalErr.Error())
	}
}
