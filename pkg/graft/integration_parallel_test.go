// Integration tests for graft parallel execution.
//
// NOTE: Some tests use advanced operator syntax that may not be fully supported.
// The ParseOpcall infinite recursion bug has been fixed, but these tests have
// other operator-related issues (ternary, calc expressions).
//
//go:build integration
// +build integration

package graft

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

// TestIntegration_ParallelIndependentOperators tests parallel evaluation of independent operators
func TestIntegration_ParallelIndependentOperators(t *testing.T) {
	Convey("Parallel Independent Operators", t, func() {

		Convey("Multiple independent grabs execute correctly", func() {
			engine := NewDefaultEngineWithConfig(EngineConfig{
				EnableParallel: true,
				MaxWorkers:     4,
			})

			yaml := []byte(`
meta:
  value1: "first"
  value2: "second"
  value3: "third"
  value4: "fourth"

results:
  r1: (( grab meta.value1 ))
  r2: (( grab meta.value2 ))
  r3: (( grab meta.value3 ))
  r4: (( grab meta.value4 ))
`)

			doc, err := engine.ParseYAML(yaml)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			r1, err := result.GetString("results.r1")
			So(err, ShouldBeNil)
			So(r1, ShouldEqual, "first")

			r2, err := result.GetString("results.r2")
			So(err, ShouldBeNil)
			So(r2, ShouldEqual, "second")

			r3, err := result.GetString("results.r3")
			So(err, ShouldBeNil)
			So(r3, ShouldEqual, "third")

			r4, err := result.GetString("results.r4")
			So(err, ShouldBeNil)
			So(r4, ShouldEqual, "fourth")
		})

		Convey("Independent calculations execute correctly", func() {
			engine := NewDefaultEngineWithConfig(EngineConfig{
				EnableParallel: true,
				MaxWorkers:     4,
			})

			yaml := []byte(`
meta:
  base: 10

calcs:
  c1: (( calc "meta.base * 1" ))
  c2: (( calc "meta.base * 2" ))
  c3: (( calc "meta.base * 3" ))
  c4: (( calc "meta.base * 4" ))
`)

			doc, err := engine.ParseYAML(yaml)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			c1, err := result.GetInt("calcs.c1")
			So(err, ShouldBeNil)
			So(c1, ShouldEqual, 10)

			c2, err := result.GetInt("calcs.c2")
			So(err, ShouldBeNil)
			So(c2, ShouldEqual, 20)

			c3, err := result.GetInt("calcs.c3")
			So(err, ShouldBeNil)
			So(c3, ShouldEqual, 30)

			c4, err := result.GetInt("calcs.c4")
			So(err, ShouldBeNil)
			So(c4, ShouldEqual, 40)
		})

		Convey("Mixed independent operators", func() {
			engine := NewDefaultEngineWithConfig(EngineConfig{
				EnableParallel: true,
				MaxWorkers:     4,
			})

			yaml := []byte(`
meta:
  name: "app"
  version: 1
  enabled: true

outputs:
  label: (( concat meta.name "-v" meta.version ))
  doubled: (( calc "meta.version * 2" ))
  status: (( meta.enabled ? "active" : "inactive" ))
`)

			doc, err := engine.ParseYAML(yaml)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			label, err := result.GetString("outputs.label")
			So(err, ShouldBeNil)
			So(label, ShouldEqual, "app-v1")

			doubled, err := result.GetInt("outputs.doubled")
			So(err, ShouldBeNil)
			So(doubled, ShouldEqual, 2)

			status, err := result.GetString("outputs.status")
			So(err, ShouldBeNil)
			So(status, ShouldEqual, "active")
		})
	})
}

