package graft_test

import (
	"fmt"
	"math/rand"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/fivetwenty-io/graft/internal/cache"
)

// =============================================================================
// Cache Hit/Miss Benchmarks
// =============================================================================

func BenchmarkCacheHitMiss(b *testing.B) {
	b.Run("LRUCache_AllHits", func(b *testing.B) {
		c := cache.NewLRUCache(1000)

		// Pre-populate cache
		for i := 0; i < 1000; i++ {
			c.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key%d", i%1000)
			_, _ = c.Get(key)
		}
	})

	b.Run("LRUCache_AllMisses", func(b *testing.B) {
		c := cache.NewLRUCache(1000)

		// Pre-populate cache with different keys
		for i := 0; i < 1000; i++ {
			c.Set(fmt.Sprintf("existing%d", i), fmt.Sprintf("value%d", i))
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("missing%d", i)
			_, _ = c.Get(key)
		}
	})

	b.Run("LRUCache_MixedHitMiss_80_20", func(b *testing.B) {
		c := cache.NewLRUCache(1000)

		// Pre-populate cache
		for i := 0; i < 800; i++ {
			c.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key%d", i%1000) // 80% hit rate
			_, _ = c.Get(key)
		}
	})

	b.Run("LRUCache_MixedHitMiss_50_50", func(b *testing.B) {
		c := cache.NewLRUCache(1000)

		// Pre-populate cache
		for i := 0; i < 500; i++ {
			c.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key%d", i%1000) // 50% hit rate
			_, _ = c.Get(key)
		}
	})

	b.Run("ShardedCache_AllHits", func(b *testing.B) {
		c := cache.NewShardedCache(cache.Options{
			MaxSize: 1000,
			Shards:  16,
		})
		defer c.Close()

		// Pre-populate cache
		for i := 0; i < 1000; i++ {
			c.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key%d", i%1000)
			_, _ = c.Get(key)
		}
	})

	b.Run("ShardedCache_AllMisses", func(b *testing.B) {
		c := cache.NewShardedCache(cache.Options{
			MaxSize: 1000,
			Shards:  16,
		})
		defer c.Close()

		// Pre-populate cache with different keys
		for i := 0; i < 1000; i++ {
			c.Set(fmt.Sprintf("existing%d", i), fmt.Sprintf("value%d", i))
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("missing%d", i)
			_, _ = c.Get(key)
		}
	})
}

// =============================================================================
// Cache Size Benchmarks
// =============================================================================

func BenchmarkCacheSizes(b *testing.B) {
	sizes := []int{100, 1000, 10000, 100000}

	for _, size := range sizes {
		b.Run(fmt.Sprintf("LRUCache_Size_%d", size), func(b *testing.B) {
			c := cache.NewLRUCache(size)

			// Pre-populate to capacity
			for i := 0; i < size; i++ {
				c.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				key := fmt.Sprintf("key%d", i%size)
				_, _ = c.Get(key)
			}
		})

		b.Run(fmt.Sprintf("ShardedCache_Size_%d", size), func(b *testing.B) {
			c := cache.NewShardedCache(cache.Options{
				MaxSize: size,
				Shards:  16,
			})
			defer c.Close()

			// Pre-populate to capacity
			for i := 0; i < size; i++ {
				c.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				key := fmt.Sprintf("key%d", i%size)
				_, _ = c.Get(key)
			}
		})
	}
}

// =============================================================================
// Cache Set Operations Benchmarks
// =============================================================================

func BenchmarkCacheSetOperations(b *testing.B) {
	b.Run("LRUCache_Set_NoEviction", func(b *testing.B) {
		c := cache.NewLRUCache(b.N + 1000)

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			c.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
		}
	})

	b.Run("LRUCache_Set_WithEviction", func(b *testing.B) {
		c := cache.NewLRUCache(100)

		// Pre-populate
		for i := 0; i < 100; i++ {
			c.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			c.Set(fmt.Sprintf("newkey%d", i), fmt.Sprintf("value%d", i))
		}
	})

	b.Run("ShardedCache_Set_NoEviction", func(b *testing.B) {
		c := cache.NewShardedCache(cache.Options{
			MaxSize: b.N + 1000,
			Shards:  16,
		})
		defer c.Close()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			c.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
		}
	})

	b.Run("ShardedCache_Set_WithEviction", func(b *testing.B) {
		c := cache.NewShardedCache(cache.Options{
			MaxSize: 100,
			Shards:  16,
		})
		defer c.Close()

		// Pre-populate
		for i := 0; i < 100; i++ {
			c.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			c.Set(fmt.Sprintf("newkey%d", i), fmt.Sprintf("value%d", i))
		}
	})

	b.Run("LRUCache_SetWithSize", func(b *testing.B) {
		c := cache.NewLRUCacheWithOptions(cache.Options{
			MaxSize:      1000,
			MaxSizeBytes: 1024 * 1024, // 1MB
		})

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			c.SetWithSize(fmt.Sprintf("key%d", i%1000), fmt.Sprintf("value%d", i), 100)
		}
	})
}

