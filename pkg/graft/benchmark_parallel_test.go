package graft_test

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// =============================================================================
// Sequential vs Parallel Evaluation Benchmarks
// =============================================================================

func BenchmarkSequentialVsParallel(b *testing.B) {
	// Generate a document with independent operators (good for parallelization)
	generateIndependentOpsDoc := func(count int) string {
		var yamlBuilder strings.Builder
		yamlBuilder.WriteString("meta:\n  base: app\n  version: 1.0\n")

		for i := 0; i < count; i++ {
			yamlBuilder.WriteString(fmt.Sprintf("value%d: (( concat meta.base \"_%d\" ))\n", i, i))
		}

		return yamlBuilder.String()
	}

	// Generate a document with dependent operators (harder to parallelize)
	generateDependentOpsDoc := func(depth int) string {
		var yamlBuilder strings.Builder
		yamlBuilder.WriteString("value_0: \"base\"\n")

		for i := 1; i < depth; i++ {
			yamlBuilder.WriteString(fmt.Sprintf("value_%d: (( grab value_%d ))\n", i, i-1))
		}

		return yamlBuilder.String()
	}

	operatorCounts := []int{10, 50, 100, 200}

	for _, count := range operatorCounts {
		b.Run(fmt.Sprintf("Independent_%d_Sequential", count), func(b *testing.B) {
			engine, _ := graft.NewEngine(graft.WithConcurrency(1))
			doc, _ := engine.ParseYAML([]byte(generateIndependentOpsDoc(count)))
			ctx := context.Background()

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_, _ = engine.Evaluate(ctx, doc)
			}
		})

		b.Run(fmt.Sprintf("Independent_%d_Parallel", count), func(b *testing.B) {
			engine, _ := graft.NewEngine(graft.WithConcurrency(runtime.NumCPU()))
			doc, _ := engine.ParseYAML([]byte(generateIndependentOpsDoc(count)))
			ctx := context.Background()

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_, _ = engine.Evaluate(ctx, doc)
			}
		})

		b.Run(fmt.Sprintf("Dependent_%d_Sequential", count), func(b *testing.B) {
			engine, _ := graft.NewEngine(graft.WithConcurrency(1))
			doc, _ := engine.ParseYAML([]byte(generateDependentOpsDoc(count)))
			ctx := context.Background()

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_, _ = engine.Evaluate(ctx, doc)
			}
		})

		b.Run(fmt.Sprintf("Dependent_%d_Parallel", count), func(b *testing.B) {
			engine, _ := graft.NewEngine(graft.WithConcurrency(runtime.NumCPU()))
			doc, _ := engine.ParseYAML([]byte(generateDependentOpsDoc(count)))
			ctx := context.Background()

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_, _ = engine.Evaluate(ctx, doc)
			}
		})
	}
}

// =============================================================================
// Worker Pool Size Benchmarks
// =============================================================================

func BenchmarkWorkerPoolSizes(b *testing.B) {
	generateDoc := func(count int) string {
		var yamlBuilder strings.Builder
		yamlBuilder.WriteString("meta:\n  base: app\n  version: 1.0\n")

		for i := 0; i < count; i++ {
			switch i % 4 {
			case 0:
				yamlBuilder.WriteString(fmt.Sprintf("value%d: (( concat meta.base \"_%d\" ))\n", i, i))
			case 1:
				yamlBuilder.WriteString(fmt.Sprintf("value%d: (( grab meta.version ))\n", i))
			case 2:
				yamlBuilder.WriteString(fmt.Sprintf("value%d: (( calc %d + %d ))\n", i, i, i+1))
			case 3:
				yamlBuilder.WriteString(fmt.Sprintf("value%d: (( calc %d > 50 ? \"large\" : \"small\" ))\n", i, i))
			}
		}

		return yamlBuilder.String()
	}

	yaml := generateDoc(100)
	workerCounts := []int{1, 2, 4, 8, 16, 32}

	for _, workers := range workerCounts {
		b.Run(fmt.Sprintf("Workers_%d", workers), func(b *testing.B) {
			engine, _ := graft.NewEngine(graft.WithConcurrency(workers))
			doc, _ := engine.ParseYAML([]byte(yaml))
			ctx := context.Background()

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_, _ = engine.Evaluate(ctx, doc)
			}
		})
	}
}

