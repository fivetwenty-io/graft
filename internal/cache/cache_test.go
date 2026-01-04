package cache

import (
	"fmt"
	"math/rand"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Test constants for repeated string literals.
const (
	testValue1 = "value1"
)

// =============================================================================
// Cache Interface Tests
// =============================================================================

func TestNewCache(t *testing.T) {
	c := New()
	if c == nil {
		t.Fatal("New() returned nil")
	}

	if c.Size() != 0 {
		t.Errorf("New cache should be empty, got size %d", c.Size())
	}
}

func TestNewCacheWithOptions(t *testing.T) {
	c := NewCache(
		WithMaxSize(100),
		WithTTL(time.Minute),
		WithShards(8),
	)

	if c == nil {
		t.Fatal("NewCache() returned nil")
	}

	// Verify it's a sharded cache with correct number of shards.
	sc, ok := c.(*ShardedCache)
	if !ok {
		t.Fatal("NewCache() should return a ShardedCache")
	}

	if sc.NumShards() != 8 {
		t.Errorf("Expected 8 shards, got %d", sc.NumShards())
	}
}

// =============================================================================
// ShardedCache Tests
// =============================================================================

func TestShardedCache_BasicOperations(t *testing.T) {
	c := NewShardedCache(Options{
		MaxSize: 100,
		Shards:  4,
	})
	defer c.Close()

	// Test Set and Get.
	c.Set("key1", testValue1)
	val, found := c.Get("key1")
	if !found {
		t.Error("Expected to find key1")
	}
	if val != testValue1 {
		t.Errorf("Expected value1, got %v", val)
	}

	// Test update.
	c.Set("key1", "value1-updated")
	val, found = c.Get("key1")
	if !found || val != "value1-updated" {
		t.Errorf("Expected value1-updated, got %v", val)
	}

	// Test missing key.
	_, found = c.Get("missing")
	if found {
		t.Error("Should not find missing key")
	}
}

func TestShardedCache_Delete(t *testing.T) {
	c := NewShardedCache(Options{
		MaxSize: 100,
		Shards:  4,
	})
	defer c.Close()

	c.Set("key1", "value1")
	c.Set("key2", "value2")

	c.Delete("key1")

	_, found := c.Get("key1")
	if found {
		t.Error("key1 should be deleted")
	}

	_, found = c.Get("key2")
	if !found {
		t.Error("key2 should still exist")
	}
}

func TestShardedCache_Clear(t *testing.T) {
	c := NewShardedCache(Options{
		MaxSize: 100,
		Shards:  4,
	})
	defer c.Close()

	for i := 0; i < 50; i++ {
		c.Set(fmt.Sprintf("key%d", i), i)
	}

	if c.Size() != 50 {
		t.Errorf("Expected size 50, got %d", c.Size())
	}

	c.Clear()

	if c.Size() != 0 {
		t.Errorf("Expected empty cache after Clear, got size %d", c.Size())
	}
}

func TestShardedCache_TTL(t *testing.T) {
	c := NewShardedCache(Options{
		MaxSize:         100,
		Shards:          4,
		TTL:             50 * time.Millisecond,
		CleanupInterval: 0, // Disable background cleanup for this test.
	})
	defer c.Close()

	c.Set("key1", "value1")

	// Should be found immediately.
	_, found := c.Get("key1")
	if !found {
		t.Error("key1 should be found immediately")
	}

	// Wait for expiration.
	time.Sleep(60 * time.Millisecond)

	// Should be expired.
	_, found = c.Get("key1")
	if found {
		t.Error("key1 should be expired")
	}
}

func TestShardedCache_SetWithTTL(t *testing.T) {
	c := NewShardedCache(Options{
		MaxSize:         100,
		Shards:          4,
		TTL:             time.Hour, // Default TTL is long.
		CleanupInterval: 0,
	})
	defer c.Close()

	// Set with short TTL.
	c.SetWithTTL("key1", "value1", 50*time.Millisecond)

	_, found := c.Get("key1")
	if !found {
		t.Error("key1 should be found immediately")
	}

	time.Sleep(60 * time.Millisecond)

	_, found = c.Get("key1")
	if found {
		t.Error("key1 should be expired")
	}
}

func TestShardedCache_Eviction(t *testing.T) {
	evicted := make(map[string]interface{})
	var mu sync.Mutex

	c := NewShardedCache(Options{
		MaxSize: 16, // 4 per shard.
		Shards:  4,
		OnEvict: func(key string, value interface{}) {
			mu.Lock()
			evicted[key] = value
			mu.Unlock()
		},
		CleanupInterval: 0,
	})
	defer c.Close()

	// Add more entries than capacity.
	for i := 0; i < 32; i++ {
		c.Set(fmt.Sprintf("key%d", i), i)
	}

	// Should have evicted some entries.
	mu.Lock()
	evictedCount := len(evicted)
	mu.Unlock()

	if evictedCount == 0 {
		t.Error("Expected some evictions")
	}

	// Cache size should be at or below max.
	if c.Size() > 16 {
		t.Errorf("Cache size %d exceeds max %d", c.Size(), 16)
	}
}

func TestShardedCache_Stats(t *testing.T) {
	c := NewShardedCache(Options{
		MaxSize:         100,
		Shards:          4,
		CleanupInterval: 0,
	})
	defer c.Close()

	// Perform operations.
	c.Set("key1", "value1")
	c.Set("key2", "value2")
	c.Get("key1")         // Hit.
	c.Get("key1")         // Hit.
	c.Get("missing")      // Miss.
	c.Get("also_missing") // Miss.

	stats := c.Stats()

	if stats.Sets != 2 {
		t.Errorf("Expected 2 sets, got %d", stats.Sets)
	}
	if stats.Hits != 2 {
		t.Errorf("Expected 2 hits, got %d", stats.Hits)
	}
	if stats.Misses != 2 {
		t.Errorf("Expected 2 misses, got %d", stats.Misses)
	}
	if stats.Size != 2 {
		t.Errorf("Expected size 2, got %d", stats.Size)
	}

	expectedHitRate := 0.5
	if stats.HitRate() != expectedHitRate {
		t.Errorf("Expected hit rate %f, got %f", expectedHitRate, stats.HitRate())
	}
}

func TestShardedCache_Contains(t *testing.T) {
	c := NewShardedCache(Options{
		MaxSize:         100,
		Shards:          4,
		CleanupInterval: 0,
	})
	defer c.Close()

	c.Set("key1", "value1")

	if !c.Contains("key1") {
		t.Error("Should contain key1")
	}
	if c.Contains("missing") {
		t.Error("Should not contain missing")
	}
}

func TestShardedCache_GetEntry(t *testing.T) {
	c := NewShardedCache(Options{
		MaxSize:         100,
		Shards:          4,
		TTL:             time.Hour,
		CleanupInterval: 0,
	})
	defer c.Close()

	c.Set("key1", "value1")

	entry, found := c.GetEntry("key1")
	if !found {
		t.Fatal("Expected to find key1")
	}

	if entry.Key != "key1" {
		t.Errorf("Expected key key1, got %s", entry.Key)
	}
	if entry.Value != testValue1 {
		t.Errorf("Expected value value1, got %v", entry.Value)
	}
	if entry.CreatedAt.IsZero() {
		t.Error("CreatedAt should be set")
	}
	if entry.ExpiresAt.IsZero() {
		t.Error("ExpiresAt should be set when TTL is used")
	}
}

func TestShardedCache_BackgroundCleanup(t *testing.T) {
	c := NewShardedCache(Options{
		MaxSize:         100,
		Shards:          4,
		TTL:             50 * time.Millisecond,
		CleanupInterval: 30 * time.Millisecond,
	})
	defer c.Close()

	c.Set("key1", "value1")
	c.Set("key2", "value2")

	if c.Size() != 2 {
		t.Errorf("Expected size 2, got %d", c.Size())
	}

	// Wait for entries to expire and cleanup to run.
	time.Sleep(150 * time.Millisecond)

	if c.Size() != 0 {
		t.Errorf("Expected size 0 after cleanup, got %d", c.Size())
	}
}

// =============================================================================
// LRUCache Tests
// =============================================================================

func TestLRUCache_BasicOperations(t *testing.T) {
	c := NewLRUCache(100)

	// Test Set and Get.
	c.Set("key1", testValue1)
	val, found := c.Get("key1")
	if !found {
		t.Error("Expected to find key1")
	}
	if val != testValue1 {
		t.Errorf("Expected value1, got %v", val)
	}

	// Test update.
	c.Set("key1", "value1-updated")
	val, found = c.Get("key1")
	if !found || val != "value1-updated" {
		t.Errorf("Expected value1-updated, got %v", val)
	}
}

func TestLRUCache_Eviction(t *testing.T) {
	c := NewLRUCache(3)

	c.Set("key1", "value1")
	c.Set("key2", "value2")
	c.Set("key3", "value3")

	// Access key1 to make it recently used.
	c.Get("key1")

	// Add another entry, should evict key2 (least recently used).
	c.Set("key4", "value4")

	_, found := c.Get("key2")
	if found {
		t.Error("key2 should be evicted")
	}

	_, found = c.Get("key1")
	if !found {
		t.Error("key1 should still exist (was accessed)")
	}
}

func TestLRUCache_EvictionOrder(t *testing.T) {
	c := NewLRUCache(3)

	c.Set("a", 1)
	c.Set("b", 2)
	c.Set("c", 3)

	// Access order: c, b, a (a is now LRU).
	c.Get("c")
	c.Get("b")
	c.Get("a")

	// Now access b to make a the middle one.
	c.Get("b")

	// c should now be the LRU.
	c.Set("d", 4) // Should evict c.

	if c.Contains("c") {
		t.Error("c should be evicted")
	}
	if !c.Contains("a") || !c.Contains("b") || !c.Contains("d") {
		t.Error("a, b, d should all exist")
	}
}

func TestLRUCache_OnEvict(t *testing.T) {
	evicted := make(map[string]interface{})

	c := NewLRUCache(2)
	c.SetOnEvict(func(key string, value interface{}) {
		evicted[key] = value
	})

	c.Set("key1", "value1")
	c.Set("key2", "value2")
	c.Set("key3", "value3") // Should evict key1.

	if _, ok := evicted["key1"]; !ok {
		t.Error("key1 should be evicted")
	}
	if evicted["key1"] != "value1" {
		t.Errorf("Expected evicted value value1, got %v", evicted["key1"])
	}
}

func TestLRUCache_Clear(t *testing.T) {
	var evictCount int

	c := NewLRUCache(10)
	c.SetOnEvict(func(key string, value interface{}) {
		evictCount++
	})

	for i := 0; i < 5; i++ {
		c.Set(fmt.Sprintf("key%d", i), i)
	}

	c.Clear()

	if c.Size() != 0 {
		t.Errorf("Expected empty cache, got size %d", c.Size())
	}
	if evictCount != 5 {
		t.Errorf("Expected 5 eviction callbacks, got %d", evictCount)
	}
}

func TestLRUCache_Peek(t *testing.T) {
	c := NewLRUCache(3)

	c.Set("key1", "value1")
	c.Set("key2", "value2")
	c.Set("key3", "value3")

	// Peek at key1 (should not affect LRU order).
	val, found := c.Peek("key1")
	if !found || val != "value1" {
		t.Error("Peek should return key1")
	}

	// Add another entry, should evict key1 (not accessed via Get).
	c.Set("key4", "value4")

	_, found = c.Get("key1")
	if found {
		t.Error("key1 should be evicted (Peek doesn't update LRU)")
	}
}

func TestLRUCache_Keys(t *testing.T) {
	c := NewLRUCache(5)

	c.Set("a", 1)
	c.Set("b", 2)
	c.Set("c", 3)

	// Access to change order.
	c.Get("a")

	keys := c.Keys()
	if len(keys) != 3 {
		t.Errorf("Expected 3 keys, got %d", len(keys))
	}

	// Keys should be in MRU order: a, c, b.
	if keys[0] != "a" {
		t.Errorf("Expected first key to be 'a' (MRU), got %s", keys[0])
	}
}

func TestLRUCache_Oldest(t *testing.T) {
	c := NewLRUCache(5)

	c.Set("a", 1)
	c.Set("b", 2)
	c.Set("c", 3)

	oldest := c.Oldest()
	if oldest == nil || oldest.Key != "a" {
		t.Error("Oldest should be 'a'")
	}

	// Access 'a' to make it newest.
	c.Get("a")

	oldest = c.Oldest()
	if oldest == nil || oldest.Key != "b" {
		t.Error("After accessing 'a', oldest should be 'b'")
	}
}

func TestLRUCache_Newest(t *testing.T) {
	c := NewLRUCache(5)

	c.Set("a", 1)
	c.Set("b", 2)
	c.Set("c", 3)

	newest := c.Newest()
	if newest == nil || newest.Key != "c" {
		t.Error("Newest should be 'c'")
	}

	// Access 'a' to make it newest.
	c.Get("a")

	newest = c.Newest()
	if newest == nil || newest.Key != "a" {
		t.Error("After accessing 'a', newest should be 'a'")
	}
}

func TestLRUCache_Resize(t *testing.T) {
	c := NewLRUCache(10)

	for i := 0; i < 10; i++ {
		c.Set(fmt.Sprintf("key%d", i), i)
	}

	if c.Size() != 10 {
		t.Errorf("Expected size 10, got %d", c.Size())
	}

	// Resize to smaller.
	c.Resize(5)

	if c.Size() != 5 {
		t.Errorf("Expected size 5 after resize, got %d", c.Size())
	}
}

func TestLRUCache_SizeBasedEviction(t *testing.T) {
	c := NewLRUCacheWithOptions(Options{
		MaxSize:      1000, // Entry limit won't trigger.
		MaxSizeBytes: 100,  // Byte limit will trigger.
	})

	// Add entries with explicit sizes.
	c.SetWithSize("key1", "value1", 40)
	c.SetWithSize("key2", "value2", 40)

	if c.SizeBytes() != 80 {
		t.Errorf("Expected 80 bytes, got %d", c.SizeBytes())
	}

	// This should trigger eviction.
	c.SetWithSize("key3", "value3", 40)

	// Should have evicted at least one entry to stay under 100 bytes.
	if c.SizeBytes() > 100 {
		t.Errorf("Size %d exceeds max 100 bytes", c.SizeBytes())
	}
}

func TestLRUCache_Stats(t *testing.T) {
	c := NewLRUCache(10)

	c.Set("key1", "value1")
	c.Get("key1")    // Hit.
	c.Get("key1")    // Hit.
	c.Get("missing") // Miss.

	stats := c.Stats()

	if stats.Hits != 2 {
		t.Errorf("Expected 2 hits, got %d", stats.Hits)
	}
	if stats.Misses != 1 {
		t.Errorf("Expected 1 miss, got %d", stats.Misses)
	}
	if stats.Sets != 1 {
		t.Errorf("Expected 1 set, got %d", stats.Sets)
	}
}

// =============================================================================
// Stats Tests
// =============================================================================

func TestStats_HitRate(t *testing.T) {
	tests := []struct {
		hits     uint64
		misses   uint64
		expected float64
	}{
		{0, 0, 0},
		{1, 0, 1.0},
		{0, 1, 0},
		{1, 1, 0.5},
		{3, 1, 0.75},
		{100, 0, 1.0},
	}

	for _, tt := range tests {
		stats := Stats{Hits: tt.hits, Misses: tt.misses}
		got := stats.HitRate()
		if got != tt.expected {
			t.Errorf("HitRate(%d, %d) = %f, want %f", tt.hits, tt.misses, got, tt.expected)
		}
	}
}

func TestStats_HitRatePercent(t *testing.T) {
	stats := Stats{Hits: 3, Misses: 1}
	expected := 75.0
	got := stats.HitRatePercent()
	if got != expected {
		t.Errorf("HitRatePercent() = %f, want %f", got, expected)
	}
}

// =============================================================================
// Entry Tests
// =============================================================================

func TestEntry_IsExpired(t *testing.T) {
	// No expiration.
	e1 := Entry{}
	if e1.IsExpired() {
		t.Error("Entry with zero ExpiresAt should not be expired")
	}

	// Not expired.
	e2 := Entry{ExpiresAt: time.Now().Add(time.Hour)}
	if e2.IsExpired() {
		t.Error("Entry with future ExpiresAt should not be expired")
	}

	// Expired.
	e3 := Entry{ExpiresAt: time.Now().Add(-time.Hour)}
	if !e3.IsExpired() {
		t.Error("Entry with past ExpiresAt should be expired")
	}
}

// =============================================================================
// Options Tests
// =============================================================================

func TestDefaultOptions(t *testing.T) {
	opts := DefaultOptions()

	if opts.MaxSize != 10000 {
		t.Errorf("Expected default MaxSize 10000, got %d", opts.MaxSize)
	}
	if opts.Shards != 16 {
		t.Errorf("Expected default Shards 16, got %d", opts.Shards)
	}
	if opts.TTL != 0 {
		t.Errorf("Expected default TTL 0, got %v", opts.TTL)
	}
	if opts.CleanupInterval != time.Minute {
		t.Errorf("Expected default CleanupInterval 1 minute, got %v", opts.CleanupInterval)
	}
}

func TestWithOptions(t *testing.T) {
	opts := DefaultOptions()

	WithMaxSize(500)(&opts)
	WithTTL(5 * time.Minute)(&opts)
	WithShards(32)(&opts)
	WithCleanupInterval(30 * time.Second)(&opts)
	WithMaxSizeBytes(1024 * 1024)(&opts)

	if opts.MaxSize != 500 {
		t.Errorf("Expected MaxSize 500, got %d", opts.MaxSize)
	}
	if opts.TTL != 5*time.Minute {
		t.Errorf("Expected TTL 5 minutes, got %v", opts.TTL)
	}
	if opts.Shards != 32 {
		t.Errorf("Expected Shards 32, got %d", opts.Shards)
	}
	if opts.CleanupInterval != 30*time.Second {
		t.Errorf("Expected CleanupInterval 30s, got %v", opts.CleanupInterval)
	}
	if opts.MaxSizeBytes != 1024*1024 {
		t.Errorf("Expected MaxSizeBytes 1MB, got %d", opts.MaxSizeBytes)
	}
}

func TestWithOnEvict(t *testing.T) {
	called := false
	fn := func(key string, value interface{}) {
		called = true
	}

	opts := DefaultOptions()
	WithOnEvict(fn)(&opts)

	if opts.OnEvict == nil {
		t.Error("OnEvict should be set")
	}

	opts.OnEvict("key", "value")
	if !called {
		t.Error("OnEvict callback should have been called")
	}
}

func TestRoundUpToPowerOf2(t *testing.T) {
	tests := []struct {
		input    int
		expected int
	}{
		{0, 1},
		{1, 1},
		{2, 2},
		{3, 4},
		{4, 4},
		{5, 8},
		{7, 8},
		{8, 8},
		{9, 16},
		{15, 16},
		{16, 16},
		{17, 32},
		{100, 128},
	}

	for _, tt := range tests {
		got := roundUpToPowerOf2(tt.input)
		if got != tt.expected {
			t.Errorf("roundUpToPowerOf2(%d) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}

// =============================================================================
// Concurrent Access Tests
// =============================================================================

func TestShardedCache_ConcurrentAccess(t *testing.T) {
	c := NewShardedCache(Options{
		MaxSize:         10000,
		Shards:          16,
		CleanupInterval: 0,
	})
	defer c.Close()

	const numGoroutines = 100
	const numOperations = 1000

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := fmt.Sprintf("key-%d-%d", id, j%100)
				c.Set(key, j)
				c.Get(key)
				if j%10 == 0 {
					c.Delete(key)
				}
			}
		}(i)
	}

	wg.Wait()

	// Just verify no panics and stats are reasonable.
	stats := c.Stats()
	if stats.Sets == 0 || stats.Hits == 0 {
		t.Error("Expected non-zero operations")
	}
}

func TestLRUCache_ConcurrentAccess(t *testing.T) {
	c := NewLRUCache(1000)

	const numGoroutines = 100
	const numOperations = 1000

	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < numOperations; j++ {
				key := fmt.Sprintf("key-%d-%d", id, j%100)
				c.Set(key, j)
				c.Get(key)
				if j%10 == 0 {
					c.Delete(key)
				}
			}
		}(i)
	}

	wg.Wait()

	// Verify no panics.
	if c.Size() == 0 {
		t.Error("Expected non-zero size after operations")
	}
}

