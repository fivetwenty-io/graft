package graft

import (
	"context"
	"sort"
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
// dependencies are placed in the same wave by the scheduler and dispatched
// to the engine's WorkerPool, one at a time in a deterministic (task-ID
// sorted) order per wave - see runOpsWithScheduler.
//
// Tree mutations (RunOp) are serialized with a mutex because the
// underlying map is not safe for concurrent writes; RunOp calls (including
// any Vault/AWS/NATS I/O they perform) never actually overlap today. This
// still benefits from correct wave-based dependency ordering and the
// pool/scheduler infrastructure, and lays the groundwork for finer-grained
// parallelism in a future phase.
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

// resolveOpIDForDependency maps a dependency cursor to the task ID of the
// operator that produces it, mirroring dataFlowContext.findDependency /
// findParentDependency (evaluator.go) exactly: try the path as-is, then its
// canonical form, then walk parent prefixes (also trying each parent's
// canonical form) until a producing operator is found. This parent-path
// fallback is required because a dependency can reference a subtree of an
// operator's result (e.g. `grab meta.vm.small` where `meta.vm` is itself
// `(( grab ... ))`) - the operator's own path (`meta.vm`) never equals the
// dependency path (`meta.vm.small`), so an exact-match-only lookup misses
// the edge. Sequential and parallel evaluation must derive identical
// dependency graphs; this keeps the two lookups in lockstep.
func resolveOpIDForDependency(ev *Evaluator, opIDMap map[string]string, path *tree.Cursor) (string, bool) {
	if id, found := opIDMap[path.String()]; found {
		return id, true
	}

	if canon, err := path.Canonical(ev.Tree); err == nil {
		if id, found := opIDMap[canon.String()]; found {
			return id, true
		}
	}

	parent := path.Copy()
	for len(parent.Nodes) > 0 {
		parent.Pop()
		if len(parent.Nodes) == 0 {
			break
		}

		if id, found := opIDMap[parent.String()]; found {
			return id, true
		}

		if canon, err := parent.Canonical(ev.Tree); err == nil {
			if id, found := opIDMap[canon.String()]; found {
				return id, true
			}
		}
	}

	return "", false
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
				if id, ok := resolveOpIDForDependency(ev, opIDMap, depCursor); ok && id != taskID && !seen[id] {
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

	// Compute dependency-ordered waves. Tasks within a wave have no
	// dependency relationship to each other per the DAG, but independent
	// operators can still contend for the same shared engine state (e.g.
	// two unrelated static_ips claims racing for the same address - see
	// op_static_ips.go's claimStaticIPMu). scheduler.Schedule builds each
	// wave from Go map iteration, which is randomized per run, so the wave
	// slice's element order is not itself deterministic.
	waves, err := scheduler.Schedule()
	if err != nil {
		parallelStats.totalErrors.Add(1)
		return err
	}

	errors := MultiError{Errors: []error{}}
	for _, wave := range waves {
		// Re-sort each wave by task ID (the operator's cursor path, e.g.
		// "jobs.static_z1...") before dispatch, independent of whatever
		// order scheduler.Schedule produced it in. Combined with
		// pool.SubmitWaitContext below (one task in flight at a time,
		// waiting for completion before submitting the next), this makes
		// same-wave execution order reproducible across runs instead of
		// depending on map iteration or goroutine-scheduling timing, without
		// touching the RunOp serialization mutex above. RunOp calls were
		// already fully serialized by that mutex (including any Vault/AWS
		// I/O inside them, not just the tree write), so dispatching one at a
		// time here removes no actual concurrency that previously existed.
		sort.Slice(wave, func(i, j int) bool { return wave[i].ID < wave[j].ID })

		for _, task := range wave {
			parallelStats.totalOps.Add(1)
			if taskErr := pool.SubmitWaitContext(ctx, task.Execute); taskErr != nil {
				parallelStats.totalErrors.Add(1)
				errors.Append(taskErr)
			}
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