// =============================================================================
// Dependency Depth Benchmarks
// =============================================================================

func BenchmarkDependencyDepths(b *testing.B) {
	generateDepthDoc := func(depth int) string {
		var yamlBuilder strings.Builder
		yamlBuilder.WriteString("base: \"root\"\n")

		for i := 1; i <= depth; i++ {
			if i == 1 {
				yamlBuilder.WriteString(fmt.Sprintf("level_%d: (( grab base ))\n", i))
			} else {
				yamlBuilder.WriteString(fmt.Sprintf("level_%d: (( grab level_%d ))\n", i, i-1))
			}
		}

		return yamlBuilder.String()
	}

	depths := []int{5, 10, 20, 50, 100}

	for _, depth := range depths {
		b.Run(fmt.Sprintf("Depth_%d_Sequential", depth), func(b *testing.B) {
			engine, _ := graft.NewEngine(graft.WithConcurrency(1))
			doc, _ := engine.ParseYAML([]byte(generateDepthDoc(depth)))
			ctx := context.Background()

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_, _ = engine.Evaluate(ctx, doc)
			}
		})

		b.Run(fmt.Sprintf("Depth_%d_Parallel", depth), func(b *testing.B) {
			engine, _ := graft.NewEngine(graft.WithConcurrency(runtime.NumCPU()))
			doc, _ := engine.ParseYAML([]byte(generateDepthDoc(depth)))
			ctx := context.Background()

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				_, _ = engine.Evaluate(ctx, doc)
			}
		})
	}
}

// =============================================================================
// Throughput Under Load Benchmarks
// =============================================================================

func BenchmarkThroughputUnderLoad(b *testing.B) {
	yaml := `
meta:
  app: "myapp"
  version: "1.0"
  env: "production"
name: (( concat meta.app "-" meta.version ))
full_name: (( concat name "-" meta.env ))
config:
  app_name: (( grab name ))
  full_name: (( grab full_name ))
  is_prod: (( calc meta.env == "production" ))
`

	b.Run("SingleGoroutine", func(b *testing.B) {
		engine, _ := graft.NewEngine()
		doc, _ := engine.ParseYAML([]byte(yaml))
		ctx := context.Background()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, _ = engine.Evaluate(ctx, doc)
		}
	})

	goroutineCounts := []int{2, 4, 8, 16, 32}

	for _, goroutines := range goroutineCounts {
		b.Run(fmt.Sprintf("Goroutines_%d", goroutines), func(b *testing.B) {
			engine, _ := graft.NewEngine()
			doc, _ := engine.ParseYAML([]byte(yaml))
			ctx := context.Background()

			b.ResetTimer()
			b.ReportAllocs()

			b.SetParallelism(goroutines)
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					_, _ = engine.Evaluate(ctx, doc)
				}
			})
		})
	}
}

// =============================================================================
// Concurrent Document Processing Benchmarks
// =============================================================================

func BenchmarkConcurrentDocumentProcessing(b *testing.B) {
	generateUniqueDoc := func(id int) string {
		return fmt.Sprintf(`
meta:
  app: "app%d"
  version: "1.%d"
name: (( concat meta.app "-" meta.version ))
config:
  name: (( grab name ))
`, id, id)
	}

	documentCounts := []int{10, 50, 100}

	for _, docCount := range documentCounts {
		b.Run(fmt.Sprintf("Sequential_%d_Docs", docCount), func(b *testing.B) {
			engine, _ := graft.NewEngine()
			ctx := context.Background()

			docs := make([]graft.Document, docCount)
			for i := 0; i < docCount; i++ {
				docs[i], _ = engine.ParseYAML([]byte(generateUniqueDoc(i)))
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				for _, doc := range docs {
					_, _ = engine.Evaluate(ctx, doc)
				}
			}
		})

		b.Run(fmt.Sprintf("Concurrent_%d_Docs", docCount), func(b *testing.B) {
			engine, _ := graft.NewEngine()
			ctx := context.Background()

			docs := make([]graft.Document, docCount)
			for i := 0; i < docCount; i++ {
				docs[i], _ = engine.ParseYAML([]byte(generateUniqueDoc(i)))
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				var wg sync.WaitGroup
				for _, doc := range docs {
					wg.Add(1)
					go func(d graft.Document) {
						defer wg.Done()
						_, _ = engine.Evaluate(ctx, d)
					}(doc)
				}
				wg.Wait()
			}
		})
	}
}

