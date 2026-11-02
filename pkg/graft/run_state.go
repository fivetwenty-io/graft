package graft

import "context"

// runEngine is the Engine an evaluator sees during one run. Every
// method goes to the shared *DefaultEngine except GetOperatorState,
// which returns the run's own state, so operators that reach their
// state through GetEngine(ev).GetOperatorState() read and write only
// this run's markers while several runs share one engine.
type runEngine struct {
	*DefaultEngine
	state *operatorState
}

// GetOperatorState returns the run's own operator state.
func (r *runEngine) GetOperatorState() OperatorState {
	return r.state
}

// runStateKey is the context key a merge carries its run state under,
// so the engine's Evaluate continues the run the merge builder started
// instead of starting another.
type runStateKey struct{}

// withRunState returns a copy of ctx carrying state as the current run's
// operator state.
func withRunState(ctx context.Context, state *operatorState) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, runStateKey{}, state)
}

// runStateFrom returns the run state ctx carries, or nil when it carries
// none.
func runStateFrom(ctx context.Context) *operatorState {
	if ctx == nil {
		return nil
	}
	state, _ := ctx.Value(runStateKey{}).(*operatorState)
	return state
}

// forRun returns a fresh operator state for one run. It copies only the
// settings - the skip, redact, and warning flags - and starts every map
// and list empty, so the run neither sees nor disturbs another run's
// markers. operatorState holds mutexes, so this builds a new value
// rather than copying e.
func (e *operatorState) forRun() *operatorState {
	e.vaultMutex.RLock()
	defer e.vaultMutex.RUnlock()
	return &operatorState{
		vaultRefs:         make(map[string][]string),
		vaultPlaceholders: make(map[string]string),
		usedIPs:           make(map[string]string),
		pathsToSort:       make(map[string]string),
		skipVault:         e.skipVault,
		skipAws:           e.skipAws,
		skipNats:          e.skipNats,
		redactMode:        e.redactMode,
		suppressWarnings:  e.suppressWarnings,
	}
}

// absorb publishes a finished run's outputs to e, the engine's state.
// The run's vault references, vault placeholders, and deferred paths are
// merged into what e already holds, so callers that read them from the
// engine after a run see what a run on an unshared engine always left
// there. When publishPrunes is true, the run's pruned paths replace e's
// as the most recently finished run's; a run that stopped before its
// prune step passes false and leaves the previous run's paths in place.
func (e *operatorState) absorb(run *operatorState, publishPrunes bool) {
	refs := run.GetVaultRefs()
	run.vaultMutex.RLock()
	placeholders := make(map[string]string, len(run.vaultPlaceholders))
	for path, key := range run.vaultPlaceholders {
		placeholders[path] = key
	}
	run.vaultMutex.RUnlock()

	e.vaultMutex.Lock()
	if e.vaultRefs == nil {
		e.vaultRefs = make(map[string][]string)
	}
	for path, keys := range refs {
		e.vaultRefs[path] = append(e.vaultRefs[path], keys...)
	}
	if e.vaultPlaceholders == nil {
		e.vaultPlaceholders = make(map[string]string)
	}
	for path, key := range placeholders {
		e.vaultPlaceholders[path] = key
	}
	e.vaultMutex.Unlock()

	if deferred := run.GetDeferredPaths(); len(deferred) > 0 {
		e.deferMutex.Lock()
		e.deferredPaths = append(e.deferredPaths, deferred...)
		e.deferMutex.Unlock()
	}

	if publishPrunes {
		run.pruneMutex.RLock()
		pruned := run.lastEvaluatedPrunedPaths
		run.pruneMutex.RUnlock()

		e.pruneMutex.Lock()
		e.lastEvaluatedPrunedPaths = pruned
		e.pruneMutex.Unlock()
	}
}

// opState returns the operator state this merge's post-processing reads
// and writes: the run state applyPostProcessing put in m.ctx, or the
// engine's own state for an engine that is not a *DefaultEngine.
func (m *mergeBuilderImpl) opState() OperatorState {
	if state := runStateFrom(m.ctx); state != nil {
		return state
	}
	return m.engine.GetOperatorState()
}
