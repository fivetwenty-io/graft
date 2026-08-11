package natsbackend

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// hookDebugFunc installs a capturing DebugFunc for the duration of the
// test and restores the previous one on cleanup.
func hookDebugFunc(t *testing.T) *[]string {
	t.Helper()
	var logs []string
	prev := DebugFunc
	DebugFunc = func(format string, args ...interface{}) {
		logs = append(logs, fmt.Sprintf(format, args...))
	}
	t.Cleanup(func() { DebugFunc = prev })
	return &logs
}

// TestFetchFromKVCached_TargetNamespaced verifies that the same storePath
// on two different targets is fetched and cached independently: before
// this wrapper existed, FetchFromKV's own internal cache key was just
// "kv:<storePath>" with no target component, so a second target's lookup
// of the same path could silently return the first target's cached value
// without ever calling the second target's real store.
func TestFetchFromKVCached_TargetNamespaced(t *testing.T) {
	ClearCache()
	t.Cleanup(ClearCache)

	callsA := 0
	fetchA := func() (interface{}, error) {
		callsA++
		return "value-from-A", nil
	}
	callsB := 0
	fetchB := func() (interface{}, error) {
		callsB++
		return "value-from-B", nil
	}

	valA, err := FetchFromKVCachedWith("targetA", "store/key", 5*time.Minute, false, fetchA)
	if err != nil {
		t.Fatalf("targetA: unexpected error: %v", err)
	}
	valB, err := FetchFromKVCachedWith("targetB", "store/key", 5*time.Minute, false, fetchB)
	if err != nil {
		t.Fatalf("targetB: unexpected error: %v", err)
	}

	if valA != "value-from-A" {
		t.Errorf("targetA: expected value-from-A, got %v", valA)
	}
	if valB != "value-from-B" {
		t.Errorf("targetB: expected value-from-B, got %v (target leak into shared cache key)", valB)
	}
	if callsA != 1 || callsB != 1 {
		t.Errorf("expected 1 fetch per target, got callsA=%d callsB=%d", callsA, callsB)
	}

	// Re-fetching targetA must hit its own cache entry, not targetB's.
	valA2, err := FetchFromKVCachedWith("targetA", "store/key", 5*time.Minute, false, fetchA)
	if err != nil {
		t.Fatalf("targetA re-fetch: unexpected error: %v", err)
	}
	if valA2 != "value-from-A" {
		t.Errorf("targetA re-fetch: expected value-from-A (cached), got %v", valA2)
	}
	if callsA != 1 {
		t.Errorf("expected targetA's second call to be served from cache, got %d underlying fetches", callsA)
	}
}

// TestFetchFromKVCached_ConcurrentSameKeyDedupes proves the request-dedup
// guarantee for the NATS KV path: concurrent requests for the identical
// (target, storePath) coalesce into one underlying fetch.
func TestFetchFromKVCached_ConcurrentSameKeyDedupes(t *testing.T) {
	ClearCache()
	t.Cleanup(ClearCache)

	var calls int32
	fetch := func() (interface{}, error) {
		atomic.AddInt32(&calls, 1)
		time.Sleep(80 * time.Millisecond)
		return "shared", nil
	}

	const n = 25
	var startGate sync.WaitGroup
	startGate.Add(1)
	var wg sync.WaitGroup
	results := make([]interface{}, n)
	errs := make([]error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			startGate.Wait()
			v, err := FetchFromKVCachedWith("prod", "store/shared-key", 5*time.Minute, false, fetch)
			results[idx] = v
			errs[idx] = err
		}(i)
	}
	startGate.Done()
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("expected exactly 1 underlying fetch for %d concurrent callers, got %d", n, got)
	}
	for i := range results {
		if errs[i] != nil {
			t.Fatalf("caller %d: unexpected error: %v", i, errs[i])
		}
		if results[i] != "shared" {
			t.Fatalf("caller %d: expected %q, got %v", i, "shared", results[i])
		}
	}
}