// =============================================================================
// Merge Operations Under Load
// =============================================================================

func BenchmarkMergeUnderLoad(b *testing.B) {
	baseYAML := `
meta:
  app: "baseapp"
  version: "1.0"
defaults:
  timeout: 30
  retries: 3
`

	overrideYAML := `
meta:
  app: "myapp"
config:
  timeout: (( grab defaults.timeout ))
  retries: (( grab defaults.retries ))
`

	b.Run("Sequential", func(b *testing.B) {
		engine, _ := graft.NewEngine()
		ctx := context.Background()

		base, _ := engine.ParseYAML([]byte(baseYAML))
		override, _ := engine.ParseYAML([]byte(overrideYAML))

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			merged, _ := engine.Merge(ctx, base, override).Execute()
			_, _ = engine.Evaluate(ctx, merged)
		}
	})

	b.Run("ConcurrentMerges", func(b *testing.B) {
		engine, _ := graft.NewEngine()
		ctx := context.Background()

		base, _ := engine.ParseYAML([]byte(baseYAML))
		override, _ := engine.ParseYAML([]byte(overrideYAML))

		b.ResetTimer()
		b.ReportAllocs()

		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				merged, _ := engine.Merge(ctx, base, override).Execute()
				_, _ = engine.Evaluate(ctx, merged)
			}
		})
	})
}

// =============================================================================
// Parallel Operator Execution Benchmarks (Simulated)
// =============================================================================

//nolint:gocyclo // benchmark function tests multiple parallel patterns
func BenchmarkParallelOperatorExecution(b *testing.B) {
	// Simulate parallel operator execution patterns

	b.Run("IndependentOperators_Sequential", func(b *testing.B) {
		operators := make([]func() int, 100)
		for i := 0; i < 100; i++ {
			idx := i
			operators[i] = func() int {
				// Simulate operator work
				sum := 0
				for j := 0; j < 100; j++ {
					sum += idx * j
				}
				return sum
			}
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			results := make([]int, len(operators))
			for j, op := range operators {
				results[j] = op()
			}
		}
	})

	b.Run("IndependentOperators_Parallel", func(b *testing.B) {
		operators := make([]func() int, 100)
		for i := 0; i < 100; i++ {
			idx := i
			operators[i] = func() int {
				// Simulate operator work
				sum := 0
				for j := 0; j < 100; j++ {
					sum += idx * j
				}
				return sum
			}
		}

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			results := make([]int, len(operators))
			var wg sync.WaitGroup

			for j, op := range operators {
				wg.Add(1)
				go func(idx int, fn func() int) {
					defer wg.Done()
					results[idx] = fn()
				}(j, op)
			}
			wg.Wait()
		}
	})

	b.Run("DependentOperators_Sequential", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			result := 1
			for j := 0; j < 100; j++ {
				result = result*2 + j
				if result > 10000000 {
					result %= 10000000
				}
			}
		}
	})

	b.Run("MixedDependency_Parallel", func(b *testing.B) {
		// Simulate a mix of independent and dependent operations
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			// Phase 1: independent operations
			results := make([]int, 50)
			var wg sync.WaitGroup

			for j := 0; j < 50; j++ {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()
					sum := 0
					for k := 0; k < 100; k++ {
						sum += idx * k
					}
					results[idx] = sum
				}(j)
			}
			wg.Wait()

			// Phase 2: dependent operations (must be sequential)
			total := 0
			for _, r := range results {
				total += r
			}
		}
	})
}

