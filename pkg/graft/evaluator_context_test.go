package graft

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// stopper cancels the evaluation's context the first time it runs and
// counts every run. Its one optional argument is a literal path it
// depends on, which chains the operators so they run one after another.
type stopper struct {
	cancel context.CancelFunc
	runs   atomic.Int64
}

func (s *stopper) Setup() error         { return nil }
func (s *stopper) Phase() OperatorPhase { return EvalPhase }

func (s *stopper) Dependencies(_ *Evaluator, args []*Expr, _ []*tree.Cursor, auto []*tree.Cursor) []*tree.Cursor {
	if len(args) == 1 && args[0].Type == Literal {
		if c, err := tree.ParseCursor(fmt.Sprint(args[0].Literal)); err == nil {
			return append(auto, c)
		}
	}
	return auto
}

func (s *stopper) Run(_ *Evaluator, _ []*Expr) (*Response, error) {
	s.runs.Add(1)
	s.cancel()
	return &Response{Type: Replace, Value: "ran"}, nil
}

// failer always fails, with a message that holds no input value.
type failer struct{}

func (failer) Setup() error         { return nil }
func (failer) Phase() OperatorPhase { return EvalPhase }
func (failer) Dependencies(_ *Evaluator, _ []*Expr, _ []*tree.Cursor, auto []*tree.Cursor) []*tree.Cursor {
	return auto
}
func (failer) Run(_ *Evaluator, _ []*Expr) (*Response, error) {
	return nil, errors.New("failer always fails")
}

// settledGoroutines waits up to two seconds for the goroutine count to
// fall to want, because the worker pool's tasks wind down on their own
// schedule, and returns the count it last saw.
func settledGoroutines(want int) int {
	deadline := time.Now().Add(2 * time.Second)
	n := runtime.NumGoroutine()
	for n > want && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
		n = runtime.NumGoroutine()
	}
	return n
}

// quietGoroutines waits up to two seconds for the goroutine count to
// hold still across two readings and returns it.
func quietGoroutines() int {
	deadline := time.Now().Add(2 * time.Second)
	n := runtime.NumGoroutine()
	for time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		m := runtime.NumGoroutine()
		if m == n {
			break
		}
		n = m
	}
	return n
}

// warmEngine builds an engine with op registered as "stopper" and runs
// one clean evaluation on it, so the goroutines an engine keeps for its
// whole life, such as its worker pool, are already counted. It returns
// the engine and the goroutine count after that clean run.
func warmEngine(t *testing.T, op Operator, parallel bool) (Engine, int) {
	t.Helper()
	engine, err := NewEngine(WithCustomOperator("stopper", op), WithCustomOperator("failer", failer{}), WithParallel(parallel))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.Evaluate(context.Background(), mustParseYAMLDoc(t, engine, "a: 1\nb: (( grab a ))\n")); err != nil {
		t.Fatalf("clean evaluation failed: %v", err)
	}
	return engine, quietGoroutines()
}

func TestEvaluateStopsBetweenOperators(t *testing.T) {
	var src strings.Builder
	src.WriteString("k00: (( stopper ))\n")
	for i := 1; i < 20; i++ {
		fmt.Fprintf(&src, "k%02d: (( stopper \"k%02d\" ))\n", i, i-1)
	}
	for _, parallel := range []bool{true, false} {
		t.Run(fmt.Sprintf("parallel=%v", parallel), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			op := &stopper{cancel: cancel}
			engine, before := warmEngine(t, op, parallel)
			doc := mustParseYAMLDoc(t, engine, src.String())
			_, err := engine.Evaluate(ctx, doc)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("err = %v, want context.Canceled", err)
			}
			if n := op.runs.Load(); n != 1 {
				t.Errorf("stopper ran %d times, want 1", n)
			}
			if want := "evaluation stopped before $.k01: context canceled"; !strings.Contains(err.Error(), want) {
				t.Errorf("err = %q, want it to contain %q", err, want)
			}
			if after := settledGoroutines(before); after > before {
				t.Errorf("%d goroutines before, %d after", before, after)
			}
		})
	}
}