// =============================================================================
// Concurrent Cache Access Benchmarks
// =============================================================================

func BenchmarkConcurrentCacheAccess(b *testing.B) {
	b.Run("LRUCache_ConcurrentReads", func(b *testing.B) {
		c := cache.NewLRUCache(10000)

		// Pre-populate
		for i := 0; i < 10000; i++ {
			c.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
		}

		b.ResetTimer()
		b.ReportAllocs()

		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				key := fmt.Sprintf("key%d", i%10000)
				_, _ = c.Get(key)
				i++
			}
		})
	})

	b.Run("LRUCache_ConcurrentWrites", func(b *testing.B) {
		c := cache.NewLRUCache(10000)

		b.ResetTimer()
		b.ReportAllocs()

		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				c.Set(fmt.Sprintf("key%d", i%10000), fmt.Sprintf("value%d", i))
				i++
			}
		})
	})

	b.Run("LRUCache_ConcurrentMixed_80Read_20Write", func(b *testing.B) {
		c := cache.NewLRUCache(10000)

		// Pre-populate
		for i := 0; i < 10000; i++ {
			c.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
		}

		b.ResetTimer()
		b.ReportAllocs()

		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				key := fmt.Sprintf("key%d", i%10000)
				if i%5 == 0 { // 20% writes
					c.Set(key, fmt.Sprintf("newvalue%d", i))
				} else { // 80% reads
					_, _ = c.Get(key)
				}
				i++
			}
		})
	})

	b.Run("ShardedCache_ConcurrentReads", func(b *testing.B) {
		c := cache.NewShardedCache(cache.Options{
			MaxSize: 10000,
			Shards:  32,
		})
		defer c.Close()

		// Pre-populate
		for i := 0; i < 10000; i++ {
			c.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
		}

		b.ResetTimer()
		b.ReportAllocs()

		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				key := fmt.Sprintf("key%d", i%10000)
				_, _ = c.Get(key)
				i++
			}
		})
	})

	b.Run("ShardedCache_ConcurrentWrites", func(b *testing.B) {
		c := cache.NewShardedCache(cache.Options{
			MaxSize: 10000,
			Shards:  32,
		})
		defer c.Close()

		b.ResetTimer()
		b.ReportAllocs()

		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				c.Set(fmt.Sprintf("key%d", i%10000), fmt.Sprintf("value%d", i))
				i++
			}
		})
	})

	b.Run("ShardedCache_ConcurrentMixed_80Read_20Write", func(b *testing.B) {
		c := cache.NewShardedCache(cache.Options{
			MaxSize: 10000,
			Shards:  32,
		})
		defer c.Close()

		// Pre-populate
		for i := 0; i < 10000; i++ {
			c.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
		}

		b.ResetTimer()
		b.ReportAllocs()

		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				key := fmt.Sprintf("key%d", i%10000)
				if i%5 == 0 { // 20% writes
					c.Set(key, fmt.Sprintf("newvalue%d", i))
				} else { // 80% reads
					_, _ = c.Get(key)
				}
				i++
			}
		})
	})
}

// =============================================================================
// Shard Count Benchmarks
// =============================================================================

func BenchmarkShardCounts(b *testing.B) {
	shardCounts := []int{1, 4, 8, 16, 32, 64}

	for _, shards := range shardCounts {
		b.Run(fmt.Sprintf("Shards_%d_Concurrent", shards), func(b *testing.B) {
			c := cache.NewShardedCache(cache.Options{
				MaxSize: 10000,
				Shards:  shards,
			})
			defer c.Close()

			// Pre-populate
			for i := 0; i < 10000; i++ {
				c.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
			}

			b.ResetTimer()
			b.ReportAllocs()

			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					key := fmt.Sprintf("key%d", i%10000)
					if i%5 == 0 {
						c.Set(key, fmt.Sprintf("newvalue%d", i))
					} else {
						_, _ = c.Get(key)
					}
					i++
				}
			})
		})
	}
}

