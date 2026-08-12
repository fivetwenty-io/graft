package graft

import (
	"runtime"
	"testing"
)

// TestConfigure_NoCacheChangeDoesNotLeakGoroutines is the F2 regression
// guard for Configure's unconditional cache rebuild. DefaultEngine.Configure
// used to re-derive the cache on every call - including calls that never
// touch a cache-affecting field - dropping the outgoing ShardedCache
// without stopping its cleanupLoop goroutine (internal/cache/shard.go).
// Configure must skip the cache rebuild entirely when nothing
// cache-affecting (CacheInstance/EnableCache/CacheSize/CacheTTL, or the
// FeatureCaching flag that gates EnableCache) changed in the delta.
//
// Tolerance mirrors
// pkg/graft/operators/nested_operator_goroutine_test.go: goroutine counts
// are not perfectly quiet at rest (GC, finalizers, scheduler noise), so
// this asserts growth stays small and bounded, not exactly zero. The leak
// this guards against is linear in call count (one goroutine per rebuild),
// so N=50 with a generous tolerance still makes a real regression
// unmistakable next to constant-size noise.
func TestConfigure_NoCacheChangeDoesNotLeakGoroutines(t *testing.T) {
	const iterations = 50
	const tolerance = 10

	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	de, ok := engine.(*DefaultEngine)
	if !ok {
		t.Fatalf("NewEngine returned %T, want *DefaultEngine", engine)
	}

	// Warm-up call so any one-time lazy init isn't mistaken for a
	// per-call leak, then let steady-state background goroutines settle
	// before the baseline is captured.
	if err := de.Configure(WithSkipVault(true)); err != nil {
		t.Fatalf("Configure warm-up: %v", err)
	}
	runtime.GC()
	runtime.Gosched()
	before := runtime.NumGoroutine()

	for i := 0; i < iterations; i++ {
		// Alternates two options that touch skip flags only - no
		// CacheInstance, EnableCache, CacheSize, CacheTTL, or
		// FeatureFlags field in the delta.
		if i%2 == 0 {
			err = de.Configure(WithSkipVault(true), WithSkipAws(false))
		} else {
			err = de.Configure(WithSkipVault(false), WithSkipAws(true))
		}
		if err != nil {
			t.Fatalf("Configure #%d failed: %v", i, err)
		}
	}

	runtime.GC()
	runtime.Gosched()
	after := runtime.NumGoroutine()

	delta := after - before
	if delta > tolerance {
		t.Fatalf("runtime.NumGoroutine() grew by %d over %d no-cache-change Configure calls (before=%d, after=%d), want growth <= %d — Configure is rebuilding (and leaking) the cache on calls that never touch a cache-affecting field", delta, iterations, before, after, tolerance)
	}
}

// TestConfigure_CacheRebuildDoesNotLeakGoroutines is the other half of the
// F2 regression guard: when a Configure call DOES change a cache-affecting
// field and a rebuild happens, the outgoing cache must be closed (stopping
// its cleanupLoop goroutine) rather than dropped in place.
func TestConfigure_CacheRebuildDoesNotLeakGoroutines(t *testing.T) {
	const iterations = 30
	const tolerance = 15

	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	de, ok := engine.(*DefaultEngine)
	if !ok {
		t.Fatalf("NewEngine returned %T, want *DefaultEngine", engine)
	}

	if err := de.Configure(WithCacheSize(1000)); err != nil {
		t.Fatalf("Configure warm-up: %v", err)
	}
	runtime.GC()
	runtime.Gosched()
	before := runtime.NumGoroutine()

	for i := 0; i < iterations; i++ {
		// WithCacheSize changes CacheSize on every call, forcing a
		// rebuild each time.
		if err := de.Configure(WithCacheSize(1000 + i)); err != nil {
			t.Fatalf("Configure #%d failed: %v", i, err)
		}
	}

	runtime.GC()
	runtime.Gosched()
	after := runtime.NumGoroutine()

	delta := after - before
	if delta > tolerance {
		t.Fatalf("runtime.NumGoroutine() grew by %d over %d cache-rebuilding Configure calls (before=%d, after=%d), want growth <= %d — outgoing caches are not being Close()d when Configure rebuilds the cache", delta, iterations, before, after, tolerance)
	}
}
