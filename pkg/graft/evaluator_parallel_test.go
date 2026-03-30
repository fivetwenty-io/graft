package graft

import (
	"context"
	"testing"

	"github.com/fivetwenty-io/graft/internal/features"
	"github.com/fivetwenty-io/graft/internal/parallel"
	"github.com/fivetwenty-io/graft/internal/utils/ansi"
	treepkg "github.com/fivetwenty-io/graft/pkg/graft/tree"

	yamlv3 "github.com/goccy/go-yaml"
)

// helper: parse YAML into map[string]interface{}
func parseYAMLForTest(t *testing.T, s string) map[string]interface{} {
	t.Helper()
	data := map[string]interface{}{}
	if err := yamlv3.Unmarshal(QuoteInjectKeys([]byte(s)), &data); err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}
	return NormalizeMap(data)
}

// helper: create an engine with parallel evaluation enabled and a worker pool
func newParallelEngine(t *testing.T) *DefaultEngine {
	t.Helper()
	opts := defaultEngineOpts()
	opts.EnableParallel = true
	e := newEngineFromOptions(&opts)

	// Enable the parallel evaluation feature flag
	e.Features = features.DefaultFlags()
	e.Features.Enable(features.FeatureParallelEvaluation)

	// Create and attach a worker pool
	pool, err := parallel.NewPool(2, 4)
	if err != nil {
		t.Fatalf("failed to create worker pool: %v", err)
	}
	e.Pool = pool
	t.Cleanup(func() {
		pool.ShutdownWait()
	})

	return e
}

// TestParallelRunPhaseBasic verifies that RunPhaseParallel produces the same
// results as RunPhase for a simple document with independent grab operators.
func TestParallelRunPhaseBasic(t *testing.T) {
	ansi.Color(false)
	SilenceWarnings(true)

	yaml := `
defaults:
  port: 8080
  host: localhost
services:
  web:
    port: (( grab defaults.port ))
    host: (( grab defaults.host ))
`
	engine := newParallelEngine(t)

	tree := parseYAMLForTest(t, yaml)
	ev := &Evaluator{
		Tree: tree,
		Deps: map[string][]treepkg.Cursor{},
	}
	ev.SetEngine(engine)

	err := ev.RunPhaseParallel(EvalPhase)
	if err != nil {
		t.Fatalf("RunPhaseParallel failed: %v", err)
	}

	// Verify that the operators were resolved
	services, ok := ev.Tree["services"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected services to be a map, got %T", ev.Tree["services"])
	}
	web, ok := services["web"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected services.web to be a map, got %T", services["web"])
	}

	if web["port"] != 8080 {
		t.Errorf("expected port=8080, got %v", web["port"])
	}
	if web["host"] != "localhost" {
		t.Errorf("expected host=localhost, got %v", web["host"])
	}
}

// TestParallelRunPhaseWithDependencies verifies that RunPhaseParallel correctly
// handles operators that depend on other operators (chained grabs).
func TestParallelRunPhaseWithDependencies(t *testing.T) {
	ansi.Color(false)
	SilenceWarnings(true)

	yaml := `
base:
  value: 42
middle:
  ref: (( grab base.value ))
top:
  result: (( grab middle.ref ))
`
	engine := newParallelEngine(t)

	tree := parseYAMLForTest(t, yaml)
	ev := &Evaluator{
		Tree: tree,
		Deps: map[string][]treepkg.Cursor{},
	}
	ev.SetEngine(engine)

	err := ev.RunPhaseParallel(EvalPhase)
	if err != nil {
		t.Fatalf("RunPhaseParallel failed: %v", err)
	}

	middle, ok := ev.Tree["middle"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected middle to be a map, got %T", ev.Tree["middle"])
	}
	if middle["ref"] != 42 {
		t.Errorf("expected middle.ref=42, got %v", middle["ref"])
	}

	top, ok := ev.Tree["top"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected top to be a map, got %T", ev.Tree["top"])
	}
	if top["result"] != 42 {
		t.Errorf("expected top.result=42, got %v", top["result"])
	}
}

// TestParallelFallbackWithoutPool verifies that RunPhaseParallel falls back
// to sequential execution when no worker pool is available.
func TestParallelFallbackWithoutPool(t *testing.T) {
	ansi.Color(false)
	SilenceWarnings(true)

	yaml := `
a: 1
b: (( grab a ))
`
	// Use a default engine without a pool
	opts := defaultEngineOpts()
	engine := newEngineFromOptions(&opts)
	engine.Features = features.DefaultFlags()

	tree := parseYAMLForTest(t, yaml)
	ev := &Evaluator{
		Tree: tree,
		Deps: map[string][]treepkg.Cursor{},
	}
	ev.SetEngine(engine)

	err := ev.RunPhaseParallel(EvalPhase)
	if err != nil {
		t.Fatalf("RunPhaseParallel fallback failed: %v", err)
	}

	if ev.Tree["b"] != 1 {
		t.Errorf("expected b=1, got %v", ev.Tree["b"])
	}
}

// TestParallelExecutionStats verifies that stats report meaningful data
// after parallel execution.
func TestParallelExecutionStats(t *testing.T) {
	stats := ParallelExecutionStats()
	if stats == nil {
		t.Fatal("expected non-nil stats")
	}
	// After implementation, "enabled" should be true (or at least present)
	if _, ok := stats["enabled"]; !ok {
		t.Error("expected 'enabled' key in stats")
	}
}

