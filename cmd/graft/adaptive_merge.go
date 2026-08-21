package main

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// This file implements `graft merge --defer-on-error`/`--adaptive`'s core
// retry loop (plans/dennis-feedback-gaps.md Item 2): on an operator
// evaluation failure, wrap each newly-failing path's expression in
// "(( defer ... ))" (applyDeferredWrapping, debug_repl.go - the same
// logic `graft debug`'s cmdDefer uses) and re-merge, repeating until the
// merge succeeds (cleanly, or partially with every deferred path's own
// expression intact) or no further progress is possible.

// adaptiveMergeMaxRounds is a safety cap on the loop. Each round defers
// at least one newly-discovered failing path or the loop stops (fixed
// point), so a real merge provably converges in at most the number of
// distinct paths that ever fail - this cap only guards against an
// unforeseen bug turning that into an infinite loop, not a limit
// expected to matter in practice.
const adaptiveMergeMaxRounds = 1000

// adaptiveMergeOptions carries the merge-builder options runAdaptiveMerge
// applies on every round, mirroring mergeAllDocs' own wiring of the same
// mergeOpts fields (FallbackAppend/CherryPick/Prune). SkipEval is
// deliberately not here: --defer-on-error with --skip-eval would have
// nothing to ever fail in the first place, since no operator runs.
type adaptiveMergeOptions struct {
	FallbackAppend bool
	CherryPick     []string
	Prune          []string
}

// adaptiveMergeResult is runAdaptiveMerge's outcome on a merge that
// eventually succeeded, cleanly or partially.
type adaptiveMergeResult struct {
	// Tree is the final, merged-and-evaluated document: every deferred
	// path still carries its own "(( ... ))" expression text.
	Tree map[string]interface{}
	// Deferred lists every path deferred during this call, sorted by
	// path - both this call's own defer-on-error deferrals and any
	// --skip-vault/--skip-aws/--skip-nats deferrals that happened along
	// the way (both are recorded on engine via AddDeferredPath - see
	// op_skip_defer.go). Empty means a clean merge: no path deferred.
	Deferred []graft.DeferredPath
}

// runAdaptiveMerge runs engine.Merge(ctx, docs...) (with opts applied),
// and on an operator-evaluation failure, wraps each newly-failing path's
// expression in "(( defer ... ))" and re-merges the partial result as a
// single document, repeating until the merge succeeds or the deferred
// set stops growing (a fixed point with errors still remaining: a hard
// failure). Factored out as a standalone function - taking an engine and
// already-parsed documents, not CLI flags or *mergeOpts - specifically
// so `graft debug`'s planned `autodefer` command can drive the same loop
// from a REPL session's current tree, not just handleMerge.
//
// Each newly-deferred path is recorded via
// engine.GetOperatorState().AddDeferredPath, attributed to its
// first-round (root-cause) error text - a path already deferred is
// excluded from consideration in every later round (see
// newlyFailingPathErrors), so a cascade of dependent failures around one
// root cause is never re-attributed to whatever a later round's
// re-evaluation happens to say instead. See
// plans/dennis-feedback-gaps.md Item 2's cascade-attribution
// requirement.
//
// A construction-time failure (bad input, a cyclic document structure,
// etc. - anything Execute() returns before any operator ever runs, so it
// is not a *graft.PartialEvaluationError) surfaces as a plain error, tree
// unavailable. A genuine operator-dependency cycle (the data-flow
// graph's topological sort finding no free node) also surfaces this way
// in practice: it is not a *graft.PathError, so nothing new gets
// deferred, so the loop treats it as no-progress and stops with the
// cycle error intact - matching Item 2's "a true cycle is a hard
// failure: stop, report the original cycle error" requirement.
func runAdaptiveMerge(ctx context.Context, engine graft.Engine, docs []graft.Document, opts adaptiveMergeOptions) (*adaptiveMergeResult, error) {
	knownFailing := map[string]bool{}
	currentDocs := docs

	for round := 0; round < adaptiveMergeMaxRounds; round++ {
		builder := engine.Merge(ctx, currentDocs...)
		if opts.FallbackAppend {
			builder = builder.WithArrayMergeStrategy(graft.AppendArrays)
		}
		if len(opts.CherryPick) > 0 {
			builder = builder.WithCherryPick(opts.CherryPick...)
		}
		if len(opts.Prune) > 0 {
			builder = builder.WithPrune(opts.Prune...)
		}

		merged, err := builder.Execute()
		if err == nil {
			data, ok := merged.GetData().(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("adaptive merge result is not a map")
			}
			deferred := engine.GetOperatorState().GetDeferredPaths()
			sort.Slice(deferred, func(i, j int) bool { return deferred[i].Path < deferred[j].Path })
			return &adaptiveMergeResult{Tree: data, Deferred: deferred}, nil
		}

		var partial *graft.PartialEvaluationError
		if !errors.As(err, &partial) {
			// No partial tree available (a construction-time failure, or
			// something that failed before any operator ran) - nothing
			// to defer, so this is a hard failure as-is.
			return nil, err
		}

		newPaths := newlyFailingPathErrors(partial.Err, knownFailing)
		if len(newPaths) == 0 {
			// Fixed point: nothing new to attribute this round - either
			// the failure recurs on an already-deferred path (should not
			// happen once wrapped, but handled the same either way), or
			// it isn't a *PathError at all (e.g. a data-flow cycle). No
			// further progress is possible.
			return nil, err
		}

		for _, pe := range newPaths {
			knownFailing[pe.Path] = true
			// Cause.Error() only, not pe.Error(): DeferredPath.Reason is
			// the bare message, with no "$.path:" prefix - the
			// --report-deferred renderer (deferred_report.go) adds that
			// prefix itself for beginning/end placement, and omits it
			// for inline (the comment's position already conveys the
			// path).
			engine.GetOperatorState().AddDeferredPath(pe.Path, pe.Cause.Error())
		}

		partialTree, ok := partial.Tree.RawData().(map[string]interface{})
		if !ok {
			return nil, err
		}
		currentDocs = []graft.Document{graft.NewDocument(deferAllUnevaluatedOperators(partialTree))}
	}

	return nil, fmt.Errorf("adaptive merge did not converge after %d rounds", adaptiveMergeMaxRounds)
}

