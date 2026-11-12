package graft

import (
	"runtime"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// pruneFallback on an evaluator with no engine marks nothing and builds
// no engine, since a default engine built only to record the prune
// would leave its cache's cleanup goroutine running.
func TestPruneFallbackWithNoEngineBuildsNone(t *testing.T) {
	here, err := tree.ParseCursor("a")
	if err != nil {
		t.Fatal(err)
	}
	ev := &Evaluator{Tree: map[string]interface{}{"a": 1}, Here: here}
	prune := &Expr{Type: OperatorCall, Operator: "prune"}

	before := runtime.NumGoroutine()
	for range 20 {
		if !pruneFallback(prune, ev) {
			t.Fatal("pruneFallback = false, want true for (( prune ))")
		}
	}
	if after := runtime.NumGoroutine(); after >= before+20 {
		t.Fatalf("goroutines went from %d to %d", before, after)
	}
	if ev.engine != nil {
		t.Fatal("pruneFallback bound an engine to the evaluator")
	}
}
