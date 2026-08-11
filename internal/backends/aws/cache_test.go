package aws

import (
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestClientPool_GetOrFetchSecret_ConcurrentReadWriteRace exercises the
// exact hazard the old GetSecretCache/SetSecretCache pair had: one
// goroutine reading a cache entry while another writes a new one for a
// different key in the same target, run under -race with many goroutines
// and many rounds. GetOrFetchSecret must never expose the backing map
// itself (only copied string values), so concurrent Get/Set-equivalent
// traffic across many keys cannot race on the map internals.
func TestClientPool_GetOrFetchSecret_ConcurrentReadWriteRace(t *testing.T) {
	pool := &ClientPool{
		secretsCache: make(map[string]map[string]string),
		paramsCache:  make(map[string]map[string]string),
	}

	const targets = 4
	const keysPerTarget = 10
	const rounds = 3

	for round := 0; round < rounds; round++ {
		var wg sync.WaitGroup
		for tI := 0; tI < targets; tI++ {
			target := fmt.Sprintf("target%d", tI)
			for kI := 0; kI < keysPerTarget; kI++ {
				secret := fmt.Sprintf("secret/%d", kI)
				wg.Add(1)
				go func(target, secret string, round int) {
					defer wg.Done()
					val, err := pool.GetOrFetchSecret(target, secret, func() (string, error) {
						return fmt.Sprintf("%s:%s:%d", target, secret, round), nil
					})
					if err != nil {
						t.Errorf("GetOrFetchSecret(%s, %s): unexpected error: %v", target, secret, err)
					}
					if val == "" {
						t.Errorf("GetOrFetchSecret(%s, %s): empty value", target, secret)
					}
				}(target, secret, round)
			}
		}
		wg.Wait()
	}
}

// TestClientPool_GetOrFetchSecret_CachesAfterFirstFetch verifies a second
// call for the same (target, secret) does not invoke fetch again.
func TestClientPool_GetOrFetchSecret_CachesAfterFirstFetch(t *testing.T) {
	pool := &ClientPool{
		secretsCache: make(map[string]map[string]string),
		paramsCache:  make(map[string]map[string]string),
	}

	var calls int32
	fetch := func() (string, error) {
		atomic.AddInt32(&calls, 1)
		return "the-secret-value", nil
	}

	for i := 0; i < 5; i++ {
		val, err := pool.GetOrFetchSecret("prod", "secret/db", fetch)
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
		if val != "the-secret-value" {
			t.Fatalf("call %d: expected cached value, got %q", i, val)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("expected exactly 1 fetch across 5 calls for the same key, got %d", got)
	}
}

// TestClientPool_GetOrFetchSecret_ConcurrentSameKeyDedupes proves D2:
// concurrent requests for the identical (target, secret) coalesce into one
// backend fetch rather than each missing the empty cache and fetching
// independently.
func TestClientPool_GetOrFetchSecret_ConcurrentSameKeyDedupes(t *testing.T) {
	pool := &ClientPool{
		secretsCache: make(map[string]map[string]string),
		paramsCache:  make(map[string]map[string]string),
	}

	var calls int32
	fetch := func() (string, error) {
		atomic.AddInt32(&calls, 1)
		time.Sleep(80 * time.Millisecond)
		return "shared-value", nil
	}

	const n = 25
	var startGate sync.WaitGroup
	startGate.Add(1)
	var wg sync.WaitGroup
	results := make([]string, n)
	errs := make([]error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			startGate.Wait()
			v, err := pool.GetOrFetchSecret("prod", "secret/shared", fetch)
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
		if results[i] != "shared-value" {
			t.Fatalf("caller %d: expected %q, got %q", i, "shared-value", results[i])
		}
	}
}

// TestClientPool_GetOrFetchSecret_DifferentTargetsNeverCollide verifies the
// same secret path on two different targets is fetched and cached
// independently (spec cluster A7 target separation carried into D2 dedup).
func TestClientPool_GetOrFetchSecret_DifferentTargetsNeverCollide(t *testing.T) {
	pool := &ClientPool{
		secretsCache: make(map[string]map[string]string),
		paramsCache:  make(map[string]map[string]string),
	}

	fetchFor := func(target string) func() (string, error) {
		return func() (string, error) { return "value-from-" + target, nil }
	}

	a, err := pool.GetOrFetchSecret("prod", "secret/db", fetchFor("prod"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	b, err := pool.GetOrFetchSecret("staging", "secret/db", fetchFor("staging"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if a != "value-from-prod" {
		t.Errorf("prod: expected value-from-prod, got %q", a)
	}
	if b != "value-from-staging" {
		t.Errorf("staging: expected value-from-staging, got %q", b)
	}
}

// TestClientPool_GetOrFetchSecret_FetchErrorNotCached verifies a failed
// fetch is not cached - a later call retries rather than replaying the
// error forever.
func TestClientPool_GetOrFetchSecret_FetchErrorNotCached(t *testing.T) {
	pool := &ClientPool{
		secretsCache: make(map[string]map[string]string),
		paramsCache:  make(map[string]map[string]string),
	}

	var calls int32
	_, err := pool.GetOrFetchSecret("prod", "secret/flaky", func() (string, error) {
		atomic.AddInt32(&calls, 1)
		return "", fmt.Errorf("temporary failure")
	})
	if err == nil {
		t.Fatal("expected error from first fetch")
	}

	val, err := pool.GetOrFetchSecret("prod", "secret/flaky", func() (string, error) {
		atomic.AddInt32(&calls, 1)
		return "recovered", nil
	})
	if err != nil {
		t.Fatalf("unexpected error on retry: %v", err)
	}
	if val != "recovered" {
		t.Errorf("expected recovered value, got %q", val)
	}
	if got := atomic.LoadInt32(&calls); got != 2 {
		t.Errorf("expected 2 fetches (failed + retry), got %d", got)
	}
}

// TestClientPool_GetOrFetchParam mirrors the secret tests for the
// parameter-store cache namespace.
func TestClientPool_GetOrFetchParam(t *testing.T) {
	pool := &ClientPool{
		secretsCache: make(map[string]map[string]string),
		paramsCache:  make(map[string]map[string]string),
	}

	var calls int32
	fetch := func() (string, error) {
		atomic.AddInt32(&calls, 1)
		return "param-value", nil
	}

	for i := 0; i < 3; i++ {
		val, err := pool.GetOrFetchParam("prod", "/app/config", fetch)
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
		if val != "param-value" {
			t.Fatalf("call %d: expected param-value, got %q", i, val)
		}
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("expected exactly 1 fetch, got %d", got)
	}
}
