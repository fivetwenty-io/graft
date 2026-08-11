package vault

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestSecretCache_GetOrFetch_CachesAfterFirstFetch verifies a second call
// for the same path does not invoke fetch again.
func TestSecretCache_GetOrFetch_CachesAfterFirstFetch(t *testing.T) {
	c := &secretCache{data: make(map[string]map[string]interface{})}

	var calls int32
	fetch := func() (map[string]interface{}, error) {
		atomic.AddInt32(&calls, 1)
		return map[string]interface{}{"key": "value"}, nil
	}

	for i := 0; i < 5; i++ {
		v, err := c.GetOrFetch("secret/db", fetch)
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
		if v["key"] != "value" {
			t.Fatalf("call %d: expected cached value, got %v", i, v)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("expected exactly 1 fetch across 5 calls for the same path, got %d", got)
	}
}

// TestSecretCache_GetOrFetch_ConcurrentSameKeyDedupes proves D2 for vault:
// concurrent requests for the identical cache key coalesce into one
// backend fetch.
func TestSecretCache_GetOrFetch_ConcurrentSameKeyDedupes(t *testing.T) {
	c := &secretCache{data: make(map[string]map[string]interface{})}

	var calls int32
	fetch := func() (map[string]interface{}, error) {
		atomic.AddInt32(&calls, 1)
		time.Sleep(80 * time.Millisecond)
		return map[string]interface{}{"password": "hunter2"}, nil
	}

	const n = 25
	var startGate sync.WaitGroup
	startGate.Add(1)
	var wg sync.WaitGroup
	results := make([]map[string]interface{}, n)
	errs := make([]error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			startGate.Wait()
			v, err := c.GetOrFetch("prod\x00secret/shared", fetch)
			results[idx] = v
			errs[idx] = err
		}(i)
	}
	startGate.Done()
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("expected exactly 1 underlying fetch for %d concurrent callers of the same key, got %d", n, got)
	}
	for i := range results {
		if errs[i] != nil {
			t.Fatalf("caller %d: unexpected error: %v", i, errs[i])
		}
		if results[i]["password"] != "hunter2" {
			t.Fatalf("caller %d: expected shared fetch result, got %v", i, results[i])
		}
	}
}

// TestSecretCache_GetOrFetch_DifferentTargetKeysNeverCollide verifies two
// keys namespaced by different targets (the "target\x00path" scheme
// op_vault.go's performVaultLookup uses) are fetched and cached
// independently.
func TestSecretCache_GetOrFetch_DifferentTargetKeysNeverCollide(t *testing.T) {
	c := &secretCache{data: make(map[string]map[string]interface{})}

	a, err := c.GetOrFetch("prod\x00secret/db", func() (map[string]interface{}, error) {
		return map[string]interface{}{"env": "prod"}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, err := c.GetOrFetch("staging\x00secret/db", func() (map[string]interface{}, error) {
		return map[string]interface{}{"env": "staging"}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if a["env"] != "prod" {
		t.Errorf("prod: expected env=prod, got %v", a)
	}
	if b["env"] != "staging" {
		t.Errorf("staging: expected env=staging, got %v", b)
	}
}

// TestSecretCache_GetOrFetch_FetchErrorNotCached verifies a failed fetch is
// not cached, so a later call retries.
func TestSecretCache_GetOrFetch_FetchErrorNotCached(t *testing.T) {
	c := &secretCache{data: make(map[string]map[string]interface{})}

	var calls int32
	_, err := c.GetOrFetch("secret/flaky", func() (map[string]interface{}, error) {
		atomic.AddInt32(&calls, 1)
		return nil, &ErrNotFound{Path: "secret/flaky"}
	})
	if err == nil {
		t.Fatal("expected error from first fetch")
	}

	v, err := c.GetOrFetch("secret/flaky", func() (map[string]interface{}, error) {
		atomic.AddInt32(&calls, 1)
		return map[string]interface{}{"key": "recovered"}, nil
	})
	if err != nil {
		t.Fatalf("unexpected error on retry: %v", err)
	}
	if v["key"] != "recovered" {
		t.Errorf("expected recovered value, got %v", v)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("expected 2 fetches (failed + retry), got %d", got)
	}
}
