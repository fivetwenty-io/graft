package reqdedup

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestGroup_ConcurrentSameKeyCallsOnce verifies that N goroutines calling Do
// with the same key while the first call is still in flight all receive the
// same result from exactly one underlying fetch - this is the core request
// dedup guarantee: N references to the same secret produce one backend
// request.
func TestGroup_ConcurrentSameKeyCallsOnce(t *testing.T) {
	var g Group[string]
	var calls atomic.Int64

	// The fetch itself sleeps, rather than the test externally releasing
	// it, so there is no window where a slow-to-schedule goroutine misses
	// the in-flight call and starts its own (see
	// TestGroup_SequentialCallsAfterCompletionRunAgain for that case).
	fetch := func() (string, error) {
		calls.Add(1)
		time.Sleep(100 * time.Millisecond)
		return "value", nil
	}

	const n = 20
	var startGate sync.WaitGroup
	startGate.Add(1)
	var wg sync.WaitGroup
	results := make([]string, n)
	errs := make([]error, n)

	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			startGate.Wait() // release all goroutines together
			v, err := g.Do("same-key", fetch)
			results[idx] = v
			errs[idx] = err
		}(i)
	}

	startGate.Done()
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 underlying fetch for %d concurrent callers of the same key, got %d", n, got)
	}
	for i, v := range results {
		if errs[i] != nil {
			t.Fatalf("caller %d: unexpected error: %v", i, errs[i])
		}
		if v != "value" {
			t.Fatalf("caller %d: expected %q, got %q", i, "value", v)
		}
	}
}

// TestGroup_DistinctKeysCallSeparately verifies that different keys are not
// coalesced together - each distinct key gets its own underlying fetch, and
// per-target separation is preserved when callers key by "target\x00path"
// or similar.
func TestGroup_DistinctKeysCallSeparately(t *testing.T) {
	var g Group[int]
	var calls atomic.Int64

	var wg sync.WaitGroup
	keys := []string{"prod\x00secret/db", "staging\x00secret/db", "prod\x00secret/api"}
	results := make([]int, len(keys))

	for i, key := range keys {
		wg.Add(1)
		go func(idx int, k string) {
			defer wg.Done()
			v, err := g.Do(k, func() (int, error) {
				calls.Add(1)
				return idx, nil
			})
			if err != nil {
				t.Errorf("key %s: unexpected error: %v", k, err)
			}
			results[idx] = v
		}(i, key)
	}
	wg.Wait()

	if got := calls.Load(); got != int64(len(keys)) {
		t.Fatalf("expected %d underlying fetches for %d distinct keys, got %d", len(keys), len(keys), got)
	}
	for i := range keys {
		if results[i] != i {
			t.Fatalf("key %d: expected result %d, got %d", i, i, results[i])
		}
	}
}

// TestGroup_ErrorPropagatesToAllWaiters verifies that when the coalesced
// fetch fails, every concurrent caller waiting on it receives the error
// (not a cached/zero success value).
func TestGroup_ErrorPropagatesToAllWaiters(t *testing.T) {
	var g Group[string]
	wantErr := errors.New("backend unavailable")

	var calls atomic.Int64
	fetch := func() (string, error) {
		calls.Add(1)
		time.Sleep(100 * time.Millisecond)
		return "", wantErr
	}

	const n = 10
	var startGate sync.WaitGroup
	startGate.Add(1)
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			startGate.Wait()
			_, err := g.Do("key", fetch)
			errs[idx] = err
		}(i)
	}

	startGate.Done()
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("expected exactly 1 underlying fetch, got %d", got)
	}
	for i, err := range errs {
		if !errors.Is(err, wantErr) {
			t.Fatalf("caller %d: expected error %v, got %v", i, wantErr, err)
		}
	}
}

// TestGroup_SequentialCallsAfterCompletionRunAgain verifies that once an
// in-flight call for a key completes, a later Do call for the same key
// performs its own fresh fetch rather than being coalesced with a call that
// already finished - Do only coalesces genuinely concurrent callers, it is
// not a cache.
func TestGroup_SequentialCallsAfterCompletionRunAgain(t *testing.T) {
	var g Group[int]
	var calls atomic.Int64

	for i := 0; i < 3; i++ {
		v, err := g.Do("key", func() (int, error) {
			return int(calls.Add(1)), nil
		})
		if err != nil {
			t.Fatalf("call %d: unexpected error: %v", i, err)
		}
		if v != i+1 {
			t.Fatalf("call %d: expected fresh fetch result %d, got %d", i, i+1, v)
		}
	}
	if got := calls.Load(); got != 3 {
		t.Fatalf("expected 3 sequential fetches, got %d", got)
	}
}

// TestGroup_ZeroValueUsable verifies a zero-value Group works without
// explicit construction, matching singleflight.Group's ergonomics.
func TestGroup_ZeroValueUsable(t *testing.T) {
	var g Group[time.Duration]
	v, err := g.Do("k", func() (time.Duration, error) { return time.Second, nil })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if v != time.Second {
		t.Fatalf("expected %v, got %v", time.Second, v)
	}
}
