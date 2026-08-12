package graft

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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

// sleepingTestOperator sleeps for a fixed duration before returning a
// static value. Used to prove computeOp for independent same-wave
// operators runs truly concurrently rather than one at a time:
// N independent instances should complete in a small multiple of one
// sleep, not N sleeps.
type sleepingTestOperator struct {
	sleep time.Duration
	value interface{}
}

func (sleepingTestOperator) Setup() error { return nil }

func (o sleepingTestOperator) Run(_ *Evaluator, _ []*Expr) (*Response, error) {
	time.Sleep(o.sleep)
	return &Response{Type: Replace, Value: o.value}, nil
}

func (sleepingTestOperator) Dependencies(_ *Evaluator, _ []*Expr, _ []*treepkg.Cursor, auto []*treepkg.Cursor) []*treepkg.Cursor {
	return auto
}

func (sleepingTestOperator) Phase() OperatorPhase { return EvalPhase }

// TestParallelTrueConcurrencySpeedup proves that independent (no
// dependency edge) operators within a wave actually execute their
// computeOp phase concurrently, not one at a time behind the pool's
// SubmitWaitContext. Four independent 150ms operators
// must finish well under the 600ms a fully-serial dispatch would take.
func TestParallelTrueConcurrencySpeedup(t *testing.T) {
	ansi.Color(false)
	SilenceWarnings(true)

	const sleep = 150 * time.Millisecond
	const serialBound = 4 * sleep

	engine := newParallelEngine(t)
	RegisterOp("sleepingtest", sleepingTestOperator{sleep: sleep, value: "done"})

	yaml := `
a: (( sleepingtest ))
b: (( sleepingtest ))
c: (( sleepingtest ))
d: (( sleepingtest ))
`
	tree := parseYAMLForTest(t, yaml)
	ev := &Evaluator{
		Tree: tree,
		Deps: map[string][]treepkg.Cursor{},
	}
	ev.SetEngine(engine)

	start := time.Now()
	err := ev.RunPhaseParallel(EvalPhase)
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("RunPhaseParallel failed: %v", err)
	}

	for _, key := range []string{"a", "b", "c", "d"} {
		if ev.Tree[key] != "done" {
			t.Errorf("%s: expected %q, got %v", key, "done", ev.Tree[key])
		}
	}

	// newParallelEngine's pool starts with 2 min workers, so 4 concurrent
	// 150ms tasks land in two batches of two (~300ms), well under the
	// fully-serial bound of 4x150ms=600ms. Assert comfortably between the
	// two so scheduling jitter cannot produce a false pass.
	if elapsed >= serialBound-100*time.Millisecond {
		t.Errorf("expected concurrent dispatch well under the %v serial bound, took %v (looks serialized)", serialBound, elapsed)
	}
}

// TestParallelWaveWiderThanPoolQueueDoesNotFail is a regression test: a
// dependency-free wave larger than the
// worker pool's task queue capacity (DefaultPoolConfig().QueueSize ==
// 1000) must still succeed. Before the fix, the concurrent group's fan-out
// used pool.SubmitContext, whose non-blocking select returns ErrPoolFull
// the instant the queue saturates; runOpsWithScheduler recorded that
// rejection as the operator's own evaluation error, so the merge failed
// outright once a wave exceeded 1000 independent operators - ordinary for
// large BOSH manifests with many independent grab/vault/concat calls, not
// a synthetic limit.
//
// Uses a small synthetic per-call delay (sleepingTestOperator) rather than
// the near-instant real `grab` operator: with instant work, the pool's two
// workers can drain the queue faster than a same-process test loop enqueues
// it, so the queue capacity is never actually exercised and the test would
// pass even on the buggy code (confirmed: an earlier grab-based version of
// this test did not reproduce the bug). A short sleep makes the producer
// (submission loop, effectively instantaneous) reliably outpace the
// consumers (2 workers x sleep-bound throughput), guaranteeing the queue
// saturates regardless of machine speed - a real CLI repro of this bug
// needed a real subprocess to observe the failure reliably, for the same
// reason. 1200 is a confirmed-failing count (n=1200 failed, n=1000 passed
// against DefaultPoolConfig's QueueSize=1000).
func TestParallelWaveWiderThanPoolQueueDoesNotFail(t *testing.T) {
	ansi.Color(false)
	SilenceWarnings(true)

	const opCount = 1200 // > DefaultPoolConfig().QueueSize (1000)
	const sleep = 2 * time.Millisecond

	engine := newParallelEngine(t)
	RegisterOp("widewavetest", sleepingTestOperator{sleep: sleep, value: "done"})

	var b strings.Builder
	for i := 0; i < opCount; i++ {
		fmt.Fprintf(&b, "k%04d: (( widewavetest ))\n", i)
	}

	tree := parseYAMLForTest(t, b.String())
	ev := &Evaluator{
		Tree: tree,
		Deps: map[string][]treepkg.Cursor{},
	}
	ev.SetEngine(engine)

	err := ev.RunPhaseParallel(EvalPhase)
	if err != nil {
		t.Fatalf("RunPhaseParallel failed for a %d-operator wave (pool queue capacity 1000): %v", opCount, err)
	}

	for i := 0; i < opCount; i++ {
		key := fmt.Sprintf("k%04d", i)
		if ev.Tree[key] != "done" {
			t.Errorf("%s: expected %q, got %v", key, "done", ev.Tree[key])
		}
	}
}