// TestIntegration_ParallelWithDependencies tests parallel execution with dependencies
func TestIntegration_ParallelWithDependencies(t *testing.T) {
	Convey("Parallel with Dependencies", t, func() {

		Convey("Sequential dependencies are respected", func() {
			engine := NewDefaultEngineWithConfig(EngineConfig{
				EnableParallel: true,
				MaxWorkers:     4,
			})

			yaml := []byte(`
meta:
  base: 10

chain:
  step1: (( calc "meta.base + 5" ))
  step2: (( calc "chain.step1 * 2" ))
  step3: (( calc "chain.step2 + chain.step1" ))
`)

			doc, err := engine.ParseYAML(yaml)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			// step1 = 10 + 5 = 15
			step1, err := result.GetInt("chain.step1")
			So(err, ShouldBeNil)
			So(step1, ShouldEqual, 15)

			// step2 = 15 * 2 = 30
			step2, err := result.GetInt("chain.step2")
			So(err, ShouldBeNil)
			So(step2, ShouldEqual, 30)

			// step3 = 30 + 15 = 45
			step3, err := result.GetInt("chain.step3")
			So(err, ShouldBeNil)
			So(step3, ShouldEqual, 45)
		})

		Convey("Diamond dependencies", func() {
			engine := NewDefaultEngineWithConfig(EngineConfig{
				EnableParallel: true,
				MaxWorkers:     4,
			})

			yaml := []byte(`
meta:
  root: 10

diamond:
  # root splits into left and right
  left: (( calc "meta.root + 1" ))
  right: (( calc "meta.root + 2" ))
  # both merge into bottom
  bottom: (( calc "diamond.left + diamond.right" ))
`)

			doc, err := engine.ParseYAML(yaml)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			left, err := result.GetInt("diamond.left")
			So(err, ShouldBeNil)
			So(left, ShouldEqual, 11)

			right, err := result.GetInt("diamond.right")
			So(err, ShouldBeNil)
			So(right, ShouldEqual, 12)

			bottom, err := result.GetInt("diamond.bottom")
			So(err, ShouldBeNil)
			So(bottom, ShouldEqual, 23)
		})

		Convey("Complex dependency graph", func() {
			engine := NewDefaultEngineWithConfig(EngineConfig{
				EnableParallel: true,
				MaxWorkers:     4,
			})

			yaml := []byte(`
meta:
  a: 1
  b: 2
  c: 3

computed:
  d: (( calc "meta.a + meta.b" ))
  e: (( calc "meta.b + meta.c" ))
  f: (( calc "computed.d + computed.e" ))
  g: (( calc "computed.f * meta.a" ))
`)

			doc, err := engine.ParseYAML(yaml)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			// d = 1 + 2 = 3
			d, err := result.GetInt("computed.d")
			So(err, ShouldBeNil)
			So(d, ShouldEqual, 3)

			// e = 2 + 3 = 5
			e, err := result.GetInt("computed.e")
			So(err, ShouldBeNil)
			So(e, ShouldEqual, 5)

			// f = 3 + 5 = 8
			f, err := result.GetInt("computed.f")
			So(err, ShouldBeNil)
			So(f, ShouldEqual, 8)

			// g = 8 * 1 = 8
			g, err := result.GetInt("computed.g")
			So(err, ShouldBeNil)
			So(g, ShouldEqual, 8)
		})
	})
}

// TestIntegration_ParallelContextCancellation tests context cancellation
func TestIntegration_ParallelContextCancellation(t *testing.T) {
	Convey("Parallel Context Cancellation", t, func() {

		Convey("Already cancelled context", func() {
			engine := NewDefaultEngineWithConfig(EngineConfig{
				EnableParallel: true,
				MaxWorkers:     4,
			})

			yaml := []byte(`
meta:
  value: "test"
result: (( grab meta.value ))
`)

			doc, err := engine.ParseYAML(yaml)
			So(err, ShouldBeNil)

			ctx, cancel := context.WithCancel(context.Background())
			cancel() // Cancel immediately

			_, err = engine.Evaluate(ctx, doc)

			// May or may not error depending on timing
			// Just ensure no panic
		})

		Convey("Context with timeout", func() {
			engine := NewDefaultEngineWithConfig(EngineConfig{
				EnableParallel: true,
				MaxWorkers:     4,
			})

			yaml := []byte(`
meta:
  value: "quick"
result: (( grab meta.value ))
`)

			doc, err := engine.ParseYAML(yaml)
			So(err, ShouldBeNil)

			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			val, err := result.GetString("result")
			So(err, ShouldBeNil)
			So(val, ShouldEqual, "quick")
		})

		Convey("Context deadline exceeded during evaluation", func() {
			engine := NewDefaultEngineWithConfig(EngineConfig{
				EnableParallel: true,
				MaxWorkers:     1, // Single worker for predictable behavior
			})

			// Simple config that should complete quickly
			yaml := []byte(`
meta:
  value: 42
result: (( grab meta.value ))
`)

			doc, err := engine.ParseYAML(yaml)
			So(err, ShouldBeNil)

			// Very short timeout - may or may not complete
			ctx, cancel := context.WithTimeout(context.Background(), 1*time.Millisecond)
			defer cancel()

			// Just ensure no panic, result depends on timing
			_, _ = engine.Evaluate(ctx, doc)
		})
	})
}