// =============================================================================
// Benchmarks
// =============================================================================

func BenchmarkShardedCache_Set(b *testing.B) {
	c := NewShardedCache(Options{
		MaxSize:         100000,
		Shards:          16,
		CleanupInterval: 0,
	})
	defer c.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Set(strconv.Itoa(i), i)
	}
}

func BenchmarkShardedCache_Get(b *testing.B) {
	c := NewShardedCache(Options{
		MaxSize:         100000,
		Shards:          16,
		CleanupInterval: 0,
	})
	defer c.Close()

	// Pre-populate.
	for i := 0; i < 10000; i++ {
		c.Set(strconv.Itoa(i), i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Get(strconv.Itoa(i % 10000))
	}
}

func BenchmarkShardedCache_SetGet(b *testing.B) {
	c := NewShardedCache(Options{
		MaxSize:         100000,
		Shards:          16,
		CleanupInterval: 0,
	})
	defer c.Close()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		key := strconv.Itoa(i % 10000)
		c.Set(key, i)
		c.Get(key)
	}
}

func BenchmarkShardedCache_ConcurrentSet(b *testing.B) {
	c := NewShardedCache(Options{
		MaxSize:         100000,
		Shards:          16,
		CleanupInterval: 0,
	})
	defer c.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			c.Set(strconv.Itoa(i), i)
			i++
		}
	})
}

