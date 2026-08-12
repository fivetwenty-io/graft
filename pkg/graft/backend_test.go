package graft

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fivetwenty-io/graft/internal/features"
)

// stubBackend is a minimal, in-memory Backend used across this file's
// tests. calls records every Get invocation's path for assertions.
type stubBackend struct {
	name    string
	data    map[string]interface{}
	err     error
	callsMu sync.Mutex
	calls   []string
}

func newStubBackend(name string) *stubBackend {
	return &stubBackend{name: name, data: make(map[string]interface{})}
}

func (b *stubBackend) Name() string { return b.name }

func (b *stubBackend) Get(_ context.Context, path string) (interface{}, error) {
	b.callsMu.Lock()
	b.calls = append(b.calls, path)
	b.callsMu.Unlock()

	if b.err != nil {
		return nil, b.err
	}
	v, ok := b.data[path]
	if !ok {
		return nil, ErrBackendNotFound
	}
	return v, nil
}

func (b *stubBackend) GetBatch(ctx context.Context, paths []string) (map[string]interface{}, error) {
	return SequentialGetBatch(ctx, paths, b.Get)
}

func (b *stubBackend) Health(_ context.Context) error { return nil }
func (b *stubBackend) Close() error                   { return nil }

func (b *stubBackend) callCount() int {
	b.callsMu.Lock()
	defer b.callsMu.Unlock()
	return len(b.calls)
}

// targetedStubBackend adds GetWithTarget on top of stubBackend, keyed as
// "target\x00path".
type targetedStubBackend struct {
	stubBackend
}

func newTargetedStubBackend(name string) *targetedStubBackend {
	return &targetedStubBackend{stubBackend: *newStubBackend(name)}
}

func (b *targetedStubBackend) GetWithTarget(ctx context.Context, target, path string) (interface{}, error) {
	return b.Get(ctx, target+"\x00"+path)
}

// flakyBackend fails failures times before succeeding, for retry tests.
type flakyBackend struct {
	name     string
	failures int
	attempts int
	mu       sync.Mutex
}

func (b *flakyBackend) Name() string { return b.name }
func (b *flakyBackend) Get(_ context.Context, path string) (interface{}, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.attempts++
	if b.attempts <= b.failures {
		return nil, errors.New("transient failure")
	}
	return "ok:" + path, nil
}
func (b *flakyBackend) GetBatch(ctx context.Context, paths []string) (map[string]interface{}, error) {
	return SequentialGetBatch(ctx, paths, b.Get)
}
func (b *flakyBackend) Health(_ context.Context) error { return nil }
func (b *flakyBackend) Close() error                   { return nil }

// recordingCache is a minimal in-memory BackendCache that records Get/Set
// calls for assertions.
type recordingCache struct {
	mu      sync.Mutex
	entries map[string]interface{}
	gets    int
	sets    int
}

func newRecordingCache() *recordingCache {
	return &recordingCache{entries: make(map[string]interface{})}
}

func (c *recordingCache) Get(key string) (interface{}, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gets++
	v, ok := c.entries[key]
	return v, ok
}

func (c *recordingCache) Set(key string, value interface{}, _ time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sets++
	c.entries[key] = value
}

func (c *recordingCache) Delete(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.entries, key)
}

func (c *recordingCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]interface{})
}

// recordingAuditLogger records every LogAccess call.
type recordingAuditLogger struct {
	mu    sync.Mutex
	calls []auditCall
}

type auditCall struct {
	backend, path string
	success       bool
	err           error
}

func (l *recordingAuditLogger) LogAccess(_ context.Context, backend, path string, success bool, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.calls = append(l.calls, auditCall{backend: backend, path: path, success: success, err: err})
}

func (l *recordingAuditLogger) count() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.calls)
}

func TestBackendRegistryDefaultsToEmpty(t *testing.T) {
	engine := NewDefaultEngine()

	if names := engine.ListBackends(); len(names) != 0 {
		t.Fatalf("expected no backends registered on a fresh engine, got %v", names)
	}
	if _, ok := engine.GetBackend("vault"); ok {
		t.Fatalf("expected GetBackend(\"vault\") to miss on a fresh engine")
	}
	if engine.IsFeatureEnabled(features.FeatureBackendRegistry) {
		t.Fatalf("expected FeatureBackendRegistry to default off")
	}
}

