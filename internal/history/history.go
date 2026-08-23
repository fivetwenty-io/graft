// Package history provides per-path merge/evaluation provenance tracking
// for the graft CLI's `merge --history`/`--trace-path`/`--show-changes`/
// `--changes-only` flags (docs/user-guide/history-tracking.md). It answers
// "where did this value come from, and how did it change" by diffing a
// sequence of whole-document snapshots (one per merge step, plus synthetic
// evaluation/post-processing steps) with internal/histdiff.Compare - the
// same dyff-backed semantic diff `graft diff` uses - rather than
// implementing a second comparison algorithm.
//
// This package holds only the pure step-diffing logic (Track/ChangedPaths).
// Building the step sequence (re-running the real merge engine once per
// file prefix, then once evaluated, then once post-processed) is CLI
// orchestration and lives in cmd/graft, mirroring how internal/histdiff
// (pure Compare) and cmd/graft/diff_render.go (orchestration + rendering)
// are split.
package history

import (
	"fmt"
	"sort"
	"strings"

	"github.com/fivetwenty-io/graft/internal/histdiff"
)

// Phase classifies when a history Entry was recorded, matching
// docs/user-guide/history-tracking.md's phase table.
type Phase int

const (
	// PhaseLoad is a path's first appearance, from the first input file.
	PhaseLoad Phase = iota
	// PhaseMerge is a later input file overwriting or adding a path.
	PhaseMerge
	// PhaseEval is an operator at a path being resolved to its final value.
	PhaseEval
	// PhasePost is post-processing (prune/cherry-pick) removing a path.
	PhasePost
)

// String renders Phase as the label used by history renderers.
func (p Phase) String() string {
	switch p {
	case PhaseLoad:
		return "LOAD"
	case PhaseMerge:
		return "MERGE"
	case PhaseEval:
		return "EVAL"
	case PhasePost:
		return "POST"
	default:
		return "UNKNOWN"
	}
}

// Entry is one recorded change to a single path at one step. Value is nil
// when Removed is true (the path was pruned/cherry-picked away, by an
// operator (( prune )) marker or a --prune/--cherry-pick CLI flag alike);
// every other entry always carries the path's new value at that step,
// including an explicit YAML null (Removed false, Value nil) - Removed is
// what tells the two apart, since both otherwise leave Value nil.
//
// Source is normally the originating step's Label, except when Removed is
// true: it is then rewritten to "<pruned>" regardless of which step
// actually produced the removal (a PhasePost CLI-flag step already used
// that label; a PhaseEval operator (( prune )) removal is relabeled to
// match, so every removal reads identically no matter which mechanism
// caused it - see Track).
type Entry struct {
	Index   int
	Source  string
	Phase   Phase
	Value   interface{}
	Removed bool
}

// PathHistory is the ordered history of one path across every step it
// appeared or changed in. FinalOK is false only when the path's last event
// was a PhasePost removal, meaning it does not exist in the final document
// and Final is meaningless (left nil).
type PathHistory struct {
	Path    string
	Entries []Entry
	Final   interface{}
	FinalOK bool
}

// StepState is one snapshot in a merge's lifecycle: Data is the complete
// document state after this step, Label identifies the step's source (a
// file path for a per-file merge step, or a sentinel like "<evaluated>"/
// "<pruned>" for the synthetic evaluation/post-processing steps), and Phase
// classifies the step per the Phase constants above.
//
// PrunedPaths, when non-empty, names the paths this step's Data reflects as
// explicitly, authoritatively removed by pruning (as opposed to a path
// whose absence Track can only infer from the raw diff) - the operator-
// queued (( prune )) paths a caller surfaced from the engine after
// evaluating this step, for example. Track treats a path in PrunedPaths as
// Removed even if, for some reason, histdiff does not independently
// classify it as histdiff.Removed at this step; every path Track already
// classifies as Removed via histdiff needs no entry here. It is nil for
// every step but a caller-identified prune step.
type StepState struct {
	Label       string
	Phase       Phase
	Data        map[string]interface{}
	PrunedPaths []string
}

