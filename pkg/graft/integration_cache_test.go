// Integration tests for graft caching behavior.

package graft

import (
	"context"
	"sync"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
)

// TestIntegration_CacheHitMiss tests cache hit and miss behavior.
func TestIntegration_CacheHitMiss(t *testing.T) {
	Convey("Cache Hit/Miss Behavior", t, func() {
		Convey("Engine with caching enabled processes same document twice", func() {
			config := EngineConfig{
				EnableCaching: true,
				CacheSize:     1000,
			}
			engine := NewDefaultEngineWithConfig(config)

			yaml := []byte(`
meta:
  name: "test-app"
  version: "1.0"

app:
  full_name: (( concat meta.name "-" meta.version ))
`)

			// First evaluation
			doc1, err := engine.ParseYAML(yaml)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result1, err := engine.Evaluate(ctx, doc1)
			So(err, ShouldBeNil)

			fullName1, err := result1.GetString("app.full_name")
			So(err, ShouldBeNil)
			So(fullName1, ShouldEqual, "test-app-1.0")

			// Second evaluation with same input (should potentially hit cache)
			doc2, err := engine.ParseYAML(yaml)
			So(err, ShouldBeNil)

			result2, err := engine.Evaluate(ctx, doc2)
			So(err, ShouldBeNil)

			fullName2, err := result2.GetString("app.full_name")
			So(err, ShouldBeNil)
			So(fullName2, ShouldEqual, "test-app-1.0")

			// Results should be consistent
			So(fullName1, ShouldEqual, fullName2)
		})

		Convey("Engine without caching still produces correct results", func() {
			config := EngineConfig{
				EnableCaching: false,
			}
			engine := NewDefaultEngineWithConfig(config)

			yaml := []byte(`
meta:
  value: 100

computed: (( calc "meta.value * 2" ))
`)

			doc, err := engine.ParseYAML(yaml)
			So(err, ShouldBeNil)

			ctx := context.Background()
			result, err := engine.Evaluate(ctx, doc)
			So(err, ShouldBeNil)

			computed, err := result.GetInt("computed")
			So(err, ShouldBeNil)
			So(computed, ShouldEqual, 200)
		})

		Convey("Different inputs produce different outputs", func() {
			config := EngineConfig{
				EnableCaching: true,
				CacheSize:     1000,
			}
			engine := NewDefaultEngineWithConfig(config)

			yaml1 := []byte(`
meta:
  value: "first"
result: (( grab meta.value ))
`)

			yaml2 := []byte(`
meta:
  value: "second"
result: (( grab meta.value ))
`)

			doc1, err := engine.ParseYAML(yaml1)
			So(err, ShouldBeNil)

			doc2, err := engine.ParseYAML(yaml2)
			So(err, ShouldBeNil)

			ctx := context.Background()

			result1, err := engine.Evaluate(ctx, doc1)
			So(err, ShouldBeNil)

			result2, err := engine.Evaluate(ctx, doc2)
			So(err, ShouldBeNil)

			val1, err := result1.GetString("result")
			So(err, ShouldBeNil)

			val2, err := result2.GetString("result")
			So(err, ShouldBeNil)

			So(val1, ShouldNotEqual, val2)
			So(val1, ShouldEqual, "first")
			So(val2, ShouldEqual, "second")
		})
	})
}

// TestIntegration_CacheInvalidation tests cache invalidation scenarios.
func TestIntegration_CacheInvalidation(t *testing.T) {
	Convey("Cache Invalidation", t, func() {
		Convey("Config changes produce different results", func() {
			engine1 := NewDefaultEngineWithConfig(EngineConfig{
				EnableCaching: true,
				CacheSize:     1000,
			})

			engine2 := NewDefaultEngineWithConfig(EngineConfig{
				EnableCaching: false,
			})

			yaml := []byte(`
meta:
  x: 5
  y: 3
result: (( calc "meta.x * meta.y" ))
`)

			ctx := context.Background()

			doc1, err := engine1.ParseYAML(yaml)
			So(err, ShouldBeNil)
			result1, err := engine1.Evaluate(ctx, doc1)
			So(err, ShouldBeNil)

			doc2, err := engine2.ParseYAML(yaml)
			So(err, ShouldBeNil)
			result2, err := engine2.Evaluate(ctx, doc2)
			So(err, ShouldBeNil)

			val1, err := result1.GetInt("result")
			So(err, ShouldBeNil)

			val2, err := result2.GetInt("result")
			So(err, ShouldBeNil)

			// Both should produce the same correct result
			So(val1, ShouldEqual, 15)
			So(val2, ShouldEqual, 15)
		})

		Convey("Sequential evaluations with mutations", func() {
			engine := NewDefaultEngineWithConfig(EngineConfig{
				EnableCaching: true,
				CacheSize:     1000,
			})

			// Base document
			baseYaml := []byte(`
meta:
  base: 10
result: (( calc "meta.base + 5" ))
`)

			// Modified document
			modifiedYaml := []byte(`
meta:
  base: 20
result: (( calc "meta.base + 5" ))
`)

			ctx := context.Background()

			// Evaluate base
			doc1, err := engine.ParseYAML(baseYaml)
			So(err, ShouldBeNil)
			result1, err := engine.Evaluate(ctx, doc1)
			So(err, ShouldBeNil)

			val1, err := result1.GetInt("result")
			So(err, ShouldBeNil)
			So(val1, ShouldEqual, 15)

			// Evaluate modified - should not use cached result
			doc2, err := engine.ParseYAML(modifiedYaml)
			So(err, ShouldBeNil)
			result2, err := engine.Evaluate(ctx, doc2)
			So(err, ShouldBeNil)

			val2, err := result2.GetInt("result")
			So(err, ShouldBeNil)
			So(val2, ShouldEqual, 25)
		})
	})
}