// TestEvaluateStopsAWideWave runs one wave of independent operators. The
// first to run cancels the context, so the operators still waiting must
// not start, the cancellation must come back on its own, and the pool
// must have drained when Evaluate returns.
func TestEvaluateStopsAWideWave(t *testing.T) {
	const width = 64
	var src strings.Builder
	for i := 0; i < width; i++ {
		fmt.Fprintf(&src, "k%02d: (( stopper ))\n", i)
	}
	src.WriteString("bad: (( failer ))\n")
	for _, parallel := range []bool{true, false} {
		t.Run(fmt.Sprintf("parallel=%v", parallel), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			op := &stopper{cancel: cancel}
			engine, before := warmEngine(t, op, parallel)
			_, err := engine.Evaluate(ctx, mustParseYAMLDoc(t, engine, src.String()))
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("err = %v, want context.Canceled", err)
			}
			var multi MultiError
			if errors.As(err, &multi) || strings.Contains(err.Error(), "failer") {
				t.Errorf("err = %q, want the cancellation alone", err)
			}
			if n := op.runs.Load(); n < 1 || n >= width {
				t.Errorf("stopper ran %d times, want at least 1 and fewer than %d", n, width)
			}
			runs := op.runs.Load()
			if after := settledGoroutines(before); after > before {
				t.Errorf("%d goroutines before, %d after", before, after)
			}
			if n := op.runs.Load(); n != runs {
				t.Errorf("stopper ran %d times after Evaluate returned, then %d", runs, n)
			}
		})
	}
}

func TestEvaluateStopsAtADeadline(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	op := &stopper{cancel: func() {}}
	engine, err := NewEngine(WithCustomOperator("stopper", op))
	if err != nil {
		t.Fatal(err)
	}
	_, err = engine.Evaluate(ctx, mustParseYAMLDoc(t, engine, "a: (( stopper ))\n"))
	if !errors.Is(err, context.DeadlineExceeded) || op.runs.Load() != 0 {
		t.Errorf("err = %v, runs = %d", err, op.runs.Load())
	}
}

func TestEvaluateRefusesACanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	op := &stopper{cancel: func() {}}
	engine, err := NewEngine(WithCustomOperator("stopper", op))
	if err != nil {
		t.Fatal(err)
	}
	_, err = engine.Evaluate(ctx, mustParseYAMLDoc(t, engine, "a: (( stopper ))\n"))
	if !errors.Is(err, context.Canceled) || op.runs.Load() != 0 {
		t.Errorf("err = %v, runs = %d", err, op.runs.Load())
	}
}

// TestEvaluatorRunStopsBetweenOperators drives the sequential evaluator
// directly, the way the merge builder's fallback does, with the context
// set on the evaluator itself.
func TestEvaluatorRunStopsBetweenOperators(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	op := &stopper{cancel: cancel}
	engine, err := NewEngine(WithCustomOperator("stopper", op), WithParallel(false))
	if err != nil {
		t.Fatal(err)
	}
	doc := mustParseYAMLDoc(t, engine, "k00: (( stopper ))\nk01: (( stopper \"k00\" ))\nk02: (( stopper \"k01\" ))\n")
	ev := engine.(*DefaultEngine).createEvaluator(treeOf(doc).(map[string]interface{}))
	ev.ctx = ctx
	err = ev.Run(nil, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if n := op.runs.Load(); n != 1 {
		t.Errorf("stopper ran %d times, want 1", n)
	}
}

func TestCanceledNamesTheStep(t *testing.T) {
	if err := canceled(context.Background(), "$.a"); err != nil {
		t.Fatalf("canceled on a live context = %v, want nil", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := canceled(ctx, "$.meta.x")
	if got, want := err.Error(), "evaluation stopped before $.meta.x: context canceled"; got != want {
		t.Errorf("err = %q, want %q", got, want)
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want it to wrap context.Canceled", err)
	}
	if got := (&Evaluator{}).context(); got != context.Background() {
		t.Errorf("context() with none set = %v, want context.Background", got)
	}
}