// Track computes per-path history across a sequence of StepStates, each
// representing the complete document state after one stage of a merge
// (one entry per input file, in order, followed by an optional evaluation
// step and an optional post-processing step). Every step is diffed against
// the previous step's Data (the first step against an empty document, so
// every path in it is recorded as a PhaseLoad addition) using
// internal/histdiff.Compare; every resulting change becomes one Entry on
// the corresponding path's PathHistory, in step order. The returned slice
// is sorted by Path.
//
// An error is returned only if histdiff.Compare itself fails (e.g. a step's
// Data contains a value YAML cannot represent) - Track does not otherwise
// validate its input.
func Track(steps []StepState) ([]PathHistory, error) {
	byPath := make(map[string]*PathHistory)
	var order []string

	prevFlat := map[string]interface{}{}
	prevLabel := "<empty>"

	for i, step := range steps {
		curFlat := flattenPaths(step.Data)

		// Comparing flattened (single-level, dotted-key) snapshots rather
		// than step.Data's real nested shape matters here: histdiff.Compare
		// reports a wholly new/removed subtree as ONE change at the
		// subtree's own root (see histdiff.fragmentToChanges - it expands
		// an ADDITION/REMOVAL fragment only one level, matching
		// pkg/graft/diff.go's convention). That is the right granularity
		// for `graft diff --changes`, but wrong for history: a path's
		// first appearance (e.g. at PhaseLoad) would then be recorded at a
		// coarser path ("database") than a later scalar overwrite at that
		// same leaf ("database.host") would use, splitting one path's
		// provenance across two unrelated PathHistory entries. Flattening
		// first means every change - first appearance or later overwrite -
		// is always reported at the same leaf path.
		changes, err := histdiff.Compare(prevLabel, prevFlat, step.Label, curFlat)
		if err != nil {
			return nil, fmt.Errorf("history: comparing step %d (%s): %w", i, step.Label, err)
		}

		prunedAt := stepPathSet(step.PrunedPaths)

		for _, c := range changes {
			ph, exists := byPath[c.Path]
			if !exists {
				ph = &PathHistory{Path: c.Path}
				byPath[c.Path] = ph
				order = append(order, c.Path)
			}

			// A path is Removed either because histdiff itself classified
			// it that way (the path is simply absent from this step's
			// Data - the normal case for both an operator (( prune ))
			// during evaluation and a --prune/--cherry-pick CLI flag) or
			// because the caller explicitly named it in this step's
			// PrunedPaths (StepState's doc comment) - a defensive,
			// authoritative signal that does not depend on histdiff's
			// classification agreeing. Removed is never true for a path
			// whose new value is merely an explicit YAML null: that stays
			// Kind Modified/Added with New nil, not Kind Removed.
			removed := c.Kind == histdiff.Removed || prunedAt[c.Path]

			var value interface{}
			if !removed && c.Kind != histdiff.Removed {
				value = c.New
			}

			source := step.Label
			if removed {
				// Every removal reads identically regardless of which
				// mechanism (operator (( prune )), --prune, --cherry-pick)
				// or which step (PhaseEval or PhasePost) caused it.
				source = "<pruned>"
			}

			ph.Entries = append(ph.Entries, Entry{
				Index:   i,
				Source:  source,
				Phase:   step.Phase,
				Value:   value,
				Removed: removed,
			})
		}

		prevFlat = curFlat
		prevLabel = step.Label
	}

	sort.Strings(order)
	result := make([]PathHistory, 0, len(order))
	for _, path := range order {
		ph := byPath[path]
		last := ph.Entries[len(ph.Entries)-1]
		if last.Removed {
			ph.FinalOK = false
			ph.Final = nil
		} else {
			ph.FinalOK = true
			ph.Final = last.Value
		}
		result = append(result, *ph)
	}
	return result, nil
}

// stepPathSet builds a set from a StepState's PrunedPaths for O(1)
// membership checks in Track's per-change loop. Returns an empty (non-nil)
// map for a nil/empty input, so a caller can always index it directly.
func stepPathSet(paths []string) map[string]bool {
	set := make(map[string]bool, len(paths))
	for _, p := range paths {
		set[p] = true
	}
	return set
}

// flattenPaths walks data recursively and returns a single-level map keyed
// by dot-joined path (e.g. "database.host") to that path's leaf value. A
// nested map is descended into; any other value (scalar, list, or an empty
// map, which carries information worth keeping - `foo: {}` differs from
// `foo` being absent) is a leaf. See Track's comment for why history
// diffing needs this flattened shape rather than comparing nested maps
// directly.
//
// Each segment is escaped (EscapePathSegment) before joining, so a literal
// map key containing a "." or "[" - the two characters this function's own
// joining, and graft's broader dotted-path syntax (pkg/graft/utils.go's
// ParsePath), both treat as structural separators - cannot collide with an
// unrelated nested path built by joining real segments. Without escaping,
// a literal top-level key "a.b" and a nested `a: {b: ...}` both flatten to
// the identical string "a.b", so Track cannot tell which one a later
// change belongs to and reports one as if it overwrote the other.
func flattenPaths(data map[string]interface{}) map[string]interface{} {
	flat := make(map[string]interface{})
	flattenInto(flat, "", data)
	return flat
}

func flattenInto(flat map[string]interface{}, prefix string, v interface{}) {
	m, isMap := v.(map[string]interface{})
	if !isMap || len(m) == 0 {
		if prefix != "" {
			flat[prefix] = v
		}
		return
	}
	for k, sub := range m {
		seg := EscapePathSegment(k)
		path := seg
		if prefix != "" {
			path = prefix + "." + seg
		}
		flattenInto(flat, path, sub)
	}
}

// EscapePathSegment quotes seg (graft's existing quoted-segment path
// syntax, e.g. `"a.b".c` - see pkg/graft/utils.go's ParsePath, which
// already accepts this form) when it contains a "." or "[", the two
// characters that would otherwise be ambiguous with flattenInto's own
// dot-joining or with bracketed list-index syntax elsewhere in graft.
// A segment with neither character is returned unchanged, so the common
// case (plain identifier keys) renders exactly as before - a history
// report for `database.host` still reads "database.host", not
// `"database"."host"`.
func EscapePathSegment(seg string) string {
	if strings.ContainsAny(seg, ".[") {
		return `"` + seg + `"`
	}
	return seg
}

// ChangedPaths filters all to only the paths with more than one Entry -
// i.e. paths that were overwritten, evaluated, or removed after their
// initial appearance, backing `merge --changes-only`.
func ChangedPaths(all []PathHistory) []PathHistory {
	var out []PathHistory
	for _, ph := range all {
		if len(ph.Entries) > 1 {
			out = append(out, ph)
		}
	}
	return out
}
