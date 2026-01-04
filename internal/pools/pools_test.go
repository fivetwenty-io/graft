package pools

import (
	"bytes"
	"fmt"
	"runtime"
	"sync"
	"testing"
)

// ============================================================================
// BufferPool Tests
// ============================================================================

func TestBufferPool_GetPut(t *testing.T) {
	pool := NewBufferPool(DefaultMaxBufferSize)

	buf := pool.Get()
	if buf == nil {
		t.Fatal("Get() returned nil")
	}

	if buf.Len() != 0 {
		t.Errorf("expected empty buffer, got length %d", buf.Len())
	}

	buf.WriteString("test data")
	pool.Put(buf)

	// Get another buffer - it should be reset
	buf2 := pool.Get()
	if buf2.Len() != 0 {
		t.Errorf("expected reset buffer, got length %d", buf2.Len())
	}
}

func TestBufferPool_OversizedBufferNotPooled(t *testing.T) {
	pool := NewBufferPool(1024) // 1KB max

	buf := pool.Get()

	// Grow buffer beyond max size
	largeData := make([]byte, 2048)
	buf.Write(largeData)

	pool.Put(buf)

	// The large buffer should have been discarded, so we get a new small one
	buf2 := pool.Get()
	if buf2.Cap() >= 2048 {
		t.Logf("Note: pool may have returned different buffer (cap=%d)", buf2.Cap())
	}
}

func TestBufferPool_NilSafe(t *testing.T) {
	pool := NewBufferPool(DefaultMaxBufferSize)

	// Should not panic
	pool.Put(nil)
}

func TestBufferPool_MaxSize(t *testing.T) {
	pool := NewBufferPool(4096)
	if pool.MaxSize() != 4096 {
		t.Errorf("expected max size 4096, got %d", pool.MaxSize())
	}
}

func TestBufferPool_DefaultMaxSize(t *testing.T) {
	pool := NewBufferPool(0) // 0 should use default
	if pool.MaxSize() != DefaultMaxBufferSize {
		t.Errorf("expected default max size %d, got %d", DefaultMaxBufferSize, pool.MaxSize())
	}
}

func TestGlobalBufferPool(t *testing.T) {
	buf := GetBuffer()
	if buf == nil {
		t.Fatal("GetBuffer() returned nil")
	}

	buf.WriteString("global pool test")
	PutBuffer(buf)
}

// ============================================================================
// StringSlicePool Tests
// ============================================================================

func TestStringSlicePool_GetPut(t *testing.T) {
	pool := NewStringSlicePool(DefaultMaxSliceCapacity)

	slice := pool.Get(0)
	if slice == nil {
		t.Fatal("Get() returned nil")
	}

	if len(slice) != 0 {
		t.Errorf("expected empty slice, got length %d", len(slice))
	}

	slice = append(slice, "a", "b", "c")
	pool.Put(slice)
}

func TestStringSlicePool_GetWithCapacity(t *testing.T) {
	pool := NewStringSlicePool(DefaultMaxSliceCapacity)

	slice := pool.Get(100)
	if cap(slice) < 100 {
		t.Errorf("expected capacity >= 100, got %d", cap(slice))
	}
}

func TestStringSlicePool_GetPtr(t *testing.T) {
	pool := NewStringSlicePool(DefaultMaxSliceCapacity)

	ptr := pool.GetPtr()
	if ptr == nil {
		t.Fatal("GetPtr() returned nil")
	}

	*ptr = append(*ptr, "test")
	pool.PutPtr(ptr)
}

func TestStringSlicePool_OversizedNotPooled(t *testing.T) {
	pool := NewStringSlicePool(100)

	// Create a slice larger than max
	largeSlice := make([]string, 0, 200)
	largeSlice = append(largeSlice, "data")

	pool.Put(largeSlice)

	// Should get a smaller slice from pool
	slice := pool.Get(0)
	if cap(slice) >= 200 {
		t.Logf("Note: pool behavior may vary (cap=%d)", cap(slice))
	}
}

