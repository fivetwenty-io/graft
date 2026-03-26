//go:build race
// +build race

package graft

import (
	"fmt"
	"sync"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	yamlv3 "gopkg.in/yaml.v3"
)

// parseYAML is a helper function to parse YAML into a map for tests
func parseYAML(s string) map[string]interface{} {
	data := make(map[string]interface{})
	if err := yamlv3.Unmarshal([]byte(s), &data); err != nil {
		panic(fmt.Sprintf("failed to parse YAML: %v", err))
	}
	return data
}

// TestTreeRaceConditions specifically tests for race conditions in tree operations
func TestTreeRaceConditions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping race condition tests in short mode")
	}

	Convey("Tree operations under concurrent access", t, func() {
		Convey("Concurrent reads and writes to same path", func() {
			data := make(map[string]interface{})
			data["meta"] = map[string]interface{}{
				"name": "initial",
			}
			tree := NewCOWTree(data)

			var wg sync.WaitGroup
			errors := make(chan error, 100)

			// Writers
			for i := 0; i < 5; i++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()
					for j := 0; j < 100; j++ {
						err := tree.Set("meta.name", fmt.Sprintf("writer-%d-%d", id, j))
						if err != nil {
							errors <- err
						}
					}
				}(i)
			}

			// Readers
			for i := 0; i < 5; i++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()
					for j := 0; j < 100; j++ {
						val, err := tree.Find("meta", "name")
						if err != nil || val == nil {
							errors <- fmt.Errorf("reader %d: nil value or error at iteration %d: %v", id, j, err)
						}
					}
				}(i)
			}

			wg.Wait()
			close(errors)

			// Check for errors - COWTree should handle concurrent access safely
			var errorCount int
			for err := range errors {
				t.Logf("Concurrent access issue: %v", err)
				errorCount++
			}

			// With COWTree, we expect no race condition errors
			So(errorCount, ShouldEqual, 0)
		})

		Convey("Concurrent modifications to different paths", func() {
			data := make(map[string]interface{})
			tree := NewCOWTree(data)

			var wg sync.WaitGroup

			// Each goroutine modifies its own section
			for i := 0; i < 10; i++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()
					section := fmt.Sprintf("section%d", id)

					for j := 0; j < 100; j++ {
						key := fmt.Sprintf("%s.key%d", section, j)
						tree.Set(key, fmt.Sprintf("value%d", j))
					}
				}(i)
			}

			wg.Wait()

			// Verify some sections exist
			for i := 0; i < 10; i++ {
				section := fmt.Sprintf("section%d", i)
				val, err := tree.Find(section)
				So(err, ShouldBeNil)
				So(val, ShouldNotBeNil)
			}
		})

		Convey("Nested map concurrent access", func() {
			data := make(map[string]interface{})
			data["root"] = map[string]interface{}{
				"level1": map[string]interface{}{
					"level2": map[string]interface{}{
						"value": 0,
					},
				},
			}
			tree := NewCOWTree(data)

			var wg sync.WaitGroup

			// Multiple goroutines accessing nested paths
			for i := 0; i < 10; i++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()
					for j := 0; j < 50; j++ {
						// Read deep value
						val, _ := tree.Find("root", "level1", "level2", "value")
						_ = val

						// Modify deep value
						tree.Set("root.level1.level2.value", id*1000+j)
					}
				}(i)
			}

			done := make(chan struct{})
			go func() {
				wg.Wait()
				close(done)
			}()

			select {
			case <-done:
				// Success
			case <-time.After(5 * time.Second):
				t.Fatal("Test timed out - possible deadlock")
			}
		})
	})
}

// TestEvaluatorRaceConditions tests for race conditions in evaluator
func TestEvaluatorRaceConditions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping race condition tests in short mode")
	}

	Convey("Evaluator operations under concurrent access", t, func() {
		Convey("Multiple evaluators on same YAML", func() {
			yamlStr := `
meta:
  name: test
  value: hello
results:
  - name: (( grab meta.name ))
    value: (( grab meta.value ))
  - name: (( concat meta.name "-2" ))
    value: (( concat meta.value " world" ))
`

			var wg sync.WaitGroup
			errors := make(chan error, 100)

			for i := 0; i < 5; i++ {
				wg.Add(1)
				go func(id int) {
					defer wg.Done()

					// Each goroutine parses its own copy
					tree := parseYAML(yamlStr)

					ev := &Evaluator{Tree: tree}

					// Create and set an engine to ensure operators are registered
					engine, engineErr := CreateDefaultEngine()
					if engineErr != nil {
						errors <- fmt.Errorf("evaluator %d engine: %v", id, engineErr)
						return
					}
					ev.SetEngine(engine)

					err := ev.Run([]string{}, []string{})
					if err != nil {
						errors <- fmt.Errorf("evaluator %d: %v", id, err)
					}
				}(i)
			}

			wg.Wait()
			close(errors)

			errorCount := 0
			for err := range errors {
				t.Logf("Evaluator error: %v", err)
				errorCount++
			}

			So(errorCount, ShouldEqual, 0)
		})

	})
}

