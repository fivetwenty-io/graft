package graft

import (
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/parallel"
	"github.com/fivetwenty-io/graft/internal/utils/ansi"
	treepkg "github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// TestParallelRunPhaseManyWorkersStress runs an operator-dense document
// through RunPhaseParallel with a NumCPU-sized worker pool. The other
// parallel tests use a 2-worker pool; this one exists so the -race CI
// run exercises the concurrent computeOp/applyResponse paths with real
// multi-worker contention - shared-state bugs like an unguarded ev.Here
// or a racy package global need genuine overlap to surface. The output
// assertion doubles as a determinism check: every value must resolve
// identically no matter which worker got there first.
func TestParallelRunPhaseManyWorkersStress(t *testing.T) {
	ansi.Color(false)
	SilenceWarnings(true)

	const groups = 64

	var b strings.Builder
	b.WriteString("meta:\n")
	for i := 0; i < groups; i++ {
		fmt.Fprintf(&b, "  key%02d: value%02d\n", i, i)
	}
	b.WriteString("out:\n")
	for i := 0; i < groups; i++ {
		fmt.Fprintf(&b, "  grab%02d: (( grab meta.key%02d ))\n", i, i)
		fmt.Fprintf(&b, "  concat%02d: (( concat \"x-\" meta.key%02d ))\n", i, i)
		fmt.Fprintf(&b, "  chain%02d: (( grab out.grab%02d ))\n", i, i)
	}

	engine := newParallelEngine(t)
	workers := runtime.NumCPU()
	if workers < 4 {
		workers = 4
	}
	pool, err := parallel.NewPool(workers, workers)
	if err != nil {
		t.Fatalf("failed to create worker pool: %v", err)
	}
	engine.Pool = pool
	t.Cleanup(func() { pool.ShutdownWait() })

	tree := parseYAMLForTest(t, b.String())
	ev := &Evaluator{
		Tree: tree,
		Deps: map[string][]treepkg.Cursor{},
	}
	ev.SetEngine(engine)

	if err := ev.RunPhaseParallel(EvalPhase); err != nil {
		t.Fatalf("RunPhaseParallel failed: %v", err)
	}

	out, ok := ev.Tree["out"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected out to be a map, got %T", ev.Tree["out"])
	}
	for i := 0; i < groups; i++ {
		want := fmt.Sprintf("value%02d", i)
		if got := out[fmt.Sprintf("grab%02d", i)]; got != want {
			t.Errorf("grab%02d = %v, want %s", i, got, want)
		}
		if got := out[fmt.Sprintf("concat%02d", i)]; got != "x-"+want {
			t.Errorf("concat%02d = %v, want x-%s", i, got, want)
		}
		if got := out[fmt.Sprintf("chain%02d", i)]; got != want {
			t.Errorf("chain%02d = %v, want %s", i, got, want)
		}
	}
}