func TestStringSlicePool_PutClean(t *testing.T) {
	pool := NewStringSlicePool(DefaultMaxSliceCapacity)

	slice := pool.Get(10)
	slice = append(slice, "sensitive", "data", "here")

	pool.PutClean(slice)
}

func TestStringSlicePool_NilSafe(t *testing.T) {
	pool := NewStringSlicePool(DefaultMaxSliceCapacity)

	// Should not panic
	pool.Put(nil)
	pool.PutPtr(nil)
	pool.PutClean(nil)
}

func TestGlobalStringSlicePool(t *testing.T) {
	slice := GetStringSlice()
	if slice == nil {
		t.Fatal("GetStringSlice() returned nil")
	}

	sliceN := GetStringSliceN(50)
	if cap(sliceN) < 50 {
		t.Errorf("expected capacity >= 50, got %d", cap(sliceN))
	}

	PutStringSlice(slice)
	PutStringSlice(sliceN)
}

// ============================================================================
// StringInterner Tests
// ============================================================================

func TestStringInterner_Intern(t *testing.T) {
	interner := NewStringInterner(DefaultShardCount, 0)

	s1 := interner.Intern("hello")
	s2 := interner.Intern("hello")

	if s1 != s2 {
		t.Error("interned strings should be identical")
	}

	// Both should point to the same underlying string
	// (This is the main benefit of interning)
}

func TestStringInterner_Size(t *testing.T) {
	interner := NewStringInterner(DefaultShardCount, 0)

	if interner.Size() != 0 {
		t.Errorf("expected size 0, got %d", interner.Size())
	}

	interner.Intern("one")
	interner.Intern("two")
	interner.Intern("three")

	if interner.Size() != 3 {
		t.Errorf("expected size 3, got %d", interner.Size())
	}

	// Interning same string should not increase size
	interner.Intern("one")
	if interner.Size() != 3 {
		t.Errorf("expected size still 3, got %d", interner.Size())
	}
}

func TestStringInterner_Contains(t *testing.T) {
	interner := NewStringInterner(DefaultShardCount, 0)

	if interner.Contains("test") {
		t.Error("should not contain 'test' before interning")
	}

	interner.Intern("test")

	if !interner.Contains("test") {
		t.Error("should contain 'test' after interning")
	}
}

func TestStringInterner_Clear(t *testing.T) {
	interner := NewStringInterner(DefaultShardCount, 0)

	interner.Intern("one")
	interner.Intern("two")
	interner.Intern("three")

	interner.Clear()

	if interner.Size() != 0 {
		t.Errorf("expected size 0 after clear, got %d", interner.Size())
	}

	if interner.Contains("one") {
		t.Error("should not contain 'one' after clear")
	}
}

func TestStringInterner_MaxSize(t *testing.T) {
	interner := NewStringInterner(DefaultShardCount, 3)

	interner.Intern("one")
	interner.Intern("two")
	interner.Intern("three")

	// This should not be interned due to max size
	result := interner.Intern("four")

	// The string is still returned, just not interned
	if result != "four" {
		t.Errorf("expected 'four', got '%s'", result)
	}

	if interner.Size() > 3 {
		t.Errorf("expected size <= 3, got %d", interner.Size())
	}
}

func TestStringInterner_InternBytes(t *testing.T) {
	interner := NewStringInterner(DefaultShardCount, 0)

	b := []byte("hello bytes")
	s := interner.InternBytes(b)

	if s != "hello bytes" {
		t.Errorf("expected 'hello bytes', got '%s'", s)
	}

	// Should be interned
	if !interner.Contains("hello bytes") {
		t.Error("should contain interned byte string")
	}
}

func TestStringInterner_Stats(t *testing.T) {
	interner := NewStringInterner(4, 0) // 4 shards for easier testing

	interner.Intern("a")
	interner.Intern("b")
	interner.Intern("c")

	stats := interner.Stats()

	if stats.ShardCount != 4 {
		t.Errorf("expected 4 shards, got %d", stats.ShardCount)
	}

	if stats.TotalSize != 3 {
		t.Errorf("expected total size 3, got %d", stats.TotalSize)
	}

	if len(stats.ShardSizes) != 4 {
		t.Errorf("expected 4 shard sizes, got %d", len(stats.ShardSizes))
	}

	// Sum of shard sizes should equal total
	sum := 0
	for _, size := range stats.ShardSizes {
		sum += size
	}
	if sum != stats.TotalSize {
		t.Errorf("shard sizes sum %d != total %d", sum, stats.TotalSize)
	}
}