// =============================================================================
// Lock Contention Benchmarks
// =============================================================================

func BenchmarkLockContention(b *testing.B) {
	b.Run("GlobalLock", func(b *testing.B) {
		var mu sync.Mutex
		counter := 0

		b.ResetTimer()
		b.ReportAllocs()

		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				mu.Lock()
				counter++
				mu.Unlock()
			}
		})
	})

	b.Run("RWLock_ReadHeavy", func(b *testing.B) {
		var mu sync.RWMutex
		counter := 0

		b.ResetTimer()
		b.ReportAllocs()

		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				if i%10 == 0 { // 10% writes
					mu.Lock()
					counter++
					mu.Unlock()
				} else { // 90% reads
					mu.RLock()
					_ = counter
					mu.RUnlock()
				}
				i++
			}
		})
	})

	b.Run("AtomicCounter", func(b *testing.B) {
		var counter int64

		b.ResetTimer()
		b.ReportAllocs()

		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				atomic.AddInt64(&counter, 1)
			}
		})
	})

	b.Run("ShardedCounters", func(b *testing.B) {
		numShards := 16
		counters := make([]int64, numShards)

		b.ResetTimer()
		b.ReportAllocs()

		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				shard := i % numShards
				atomic.AddInt64(&counters[shard], 1)
				i++
			}
		})
	})
}

// =============================================================================
// Context Cancellation Benchmarks
// =============================================================================

func BenchmarkContextCancellation(b *testing.B) {
	yaml := `
meta:
  app: "myapp"
  version: "1.0"
name: (( concat meta.app "-" meta.version ))
config:
  name: (( grab name ))
`

	b.Run("NoTimeout", func(b *testing.B) {
		engine, _ := graft.NewEngine()
		doc, _ := engine.ParseYAML([]byte(yaml))
		ctx := context.Background()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, _ = engine.Evaluate(ctx, doc)
		}
	})

	b.Run("WithTimeout", func(b *testing.B) {
		engine, _ := graft.NewEngine()
		doc, _ := engine.ParseYAML([]byte(yaml))

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			_, _ = engine.Evaluate(ctx, doc)
			cancel()
		}
	})

	b.Run("WithCancel", func(b *testing.B) {
		engine, _ := graft.NewEngine()
		doc, _ := engine.ParseYAML([]byte(yaml))

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			ctx, cancel := context.WithCancel(context.Background())
			_, _ = engine.Evaluate(ctx, doc)
			cancel()
		}
	})
}

// =============================================================================
// Channel-Based Coordination Benchmarks
// =============================================================================

func BenchmarkChannelCoordination(b *testing.B) {
	b.Run("UnbufferedChannel", func(b *testing.B) {
		ch := make(chan int)

		go func() {
			for {
				<-ch
			}
		}()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			ch <- i
		}
	})

	b.Run("BufferedChannel_10", func(b *testing.B) {
		ch := make(chan int, 10)

		go func() {
			for {
				<-ch
			}
		}()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			ch <- i
		}
	})

	b.Run("BufferedChannel_100", func(b *testing.B) {
		ch := make(chan int, 100)

		go func() {
			for {
				<-ch
			}
		}()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			ch <- i
		}
	})

	b.Run("BufferedChannel_1000", func(b *testing.B) {
		ch := make(chan int, 1000)

		go func() {
			for {
				<-ch
			}
		}()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			ch <- i
		}
	})
}

// =============================================================================
// Work Distribution Patterns
// =============================================================================