// TestParallelRunPhaseRaceSafety runs parallel evaluation under the race
// detector with a document containing many independent operators to stress
// concurrent access to the tree.
func TestParallelRunPhaseRaceSafety(t *testing.T) {
	ansi.Color(false)
	SilenceWarnings(true)

	// Build a document with many independent grabs — all reading from "src"
	yaml := `
src:
  a: 1
  b: 2
  c: 3
  d: 4
  e: 5
dst:
  a: (( grab src.a ))
  b: (( grab src.b ))
  c: (( grab src.c ))
  d: (( grab src.d ))
  e: (( grab src.e ))
`
	engine := newParallelEngine(t)

	tree := parseYAMLForTest(t, yaml)
	ev := &Evaluator{
		Tree: tree,
		Deps: map[string][]treepkg.Cursor{},
	}
	ev.SetEngine(engine)

	err := ev.RunPhaseParallel(EvalPhase)
	if err != nil {
		t.Fatalf("RunPhaseParallel failed: %v", err)
	}

	dst, ok := ev.Tree["dst"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected dst to be a map, got %T", ev.Tree["dst"])
	}

	expected := map[string]int{
		"a": 1, "b": 2, "c": 3, "d": 4, "e": 5,
	}
	for k, want := range expected {
		if dst[k] != want {
			t.Errorf("dst.%s: expected %d, got %v", k, want, dst[k])
		}
	}
}

// TestParallelEngineEvaluate tests the full engine.evaluate path with parallel
// enabled, ensuring the wiring in engine.go dispatches to RunPhaseParallel.
func TestParallelEngineEvaluate(t *testing.T) {
	ansi.Color(false)
	SilenceWarnings(true)

	yaml := `
x: hello
y: (( grab x ))
`
	engine := newParallelEngine(t)

	tree := parseYAMLForTest(t, yaml)
	ev := engine.createEvaluator(tree)

	ctx := context.Background()
	err := engine.evaluate(ctx, ev)
	if err != nil {
		t.Fatalf("engine.evaluate with parallel failed: %v", err)
	}

	if ev.Tree["y"] != "hello" {
		t.Errorf("expected y=hello, got %v", ev.Tree["y"])
	}
}

// TestParallelNoOps verifies RunPhaseParallel handles an empty phase gracefully.
func TestParallelNoOps(t *testing.T) {
	ansi.Color(false)
	SilenceWarnings(true)

	yaml := `
a: 1
b: 2
`
	engine := newParallelEngine(t)

	tree := parseYAMLForTest(t, yaml)
	ev := &Evaluator{
		Tree: tree,
		Deps: map[string][]treepkg.Cursor{},
	}
	ev.SetEngine(engine)

	err := ev.RunPhaseParallel(EvalPhase)
	if err != nil {
		t.Fatalf("RunPhaseParallel with no ops failed: %v", err)
	}
}

// TestParallelRunOpsParallel verifies RunOpsParallel executes operations
// correctly under the parallel scheduler.
func TestParallelRunOpsParallel(t *testing.T) {
	ansi.Color(false)
	SilenceWarnings(true)

	yaml := `
src: world
greeting: (( concat "hello " src ))
`
	engine := newParallelEngine(t)

	tree := parseYAMLForTest(t, yaml)
	ev := &Evaluator{
		Tree:          tree,
		Deps:          map[string][]treepkg.Cursor{},
		DataflowOrder: "alphabetical",
	}
	ev.SetEngine(engine)

	// Get the ops via DataFlow, then run them through RunOpsParallel
	ops, err := ev.DataFlow(EvalPhase)
	if err != nil {
		t.Fatalf("DataFlow failed: %v", err)
	}

	err = ev.RunOpsParallel(ops)
	if err != nil {
		t.Fatalf("RunOpsParallel failed: %v", err)
	}

	if ev.Tree["greeting"] != "hello world" {
		t.Errorf("expected 'hello world', got %v", ev.Tree["greeting"])
	}
}

// TestParallelStatsAfterExecution verifies stats are updated after
// a real parallel run.
func TestParallelStatsAfterExecution(t *testing.T) {
	ansi.Color(false)
	SilenceWarnings(true)

	// First reset any global counters
	resetParallelStats()

	yaml := `
a: 1
b: (( grab a ))
`
	engine := newParallelEngine(t)

	tree := parseYAMLForTest(t, yaml)
	ev := &Evaluator{
		Tree: tree,
		Deps: map[string][]treepkg.Cursor{},
	}
	ev.SetEngine(engine)

	err := ev.RunPhaseParallel(EvalPhase)
	if err != nil {
		t.Fatalf("RunPhaseParallel failed: %v", err)
	}

	stats := ParallelExecutionStats()
	if stats["enabled"] != true {
		t.Errorf("expected enabled=true, got %v", stats["enabled"])
	}

	totalOps, ok := stats["total_ops"].(int64)
	if !ok {
		t.Fatalf("expected total_ops to be int64, got %T", stats["total_ops"])
	}
	if totalOps < 1 {
		t.Errorf("expected total_ops >= 1, got %d", totalOps)
	}
}