func BenchmarkShardedCache_ConcurrentGet(b *testing.B) {
	c := NewShardedCache(Options{
		MaxSize:         100000,
		Shards:          16,
		CleanupInterval: 0,
	})
	defer c.Close()

	// Pre-populate.
	for i := 0; i < 10000; i++ {
		c.Set(strconv.Itoa(i), i)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			c.Get(strconv.Itoa(i % 10000))
			i++
		}
	})
}

func BenchmarkShardedCache_ConcurrentMixed(b *testing.B) {
	c := NewShardedCache(Options{
		MaxSize:         100000,
		Shards:          16,
		CleanupInterval: 0,
	})
	defer c.Close()

	// Pre-populate.
	for i := 0; i < 10000; i++ {
		c.Set(strconv.Itoa(i), i)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		//nolint:gosec // G404: Weak random is acceptable in benchmarks for reproducibility
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		i := 0
		for pb.Next() {
			key := strconv.Itoa(i % 10000)
			if rng.Float32() < 0.8 {
				c.Get(key)
			} else {
				c.Set(key, i)
			}
			i++
		}
	})
}

func BenchmarkLRUCache_Set(b *testing.B) {
	c := NewLRUCache(100000)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Set(strconv.Itoa(i), i)
	}
}

func BenchmarkLRUCache_Get(b *testing.B) {
	c := NewLRUCache(100000)

	// Pre-populate.
	for i := 0; i < 10000; i++ {
		c.Set(strconv.Itoa(i), i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Get(strconv.Itoa(i % 10000))
	}
}

func BenchmarkLRUCache_ConcurrentMixed(b *testing.B) {
	c := NewLRUCache(100000)

	// Pre-populate.
	for i := 0; i < 10000; i++ {
		c.Set(strconv.Itoa(i), i)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		//nolint:gosec // G404: Weak random is acceptable in benchmarks for reproducibility
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		i := 0
		for pb.Next() {
			key := strconv.Itoa(i % 10000)
			if rng.Float32() < 0.8 {
				c.Get(key)
			} else {
				c.Set(key, i)
			}
			i++
		}
	})
}