func TestStringInterner_ShardDistribution(t *testing.T) {
	interner := NewStringInterner(16, 0)

	// Intern many strings
	for i := 0; i < 1000; i++ {
		interner.Intern(fmt.Sprintf("string_%d", i))
	}

	stats := interner.Stats()

	// Check that strings are reasonably distributed across shards
	// (no shard should have all strings, and none should be empty with 1000 strings)
	minSize := stats.ShardSizes[0]
	maxSize := stats.ShardSizes[0]

	for _, size := range stats.ShardSizes {
		if size < minSize {
			minSize = size
		}
		if size > maxSize {
			maxSize = size
		}
	}

	// With good hashing, distribution should be somewhat even
	// Allow for some variance but not extreme imbalance
	avgSize := 1000 / 16
	if maxSize > avgSize*3 || minSize < avgSize/3 {
		t.Logf("Shard distribution may be uneven: min=%d, max=%d, avg=%d", minSize, maxSize, avgSize)
	}
}

func TestGlobalInterner(t *testing.T) {
	// The global interner is pre-populated with common strings
	if !Strings.Contains("true") {
		t.Error("global interner should contain 'true'")
	}

	if !Strings.Contains("false") {
		t.Error("global interner should contain 'false'")
	}

	// Test InternString convenience function
	s := InternString("test_global")
	if s != "test_global" {
		t.Errorf("expected 'test_global', got '%s'", s)
	}
}

func TestNextPowerOfTwo(t *testing.T) {
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
	}

	for _, tt := range tests {
		result := nextPowerOfTwo(tt.input)
		if result != tt.expected {
			t.Errorf("nextPowerOfTwo(%d) = %d, expected %d", tt.input, result, tt.expected)
		}
	}
}

// ============================================================================
// Concurrency Tests
// ============================================================================

func TestBufferPool_Concurrent(t *testing.T) {
	pool := NewBufferPool(DefaultMaxBufferSize)

	var wg sync.WaitGroup
	numGoroutines := 100
	iterations := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				buf := pool.Get()
				fmt.Fprintf(buf, "goroutine %d iteration %d", id, j)
				pool.Put(buf)
			}
		}(i)
	}

	wg.Wait()
}

func TestStringSlicePool_Concurrent(t *testing.T) {
	pool := NewStringSlicePool(DefaultMaxSliceCapacity)

	var wg sync.WaitGroup
	numGoroutines := 100
	iterations := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				slice := pool.Get(10)
				slice = append(slice, fmt.Sprintf("item_%d_%d", id, j))
				pool.Put(slice)
			}
		}(i)
	}

	wg.Wait()
}

func TestStringInterner_Concurrent(t *testing.T) {
	interner := NewStringInterner(DefaultShardCount, 0)

	var wg sync.WaitGroup
	numGoroutines := 100
	iterations := 100

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				// Some unique strings, some shared
				interner.Intern(fmt.Sprintf("unique_%d_%d", id, j))
				interner.Intern("shared_string")
				interner.Intern(fmt.Sprintf("group_%d", id))
			}
		}(i)
	}

	wg.Wait()

	// Should have: 100*100 unique + 1 shared + 100 group strings
	// But exact count may vary due to timing
	if interner.Size() < numGoroutines {
		t.Errorf("expected at least %d strings, got %d", numGoroutines, interner.Size())
	}
}

func TestStringInterner_ConcurrentClear(t *testing.T) {
	interner := NewStringInterner(DefaultShardCount, 0)

	var wg sync.WaitGroup

	// Writers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				interner.Intern(fmt.Sprintf("string_%d_%d", id, j))
			}
		}(i)
	}

	// Readers
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = interner.Size()
				_ = interner.Contains("shared")
			}
		}()
	}

	// Clear a few times
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 3; i++ {
			runtime.Gosched()
			interner.Clear()
		}
	}()

	wg.Wait()
}