// orderSensitiveProbeOperator implements OrderSensitive and records
// whether more than one instance was ever running concurrently, proving
// the scheduler dispatches OrderSensitive operators one at a time within
// a wave even though they carry no DataFlow dependency edge between them.
type orderSensitiveProbeOperator struct {
	inFlight        *int32
	overlapObserved *int32
	sleep           time.Duration
}

func (orderSensitiveProbeOperator) Setup() error { return nil }

func (o orderSensitiveProbeOperator) Run(_ *Evaluator, _ []*Expr) (*Response, error) {
	if atomic.AddInt32(o.inFlight, 1) > 1 {
		atomic.StoreInt32(o.overlapObserved, 1)
	}
	time.Sleep(o.sleep)
	atomic.AddInt32(o.inFlight, -1)
	return &Response{Type: Replace, Value: "claimed"}, nil
}

func (orderSensitiveProbeOperator) Dependencies(_ *Evaluator, _ []*Expr, _ []*treepkg.Cursor, auto []*treepkg.Cursor) []*treepkg.Cursor {
	return auto
}

func (orderSensitiveProbeOperator) Phase() OperatorPhase { return EvalPhase }

// OrderSensitive marks this operator so the scheduler serializes it
// against other same-wave instances - see interfaces.go's OrderSensitive
// and evaluator_parallel.go's isOrderSensitiveOp/partitioning.
func (orderSensitiveProbeOperator) OrderSensitive() bool { return true }

// TestParallelOrderSensitiveOperatorRunsSequentially proves the
// OrderSensitive partitioning (see op_static_ips.go's
// real use of it) actually prevents concurrent dispatch of same-wave
// OrderSensitive operators, which have no DataFlow dependency edge
// between them and would otherwise land in the true-concurrency group.
func TestParallelOrderSensitiveOperatorRunsSequentially(t *testing.T) {
	ansi.Color(false)
	SilenceWarnings(true)

	var inFlight, overlapObserved int32

	engine := newParallelEngine(t)
	op := orderSensitiveProbeOperator{
		inFlight:        &inFlight,
		overlapObserved: &overlapObserved,
		sleep:           20 * time.Millisecond,
	}
	RegisterOp("ordersensitivetest", op)

	yaml := `
a: (( ordersensitivetest ))
b: (( ordersensitivetest ))
c: (( ordersensitivetest ))
d: (( ordersensitivetest ))
`
	tree := parseYAMLForTest(t, yaml)
	ev := &Evaluator{
		Tree: tree,
		Deps: map[string][]treepkg.Cursor{},
	}
	ev.SetEngine(engine)

	if err := ev.RunPhaseParallel(EvalPhase); err != nil {
		t.Fatalf("RunPhaseParallel failed: %v", err)
	}

	for _, key := range []string{"a", "b", "c", "d"} {
		if ev.Tree[key] != "claimed" {
			t.Errorf("%s: expected %q, got %v", key, "claimed", ev.Tree[key])
		}
	}

	if atomic.LoadInt32(&overlapObserved) != 0 {
		t.Error("OrderSensitive operators overlapped in time; expected strictly sequential dispatch within the wave")
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
	err := engine.evaluate(ctx, ev, false)
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