// =============================================================================
// TTL Cache Benchmarks
// =============================================================================

func BenchmarkTTLCache(b *testing.B) {
	b.Run("ShardedCache_SetWithTTL", func(b *testing.B) {
		c := cache.NewShardedCache(cache.Options{
			MaxSize: 10000,
			Shards:  16,
			TTL:     time.Minute,
		})
		defer c.Close()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			c.SetWithTTL(fmt.Sprintf("key%d", i%10000), fmt.Sprintf("value%d", i), time.Minute)
		}
	})

	b.Run("ShardedCache_GetWithExpiry", func(b *testing.B) {
		c := cache.NewShardedCache(cache.Options{
			MaxSize: 10000,
			Shards:  16,
			TTL:     time.Hour, // Long TTL to not expire during test
		})
		defer c.Close()

		// Pre-populate
		for i := 0; i < 10000; i++ {
			c.SetWithTTL(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i), time.Hour)
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key%d", i%10000)
			_, _ = c.Get(key)
		}
	})

	b.Run("ShardedCache_WithCleanup", func(b *testing.B) {
		c := cache.NewShardedCache(cache.Options{
			MaxSize:         10000,
			Shards:          16,
			TTL:             time.Hour,
			CleanupInterval: time.Second,
		})
		defer c.Close()

		// Pre-populate
		for i := 0; i < 10000; i++ {
			c.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key%d", i%10000)
			_, _ = c.Get(key)
		}
	})
}

// =============================================================================
// Memory Usage Benchmarks
// =============================================================================

func BenchmarkCacheMemoryUsage(b *testing.B) {
	b.Run("LRUCache_MemoryGrowth", func(b *testing.B) {
		var m1, m2 runtime.MemStats

		runtime.GC()
		runtime.ReadMemStats(&m1)

		c := cache.NewLRUCache(100000)
		for i := 0; i < 100000; i++ {
			c.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value-with-some-content-%d", i))
		}

		runtime.ReadMemStats(&m2)
		b.ReportMetric(float64(m2.Alloc-m1.Alloc)/1024/1024, "MB-used")
		b.ReportMetric(float64(m2.Alloc-m1.Alloc)/100000, "bytes-per-entry")

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = c.Get(fmt.Sprintf("key%d", i%100000))
		}
	})

	b.Run("ShardedCache_MemoryGrowth", func(b *testing.B) {
		var m1, m2 runtime.MemStats

		runtime.GC()
		runtime.ReadMemStats(&m1)

		c := cache.NewShardedCache(cache.Options{
			MaxSize: 100000,
			Shards:  32,
		})
		defer c.Close()

		for i := 0; i < 100000; i++ {
			c.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value-with-some-content-%d", i))
		}

		runtime.ReadMemStats(&m2)
		b.ReportMetric(float64(m2.Alloc-m1.Alloc)/1024/1024, "MB-used")
		b.ReportMetric(float64(m2.Alloc-m1.Alloc)/100000, "bytes-per-entry")

		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, _ = c.Get(fmt.Sprintf("key%d", i%100000))
		}
	})
}

// =============================================================================
// Cache Comparison Benchmarks
// =============================================================================

func BenchmarkCacheComparison(b *testing.B) {
	b.Run("LRU_vs_Sharded_Read", func(b *testing.B) {
		lru := cache.NewLRUCache(10000)
		sharded := cache.NewShardedCache(cache.Options{
			MaxSize: 10000,
			Shards:  16,
		})
		defer sharded.Close()

		// Pre-populate both
		for i := 0; i < 10000; i++ {
			key := fmt.Sprintf("key%d", i)
			val := fmt.Sprintf("value%d", i)
			lru.Set(key, val)
			sharded.Set(key, val)
		}

		b.Run("LRUCache", func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, _ = lru.Get(fmt.Sprintf("key%d", i%10000))
			}
		})

		b.Run("ShardedCache", func(b *testing.B) {
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				_, _ = sharded.Get(fmt.Sprintf("key%d", i%10000))
			}
		})
	})

	b.Run("LRU_vs_Sharded_Write", func(b *testing.B) {
		b.Run("LRUCache", func(b *testing.B) {
			lru := cache.NewLRUCache(10000)
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				lru.Set(fmt.Sprintf("key%d", i%10000), fmt.Sprintf("value%d", i))
			}
		})

		b.Run("ShardedCache", func(b *testing.B) {
			sharded := cache.NewShardedCache(cache.Options{
				MaxSize: 10000,
				Shards:  16,
			})
			defer sharded.Close()

			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				sharded.Set(fmt.Sprintf("key%d", i%10000), fmt.Sprintf("value%d", i))
			}
		})
	})

	b.Run("LRU_vs_Sharded_Concurrent", func(b *testing.B) {
		b.Run("LRUCache", func(b *testing.B) {
			lru := cache.NewLRUCache(10000)
			for i := 0; i < 10000; i++ {
				lru.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
			}

			b.ResetTimer()
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					key := fmt.Sprintf("key%d", i%10000)
					if i%5 == 0 {
						lru.Set(key, fmt.Sprintf("newvalue%d", i))
					} else {
						_, _ = lru.Get(key)
					}
					i++
				}
			})
		})

		b.Run("ShardedCache", func(b *testing.B) {
			sharded := cache.NewShardedCache(cache.Options{
				MaxSize: 10000,
				Shards:  32,
			})
			defer sharded.Close()

			for i := 0; i < 10000; i++ {
				sharded.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
			}

			b.ResetTimer()
			b.ReportAllocs()
			b.RunParallel(func(pb *testing.PB) {
				i := 0
				for pb.Next() {
					key := fmt.Sprintf("key%d", i%10000)
					if i%5 == 0 {
						sharded.Set(key, fmt.Sprintf("newvalue%d", i))
					} else {
						_, _ = sharded.Get(key)
					}
					i++
				}
			})
		})
	})
}