// deferAllUnevaluatedOperators returns a deep copy of tree with every
// leaf string that still looks like an unevaluated "(( ... ))" operator
// call - wherever it is in the tree, not just at an explicitly-failing
// path - rewritten to "(( defer ... ))" (deferWrapIfOperator,
// debug_repl.go; already-deferred or non-operator leaves are left
// alone).
//
// This must be broader than applyDeferredWrapping's path-keyed rewrite
// (which only touches the exact paths a caller names - the right
// behavior for graft debug's cmdDefer, where a human chooses which
// paths to defer): a (( grab ))/(( concat ))/etc. elsewhere in the tree
// that copied a failed operator's still-raw expression text succeeds
// (see runAdaptiveMerge's doc comment), so by the time a round fails,
// that copy is itself just another leaf holding "(( ... ))"-shaped text
// - indistinguishable, once evaluation has run once, from a genuinely
// unevaluated operator, since the evaluator's own operator detection is
// purely textual (any string matching the pattern is a candidate call,
// regardless of how it got there). If a copy like this were not wrapped
// too, re-merging the partial tree would evaluate it as a brand-new,
// independent operator call next round and - for a copy of a
// vault/awsparam/awssecret/nats call specifically - fail it a second
// time under its own path, misattributing a downstream copy as its own
// root-cause failure instead of the value it actually is.
//
// Only genuinely-failing paths (see newlyFailingPathErrors) are ever
// reported in adaptiveMergeResult.Deferred; this function's broader
// wrapping is purely a re-merge safety measure, so not everything it
// wraps has a matching Deferred entry.
func deferAllUnevaluatedOperators(tree map[string]interface{}) map[string]interface{} {
	out := deepCopyTree(tree)
	deferAllUnevaluatedOperatorsIn(out)
	return out
}

// deferAllUnevaluatedOperatorsIn walks v (a map or slice, recursively)
// in place, rewriting every qualifying string leaf via
// deferWrapIfOperator.
func deferAllUnevaluatedOperatorsIn(v interface{}) {
	switch val := v.(type) {
	case map[string]interface{}:
		for k, sub := range val {
			if s, ok := sub.(string); ok {
				if wrapped, changed := deferWrapIfOperator(s); changed {
					val[k] = wrapped
					continue
				}
			}
			deferAllUnevaluatedOperatorsIn(sub)
		}
	case []interface{}:
		for i, sub := range val {
			if s, ok := sub.(string); ok {
				if wrapped, changed := deferWrapIfOperator(s); changed {
					val[i] = wrapped
					continue
				}
			}
			deferAllUnevaluatedOperatorsIn(sub)
		}
	}
}

// newlyFailingPathErrors extracts every *graft.PathError from err (a
// graft.MultiError, or a bare *PathError for the ParamPhase-abort case)
// whose Path is not already in known, so the same failing path is never
// re-attributed to a later round's error text (see runAdaptiveMerge's
// "first-round/root error" attribution requirement).
func newlyFailingPathErrors(err error, known map[string]bool) []*graft.PathError {
	var found []*graft.PathError
	seen := map[string]bool{} // dedup within this one round's error set

	consider := func(candidate error) {
		var pe *graft.PathError
		if !errors.As(candidate, &pe) {
			return
		}
		if known[pe.Path] || seen[pe.Path] {
			return
		}
		seen[pe.Path] = true
		found = append(found, pe)
	}

	var multi graft.MultiError
	if errors.As(err, &multi) {
		for _, e := range multi.Errors {
			consider(e)
		}
		return found
	}
	consider(err)
	return found
}