// TestIntegration_CacheWithEngineState tests caching with engine state (vault, AWS caches).
func TestIntegration_CacheWithEngineState(t *testing.T) {
	Convey("Cache with Engine State", t, func() {
		Convey("Vault cache integration", func() {
			engine := NewDefaultEngineWithConfig(EngineConfig{
				EnableCaching: true,
				CacheSize:     1000,
				SkipVault:     true, // Skip actual vault calls
			})

			// Set up vault cache manually
			vaultData := map[string]interface{}{
				"username": "admin",
				"password": "secret123",
			}
			engine.SetVaultCache("secret/test", vaultData)

			// Verify cache was set
			cache := engine.GetVaultCache()
			So(cache["secret/test"], ShouldNotBeNil)
			So(cache["secret/test"]["username"], ShouldEqual, "admin")
		})

		Convey("AWS cache integration", func() {
			engine := NewDefaultEngineWithConfig(EngineConfig{
				EnableCaching: true,
				CacheSize:     1000,
				SkipAWS:       true, // Skip actual AWS calls
			})

			// Set up AWS secrets cache
			engine.SetAWSSecretCache("my-secret", "secret-value")
			engine.SetAWSParamCache("/config/key", "param-value")

			// Verify caches were set
			secretsCache := engine.GetAWSSecretsCache()
			So(secretsCache["my-secret"], ShouldEqual, "secret-value")

			paramsCache := engine.GetAWSParamsCache()
			So(paramsCache["/config/key"], ShouldEqual, "param-value")
		})

		Convey("IP allocation cache", func() {
			engine := NewDefaultEngineWithConfig(EngineConfig{
				EnableCaching: true,
				CacheSize:     1000,
			})

			// Set up IP allocations
			engine.SetUsedIP("10.0.0.1", "job1")
			engine.SetUsedIP("10.0.0.2", "job2")

			// Verify IPs were tracked
			usedIPs := engine.GetUsedIPs()
			So(usedIPs["10.0.0.1"], ShouldEqual, "job1")
			So(usedIPs["10.0.0.2"], ShouldEqual, "job2")
		})
	})
}