//nolint:gocyclo // benchmark function tests multiple work distribution patterns
func BenchmarkWorkDistribution(b *testing.B) {
	workItems := 1000

	b.Run("SingleWorker", func(b *testing.B) {
		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			results := make([]int, workItems)
			for j := 0; j < workItems; j++ {
				results[j] = j * j
			}
		}
	})

	b.Run("FanOut_FanIn", func(b *testing.B) {
		numWorkers := runtime.NumCPU()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			jobs := make(chan int, workItems)
			results := make(chan int, workItems)

			// Start workers
			var wg sync.WaitGroup
			for w := 0; w < numWorkers; w++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for j := range jobs {
						results <- j * j
					}
				}()
			}

			// Send jobs
			for j := 0; j < workItems; j++ {
				jobs <- j
			}
			close(jobs)

			// Wait for workers
			wg.Wait()
			close(results)

			// Collect results
			collected := make([]int, 0, workItems)
			for r := range results {
				collected = append(collected, r)
			}
			_ = collected
		}
	})

	b.Run("WorkerPool", func(b *testing.B) {
		numWorkers := runtime.NumCPU()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			jobs := make(chan int, workItems)
			results := make([]int, workItems)
			var resultsLock sync.Mutex

			var wg sync.WaitGroup
			for w := 0; w < numWorkers; w++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for j := range jobs {
						result := j * j
						resultsLock.Lock()
						results[j] = result
						resultsLock.Unlock()
					}
				}()
			}

			for j := 0; j < workItems; j++ {
				jobs <- j
			}
			close(jobs)
			wg.Wait()
		}
	})

	b.Run("WorkStealing_Simulated", func(b *testing.B) {
		numWorkers := runtime.NumCPU()

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			results := make([]int, workItems)
			workPerWorker := workItems / numWorkers

			var wg sync.WaitGroup
			for w := 0; w < numWorkers; w++ {
				wg.Add(1)
				start := w * workPerWorker
				end := start + workPerWorker
				if w == numWorkers-1 {
					end = workItems
				}

				go func(s, e int) {
					defer wg.Done()
					for j := s; j < e; j++ {
						results[j] = j * j
					}
				}(start, end)
			}
			wg.Wait()
		}
	})
}

// =============================================================================
// Scaling Benchmarks
// =============================================================================

func BenchmarkParallelScaling(b *testing.B) {
	generateLargeDoc := func(operators int) string {
		var yamlBuilder strings.Builder
		yamlBuilder.WriteString("meta:\n  base: app\n  version: 1.0\n")

		for i := 0; i < operators; i++ {
			yamlBuilder.WriteString(fmt.Sprintf("value%d: (( concat meta.base \"_%d\" ))\n", i, i))
		}

		return yamlBuilder.String()
	}

	sizes := []int{50, 100, 200, 500}
	concurrencies := []int{1, 2, 4, 8}

	for _, size := range sizes {
		for _, conc := range concurrencies {
			b.Run(fmt.Sprintf("Ops_%d_Conc_%d", size, conc), func(b *testing.B) {
				engine, _ := graft.NewEngine(graft.WithConcurrency(conc))
				doc, _ := engine.ParseYAML([]byte(generateLargeDoc(size)))
				ctx := context.Background()

				b.ResetTimer()
				b.ReportAllocs()

				for i := 0; i < b.N; i++ {
					_, _ = engine.Evaluate(ctx, doc)
				}
			})
		}
	}
}

// =============================================================================
// Memory Allocation Under Parallel Load
// =============================================================================

func BenchmarkParallelMemoryAllocation(b *testing.B) {
	yaml := `
meta:
  app: "myapp"
  version: "1.0"
name: (( concat meta.app "-" meta.version ))
config:
  name: (( grab name ))
`

	b.Run("Sequential_Memory", func(b *testing.B) {
		engine, _ := graft.NewEngine()
		doc, _ := engine.ParseYAML([]byte(yaml))
		ctx := context.Background()

		var m1, m2 runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&m1)

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			_, _ = engine.Evaluate(ctx, doc)
		}

		runtime.ReadMemStats(&m2)
		b.ReportMetric(float64(m2.TotalAlloc-m1.TotalAlloc)/float64(b.N), "bytes/op-total")
	})

	b.Run("Parallel_Memory", func(b *testing.B) {
		engine, _ := graft.NewEngine()
		doc, _ := engine.ParseYAML([]byte(yaml))
		ctx := context.Background()

		var m1, m2 runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&m1)

		b.ResetTimer()
		b.ReportAllocs()

		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				_, _ = engine.Evaluate(ctx, doc)
			}
		})

		runtime.ReadMemStats(&m2)
		b.ReportMetric(float64(m2.TotalAlloc-m1.TotalAlloc)/float64(b.N), "bytes/op-total")
	})
}

