package graft

import (
	"context"
	"testing"
)

// These tests prove the :nocache modifier actually bypasses the registry's
// backend cache wrapper, for each operator that consults the registry
// (vault, awsparam, nats — awssecret shares awsparam's code path). The
// contract, in one flow per backend:
//
//  1. two :nocache fetches hit the backend twice (no cache read),
//  2. a following plain fetch hits the backend again (the nocache fetches
//     wrote nothing — a nocache fetch must not poison or refresh the
//     shared entry),
//  3. a second plain fetch is served from cache (the plain fetch wrote),
//  4. a final :nocache fetch hits the backend even though a cache entry
//     now exists (no cache read despite a warm cache).
func TestNoCacheBypassesRegistryBackendCache(t *testing.T) {
	run := func(t *testing.T, m *MockEngine, src string) {
		t.Helper()
		doc, err := m.ParseYAML([]byte(src))
		if err != nil {
			t.Fatalf("parse failed: %v", err)
		}
		result, err := m.Merge(context.Background(), doc).Execute()
		if err != nil {
			t.Fatalf("merge failed: %v", err)
		}
		v, err := result.Get("v")
		if err != nil || v != "s3cr3t" {
			t.Fatalf("expected v to resolve to s3cr3t, got %v (err %v)", v, err)
		}
	}

	assertCalls := func(t *testing.T, got, want int, step string) {
		t.Helper()
		if got != want {
			t.Fatalf("%s: expected %d backend calls, got %d", step, want, got)
		}
	}

	t.Run("vault", func(t *testing.T) {
		m := NewMockEngine(WithBackendCache("vault", newRecordingCache()))
		m.MockVault("secret/creds:pass", "s3cr3t")

		nocacheDoc := "v: (( vault:nocache \"secret/creds:pass\" ))\n"
		plainDoc := "v: (( vault \"secret/creds:pass\" ))\n"

		run(t, m, nocacheDoc)
		assertCalls(t, len(m.VaultCalls()), 1, "first nocache fetch")
		run(t, m, nocacheDoc)
		assertCalls(t, len(m.VaultCalls()), 2, "second nocache fetch (no cache read)")
		run(t, m, plainDoc)
		assertCalls(t, len(m.VaultCalls()), 3, "plain fetch after nocache (no cache write happened)")
		run(t, m, plainDoc)
		assertCalls(t, len(m.VaultCalls()), 3, "second plain fetch (served from cache)")
		run(t, m, nocacheDoc)
		assertCalls(t, len(m.VaultCalls()), 4, "nocache fetch with warm cache (read skipped)")
	})

	t.Run("awsparam", func(t *testing.T) {
		m := NewMockEngine(WithBackendCache("awsparam", newRecordingCache()))
		m.MockAWSParam("app/password", "s3cr3t")

		nocacheDoc := "v: (( awsparam:nocache \"app/password\" ))\n"
		plainDoc := "v: (( awsparam \"app/password\" ))\n"

		run(t, m, nocacheDoc)
		run(t, m, nocacheDoc)
		assertCalls(t, len(m.AWSCalls()), 2, "two nocache fetches")
		run(t, m, plainDoc)
		assertCalls(t, len(m.AWSCalls()), 3, "plain fetch after nocache (no cache write happened)")
		run(t, m, plainDoc)
		assertCalls(t, len(m.AWSCalls()), 3, "second plain fetch (served from cache)")
		run(t, m, nocacheDoc)
		assertCalls(t, len(m.AWSCalls()), 4, "nocache fetch with warm cache (read skipped)")
	})

	t.Run("nats", func(t *testing.T) {
		m := NewMockEngine(WithBackendCache("nats", newRecordingCache()))
		m.MockNATS("kv:config/creds", "s3cr3t")

		nocacheDoc := "v: (( nats:nocache \"kv:config/creds\" ))\n"
		plainDoc := "v: (( nats \"kv:config/creds\" ))\n"

		run(t, m, nocacheDoc)
		run(t, m, nocacheDoc)
		assertCalls(t, len(m.NATSCalls()), 2, "two nocache fetches")
		run(t, m, plainDoc)
		assertCalls(t, len(m.NATSCalls()), 3, "plain fetch after nocache (no cache write happened)")
		run(t, m, plainDoc)
		assertCalls(t, len(m.NATSCalls()), 3, "second plain fetch (served from cache)")
		run(t, m, nocacheDoc)
		assertCalls(t, len(m.NATSCalls()), 4, "nocache fetch with warm cache (read skipped)")
	})
}

// TestNoCacheDoesNotContaminateCacheKeys pins that the modifier changes
// cache BEHAVIOR only, never cache IDENTITY: a plain fetch after a nocache
// fetch looks up the exact same key a plain-only flow would use, so the
// modifier can neither split one logical entry into two nor collide two
// distinct ones.
func TestNoCacheDoesNotContaminateCacheKeys(t *testing.T) {
	cache := newRecordingCache()
	m := NewMockEngine(WithBackendCache("vault", cache))
	m.MockVault("secret/creds:pass", "s3cr3t")

	docs := []string{
		"v: (( vault:nocache \"secret/creds:pass\" ))\n",
		"v: (( vault \"secret/creds:pass\" ))\n",
	}
	for _, src := range docs {
		doc, err := m.ParseYAML([]byte(src))
		if err != nil {
			t.Fatalf("parse failed: %v", err)
		}
		if _, err := m.Merge(context.Background(), doc).Execute(); err != nil {
			t.Fatalf("merge failed: %v", err)
		}
	}

	// Only the plain fetch stored anything, under the unmodified path key.
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.sets != 1 {
		t.Fatalf("expected exactly one cache write (the plain fetch), got %d", cache.sets)
	}
	if _, ok := cache.entries["secret/creds:pass"]; !ok || len(cache.entries) != 1 {
		t.Fatalf("cache key contaminated by modifier: entries %v", cache.entries)
	}
}

// TestNoCacheHonoredOnNestedOperatorCalls pins that the modifier survives
// argument position: (( concat "p-" (vault:nocache ...) )) must bypass
// the backend cache exactly like the top-level form — every merge hits
// the backend, and nothing is ever written to the cache. The nested
// evaluation path builds its own Opcall (operators'
// evaluateNestedOperator) rather than running the parser's, so a nested
// call that drops the parsed flag silently serves a stale cached secret.
func TestNoCacheHonoredOnNestedOperatorCalls(t *testing.T) {
	cache := newRecordingCache()
	m := NewMockEngine(WithBackendCache("vault", cache))
	m.MockVault("secret/creds:pass", "s3cr3t")

	src := "v: (( concat \"p-\" (vault:nocache \"secret/creds:pass\") ))\n"
	for i := 1; i <= 3; i++ {
		doc, err := m.ParseYAML([]byte(src))
		if err != nil {
			t.Fatalf("parse failed: %v", err)
		}
		result, err := m.Merge(context.Background(), doc).Execute()
		if err != nil {
			t.Fatalf("merge %d failed: %v", i, err)
		}
		v, err := result.Get("v")
		if err != nil || v != "p-s3cr3t" {
			t.Fatalf("merge %d: expected v to resolve to p-s3cr3t, got %v (err %v)", i, v, err)
		}
		if got := len(m.VaultCalls()); got != i {
			t.Fatalf("merge %d: expected %d backend calls (no cache read), got %d", i, i, got)
		}
	}

	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.sets != 0 {
		t.Fatalf("nested nocache fetch wrote to the cache %d time(s)", cache.sets)
	}
}