// TestIntegration_ParallelWithCaching tests parallel execution with caching
func TestIntegration_ParallelWithCaching(t *testing.T) {
	Convey("Parallel with Caching", t, func() {

		Convey("Cached evaluations in parallel", func() {
			engine := NewDefaultEngineWithConfig(EngineConfig{
				EnableParallel: true,
				MaxWorkers:     4,
				EnableCaching:  true,
				CacheSize:      1000,
			})

			yaml := []byte(`
meta:
  value: "cached-parallel"
result: (( grab meta.value ))
`)

			var wg sync.WaitGroup
			results := make([]string, 10)
			errors := make([]error, 10)

			for i := 0; i < 10; i++ {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()

					doc, err := engine.ParseYAML(yaml)
					if err != nil {
						errors[idx] = err
						return
					}

					ctx := context.Background()
					result, err := engine.Evaluate(ctx, doc)
					if err != nil {
						errors[idx] = err
						return
					}

					val, err := result.GetString("result")
					if err != nil {
						errors[idx] = err
						return
					}

					results[idx] = val
				}(i)
			}

			wg.Wait()

			for i := range errors {
				So(errors[i], ShouldBeNil)
				So(results[i], ShouldEqual, "cached-parallel")
			}
		})

		Convey("Cache consistency under parallel load", func() {
			engine := NewDefaultEngineWithConfig(EngineConfig{
				EnableParallel: true,
				MaxWorkers:     8,
				EnableCaching:  true,
				CacheSize:      100,
			})

			yaml := []byte(`
meta:
  x: 5
  y: 10
result: (( calc "meta.x * meta.y" ))
`)

			var successCount int64
			var wg sync.WaitGroup

			for i := 0; i < 50; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()

					doc, err := engine.ParseYAML(yaml)
					if err != nil {
						return
					}

					ctx := context.Background()
					result, err := engine.Evaluate(ctx, doc)
					if err != nil {
						return
					}

					val, err := result.GetInt("result")
					if err != nil {
						return
					}

					if val == 50 {
						atomic.AddInt64(&successCount, 1)
					}
				}()
			}

			wg.Wait()

			// All evaluations should produce consistent results
			So(successCount, ShouldEqual, 50)
		})
	})
}

// TestIntegration_ParallelWorkerConfiguration tests different worker configurations
func TestIntegration_ParallelWorkerConfiguration(t *testing.T) {
	Convey("Parallel Worker Configuration", t, func() {

		workerCounts := []int{1, 2, 4, 8}

		for _, workers := range workerCounts {
			Convey("With "+string(rune('0'+workers))+" workers", func() {
				engine := NewDefaultEngineWithConfig(EngineConfig{
					EnableParallel: true,
					MaxWorkers:     workers,
				})

				yaml := []byte(`
meta:
  items:
    - value: 1
    - value: 2
    - value: 3

sum: (( calc "meta.items.0.value + meta.items.1.value + meta.items.2.value" ))
`)

				doc, err := engine.ParseYAML(yaml)
				So(err, ShouldBeNil)

				ctx := context.Background()
				result, err := engine.Evaluate(ctx, doc)
				So(err, ShouldBeNil)

				sum, err := result.GetInt("sum")
				So(err, ShouldBeNil)
				So(sum, ShouldEqual, 6)
			})
		}
	})
}