func TestRegisterBackendGetListUnregister(t *testing.T) {
	engine := NewDefaultEngine()
	b := newStubBackend("mystore")

	if err := engine.RegisterBackend(b); err != nil {
		t.Fatalf("RegisterBackend: %v", err)
	}

	got, ok := engine.GetBackend("mystore")
	if !ok {
		t.Fatalf("GetBackend(\"mystore\"): expected ok=true")
	}
	if got.Name() != "mystore" {
		t.Fatalf("GetBackend(\"mystore\").Name() = %q, want %q", got.Name(), "mystore")
	}

	if names := engine.ListBackends(); len(names) != 1 || names[0] != "mystore" {
		t.Fatalf("ListBackends() = %v, want [mystore]", names)
	}

	if err := engine.UnregisterBackend("mystore"); err != nil {
		t.Fatalf("UnregisterBackend: %v", err)
	}
	if _, ok := engine.GetBackend("mystore"); ok {
		t.Fatalf("GetBackend(\"mystore\") after UnregisterBackend: expected ok=false")
	}
	if err := engine.UnregisterBackend("mystore"); err == nil {
		t.Fatalf("UnregisterBackend on an already-unregistered name: expected an error")
	}
}

func TestRegisterBackendRejectsNilAndEmptyName(t *testing.T) {
	engine := NewDefaultEngine()

	if err := engine.RegisterBackend(nil); err == nil {
		t.Fatalf("RegisterBackend(nil): expected an error")
	}
	if err := engine.RegisterBackend(newStubBackend("")); err == nil {
		t.Fatalf("RegisterBackend with empty Name(): expected an error")
	}
}

func TestRegisterBackendOverwritesSilently(t *testing.T) {
	engine := NewDefaultEngine()
	first := newStubBackend("dup")
	first.data["p"] = "first"
	second := newStubBackend("dup")
	second.data["p"] = "second"

	if err := engine.RegisterBackend(first); err != nil {
		t.Fatalf("RegisterBackend(first): %v", err)
	}
	if err := engine.RegisterBackend(second); err != nil {
		t.Fatalf("RegisterBackend(second): %v", err)
	}

	got, _ := engine.GetBackend("dup")
	val, err := got.Get(context.Background(), "p")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "second" {
		t.Fatalf("Get(\"p\") = %v, want the second registration's value", val)
	}
}

func TestWithBackendRegistersAtConstruction(t *testing.T) {
	b := newStubBackend("ctorbackend")
	b.data["x"] = "y"

	eng, err := NewEngine(WithBackend(b))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	got, ok := eng.GetBackend("ctorbackend")
	if !ok {
		t.Fatalf("GetBackend(\"ctorbackend\"): expected ok=true")
	}
	val, err := got.Get(context.Background(), "x")
	if err != nil || val != "y" {
		t.Fatalf("Get(\"x\") = (%v, %v), want (\"y\", nil)", val, err)
	}
}

func TestWithBackendNilIsNoOp(t *testing.T) {
	eng, err := NewEngine(WithBackend(nil))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	if names := eng.ListBackends(); len(names) != 0 {
		t.Fatalf("ListBackends() = %v, want empty after WithBackend(nil)", names)
	}
}

func TestTargetedBackendCapabilityPreservedThroughWrapping(t *testing.T) {
	tb := newTargetedStubBackend("tstore")
	tb.data["prod\x00path"] = "prod-value"

	// No cache/retry/audit configured: wrapBackendForRegistry must still
	// preserve the TargetedBackend capability.
	wrapped := wrapBackendForRegistry("tstore", tb, nil, nil, nil)

	if _, ok := wrapped.(TargetedBackend); !ok {
		t.Fatalf("wrapped backend lost TargetedBackend capability")
	}

	got := wrapped.(TargetedBackend)
	val, err := got.GetWithTarget(context.Background(), "prod", "path")
	if err != nil {
		t.Fatalf("GetWithTarget: %v", err)
	}
	if val != "prod-value" {
		t.Fatalf("GetWithTarget = %v, want prod-value", val)
	}
}

func TestNonTargetedBackendStaysNonTargetedAfterWrapping(t *testing.T) {
	b := newStubBackend("plain")
	wrapped := wrapBackendForRegistry("plain", b, nil, nil, nil)

	if _, ok := wrapped.(TargetedBackend); ok {
		t.Fatalf("a plain Backend must not gain TargetedBackend capability from wrapping")
	}
}

func TestBackendCacheWrapperShortCircuitsSecondGet(t *testing.T) {
	engine := NewDefaultEngine()
	b := newStubBackend("cached")
	b.data["p"] = "v"
	cache := newRecordingCache()

	engine.backendCaches = map[string]BackendCache{"cached": cache}
	if err := engine.RegisterBackend(b); err != nil {
		t.Fatalf("RegisterBackend: %v", err)
	}

	backend, _ := engine.GetBackend("cached")
	ctx := context.Background()

	if _, err := backend.Get(ctx, "p"); err != nil {
		t.Fatalf("first Get: %v", err)
	}
	if _, err := backend.Get(ctx, "p"); err != nil {
		t.Fatalf("second Get: %v", err)
	}

	if got := b.callCount(); got != 1 {
		t.Fatalf("underlying backend Get called %d times, want 1 (second call should hit cache)", got)
	}
	if cache.sets != 1 {
		t.Fatalf("cache.Set called %d times, want 1", cache.sets)
	}
}

