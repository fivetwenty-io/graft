//go:build race
// +build race

package graft

import (
	"fmt"
	"sync"
	"testing"
	"time"

	. "github.com/smartystreets/goconvey/convey"
	yamlv3 "github.com/goccy/go-yaml"
)

// parseYAML is a helper function to parse YAML into a map for tests
func parseYAML(s string) map[string]interface{} {
	data := make(map[string]interface{})
	if err := yamlv3.Unmarshal(QuoteInjectKeys([]byte(s)), &data); err != nil {
		panic(fmt.Sprintf("failed to parse YAML: %v", err))
	}
	return NormalizeMap(data)
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