// TestIntegration_ParallelDataFlowOrder tests dataflow ordering modes
func TestIntegration_ParallelDataFlowOrder(t *testing.T) {
	Convey("Parallel DataFlow Order", t, func() {

		Convey("Alphabetical order", func() {
			engine := NewDefaultEngineWithConfig(EngineConfig{
				EnableParallel: true,
				MaxWorkers:     4,
				DataflowOrder:  "alphabetical",
			})

			yaml := []byte(`
meta:
  base: 10

z_last: (( calc "meta.base + 3" ))
a_first: (( calc "meta.base + 1" ))
m_middle: (( calc "meta.base + 2" ))
`)

			doc, err := engine.ParseYAML(yaml)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			first, err := result.GetInt("a_first")
			So(err, ShouldBeNil)
			So(first, ShouldEqual, 11)

			middle, err := result.GetInt("m_middle")
			So(err, ShouldBeNil)
			So(middle, ShouldEqual, 12)

			last, err := result.GetInt("z_last")
			So(err, ShouldBeNil)
			So(last, ShouldEqual, 13)
		})

		Convey("Insertion order", func() {
			engine := NewDefaultEngineWithConfig(EngineConfig{
				EnableParallel: true,
				MaxWorkers:     4,
				DataflowOrder:  "insertion",
			})

			yaml := []byte(`
meta:
  value: 100

first: (( grab meta.value ))
second: (( calc "first / 2" ))
third: (( calc "second + 10" ))
`)

			doc, err := engine.ParseYAML(yaml)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			first, err := result.GetInt("first")
			So(err, ShouldBeNil)
			So(first, ShouldEqual, 100)

			second, err := result.GetInt("second")
			So(err, ShouldBeNil)
			So(second, ShouldEqual, 50)

			third, err := result.GetInt("third")
			So(err, ShouldBeNil)
			So(third, ShouldEqual, 60)
		})
	})
}

// TestIntegration_ParallelStress tests parallel execution under stress
func TestIntegration_ParallelStress(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress tests in short mode")
	}

	Convey("Parallel Stress Tests", t, func() {

		Convey("Many concurrent evaluations", func() {
			engine := NewDefaultEngineWithConfig(EngineConfig{
				EnableParallel: true,
				MaxWorkers:     8,
				EnableCaching:  true,
				CacheSize:      1000,
			})

			yaml := []byte(`
meta:
  counter: 0
result: (( calc "meta.counter + 1" ))
`)

			var successCount int64
			var wg sync.WaitGroup
			concurrency := 100

			for i := 0; i < concurrency; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()

					doc, err := engine.ParseYAML(yaml)
					if err != nil {
						return
					}

					ctx := context.Background()
					_, err = engine.Evaluate(ctx, doc)
					if err == nil {
						atomic.AddInt64(&successCount, 1)
					}
				}()
			}

			wg.Wait()

			// Most evaluations should succeed
			So(successCount, ShouldBeGreaterThan, int64(concurrency*9/10))
		})

		Convey("Large document parallel evaluation", func() {
			engine := NewDefaultEngineWithConfig(EngineConfig{
				EnableParallel: true,
				MaxWorkers:     4,
			})

			// Build a large config with many operators
			yamlContent := "meta:\n  base: 10\n\ncalculations:\n"
			for i := 0; i < 50; i++ {
				yamlContent += "  calc_" + string(rune('a'+i%26)) + string(rune('0'+i/26)) + ": (( calc \"meta.base + " + string(rune('0'+i%10)) + "\" ))\n"
			}

			yaml := []byte(yamlContent)

			doc, err := engine.ParseYAML(yaml)
			So(err, ShouldBeNil)

			start := time.Now()
			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			duration := time.Since(start)

			So(err, ShouldBeNil)
			So(result, ShouldNotBeNil)
			// Should complete in reasonable time
			So(duration, ShouldBeLessThan, 10*time.Second)
		})

		Convey("Sustained parallel load", func() {
			engine := NewDefaultEngineWithConfig(EngineConfig{
				EnableParallel: true,
				MaxWorkers:     4,
				EnableCaching:  true,
				CacheSize:      500,
			})

			yaml := []byte(`
meta:
  x: 3
  y: 4
result: (( calc "meta.x * meta.y" ))
`)

			duration := 2 * time.Second
			deadline := time.Now().Add(duration)
			var successCount int64
			var wg sync.WaitGroup

			// Spawn workers that continuously evaluate
			for i := 0; i < 4; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()

					for time.Now().Before(deadline) {
						doc, err := engine.ParseYAML(yaml)
						if err != nil {
							continue
						}

						ctx := context.Background()
						result, err := engine.Evaluate(ctx, doc)
						if err != nil {
							continue
						}

						val, err := result.GetInt("result")
						if err == nil && val == 12 {
							atomic.AddInt64(&successCount, 1)
						}
					}
				}()
			}

			wg.Wait()

			// Should have completed many evaluations
			So(successCount, ShouldBeGreaterThan, 10)
		})
	})
}