// TestOperatorRaceConditions tests individual operators for thread safety
func TestOperatorRaceConditions(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping race condition tests in short mode")
	}

	Convey("Operator thread safety", t, func() {
		Convey("Concurrent tree access via evaluator", func() {
			data := make(map[string]interface{})
			data["source"] = map[string]interface{}{
				"value": "test",
			}

			ev := &Evaluator{Tree: data}

			var wg sync.WaitGroup
			results := make(chan interface{}, 100)

			for i := 0; i < 10; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for j := 0; j < 10; j++ {
						// Access the tree value through evaluator
						if source, ok := ev.Tree["source"].(map[string]interface{}); ok {
							if val, ok := source["value"]; ok {
								results <- val
							}
						}
					}
				}()
			}

			wg.Wait()
			close(results)

			// All results should be "test"
			for result := range results {
				So(result, ShouldEqual, "test")
			}
		})

		Convey("COWTree concurrent increment simulation", func() {
			data := make(map[string]interface{})
			data["counter"] = 0
			tree := NewCOWTree(data)

			var wg sync.WaitGroup
			var mu sync.Mutex // External mutex for atomic read-modify-write

			for i := 0; i < 10; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for j := 0; j < 100; j++ {
						// Simulate atomic increment operation
						// Use external mutex since COWTree doesn't support atomic read-modify-write
						mu.Lock()
						current, err := tree.Find("counter")
						if err == nil {
							if val, ok := current.(int); ok {
								tree.Set("counter", val+1)
							}
						}
						mu.Unlock()
					}
				}()
			}

			wg.Wait()

			finalValue, err := tree.Find("counter")
			So(err, ShouldBeNil)
			// With proper locking, final value should be 1000
			t.Logf("Final counter value: %v (expected 1000)", finalValue)
			So(finalValue, ShouldEqual, 1000)
		})
	})
}

// TestDeadlockScenarios tests for potential deadlock situations
func TestDeadlockScenarios(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping deadlock tests in short mode")
	}

	Convey("Deadlock detection", t, func() {
		Convey("Circular operator dependencies", func() {
			yamlStr := `
a: (( grab b ))
b: (( grab c ))
c: (( grab a ))
`
			tree := parseYAML(yamlStr)
			ev := &Evaluator{Tree: tree}

			// Create and set an engine to ensure operators are registered
			engine, engineErr := CreateDefaultEngine()
			So(engineErr, ShouldBeNil)
			ev.SetEngine(engine)

			done := make(chan error, 1)
			go func() {
				done <- ev.Run([]string{}, []string{})
			}()

			select {
			case err := <-done:
				So(err, ShouldNotBeNil)
				So(err.Error(), ShouldContainSubstring, "cycle")
			case <-time.After(2 * time.Second):
				// Current implementation detects cycles, so this shouldn't happen
				t.Fatal("Deadlock detected - circular dependency not caught")
			}
		})

		Convey("Concurrent evaluator with interdependencies", func() {
			yamlStr := `
shared:
  value: 1
workers:
  - id: 1
    value: (( shared.value + 1 ))
  - id: 2
    value: (( shared.value + 2 ))
  - id: 3
    value: (( shared.value + 3 ))
`

			// Multiple evaluators trying to resolve same dependencies
			var wg sync.WaitGroup
			for i := 0; i < 3; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					// Each goroutine parses its own copy
					tree := parseYAML(yamlStr)
					ev := &Evaluator{Tree: tree}

					// Create and set an engine to ensure operators are registered
					engine, _ := CreateDefaultEngine()
					ev.SetEngine(engine)

					ev.Run([]string{}, []string{})
				}()
			}

			done := make(chan struct{})
			go func() {
				wg.Wait()
				close(done)
			}()

			select {
			case <-done:
				// Success - no deadlock
			case <-time.After(5 * time.Second):
				t.Fatal("Potential deadlock in concurrent evaluation")
			}
		})
	})
}