// =============================================================================
// Cache Stats Benchmarks
// =============================================================================

func BenchmarkCacheStats(b *testing.B) {
	b.Run("LRUCache_Stats", func(b *testing.B) {
		c := cache.NewLRUCache(10000)
		for i := 0; i < 10000; i++ {
			c.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_ = c.Stats()
		}
	})

	b.Run("ShardedCache_Stats", func(b *testing.B) {
		c := cache.NewShardedCache(cache.Options{
			MaxSize: 10000,
			Shards:  32,
		})
		defer c.Close()

		for i := 0; i < 10000; i++ {
			c.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_ = c.Stats()
		}
	})

	b.Run("ShardedCache_ShardStats", func(b *testing.B) {
		c := cache.NewShardedCache(cache.Options{
			MaxSize: 10000,
			Shards:  32,
		})
		defer c.Close()

		for i := 0; i < 10000; i++ {
			c.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_ = c.ShardStats()
		}
	})
}

// =============================================================================
// Cache Clear and Delete Benchmarks
// =============================================================================

func BenchmarkCacheClearDelete(b *testing.B) {
	b.Run("LRUCache_Delete", func(b *testing.B) {
		c := cache.NewLRUCache(10000)

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key%d", i%10000)
			c.Set(key, fmt.Sprintf("value%d", i))
			c.Delete(key)
		}
	})

	b.Run("ShardedCache_Delete", func(b *testing.B) {
		c := cache.NewShardedCache(cache.Options{
			MaxSize: 10000,
			Shards:  16,
		})
		defer c.Close()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key%d", i%10000)
			c.Set(key, fmt.Sprintf("value%d", i))
			c.Delete(key)
		}
	})

	b.Run("LRUCache_Clear", func(b *testing.B) {
		c := cache.NewLRUCache(1000)

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			// Populate
			for j := 0; j < 100; j++ {
				c.Set(fmt.Sprintf("key%d", j), fmt.Sprintf("value%d", j))
			}
			c.Clear()
		}
	})

	b.Run("ShardedCache_Clear", func(b *testing.B) {
		c := cache.NewShardedCache(cache.Options{
			MaxSize: 1000,
			Shards:  16,
		})
		defer c.Close()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			// Populate
			for j := 0; j < 100; j++ {
				c.Set(fmt.Sprintf("key%d", j), fmt.Sprintf("value%d", j))
			}
			c.Clear()
		}
	})
}

// =============================================================================
// Workload Pattern Benchmarks
// =============================================================================