// BenchmarkShardedCache_HighContention tests performance under high contention
// by using a small key space that maps to the same shards.
func BenchmarkShardedCache_HighContention(b *testing.B) {
	c := NewShardedCache(Options{
		MaxSize:         10000,
		Shards:          16,
		CleanupInterval: 0,
	})
	defer c.Close()

	// Pre-populate with a small key space.
	for i := 0; i < 100; i++ {
		c.Set(strconv.Itoa(i), i)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		//nolint:gosec // G404: Weak random is acceptable in benchmarks for reproducibility
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		for pb.Next() {
			key := strconv.Itoa(rng.Intn(100))
			if rng.Float32() < 0.5 {
				c.Get(key)
			} else {
				c.Set(key, rng.Int())
			}
		}
	})
}

// BenchmarkCompareShardCounts compares performance with different shard counts.
func BenchmarkCompareShardCounts(b *testing.B) {
	shardCounts := []int{1, 4, 8, 16, 32, 64}

	for _, shards := range shardCounts {
		b.Run(fmt.Sprintf("shards=%d", shards), func(b *testing.B) {
			c := NewShardedCache(Options{
				MaxSize:         100000,
				Shards:          shards,
				CleanupInterval: 0,
			})
			defer c.Close()

			// Pre-populate.
			for i := 0; i < 10000; i++ {
				c.Set(strconv.Itoa(i), i)
			}

			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				//nolint:gosec // G404: Weak random is acceptable in benchmarks for reproducibility
				rng := rand.New(rand.NewSource(time.Now().UnixNano()))
				i := 0
				for pb.Next() {
					key := strconv.Itoa(i % 10000)
					if rng.Float32() < 0.8 {
						c.Get(key)
					} else {
						c.Set(key, i)
					}
					i++
				}
			})
		})
	}
}

// BenchmarkShardedCache_WithEviction tests performance when eviction is occurring.
func BenchmarkShardedCache_WithEviction(b *testing.B) {
	c := NewShardedCache(Options{
		MaxSize:         1000, // Small to trigger evictions.
		Shards:          16,
		CleanupInterval: 0,
	})
	defer c.Close()

	var counter atomic.Int64

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			i := counter.Add(1)
			c.Set(strconv.FormatInt(i, 10), i)
		}
	})
}