// =============================================================================
// Synchronization Overhead Benchmarks
// =============================================================================

func BenchmarkSyncOverhead(b *testing.B) {
	b.Run("NoSync", func(b *testing.B) {
		counter := 0

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			counter++
		}
	})

	b.Run("MutexSync", func(b *testing.B) {
		var mu sync.Mutex
		counter := 0

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			mu.Lock()
			counter++
			mu.Unlock()
		}
	})

	b.Run("RWMutexRead", func(b *testing.B) {
		var mu sync.RWMutex
		counter := 0

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			mu.RLock()
			_ = counter
			mu.RUnlock()
		}
	})

	b.Run("RWMutexWrite", func(b *testing.B) {
		var mu sync.RWMutex
		counter := 0

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			mu.Lock()
			counter++
			mu.Unlock()
		}
	})

	b.Run("AtomicOp", func(b *testing.B) {
		var counter int64

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			atomic.AddInt64(&counter, 1)
		}
	})

	b.Run("ChannelSend", func(b *testing.B) {
		ch := make(chan int, 1)

		b.ResetTimer()
		b.ReportAllocs()

		for i := 0; i < b.N; i++ {
			ch <- i
			<-ch
		}
	})
}

// =============================================================================
// Concurrent Map Access Patterns
// =============================================================================

func BenchmarkConcurrentMapAccess(b *testing.B) {
	b.Run("SyncMap_ReadHeavy", func(b *testing.B) {
		var m sync.Map

		// Pre-populate
		for i := 0; i < 1000; i++ {
			m.Store(fmt.Sprintf("key%d", i), i)
		}

		b.ResetTimer()
		b.ReportAllocs()

		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				if i%10 == 0 {
					m.Store(fmt.Sprintf("key%d", i%1000), i)
				} else {
					m.Load(fmt.Sprintf("key%d", i%1000))
				}
				i++
			}
		})
	})

	b.Run("MutexMap_ReadHeavy", func(b *testing.B) {
		var mu sync.RWMutex
		m := make(map[string]int)

		// Pre-populate
		for i := 0; i < 1000; i++ {
			m[fmt.Sprintf("key%d", i)] = i
		}

		b.ResetTimer()
		b.ReportAllocs()

		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				if i%10 == 0 {
					mu.Lock()
					m[fmt.Sprintf("key%d", i%1000)] = i
					mu.Unlock()
				} else {
					mu.RLock()
					_ = m[fmt.Sprintf("key%d", i%1000)]
					mu.RUnlock()
				}
				i++
			}
		})
	})

	b.Run("ShardedMap", func(b *testing.B) {
		numShards := 16
		type shard struct {
			mu sync.RWMutex
			m  map[string]int
		}
		shards := make([]shard, numShards)
		for i := range shards {
			shards[i].m = make(map[string]int)
		}

		getShard := func(key string) *shard {
			h := 0
			for _, c := range key {
				h = 31*h + int(c)
			}
			if h < 0 {
				h = -h
			}
			return &shards[h%numShards]
		}

		// Pre-populate
		for i := 0; i < 1000; i++ {
			key := fmt.Sprintf("key%d", i)
			s := getShard(key)
			s.m[key] = i
		}

		b.ResetTimer()
		b.ReportAllocs()

		b.RunParallel(func(pb *testing.PB) {
			i := 0
			for pb.Next() {
				key := fmt.Sprintf("key%d", i%1000)
				s := getShard(key)
				if i%10 == 0 {
					s.mu.Lock()
					s.m[key] = i
					s.mu.Unlock()
				} else {
					s.mu.RLock()
					_ = s.m[key]
					s.mu.RUnlock()
				}
				i++
			}
		})
	})
}
