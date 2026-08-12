package graft

import (
	"context"
	"fmt"
	"testing"
	"time"
)
// TestWithCacheSize_ObservableEffect proves WithCacheSize actually bounds
// the constructed cache's capacity (via eviction), not merely
// EngineOptions.CacheSize.
func TestWithCacheSize_ObservableEffect(t *testing.T) {
	small, err := NewEngine(WithCacheSize(1))
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	smallCache := small.(*DefaultEngine).GetCache()
	if smallCache == nil {
		t.Fatal("expected a cache instance (caching enabled by default)")
	}
	for i := 0; i < 200; i++ {
		smallCache.Set(fmt.Sprintf("key-%d", i), i)
	}
	if smallCache.Stats().Evictions == 0 {
		t.Error("WithCacheSize(1): expected evictions after inserting 200 entries, got none - WithCacheSize has no observable effect")
	}

	large, err := NewEngine(WithCacheSize(1_000_000))
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	largeCache := large.(*DefaultEngine).GetCache()
	if largeCache == nil {
		t.Fatal("expected a cache instance (caching enabled by default)")
	}
	for i := 0; i < 200; i++ {
		largeCache.Set(fmt.Sprintf("key-%d", i), i)
	}
	if evictions := largeCache.Stats().Evictions; evictions != 0 {
		t.Errorf("WithCacheSize(1_000_000): expected no evictions after inserting 200 entries, got %d", evictions)
	}
	if size := largeCache.Size(); size != 200 {
		t.Errorf("WithCacheSize(1_000_000): expected all 200 entries retained, got Size()=%d", size)
	}
}

// TestWithCacheDisabled_ObservableEffect proves WithCacheDisabled results
// in no cache instance being constructed at all.
func TestWithCacheDisabled_ObservableEffect(t *testing.T) {
	engine, err := NewEngine(WithCacheDisabled())
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	if c := engine.(*DefaultEngine).GetCache(); c != nil {
		t.Errorf("WithCacheDisabled(): expected no cache instance, got %T", c)
	}
}

// TestWithCacheTTL_ObservableEffect proves WithCacheTTL causes cache
// entries to actually expire, not merely records a TTL value nothing
// consults.
func TestWithCacheTTL_ObservableEffect(t *testing.T) {
	const ttl = 20 * time.Millisecond
	engine, err := NewEngine(WithCacheTTL(ttl))
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	c := engine.(*DefaultEngine).GetCache()
	if c == nil {
		t.Fatal("expected a cache instance (caching enabled by default)")
	}

	c.Set("k", "v")
	if _, found := c.Get("k"); !found {
		t.Fatal("expected entry to be present immediately after Set")
	}

	// Poll rather than a single fixed sleep, to tolerate scheduler jitter
	// without inflating the test's worst-case runtime.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, found := c.Get("k"); !found {
			return // expired, as expected
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("WithCacheTTL: entry never expired within 2s of a 20ms TTL")
}

// TestWithOperators_ObservableEffect proves WithOperators' bulk-registered
// operators are actually consulted during evaluation, the same way
// WithCustomOperator's single-operator form is (see
// custom_operator_eval_test.go), and that WithOperators merges into
// (rather than replaces) operators from an earlier WithCustomOperator call.
func TestWithOperators_ObservableEffect(t *testing.T) {
	engine, err := NewEngine(
		WithCustomOperator("upper", &upperOperator{}),
		WithOperators(map[string]Operator{"upper2": &upperOperator{}}),
	)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	doc := mustParseYAMLDoc(t, engine, "a: (( upper \"one\" ))\nb: (( upper2 \"two\" ))\n")
	result, err := engine.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}

	if got, err := result.GetString("a"); err != nil || got != "ONE" {
		t.Errorf("a = %q, err = %v; want %q (operator from WithCustomOperator not merged/consulted)", got, err, "ONE")
	}
	if got, err := result.GetString("b"); err != nil || got != "TWO" {
		t.Errorf("b = %q, err = %v; want %q (operator from WithOperators not consulted)", got, err, "TWO")
	}
}

// TestWithOperators_EmptyMapIsNoOp proves an empty/nil map does not
// allocate or otherwise disturb CustomOperators.
func TestWithOperators_EmptyMapIsNoOp(t *testing.T) {
	engine, err := NewEngine(WithOperators(nil))
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	doc := mustParseYAMLDoc(t, engine, "key: value\n")
	if _, err := engine.Evaluate(context.Background(), doc); err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
}