func TestBackendRetryWrapperRetriesUntilSuccess(t *testing.T) {
	engine := NewDefaultEngine()
	flaky := &flakyBackend{name: "flaky", failures: 2}

	engine.backendRetry = map[string]RetryConfig{
		"flaky": {MaxAttempts: 5, InitialInterval: time.Millisecond, Multiplier: 1},
	}
	if err := engine.RegisterBackend(flaky); err != nil {
		t.Fatalf("RegisterBackend: %v", err)
	}

	backend, _ := engine.GetBackend("flaky")
	val, err := backend.Get(context.Background(), "x")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if val != "ok:x" {
		t.Fatalf("Get = %v, want ok:x", val)
	}
	if flaky.attempts != 3 {
		t.Fatalf("attempts = %d, want 3 (2 failures + 1 success)", flaky.attempts)
	}
}

func TestBackendRetryWrapperGivesUpAfterMaxAttempts(t *testing.T) {
	engine := NewDefaultEngine()
	flaky := &flakyBackend{name: "alwaysfails", failures: 100}

	engine.backendRetry = map[string]RetryConfig{
		"alwaysfails": {MaxAttempts: 3, InitialInterval: time.Millisecond, Multiplier: 1},
	}
	if err := engine.RegisterBackend(flaky); err != nil {
		t.Fatalf("RegisterBackend: %v", err)
	}

	backend, _ := engine.GetBackend("alwaysfails")
	if _, err := backend.Get(context.Background(), "x"); err == nil {
		t.Fatalf("Get: expected an error after exhausting retries")
	}
	if flaky.attempts != 3 {
		t.Fatalf("attempts = %d, want 3 (MaxAttempts)", flaky.attempts)
	}
}

func TestBackendRetryWrapperDoesNotRetryNotFoundByDefault(t *testing.T) {
	engine := NewDefaultEngine()
	b := newStubBackend("notfound")
	// no data seeded: every Get returns ErrBackendNotFound

	engine.backendRetry = map[string]RetryConfig{
		"notfound": {MaxAttempts: 5, InitialInterval: time.Millisecond, Multiplier: 1},
	}
	if err := engine.RegisterBackend(b); err != nil {
		t.Fatalf("RegisterBackend: %v", err)
	}

	backend, _ := engine.GetBackend("notfound")
	if _, err := backend.Get(context.Background(), "missing"); !errors.Is(err, ErrBackendNotFound) {
		t.Fatalf("Get error = %v, want ErrBackendNotFound", err)
	}
	if got := b.callCount(); got != 1 {
		t.Fatalf("underlying backend Get called %d times, want 1 (not-found must not be retried by default)", got)
	}
}

func TestBackendAuditLoggerRecordsSuccessAndFailure(t *testing.T) {
	engine := NewDefaultEngine()
	b := newStubBackend("audited")
	b.data["ok"] = "value"
	audit := &recordingAuditLogger{}

	engine.auditLogger = audit
	if err := engine.RegisterBackend(b); err != nil {
		t.Fatalf("RegisterBackend: %v", err)
	}

	backend, _ := engine.GetBackend("audited")
	ctx := context.Background()
	if _, err := backend.Get(ctx, "ok"); err != nil {
		t.Fatalf("Get(\"ok\"): %v", err)
	}
	if _, err := backend.Get(ctx, "missing"); err == nil {
		t.Fatalf("Get(\"missing\"): expected an error")
	}

	if audit.count() != 2 {
		t.Fatalf("audit log recorded %d calls, want 2", audit.count())
	}
	audit.mu.Lock()
	first, second := audit.calls[0], audit.calls[1]
	audit.mu.Unlock()

	if !first.success || first.path != "ok" {
		t.Fatalf("first audit call = %+v, want success path=ok", first)
	}
	if second.success || second.path != "missing" {
		t.Fatalf("second audit call = %+v, want failure path=missing", second)
	}
}

func TestWithBackendRetryAndCacheAndAuditLoggerOptions(t *testing.T) {
	b := newStubBackend("full")
	b.data["k"] = "v"
	cache := newRecordingCache()
	audit := &recordingAuditLogger{}

	eng, err := NewEngine(
		WithBackend(b),
		WithBackendCache("full", cache),
		WithBackendRetry("full", RetryConfig{MaxAttempts: 2, InitialInterval: time.Millisecond}),
		WithAuditLogger(audit),
	)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	backend, ok := eng.GetBackend("full")
	if !ok {
		t.Fatalf("GetBackend(\"full\"): expected ok=true")
	}
	val, err := backend.Get(context.Background(), "k")
	if err != nil || val != "v" {
		t.Fatalf("Get(\"k\") = (%v, %v), want (\"v\", nil)", val, err)
	}
	if audit.count() != 1 {
		t.Fatalf("audit log recorded %d calls, want 1", audit.count())
	}
	if cache.sets != 1 {
		t.Fatalf("cache.Set called %d times, want 1", cache.sets)
	}
}

