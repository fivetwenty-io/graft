package graft

import (
	"context"
	"sync"
	"sync/atomic"

	"github.com/fivetwenty-io/graft/internal/parallel"
	"github.com/fivetwenty-io/graft/log"
	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// parallelStats tracks global statistics for parallel execution.
var parallelStats struct {
	totalOps     atomic.Int64
	totalPhases  atomic.Int64
	totalErrors  atomic.Int64
	schedulerRun atomic.Int64
}

// resetParallelStats resets all parallel execution statistics to zero.
func resetParallelStats() {
	parallelStats.totalOps.Store(0)
	parallelStats.totalPhases.Store(0)
	parallelStats.totalErrors.Store(0)
	parallelStats.schedulerRun.Store(0)
}

// RunPhaseParallel runs a phase using DAG-based parallel scheduling.
//
// It computes the dataflow dependency graph for the phase and builds a
// parallel.Scheduler with one task per operator. Tasks that share no
// dependencies are placed in the same wave by the scheduler and submitted
// to the engine's WorkerPool.
//
// Tree mutations (RunOp) are serialized with a mutex because the
// underlying map is not safe for concurrent writes. This still benefits
// from correct wave-based ordering and lays the groundwork for
// finer-grained parallelism in a future phase.
//
// If no WorkerPool is available on the engine, execution falls back to
// the sequential RunOps path.
func (ev *Evaluator) RunPhaseParallel(p OperatorPhase) error {
	err := SetupOperators(p)
	if err != nil {
		return err
	}

	ops, err := ev.DataFlow(p)
	if err != nil {
		return err
	}

	if len(ops) == 0 {
		return nil
	}

	// Retrieve the worker pool from the engine
	pool := getWorkerPool(ev)
	if pool == nil {
		log.DEBUG("parallel: no worker pool, falling back to sequential")
		return ev.RunOps(ops)
	}

	return ev.runOpsWithScheduler(context.Background(), pool, ops)
}

// RunOpsParallel executes a pre-sorted list of operations using the
// parallel scheduler. Falls back to sequential if no pool is available.
func (ev *Evaluator) RunOpsParallel(ops []*Opcall) error {
	if len(ops) == 0 {
		return nil
	}

	pool := getWorkerPool(ev)
	if pool == nil {
		log.DEBUG("parallel: no worker pool, falling back to sequential")
		return ev.RunOps(ops)
	}

	return ev.runOpsWithScheduler(context.Background(), pool, ops)
}

// runOpsWithScheduler builds a parallel.Scheduler from the given ops,
// computes inter-op dependencies, and executes via the pool.
func (ev *Evaluator) runOpsWithScheduler(ctx context.Context, pool *parallel.WorkerPool, ops []*Opcall) error {
	parallelStats.totalPhases.Add(1)

	scheduler := parallel.NewScheduler()

	// Collect all op locations for dependency computation
	allLocs := make([]*tree.Cursor, 0, len(ops))
	for _, op := range ops {
		if op.Where() != nil {
			allLocs = append(allLocs, op.Where())
		}
	}

	// Build a lookup from cursor-string to task ID for dependency mapping
	opIDMap := make(map[string]string, len(ops))
	for _, op := range ops {
		if op.Where() != nil {
			opIDMap[op.Where().String()] = op.Where().String()
			if op.canonical != nil {
				opIDMap[op.canonical.String()] = op.Where().String()
			}
		}
	}

	// Mutex to serialize RunOp calls (tree is not concurrency-safe)
	var mu sync.Mutex

	// Add tasks to the scheduler
	for _, op := range ops {
		taskOp := op // capture for closure
		taskID := taskOp.Where().String()

		// Compute dependency task IDs
		var depIDs []string
		if taskOp.op != nil {
			depCursors := taskOp.Dependencies(ev, allLocs)
			seen := make(map[string]bool)
			for _, depCursor := range depCursors {
				depKey := depCursor.String()
				if id, ok := opIDMap[depKey]; ok && id != taskID && !seen[id] {
					depIDs = append(depIDs, id)
					seen[id] = true
				}
			}
		}

		task := &parallel.Task{
			ID:           taskID,
			Dependencies: depIDs,
			Execute: func(_ context.Context) error {
				mu.Lock()
				defer mu.Unlock()
				return ev.RunOp(taskOp)
			},
		}

		if err := scheduler.AddTask(task); err != nil {
			// Duplicate task ID — skip (can happen with canonical/alias paths)
			log.DEBUG("parallel: skipping duplicate task %s: %v", taskID, err)
			continue
		}
	}

	parallelStats.schedulerRun.Add(1)

	// Execute all tasks in dependency order via the pool
	results, err := scheduler.Execute(ctx, pool)
	if err != nil {
		parallelStats.totalErrors.Add(1)
		return err
	}

	// Collect errors from task results
	errors := MultiError{Errors: []error{}}
	for _, result := range results {
		parallelStats.totalOps.Add(1)
		if result.Error != nil {
			parallelStats.totalErrors.Add(1)
			errors.Append(result.Error)
		}
	}

	if len(errors.Errors) > 0 {
		return errors
	}
	return nil
}

// getWorkerPool extracts the WorkerPool from the evaluator's engine.
func getWorkerPool(ev *Evaluator) *parallel.WorkerPool {
	eng := GetEngine(ev)
	if eng == nil {
		return nil
	}

	de, ok := eng.(*DefaultEngine)
	if !ok {
		return nil
	}

	return de.GetWorkerPool()
}

// ParallelExecutionStats returns statistics about parallel execution.
func ParallelExecutionStats() map[string]interface{} {
	total := parallelStats.totalOps.Load()
	return map[string]interface{}{
		"enabled":        total > 0,
		"total_ops":      total,
		"total_phases":   parallelStats.totalPhases.Load(),
		"total_errors":   parallelStats.totalErrors.Load(),
		"scheduler_runs": parallelStats.schedulerRun.Load(),
	}
}