// TestFetchFromObjectCached_TargetNamespaced mirrors the KV test for the
// object-store cache namespace.
func TestFetchFromObjectCached_TargetNamespaced(t *testing.T) {
	ClearCache()
	t.Cleanup(ClearCache)

	callsA, callsB := 0, 0
	valA, err := FetchFromObjectCachedWith("targetA", "bucket/obj", 5*time.Minute, false, func() (interface{}, error) {
		callsA++
		return "obj-from-A", nil
	})
	if err != nil {
		t.Fatalf("targetA: unexpected error: %v", err)
	}
	valB, err := FetchFromObjectCachedWith("targetB", "bucket/obj", 5*time.Minute, false, func() (interface{}, error) {
		callsB++
		return "obj-from-B", nil
	})
	if err != nil {
		t.Fatalf("targetB: unexpected error: %v", err)
	}

	if valA != "obj-from-A" {
		t.Errorf("targetA: expected obj-from-A, got %v", valA)
	}
	if valB != "obj-from-B" {
		t.Errorf("targetB: expected obj-from-B, got %v (target leak)", valB)
	}
	if callsA != 1 || callsB != 1 {
		t.Errorf("expected 1 fetch per target, got callsA=%d callsB=%d", callsA, callsB)
	}
}

// TestFetchFromKVCachedWith_AuditLogsOnMissAndHit is a regression test:
// op_nats.go's old fetchFromKV emitted an "AUDIT: Accessing KV
// store" line before checking the cache, so both a miss and a later hit
// for the same path were audited. Moving the cache into this package
// dropped the hit-path line - FetchFromKV (the miss-only real backend
// call) still logs its own "Accessing", but nothing logged a hit. This
// asserts both a miss and a hit produce an audit line when auditLogging
// is true, and neither does when it is false.
func TestFetchFromKVCachedWith_AuditLogsOnMissAndHit(t *testing.T) {
	ClearCache()
	t.Cleanup(ClearCache)
	logs := hookDebugFunc(t)

	fetch := func() (interface{}, error) { return "v", nil }

	// Miss.
	if _, err := FetchFromKVCachedWith("audit-target", "store/audit-key", 5*time.Minute, true, fetch); err != nil {
		t.Fatalf("miss: unexpected error: %v", err)
	}
	if !anyContains(*logs, "AUDIT") || !anyContains(*logs, "store/audit-key") {
		t.Errorf("expected an AUDIT log line naming the path on a miss, got: %v", *logs)
	}

	*logs = nil

	// Hit - fetch must not run, but the access must still be audited.
	if _, err := FetchFromKVCachedWith("audit-target", "store/audit-key", 5*time.Minute, true, func() (interface{}, error) {
		t.Fatal("fetch should not run on a cache hit")
		return nil, nil
	}); err != nil {
		t.Fatalf("hit: unexpected error: %v", err)
	}
	if !anyContains(*logs, "AUDIT") || !anyContains(*logs, "store/audit-key") {
		t.Errorf("expected an AUDIT log line naming the path on a hit (a hit is still an access), got: %v", *logs)
	}
}

// TestFetchFromKVCachedWith_NoAuditWhenDisabled verifies auditLogging=false
// produces no AUDIT lines from this seam, on either a miss or a hit.
func TestFetchFromKVCachedWith_NoAuditWhenDisabled(t *testing.T) {
	ClearCache()
	t.Cleanup(ClearCache)
	logs := hookDebugFunc(t)

	fetch := func() (interface{}, error) { return "v", nil }

	if _, err := FetchFromKVCachedWith("no-audit-target", "store/key", 5*time.Minute, false, fetch); err != nil {
		t.Fatalf("miss: unexpected error: %v", err)
	}
	if _, err := FetchFromKVCachedWith("no-audit-target", "store/key", 5*time.Minute, false, fetch); err != nil {
		t.Fatalf("hit: unexpected error: %v", err)
	}
	if anyContains(*logs, "AUDIT") {
		t.Errorf("expected no AUDIT lines with auditLogging=false, got: %v", *logs)
	}
}