func TestSequentialGetBatch(t *testing.T) {
	b := newStubBackend("batch")
	b.data["a"] = "1"
	b.data["b"] = "2"

	results, err := b.GetBatch(context.Background(), []string{"a", "b", "missing"})
	if err != nil {
		t.Fatalf("GetBatch: %v", err)
	}
	if len(results) != 2 || results["a"] != "1" || results["b"] != "2" {
		t.Fatalf("GetBatch results = %v, want a=1 b=2 (missing omitted)", results)
	}
	if _, ok := results["missing"]; ok {
		t.Fatalf("GetBatch included a not-found path in results")
	}
}

func TestSequentialGetBatchPropagatesNonNotFoundError(t *testing.T) {
	b := newStubBackend("batcherr")
	b.err = errors.New("boom")

	if _, err := b.GetBatch(context.Background(), []string{"a"}); err == nil {
		t.Fatalf("GetBatch: expected an error to propagate")
	}
}

func TestBackendErrorErrorAndUnwrap(t *testing.T) {
	cause := errors.New("dial tcp: timeout")
	be := &BackendError{
		Backend: "vault", Target: "prod", Path: "secret/x", Message: cause.Error(), Cause: cause,
	}

	msg := be.Error()
	for _, want := range []string{"vault", "prod", "secret/x", "dial tcp: timeout"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("BackendError.Error() = %q, missing %q", msg, want)
		}
	}
	if !errors.Is(be, cause) {
		t.Fatalf("errors.Is(be, cause) = false, want true (Unwrap must expose Cause)")
	}
}

func TestBackendErrorWrappedInsideGraftError(t *testing.T) {
	cause := errors.New("boom")
	be := &BackendError{Backend: "vault", Message: cause.Error(), Cause: cause}
	wrapped := NewExternalError("vault", cause.Error(), be)

	if wrapped.Type != ExternalError {
		t.Fatalf("wrapped.Type = %v, want ExternalError", wrapped.Type)
	}

	var gotBackendErr *BackendError
	if !errors.As(error(wrapped), &gotBackendErr) {
		t.Fatalf("errors.As(wrapped, &BackendError{}) = false, want true")
	}
	if gotBackendErr != be {
		t.Fatalf("errors.As found a different *BackendError than the one wrapped")
	}
}

func TestWithBackendRegistryOption(t *testing.T) {
	on, err := NewEngine(WithBackendRegistry(true))
	if err != nil {
		t.Fatalf("NewEngine(WithBackendRegistry(true)): %v", err)
	}
	if !on.IsFeatureEnabled(features.FeatureBackendRegistry) {
		t.Fatalf("WithBackendRegistry(true): flag not enabled")
	}

	off, err := NewEngine(WithBackendRegistry(false))
	if err != nil {
		t.Fatalf("NewEngine(WithBackendRegistry(false)): %v", err)
	}
	if off.IsFeatureEnabled(features.FeatureBackendRegistry) {
		t.Fatalf("WithBackendRegistry(false): flag unexpectedly enabled")
	}

	unspecified, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine(): %v", err)
	}
	if unspecified.IsFeatureEnabled(features.FeatureBackendRegistry) {
		t.Fatalf("default engine: flag unexpectedly enabled")
	}
}

func TestWithBackendRegistryOverridesFeatureFlags(t *testing.T) {
	ff := features.DefaultFlags() // FeatureBackendRegistry off in ff
	eng, err := NewEngine(WithFeatureFlags(ff), WithBackendRegistry(true))
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	if !eng.IsFeatureEnabled(features.FeatureBackendRegistry) {
		t.Fatalf("WithBackendRegistry(true) did not override a WithFeatureFlags value with the flag off")
	}
}

func TestBackendRegistryConcurrentAccess(t *testing.T) {
	engine := NewDefaultEngine()
	const n = 50

	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			name := fmt.Sprintf("backend-%d", i%5)
			b := newStubBackend(name)
			_ = engine.RegisterBackend(b)
			_, _ = engine.GetBackend(name)
			_ = engine.ListBackends()
		}(i)
	}
	wg.Wait()

	// Every goroutine registered under one of 5 names; all 5 must be
	// present (no lost writes, no corrupted map under -race).
	if names := engine.ListBackends(); len(names) != 5 {
		t.Fatalf("ListBackends() = %v, want 5 distinct names", names)
	}
}