func BenchmarkCacheWorkloads(b *testing.B) {
	b.Run("ZipfianDistribution", func(b *testing.B) {
		c := cache.NewShardedCache(cache.Options{
			MaxSize: 10000,
			Shards:  32,
		})
		defer c.Close()

		// Pre-populate
		for i := 0; i < 10000; i++ {
			c.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
		}

		// Create zipf distribution (some keys much more popular)
		//nolint:gosec // G404: Weak random is acceptable in benchmarks for reproducibility
		rng := rand.New(rand.NewSource(42))
		zipf := rand.NewZipf(rng, 1.2, 1.0, 9999)

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key%d", zipf.Uint64())
			_, _ = c.Get(key)
		}
	})

	b.Run("UniformDistribution", func(b *testing.B) {
		c := cache.NewShardedCache(cache.Options{
			MaxSize: 10000,
			Shards:  32,
		})
		defer c.Close()

		// Pre-populate
		for i := 0; i < 10000; i++ {
			c.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
		}

		//nolint:gosec // G404: Weak random is acceptable in benchmarks for reproducibility
		rng := rand.New(rand.NewSource(42))

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key%d", rng.Intn(10000))
			_, _ = c.Get(key)
		}
	})

	b.Run("SequentialAccess", func(b *testing.B) {
		c := cache.NewShardedCache(cache.Options{
			MaxSize: 10000,
			Shards:  32,
		})
		defer c.Close()

		// Pre-populate
		for i := 0; i < 10000; i++ {
			c.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			key := fmt.Sprintf("key%d", i%10000)
			_, _ = c.Get(key)
		}
	})

	b.Run("HotColdWorkload", func(b *testing.B) {
		c := cache.NewShardedCache(cache.Options{
			MaxSize: 10000,
			Shards:  32,
		})
		defer c.Close()

		// Pre-populate
		for i := 0; i < 10000; i++ {
			c.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
		}

		//nolint:gosec // G404: Weak random is acceptable in benchmarks for reproducibility
		rng := rand.New(rand.NewSource(42))

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			// 90% hot keys (0-99), 10% cold keys (100-9999)
			var key string
			if rng.Float32() < 0.9 {
				key = fmt.Sprintf("key%d", rng.Intn(100))
			} else {
				key = fmt.Sprintf("key%d", 100+rng.Intn(9900))
			}
			_, _ = c.Get(key)
		}
	})
}

// =============================================================================
// Eviction Callback Benchmarks
// =============================================================================

func BenchmarkEvictionCallback(b *testing.B) {
	b.Run("WithCallback", func(b *testing.B) {
		evictions := 0
		var mu sync.Mutex

		c := cache.NewLRUCacheWithOptions(cache.Options{
			MaxSize: 100,
			OnEvict: func(key string, value interface{}) {
				mu.Lock()
				evictions++
				mu.Unlock()
			},
		})

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			c.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
		}
	})

	b.Run("WithoutCallback", func(b *testing.B) {
		c := cache.NewLRUCache(100)

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			c.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
		}
	})
}

// =============================================================================
// Cache Keys Enumeration Benchmark
// =============================================================================

func BenchmarkCacheKeys(b *testing.B) {
	b.Run("LRUCache_Keys", func(b *testing.B) {
		c := cache.NewLRUCache(10000)
		for i := 0; i < 10000; i++ {
			c.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_ = c.Keys()
		}
	})

	b.Run("ShardedCache_Keys", func(b *testing.B) {
		c := cache.NewShardedCache(cache.Options{
			MaxSize: 10000,
			Shards:  32,
		})
		defer c.Close()

		for i := 0; i < 10000; i++ {
			c.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_ = c.Keys()
		}
	})
}

// =============================================================================
// Cache Contains Benchmark
// =============================================================================

func BenchmarkCacheContains(b *testing.B) {
	b.Run("LRUCache_Contains_Hit", func(b *testing.B) {
		c := cache.NewLRUCache(10000)
		for i := 0; i < 10000; i++ {
			c.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_ = c.Contains(fmt.Sprintf("key%d", i%10000))
		}
	})

	b.Run("ShardedCache_Contains_Hit", func(b *testing.B) {
		c := cache.NewShardedCache(cache.Options{
			MaxSize: 10000,
			Shards:  32,
		})
		defer c.Close()

		for i := 0; i < 10000; i++ {
			c.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_ = c.Contains(fmt.Sprintf("key%d", i%10000))
		}
	})

	b.Run("LRUCache_Contains_Miss", func(b *testing.B) {
		c := cache.NewLRUCache(10000)
		for i := 0; i < 10000; i++ {
			c.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_ = c.Contains(fmt.Sprintf("missing%d", i))
		}
	})

	b.Run("ShardedCache_Contains_Miss", func(b *testing.B) {
		c := cache.NewShardedCache(cache.Options{
			MaxSize: 10000,
			Shards:  32,
		})
		defer c.Close()

		for i := 0; i < 10000; i++ {
			c.Set(fmt.Sprintf("key%d", i), fmt.Sprintf("value%d", i))
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_ = c.Contains(fmt.Sprintf("missing%d", i))
		}
	})
}