// TestFetchFromKVCachedWith_MissAuditNotDoubled verifies the miss path
// logs "Accessing" exactly once (from this seam), not twice (this seam
// plus FetchFromKV's own line) - the fix removes the duplicate from
// FetchFromKV/FetchFromObject in client.go rather than adding a second
// copy here.
func TestFetchFromKVCachedWith_MissAuditNotDoubled(t *testing.T) {
	ClearCache()
	t.Cleanup(ClearCache)
	logs := hookDebugFunc(t)

	if _, err := FetchFromKVCachedWith("dup-target", "store/dup-key", 5*time.Minute, true, func() (interface{}, error) {
		return "v", nil
	}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	count := 0
	for _, l := range *logs {
		if strings.Contains(l, "AUDIT") && strings.Contains(l, "Accessing") && strings.Contains(l, "store/dup-key") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("expected exactly 1 \"AUDIT: Accessing\" line for a miss, got %d: %v", count, *logs)
	}
}

// TestFetchFromKVCachedWith_HitRecordsMeasuredDuration is a regression
// test: the cache-hit path called
// GlobalMetrics.RecordOperation with a hardcoded 0 duration instead of
// measuring elapsed time. A hardcoded 0 means TotalDuration never
// increases across any number of hits; a measured value does.
func TestFetchFromKVCachedWith_HitRecordsMeasuredDuration(t *testing.T) {
	ClearCache()
	t.Cleanup(ClearCache)

	// Prime the cache with a miss.
	if _, err := FetchFromKVCachedWith("measure-target", "store/measure-key", 5*time.Minute, false, func() (interface{}, error) {
		return "v", nil
	}); err != nil {
		t.Fatalf("priming miss: unexpected error: %v", err)
	}

	before := GlobalMetrics.GetStats()["kv"]

	if _, err := FetchFromKVCachedWith("measure-target", "store/measure-key", 5*time.Minute, false, func() (interface{}, error) {
		t.Fatal("fetch should not run on a cache hit")
		return nil, nil
	}); err != nil {
		t.Fatalf("hit: unexpected error: %v", err)
	}

	after := GlobalMetrics.GetStats()["kv"]
	if after.CacheHits != before.CacheHits+1 {
		t.Fatalf("expected CacheHits to increment by 1, before=%d after=%d", before.CacheHits, after.CacheHits)
	}
	if after.TotalDuration <= before.TotalDuration {
		t.Errorf("expected the cache-hit duration to be measured (TotalDuration to increase), before=%v after=%v - looks hardcoded to 0", before.TotalDuration, after.TotalDuration)
	}
}

// TestFetchFromObjectCachedWith_AuditLogsOnMissAndHit and
// TestFetchFromObjectCachedWith_HitRecordsMeasuredDuration mirror the KV
// tests above for the object-store seam - the same audit-logging and
// duration-measurement bugs affected FetchFromObjectCachedWith
// identically.
func TestFetchFromObjectCachedWith_AuditLogsOnMissAndHit(t *testing.T) {
	ClearCache()
	t.Cleanup(ClearCache)
	logs := hookDebugFunc(t)

	if _, err := FetchFromObjectCachedWith("audit-target", "bucket/audit-key", 5*time.Minute, true, func() (interface{}, error) {
		return "v", nil
	}); err != nil {
		t.Fatalf("miss: unexpected error: %v", err)
	}
	if !anyContains(*logs, "AUDIT") || !anyContains(*logs, "bucket/audit-key") {
		t.Errorf("expected an AUDIT log line naming the path on a miss, got: %v", *logs)
	}

	*logs = nil

	if _, err := FetchFromObjectCachedWith("audit-target", "bucket/audit-key", 5*time.Minute, true, func() (interface{}, error) {
		t.Fatal("fetch should not run on a cache hit")
		return nil, nil
	}); err != nil {
		t.Fatalf("hit: unexpected error: %v", err)
	}
	if !anyContains(*logs, "AUDIT") || !anyContains(*logs, "bucket/audit-key") {
		t.Errorf("expected an AUDIT log line naming the path on a hit, got: %v", *logs)
	}
}

func TestFetchFromObjectCachedWith_HitRecordsMeasuredDuration(t *testing.T) {
	ClearCache()
	t.Cleanup(ClearCache)

	if _, err := FetchFromObjectCachedWith("measure-target", "bucket/measure-key", 5*time.Minute, false, func() (interface{}, error) {
		return "v", nil
	}); err != nil {
		t.Fatalf("priming miss: unexpected error: %v", err)
	}

	before := GlobalMetrics.GetStats()[StoreObj]

	if _, err := FetchFromObjectCachedWith("measure-target", "bucket/measure-key", 5*time.Minute, false, func() (interface{}, error) {
		t.Fatal("fetch should not run on a cache hit")
		return nil, nil
	}); err != nil {
		t.Fatalf("hit: unexpected error: %v", err)
	}

	after := GlobalMetrics.GetStats()[StoreObj]
	if after.CacheHits != before.CacheHits+1 {
		t.Fatalf("expected CacheHits to increment by 1, before=%d after=%d", before.CacheHits, after.CacheHits)
	}
	if after.TotalDuration <= before.TotalDuration {
		t.Errorf("expected the cache-hit duration to be measured, before=%v after=%v - looks hardcoded to 0", before.TotalDuration, after.TotalDuration)
	}
}

// anyContains reports whether any string in ss contains substr.
func anyContains(ss []string, substr string) bool {
	for _, s := range ss {
		if strings.Contains(s, substr) {
			return true
		}
	}
	return false
}
