package graft

import (
	"sync/atomic"
	"testing"

	"github.com/fivetwenty-io/graft/internal/parallel"
)

// TestAddSchedulerTasksRejectsConcurrentEntry locks in the invariant
// addSchedulerTasks' doc comment promises: dependency computation runs
// single-threaded, before any wave dispatch starts goroutines, because
// it mutates the shared ev.Here cursor. A second computation entering
// while one is in flight would silently corrupt relative-path
// dependency edges; the guard turns that silent corruption into a
// panic at the point of the bug.
func TestAddSchedulerTasksRejectsConcurrentEntry(t *testing.T) {
	ev := &Evaluator{Tree: map[string]interface{}{}}

	// Simulate another goroutine mid-computation.
	atomic.AddInt32(&ev.depComputing, 1)

	defer func() {
		if recover() == nil {
			t.Fatal("expected addSchedulerTasks to panic on concurrent dependency computation")
		}
	}()
	ev.addSchedulerTasks(parallel.NewScheduler(), nil, nil, nil)
}

// TestAddSchedulerTasksGuardResets proves the guard releases on normal
// return: two sequential computations on one evaluator (one per
// evaluation phase) must both be admitted.
func TestAddSchedulerTasksGuardResets(t *testing.T) {
	ev := &Evaluator{Tree: map[string]interface{}{}}

	ev.addSchedulerTasks(parallel.NewScheduler(), nil, nil, nil)
	ev.addSchedulerTasks(parallel.NewScheduler(), nil, nil, nil)

	if got := atomic.LoadInt32(&ev.depComputing); got != 0 {
		t.Fatalf("depComputing = %d after sequential runs, want 0", got)
	}
}
