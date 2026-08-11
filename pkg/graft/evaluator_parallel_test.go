package graft

import (
	"context"
	"strings"
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

// paramShortCircuitFixture is a document with one unresolved (( param ))
// (ParamPhase) and one broken (( grab )) of a nonexistent path (EvalPhase).
// Matches spruce's evaluator.go Run(): a ParamPhase error must abort the
// run before EvalPhase executes, so only the param error is ever reported.
const paramShortCircuitFixture = `
meta:
  name: (( param "meta.name is required" ))
broken:
  value: (( grab does.not.exist ))
`

// TestPhaseGating_ParamShortCircuit_Sequential verifies that the default
// (non-parallel) engine aborts evaluation at ParamPhase and never runs
// EvalPhase, so a broken (( grab )) elsewhere in the document never
// surfaces its own error alongside the param error.
func TestPhaseGating_ParamShortCircuit_Sequential(t *testing.T) {
	ansi.Color(false)
	SilenceWarnings(true)

	engine, err := CreateDefaultEngine()
	if err != nil {
		t.Fatalf("CreateDefaultEngine failed: %v", err)
	}

	doc, err := engine.ParseYAML([]byte(paramShortCircuitFixture))
	if err != nil {
		t.Fatalf("ParseYAML failed: %v", err)
	}

	_, err = engine.Evaluate(context.Background(), doc)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}

	msg := err.Error()
	if !strings.Contains(msg, "meta.name is required") {
		t.Errorf("expected param error in output, got: %s", msg)
	}
	if strings.Contains(msg, "does.not.exist") {
		t.Errorf("EvalPhase (( grab )) error leaked through ParamPhase short-circuit: %s", msg)
	}
}

// TestPhaseGating_ParamShortCircuit_Parallel verifies the parallel
// evaluator path (RunPhaseParallel, driven by the same DefaultEngine.evaluate
// phase loop) applies identical phase gating: a ParamPhase error still
// aborts before EvalPhase runs, even with a worker pool attached.
func TestPhaseGating_ParamShortCircuit_Parallel(t *testing.T) {
	ansi.Color(false)
	SilenceWarnings(true)

	engine := newParallelEngine(t)

	doc, err := engine.ParseYAML([]byte(paramShortCircuitFixture))
	if err != nil {
		t.Fatalf("ParseYAML failed: %v", err)
	}

	_, err = engine.Evaluate(context.Background(), doc)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}

	msg := err.Error()
	if !strings.Contains(msg, "meta.name is required") {
		t.Errorf("expected param error in output, got: %s", msg)
	}
	if strings.Contains(msg, "does.not.exist") {
		t.Errorf("EvalPhase (( grab )) error leaked through ParamPhase short-circuit (parallel path): %s", msg)
	}
}

// mergeEvalAccumulateFixture has one broken (( inject )) (MergePhase error,
// no ParamPhase op present) and one broken (( grab )) (EvalPhase error).
// Matches spruce's evaluator.go Run(): MergePhase errors are appended to a
// running MultiError, not returned immediately, so ParamPhase and EvalPhase
// still run and their errors are combined into a single report.
const mergeEvalAccumulateFixture = `
meta:
  broken_inject: (( inject nonexistent.path.here ))
result:
  grabbed: (( grab does.not.exist ))
`

// mergeParamEvalFixture adds an unresolved (( param )) on top of
// mergeEvalAccumulateFixture. Matches spruce: a ParamPhase error still
// aborts before EvalPhase runs and is returned alone, dropping any
// MergePhase errors accumulated earlier in the same run.
const mergeParamEvalFixture = `
meta:
  broken_inject: (( inject nonexistent.path.here ))
  name: (( param "meta.name is required" ))
result:
  grabbed: (( grab does.not.exist ))
`

// TestPhaseGating_MergeErrorsAccumulate_Sequential verifies the default
// (non-parallel) engine continues past a MergePhase error into ParamPhase
// and EvalPhase, combining the MergePhase and EvalPhase errors into one
// report - matching spruce's Run(), which does not short-circuit on
// MergePhase errors.
func TestPhaseGating_MergeErrorsAccumulate_Sequential(t *testing.T) {
	ansi.Color(false)
	SilenceWarnings(true)

	engine, err := CreateDefaultEngine()
	if err != nil {
		t.Fatalf("CreateDefaultEngine failed: %v", err)
	}

	doc, err := engine.ParseYAML([]byte(mergeEvalAccumulateFixture))
	if err != nil {
		t.Fatalf("ParseYAML failed: %v", err)
	}

	_, err = engine.Evaluate(context.Background(), doc)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}

	msg := err.Error()
	if !strings.Contains(msg, "nonexistent") {
		t.Errorf("expected MergePhase (( inject )) error in combined output, got: %s", msg)
	}
	if !strings.Contains(msg, "does.not.exist") {
		t.Errorf("expected EvalPhase (( grab )) error in combined output (MergePhase error must not abort EvalPhase), got: %s", msg)
	}
}

// TestPhaseGating_MergeErrorsAccumulate_Parallel verifies the parallel
// evaluator path applies identical phase gating: a MergePhase error does
// not abort ParamPhase/EvalPhase, even with a worker pool attached.
func TestPhaseGating_MergeErrorsAccumulate_Parallel(t *testing.T) {
	ansi.Color(false)
	SilenceWarnings(true)

	engine := newParallelEngine(t)

	doc, err := engine.ParseYAML([]byte(mergeEvalAccumulateFixture))
	if err != nil {
		t.Fatalf("ParseYAML failed: %v", err)
	}

	_, err = engine.Evaluate(context.Background(), doc)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}

	msg := err.Error()
	if !strings.Contains(msg, "nonexistent") {
		t.Errorf("expected MergePhase (( inject )) error in combined output, got: %s", msg)
	}
	if !strings.Contains(msg, "does.not.exist") {
		t.Errorf("expected EvalPhase (( grab )) error in combined output (MergePhase error must not abort EvalPhase, parallel path), got: %s", msg)
	}
}

// TestPhaseGating_ParamErrorDropsMergeErrors_Sequential verifies that when
// both a MergePhase error and a ParamPhase error occur in the same run, the
// ParamPhase error still aborts before EvalPhase runs and is returned
// alone - matching spruce's Run(), which returns paramErrs directly without
// combining the MergePhase errors accumulated earlier in the same run.
func TestPhaseGating_ParamErrorDropsMergeErrors_Sequential(t *testing.T) {
	ansi.Color(false)
	SilenceWarnings(true)

	engine, err := CreateDefaultEngine()
	if err != nil {
		t.Fatalf("CreateDefaultEngine failed: %v", err)
	}

	doc, err := engine.ParseYAML([]byte(mergeParamEvalFixture))
	if err != nil {
		t.Fatalf("ParseYAML failed: %v", err)
	}

	_, err = engine.Evaluate(context.Background(), doc)
	if err == nil {
		t.Fatal("expected an error, got nil")
	}

	msg := err.Error()
	if !strings.Contains(msg, "meta.name is required") {
		t.Errorf("expected param error in output, got: %s", msg)
	}
	if strings.Contains(msg, "nonexistent") {
		t.Errorf("MergePhase (( inject )) error leaked through ParamPhase short-circuit: %s", msg)
	}
	if strings.Contains(msg, "does.not.exist") {
		t.Errorf("EvalPhase (( grab )) error leaked through ParamPhase short-circuit: %s", msg)
	}
}