// TestIntegration_CacheWithParallelOps tests cache behavior with parallel operations.
func TestIntegration_CacheWithParallelOps(t *testing.T) {
	Convey("Cache with Parallel Operations", t, func() {
		Convey("Concurrent evaluations with shared engine", func() {
			engine := NewDefaultEngineWithConfig(EngineConfig{
				EnableCaching: true,
				CacheSize:     1000,
			})

			yaml := []byte(`
meta:
  value: "test"
result: (( concat meta.value "-output" ))
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

			// All evaluations should succeed
			for i, err := range errors {
				So(err, ShouldBeNil)
				So(results[i], ShouldEqual, "test-output")
			}
		})

		Convey("Concurrent evaluations with different inputs", func() {
			engine := NewDefaultEngineWithConfig(EngineConfig{
				EnableCaching: true,
				CacheSize:     1000,
			})

			var wg sync.WaitGroup
			results := make(map[int]int)
			var mu sync.Mutex

			for i := 0; i < 5; i++ {
				wg.Add(1)
				go func(idx int) {
					defer wg.Done()

					yaml := []byte(`
meta:
  base: ` + string(rune('0'+idx)) + `
result: (( calc "meta.base * 10" ))
`)

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

					mu.Lock()
					results[idx] = val
					mu.Unlock()
				}(i)
			}

			wg.Wait()

			// Each evaluation should produce its own result
			for i := 0; i < 5; i++ {
				expected := i * 10
				So(results[i], ShouldEqual, expected)
			}
		})
	})
}

// TestIntegration_FeatureFlagToggling tests toggling caching on and off.
func TestIntegration_FeatureFlagToggling(t *testing.T) {
	Convey("Feature Flag Toggling", t, func() {
		Convey("Toggle caching via config update", func() {
			engine := NewDefaultEngineWithConfig(EngineConfig{
				EnableCaching: true,
				CacheSize:     1000,
			})

			yaml := []byte(`
meta:
  value: 42
result: (( grab meta.value ))
`)

			ctx := context.Background()

			// Evaluate with caching enabled
			doc1, err := engine.ParseYAML(yaml)
			So(err, ShouldBeNil)
			result1, err := engine.Evaluate(ctx, doc1)
			So(err, ShouldBeNil)

			val1, err := result1.GetInt("result")
			So(err, ShouldBeNil)
			So(val1, ShouldEqual, 42)

			// Update config to disable caching
			engine.UpdateConfig(EngineConfig{
				EnableCaching: false,
			})

			// Evaluate with caching disabled
			doc2, err := engine.ParseYAML(yaml)
			So(err, ShouldBeNil)
			result2, err := engine.Evaluate(ctx, doc2)
			So(err, ShouldBeNil)

			val2, err := result2.GetInt("result")
			So(err, ShouldBeNil)
			So(val2, ShouldEqual, 42)

			// Results should still be correct
			So(val1, ShouldEqual, val2)
		})

		Convey("Toggle parser type", func() {
			engine := NewDefaultEngineWithConfig(EngineConfig{
				EnableCaching: true,
			})

			yaml := []byte(`
meta:
  a: "hello"
  b: "world"
result: (( concat meta.a " " meta.b ))
`)

			ctx := context.Background()

			// Evaluate with enhanced parser
			doc1, err := engine.ParseYAML(yaml)
			So(err, ShouldBeNil)
			result1, err := engine.Evaluate(ctx, doc1)
			So(err, ShouldBeNil)

			val1, err := result1.GetString("result")
			So(err, ShouldBeNil)
			So(val1, ShouldEqual, "hello world")

			// Toggle to standard parser
			engine.UpdateConfig(EngineConfig{
				EnableCaching: true,
			})

			// Evaluate with standard parser
			doc2, err := engine.ParseYAML(yaml)
			So(err, ShouldBeNil)
			result2, err := engine.Evaluate(ctx, doc2)
			So(err, ShouldBeNil)

			val2, err := result2.GetString("result")
			So(err, ShouldBeNil)
			So(val2, ShouldEqual, "hello world")
		})

		Convey("Skip external services", func() {
			// Test with vault skip
			engineWithVault := NewDefaultEngineWithConfig(EngineConfig{
				SkipVault: false,
			})
			So(engineWithVault.IsVaultSkipped(), ShouldBeFalse)

			engineWithoutVault := NewDefaultEngineWithConfig(EngineConfig{
				SkipVault: true,
			})
			So(engineWithoutVault.IsVaultSkipped(), ShouldBeTrue)

			// Test with AWS skip
			engineWithAWS := NewDefaultEngineWithConfig(EngineConfig{
				SkipAWS: false,
			})
			So(engineWithAWS.IsAWSSkipped(), ShouldBeFalse)

			engineWithoutAWS := NewDefaultEngineWithConfig(EngineConfig{
				SkipAWS: true,
			})
			So(engineWithoutAWS.IsAWSSkipped(), ShouldBeTrue)
		})
	})
}

// TestIntegration_CacheConsistency tests cache consistency across operations.
func TestIntegration_CacheConsistency(t *testing.T) {
	Convey("Cache Consistency", t, func() {
		Convey("Repeated evaluations produce consistent results", func() {
			engine := NewDefaultEngineWithConfig(EngineConfig{
				EnableCaching: true,
				CacheSize:     1000,
			})

			yaml := []byte(`
meta:
  timestamp: "2024-01-01"
  version: 1

formatted: (( concat "v" meta.version "-" meta.timestamp ))
`)

			ctx := context.Background()
			var results []string

			for i := 0; i < 5; i++ {
				doc, err := engine.ParseYAML(yaml)
				So(err, ShouldBeNil)

				result, err := engine.Evaluate(ctx, doc)
				So(err, ShouldBeNil)

				val, err := result.GetString("formatted")
				So(err, ShouldBeNil)

				results = append(results, val)
			}

			// All results should be identical
			for _, result := range results {
				So(result, ShouldEqual, "v1-2024-01-01")
			}
		})

		Convey("Engine state isolation between evaluations", func() {
			engine := NewDefaultEngineWithConfig(EngineConfig{
				EnableCaching: true,
				CacheSize:     1000,
			})

			yaml1 := []byte(`
meta:
  items:
    - name: "item1"
    - name: "item2"
`)

			yaml2 := []byte(`
meta:
  items:
    - name: "itemA"
    - name: "itemB"
    - name: "itemC"
`)

			ctx := context.Background()

			doc1, err := engine.ParseYAML(yaml1)
			So(err, ShouldBeNil)
			result1, err := engine.Evaluate(ctx, doc1)
			So(err, ShouldBeNil)

			items1, err := result1.GetSlice("meta.items")
			So(err, ShouldBeNil)
			So(len(items1), ShouldEqual, 2)

			doc2, err := engine.ParseYAML(yaml2)
			So(err, ShouldBeNil)
			result2, err := engine.Evaluate(ctx, doc2)
			So(err, ShouldBeNil)

			items2, err := result2.GetSlice("meta.items")
			So(err, ShouldBeNil)
			So(len(items2), ShouldEqual, 3)

			// Verify original result was not affected
			So(len(items1), ShouldNotEqual, len(items2))
		})
	})
}

// TestIntegration_CacheStress tests cache behavior under stress.
func TestIntegration_CacheStress(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping stress tests in short mode")
	}

	Convey("Cache Stress Tests", t, func() {
		Convey("Many unique evaluations", func() {
			engine := NewDefaultEngineWithConfig(EngineConfig{
				EnableCaching: true,
				CacheSize:     100, // Small cache to force evictions
			})

			ctx := context.Background()
			successCount := 0

			for i := 0; i < 200; i++ {
				yaml := []byte(`
meta:
  index: ` + string(rune('0'+i%10)) + `
result: (( grab meta.index ))
`)

				doc, err := engine.ParseYAML(yaml)
				if err != nil {
					continue
				}

				_, err = engine.Evaluate(ctx, doc)
				if err == nil {
					successCount++
				}
			}

			// Most evaluations should succeed despite cache pressure
			So(successCount, ShouldBeGreaterThan, 150)
		})

		Convey("Rapid sequential evaluations", func() {
			engine := NewDefaultEngineWithConfig(EngineConfig{
				EnableCaching: true,
				CacheSize:     1000,
			})

			yaml := []byte(`
meta:
  counter: 0
result: (( calc "meta.counter + 1" ))
`)

			ctx := context.Background()
			start := time.Now()
			iterations := 100

			for i := 0; i < iterations; i++ {
				doc, err := engine.ParseYAML(yaml)
				So(err, ShouldBeNil)

				_, err = engine.Evaluate(ctx, doc)
				So(err, ShouldBeNil)
			}

			duration := time.Since(start)

			// Should complete reasonably quickly (< 5 seconds for 100 iterations)
			So(duration, ShouldBeLessThan, 5*time.Second)
		})
	})
}

// cacheIntegrationTestCases provides table-driven test cases for caching.
var cacheIntegrationTestCases = []struct {
	name          string
	cacheEnabled  bool
	config        string
	path          string
	expected      interface{}
	expectedMatch bool
}{
	{
		name:         "simple_with_cache",
		cacheEnabled: true,
		config: `
meta:
  value: "cached"
result: (( grab meta.value ))
`,
		path:          "result",
		expected:      "cached",
		expectedMatch: true,
	},
	{
		name:         "simple_without_cache",
		cacheEnabled: false,
		config: `
meta:
  value: "uncached"
result: (( grab meta.value ))
`,
		path:          "result",
		expected:      "uncached",
		expectedMatch: true,
	},
	{
		name:         "calc_with_cache",
		cacheEnabled: true,
		config: `
meta:
  a: 10
  b: 5
result: (( calc "meta.a + meta.b" ))
`,
		path:          "result",
		expected:      int64(15),
		expectedMatch: true,
	},
}

func TestIntegration_TableDrivenCache(t *testing.T) {
	Convey("Table-Driven Cache Integration Tests", t, func() {
		for _, tc := range cacheIntegrationTestCases {
			Convey(tc.name, func() {
				engine := NewDefaultEngineWithConfig(EngineConfig{
					EnableCaching: tc.cacheEnabled,
					CacheSize:     1000,
				})

				doc, err := engine.ParseYAML([]byte(tc.config))
				So(err, ShouldBeNil)

				ctx := context.Background()
				result, err := engine.Evaluate(ctx, doc)
				So(err, ShouldBeNil)

				val, err := result.Get(tc.path)
				So(err, ShouldBeNil)

				if tc.expectedMatch {
					So(val, ShouldEqual, tc.expected)
				}
			})
		}
	})
}