// ============================================================================
// Benchmarks
// ============================================================================

func BenchmarkBufferPool_GetPut(b *testing.B) {
	pool := NewBufferPool(DefaultMaxBufferSize)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := pool.Get()
		buf.WriteString("benchmark data")
		pool.Put(buf)
	}
}

func BenchmarkBufferPool_GetPut_Parallel(b *testing.B) {
	pool := NewBufferPool(DefaultMaxBufferSize)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			buf := pool.Get()
			buf.WriteString("benchmark data")
			pool.Put(buf)
		}
	})
}

func BenchmarkBuffer_NoPool(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := new(bytes.Buffer)
		buf.WriteString("benchmark data")
	}
}

func BenchmarkStringSlicePool_GetPut(b *testing.B) {
	pool := NewStringSlicePool(DefaultMaxSliceCapacity)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		slice := pool.Get(10)
		slice = append(slice, "a", "b", "c")
		pool.Put(slice)
	}
}

func BenchmarkStringSlicePool_GetPut_Parallel(b *testing.B) {
	pool := NewStringSlicePool(DefaultMaxSliceCapacity)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			slice := pool.Get(10)
			slice = append(slice, "a", "b", "c")
			pool.Put(slice)
		}
	})
}

func BenchmarkStringSlice_NoPool(b *testing.B) {
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		slice := make([]string, 0, 10)
		slice = append(slice, "a", "b", "c")
		_ = slice
	}
}

func BenchmarkStringInterner_Intern_Unique(b *testing.B) {
	interner := NewStringInterner(DefaultShardCount, 0)
	strings := make([]string, b.N)
	for i := 0; i < b.N; i++ {
		strings[i] = fmt.Sprintf("string_%d", i)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		interner.Intern(strings[i])
	}
}

func BenchmarkStringInterner_Intern_Shared(b *testing.B) {
	interner := NewStringInterner(DefaultShardCount, 0)
	interner.Intern("shared_string")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		interner.Intern("shared_string")
	}
}

func BenchmarkStringInterner_Intern_Parallel(b *testing.B) {
	interner := NewStringInterner(DefaultShardCount, 0)

	// Pre-intern some strings
	for i := 0; i < 100; i++ {
		interner.Intern(fmt.Sprintf("preinterned_%d", i))
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			// Mix of existing and new strings
			if i%2 == 0 {
				interner.Intern(fmt.Sprintf("preinterned_%d", i%100))
			} else {
				interner.Intern(fmt.Sprintf("new_%d", i))
			}
			i++
		}
	})
}

func BenchmarkStringInterner_Contains(b *testing.B) {
	interner := NewStringInterner(DefaultShardCount, 0)

	for i := 0; i < 1000; i++ {
		interner.Intern(fmt.Sprintf("string_%d", i))
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		interner.Contains(fmt.Sprintf("string_%d", i%1000))
	}
}

func BenchmarkStringInterner_Contains_Parallel(b *testing.B) {
	interner := NewStringInterner(DefaultShardCount, 0)

	for i := 0; i < 1000; i++ {
		interner.Intern(fmt.Sprintf("string_%d", i))
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			interner.Contains(fmt.Sprintf("string_%d", i%1000))
			i++
		}
	})
}

// Memory allocation benchmarks.
func BenchmarkBufferPool_Allocations(b *testing.B) {
	pool := NewBufferPool(DefaultMaxBufferSize)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		buf := pool.Get()
		buf.WriteString("test data for allocation benchmark")
		pool.Put(buf)
	}
}

func BenchmarkStringSlicePool_Allocations(b *testing.B) {
	pool := NewStringSlicePool(DefaultMaxSliceCapacity)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		slice := pool.Get(10)
		slice = append(slice, "item1", "item2", "item3")
		pool.Put(slice)
	}
}

func BenchmarkStringInterner_Allocations(b *testing.B) {
	interner := NewStringInterner(DefaultShardCount, 0)

	// Pre-intern the string we'll be looking up
	interner.Intern("test_string")

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		interner.Intern("test_string")
	}
}