// TestIntegration_ParallelErrorHandling tests error handling in parallel execution
func TestIntegration_ParallelErrorHandling(t *testing.T) {
	Convey("Parallel Error Handling", t, func() {

		Convey("Error in one operator does not affect others", func() {
			engine := NewDefaultEngineWithConfig(EngineConfig{
				EnableParallel: true,
				MaxWorkers:     4,
			})

			yaml := []byte(`
meta:
  valid: 42

valid_result: (( grab meta.valid ))
# The error case is handled by the evaluator
`)

			doc, err := engine.ParseYAML(yaml)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			val, err := result.GetInt("valid_result")
			So(err, ShouldBeNil)
			So(val, ShouldEqual, 42)
		})

		Convey("Missing reference with fallback", func() {
			engine := NewDefaultEngineWithConfig(EngineConfig{
				EnableParallel: true,
				MaxWorkers:     4,
			})

			yaml := []byte(`
result: (( grab meta.missing || "default" ))
`)

			doc, err := engine.ParseYAML(yaml)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			val, err := result.GetString("result")
			So(err, ShouldBeNil)
			So(val, ShouldEqual, "default")
		})
	})
}

// parallelIntegrationTestCases provides table-driven test cases for parallel execution
var parallelIntegrationTestCases = []struct {
	name     string
	workers  int
	config   string
	path     string
	expected interface{}
}{
	{
		name:    "single_worker_grab",
		workers: 1,
		config: `
meta:
  value: "single"
result: (( grab meta.value ))
`,
		path:     "result",
		expected: "single",
	},
	{
		name:    "multi_worker_calc",
		workers: 4,
		config: `
meta:
  a: 5
  b: 7
result: (( calc "meta.a * meta.b" ))
`,
		path:     "result",
		expected: int64(35),
	},
	{
		name:    "many_workers_concat",
		workers: 8,
		config: `
meta:
  prefix: "hello"
  suffix: "world"
result: (( concat meta.prefix "-" meta.suffix ))
`,
		path:     "result",
		expected: "hello-world",
	},
}

func TestIntegration_TableDrivenParallel(t *testing.T) {
	Convey("Table-Driven Parallel Integration Tests", t, func() {
		for _, tc := range parallelIntegrationTestCases {
			Convey(tc.name, func() {
				engine := NewDefaultEngineWithConfig(EngineConfig{
					EnableParallel: true,
					MaxWorkers:     tc.workers,
				})

				doc, err := engine.ParseYAML([]byte(tc.config))
				So(err, ShouldBeNil)

				ctx := context.Background()
				result, err := engine.Evaluate(ctx, doc)
				So(err, ShouldBeNil)

				val, err := result.Get(tc.path)
				So(err, ShouldBeNil)
				So(val, ShouldEqual, tc.expected)
			})
		}
	})
}

// TestIntegration_ParallelStatsAndMetrics tests parallel execution statistics
func TestIntegration_ParallelStatsAndMetrics(t *testing.T) {
	Convey("Parallel Statistics and Metrics", t, func() {

		Convey("ParallelExecutionStats returns expected structure", func() {
			stats := ParallelExecutionStats()
			So(stats, ShouldNotBeNil)

			// Phase 1 implementation returns disabled status
			enabled, ok := stats["enabled"].(bool)
			So(ok, ShouldBeTrue)
			So(enabled, ShouldBeFalse) // Parallel not fully implemented in Phase 1

			message, ok := stats["message"].(string)
			So(ok, ShouldBeTrue)
			So(message, ShouldNotBeEmpty)
		})

		Convey("Engine config reflects parallel settings", func() {
			engine := NewDefaultEngineWithConfig(EngineConfig{
				EnableParallel: true,
				MaxWorkers:     8,
			})

			So(engine.config.EnableParallel, ShouldBeTrue)
			So(engine.config.MaxWorkers, ShouldEqual, 8)
		})
	})
}
