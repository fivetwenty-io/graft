package main

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	"github.com/fivetwenty-io/graft/internal/history"
	"github.com/fivetwenty-io/graft/internal/utils/ansi"
	"github.com/fivetwenty-io/graft/log"
)

// handleMergeHistory implements `merge --history`/`--trace-path`/
// `--show-changes`/`--changes-only` (docs/user-guide/history-tracking.md):
// instead of printing the merged YAML document, it prints a report of how
// each path in the final document was derived. opts.validateHistoryFlags
// must already have been checked by the caller (handleMerge).
func handleMergeHistory(opts *mergeOpts) int {
	steps, docCount, err := buildMergeHistorySteps(opts, nil, -1)
	if err != nil {
		log.PrintStdErrf("%s\n", err.Error())
		return 2
	}

	all, err := history.Track(steps)
	if err != nil {
		log.PrintStdErrf("%s\n", ansi.Sprintf("@R{Error tracking merge history}: %s", err.Error()))
		return 2
	}

	switch {
	case opts.TracePath != "":
		ph, found := findPathHistory(all, opts.TracePath)
		if !found {
			log.PrintStdErrf("%s\n", ansi.Sprintf("@R{No history found for path} @m{%s}", opts.TracePath))
			return 2
		}
		printStdOutf("%s", renderTracePath(ph))
	case opts.ShowChanges:
		printStdOutf("%s", renderShowChanges(all, docCount))
	case opts.ChangesOnly:
		printStdOutf("%s", renderChangesOnly(all))
	default: // opts.History
		printStdOutf("%s", renderHistory(all))
	}
	return 0
}

// cachedMergeFile holds one resolved input file's raw bytes, read once so
// buildMergeHistorySteps can replay the same file through mergeAllDocs
// multiple times (each YamlFile.Reader is single-use).
type cachedMergeFile struct {
	Path string
	Data []byte
}

// historyDocRewriter transforms one input file's raw bytes before
// buildMergeHistorySteps replays it. A rewriter must be total: it returns
// the bytes it was given, unchanged, for any document it has nothing to
// say about, rather than reporting an error.
type historyDocRewriter func(data []byte) []byte

// buildMergeHistorySteps replays a multi-file merge one file at a time
// (each step's Data is the complete raw, unevaluated merge of every file up
// to and including that one), then appends a synthetic evaluation step
// (the fully evaluated merge) and, if --prune/--cherry-pick were requested,
// a synthetic post-processing step (the actual final CLI output), producing
// the step sequence internal/history.Track needs. This re-runs graft's real
// merge engine once per file prefix rather than reimplementing merge
// semantics, at O(N) merge calls for N input files - acceptable for a CLI
// history report over the small file counts merge invocations actually use.
//
// The returned int is the number of documents actually merged - one per
// resolveMergeInputFiles entry, which is already document-granular (a
// single CLI file argument expands to several entries under -m when it
// contains multiple "---"-separated documents). Callers computing a
// summary count (e.g. --show-changes's "Merge Summary: N files -> ..."
// header) must use this, not len(opts.Files), or a -m invocation
// understates the count: one CLI argument, several documents.
//
// rewrite, when non-nil, transforms each input file's bytes once, after
// they are read and before any merge replays them. It exists for `graft
// debug`'s `history` command, which must apply the session's deferred
// paths (see debugSession.cmdHistory) to every replay this function
// performs - including the final evaluation step, which is where an
// undeferred operator would otherwise abort the whole report. Rewriting
// the input bytes rather than patching a merged tree is what makes the
// deferral survive: every step below re-merges the files from scratch, so
// a change applied anywhere else would be discarded. `merge --history`
// passes nil and is unaffected.
//
// limit, when >= 0, truncates the sequence to a debug session's step
// point: file-prefix steps 0..min(limit, file count-1) only, the
// "<evaluated>" step only when limit >= the file count, and never the
// post-processing step. Files past the truncation point are not merged
// or evaluated, so unreached steps cost nothing; the returned document
// count is then the truncated count. limit counts single-document file
// steps (graft debug, the only limiting caller, never enables
// multi-document mode). limit < 0 means no truncation (the full sequence,
// as before).
func buildMergeHistorySteps(opts *mergeOpts, rewrite historyDocRewriter, limit int) ([]history.StepState, int, error) {
	files, err := resolveMergeInputFiles(opts)
	if err != nil {
		return nil, 0, err
	}
	if len(files) == 0 {
		return nil, 0, ansi.Errorf("@R{Missing Input}: no files to track history for")
	}

	fileSteps, includeEval, includePost := historyStepPlan(limit, len(files), opts)
	files = files[:fileSteps]

	cached := make([]cachedMergeFile, len(files))
	for i := range files {
		data, readErr := readFile(&files[i])
		if readErr != nil {
			return nil, 0, readErr
		}
		if rewrite != nil {
			data = rewrite(data)
		}
		cached[i] = cachedMergeFile{Path: files[i].Path, Data: data}
	}

	freshFiles := func(n int) []YamlFile {
		out := make([]YamlFile, n)
		for i := 0; i < n; i++ {
			out[i] = YamlFile{Path: cached[i].Path, Reader: io.NopCloser(bytes.NewReader(cached[i].Data))}
		}
		return out
	}

	// Raw structural merge, one file prefix at a time: no evaluation, no
	// prune/cherry-pick, so each step's Data reflects only that prefix's
	// merge contributions (PhaseLoad for the first file, PhaseMerge for
	// every later one).
	rawOpts := *opts
	rawOpts.SkipEval = true
	rawOpts.Prune = nil
	rawOpts.CherryPick = nil

	steps := make([]history.StepState, 0, len(cached)+2)
	for i := range cached {
		data, _, mergeErr := mergeAllDocs(freshFiles(i+1), &rawOpts)
		if mergeErr != nil {
			return nil, 0, mergeErr
		}
		phase := history.PhaseMerge
		if i == 0 {
			phase = history.PhaseLoad
		}
		steps = append(steps, history.StepState{Label: cached[i].Path, Phase: phase, Data: data})
	}

	// Full evaluation of all files together, still without prune/cherry-pick,
	// isolating "what did evaluation change" from "what did post-processing
	// remove".
	if includeEval {
		evalOpts := *opts
		evalOpts.Prune = nil
		evalOpts.CherryPick = nil
		evaluatedData, evalEngine, evalErr := mergeAllDocs(freshFiles(len(cached)), &evalOpts)
		if evalErr != nil {
			return nil, 0, evalErr
		}

		// Surface the paths an operator (( prune )) marker actually removed
		// during this evaluation: evaluate() applies them to the tree
		// unconditionally (independent of --prune/--cherry-pick), so
		// evaluatedData already reflects the removal, but GetKeysToPrune's own
		// state is reset before mergeAllDocs returns - GetLastEvaluatedPrunedPaths
		// is what survives (pkg/graft/engine.go). Attaching them to this step
		// lets Track mark the corresponding Entry Removed (and print "<pruned>")
		// instead of leaving it indistinguishable from a path that merely
		// evaluated to an explicit YAML null.
		var evalPrunedPaths []string
		if evalEngine != nil {
			evalPrunedPaths = evalEngine.GetOperatorState().GetLastEvaluatedPrunedPaths()
		}
		steps = append(steps, history.StepState{
			Label:       "<evaluated>",
			Phase:       history.PhaseEval,
			Data:        evaluatedData,
			PrunedPaths: evalPrunedPaths,
		})
	}

	if includePost {
		postData, _, postErr := mergeAllDocs(freshFiles(len(cached)), opts)
		if postErr != nil {
			return nil, 0, postErr
		}
		steps = append(steps, history.StepState{Label: "<pruned>", Phase: history.PhasePost, Data: postData})
	}

	return steps, len(cached), nil
}

// historyStepPlan decides how much of the replay buildMergeHistorySteps
// builds for a given limit: how many file-prefix steps, whether the
// "<evaluated>" step runs, and whether the post-processing step runs.
// limit < 0 means the full sequence.
func historyStepPlan(limit, fileCount int, opts *mergeOpts) (fileSteps int, includeEval, includePost bool) {
	fileSteps = fileCount
	includeEval = true
	includePost = len(opts.Prune) > 0 || len(opts.CherryPick) > 0
	if limit < 0 {
		return fileSteps, includeEval, includePost
	}
	if limit+1 < fileCount {
		fileSteps = limit + 1
	}
	return fileSteps, limit >= fileCount, false
}

// findPathHistory returns the PathHistory for path, if any.
func findPathHistory(all []history.PathHistory, path string) (history.PathHistory, bool) {
	for _, ph := range all {
		if ph.Path == path {
			return ph, true
		}
	}
	return history.PathHistory{}, false
}

// sourceColumnWidth is the padding width history renderers use for the
// "[N] source" / "Final" column, wide enough for the "Final" label itself
// plus a few characters of breathing room before the "→".
const sourceColumnWidth = 18

// inlineValue renders a history Entry/Final value as a single display
// string. Multi-line YAML (a nested map/list value) is collapsed to its
// first line plus an ellipsis marker rather than breaking the renderer's
// one-line-per-entry table layout; nil renders as "~", graft's own YAML
// null convention (reusing yamlValueLines from diff_render.go, so history
// and `graft diff --changes` render values identically).
func inlineValue(v interface{}) string {
	lines := yamlValueLines(v)
	if len(lines) == 1 {
		return lines[0]
	}
	return lines[0] + " …"
}

// looksLikeOperator reports whether s is (after trimming whitespace) a
// graft operator expression "(( ... ))", the shape an unevaluated
// merged-but-not-yet-resolved value takes.
func looksLikeOperator(v interface{}) bool {
	s, ok := v.(string)
	if !ok {
		return false
	}
	trimmed := strings.TrimSpace(s)
	return strings.HasPrefix(trimmed, "((") && strings.HasSuffix(trimmed, "))")
}

// operatorName extracts the operator name from a "(( name args... ))"
// string (see looksLikeOperator); returns "" if s doesn't match that shape.
func operatorName(v interface{}) string {
	s, ok := v.(string)
	if !ok {
		return ""
	}
	trimmed := strings.TrimSpace(s)
	if !strings.HasPrefix(trimmed, "((") || !strings.HasSuffix(trimmed, "))") {
		return ""
	}
	inner := strings.TrimSpace(trimmed[2 : len(trimmed)-2])
	if idx := strings.IndexAny(inner, " \t"); idx >= 0 {
		return inner[:idx]
	}
	return inner
}

// renderHistory implements `merge --history`: the full per-path history for
// every path in the final document.
func renderHistory(all []history.PathHistory) string {
	var buf strings.Builder
	buf.WriteString("Merge History:\n")
	for _, ph := range all {
		buf.WriteString("\n")
		fmt.Fprintf(&buf, "%s:\n", ph.Path)
		for _, e := range ph.Entries {
			writeHistoryEntryLine(&buf, e)
		}
		writeHistoryFinalLine(&buf, ph, len(ph.Entries) == 1)
	}
	return buf.String()
}

// renderTracePath implements `merge --trace-path <path>`: one path's
// history, each entry annotated with a "Type:" line classifying the raw
// value (a graft operator expression, or a plain value).
func renderTracePath(ph history.PathHistory) string {
	var buf strings.Builder
	fmt.Fprintf(&buf, "%s:\n", ph.Path)
	for i, e := range ph.Entries {
		if i > 0 {
			buf.WriteString("\n")
		}
		writeHistoryEntryLine(&buf, e)
		switch {
		case e.Removed:
			buf.WriteString("      Type: removed\n")
		case looksLikeOperator(e.Value):
			fmt.Fprintf(&buf, "      Type: operator (%s)\n", operatorName(e.Value))
		default:
			buf.WriteString("      Type: value\n")
		}
	}
	buf.WriteString("\n")
	writeHistoryFinalLine(&buf, ph, false)
	return buf.String()
}

// historyEntryLine renders one entry's "[N] source → value" line body
// (no leading indent, no newline) - the shared format for history
// blocks and the tree's annotation lines. Only an entry that is
// genuinely Removed (an operator (( prune )) marker or a
// --prune/--cherry-pick CLI flag actually took this path out of the
// document - history.Entry's doc comment) prints "<pruned>"; every
// other entry recorded at a PhasePost step prints its real new value.
func historyEntryLine(e history.Entry) string {
	source := fmt.Sprintf("[%d] %s", e.Index, e.Source)
	val := inlineValue(e.Value)
	if e.Removed {
		val = "<pruned>"
	}
	return fmt.Sprintf("%-*s → %s", sourceColumnWidth, source, val)
}

// writeHistoryEntryLine prints one indented historyEntryLine.
func writeHistoryEntryLine(buf *strings.Builder, e history.Entry) {
	fmt.Fprintf(buf, "  %s\n", historyEntryLine(e))
}

func writeHistoryFinalLine(buf *strings.Builder, ph history.PathHistory, unchanged bool) {
	if !ph.FinalOK {
		fmt.Fprintf(buf, "  %-*s → <pruned>\n", sourceColumnWidth, "Final")
		return
	}
	suffix := ""
	if unchanged {
		suffix = "  (unchanged)"
	}
	fmt.Fprintf(buf, "  %-*s → %s%s\n", sourceColumnWidth, "Final", inlineValue(ph.Final), suffix)
}

// The classifications changeKind can return.
const (
	changeUnchanged = "unchanged"
	changeAdded     = "added"
	changeChanged   = "changed"
	changeRemoved   = "removed"
)

// changeKind classifies a PathHistory for --show-changes/--changes-only:
// "unchanged" (present since the first file, never touched again -
// excluded from both reports), "added" (first appears after the first
// file, and survives to the final document), "changed" (touched more than
// once - overwritten by a later file, evaluated, or both - and survives),
// or "removed" (pruned/cherry-picked away by post-processing).
func changeKind(ph history.PathHistory) string {
	if !ph.FinalOK {
		return changeRemoved
	}
	if len(ph.Entries) == 1 {
		if ph.Entries[0].Phase == history.PhaseLoad {
			return changeUnchanged
		}
		return changeAdded
	}
	return changeChanged
}

// renderShowChanges implements `merge --show-changes`.
func renderShowChanges(all []history.PathHistory, fileCount int) string {
	added, changed, removed := 0, 0, 0
	for _, ph := range all {
		switch changeKind(ph) {
		case changeAdded:
			added++
		case changeChanged:
			changed++
		case changeRemoved:
			removed++
		}
	}

	var buf strings.Builder
	fmt.Fprintf(&buf, "Merge Summary: %s → %s (%d changed, %d added, %d removed)\n",
		pluralCount(fileCount, "file"), pluralCount(len(all), "key"), changed, added, removed)

	for _, ph := range all {
		kind := changeKind(ph)
		if kind == changeUnchanged {
			continue
		}

		buf.WriteString("\n")
		fmt.Fprintf(&buf, "%s:\n", ph.Path)

		if kind == changeAdded {
			e := ph.Entries[0]
			fmt.Fprintf(&buf, "  + %-*s %s\n", sourceColumnWidth-2, e.Source, inlineValue(e.Value))
			continue
		}

		for i, e := range ph.Entries {
			if e.Removed {
				buf.WriteString("  - <pruned>\n")
				continue
			}
			marker := "✗"
			if i == len(ph.Entries)-1 {
				switch {
				case e.Phase == history.PhaseEval:
					marker = "✓"
				case looksLikeOperator(e.Value):
					marker = "○"
				default:
					marker = "✓"
				}
			}
			fmt.Fprintf(&buf, "  %s %-*s %s\n", marker, sourceColumnWidth-2, e.Source, inlineValue(e.Value))
		}
	}

	return buf.String()
}

// renderChangesOnly implements `merge --changes-only`: a compact list of
// every path that differs between the first input file and the final
// document (additions, overwrites, evaluations, and removals alike),
// omitting paths that were present in the first file and never touched
// again.
func renderChangesOnly(all []history.PathHistory) string {
	type row struct {
		path, old, new string
	}
	var rows []row
	for _, ph := range all {
		if changeKind(ph) == changeUnchanged {
			continue
		}

		old := noneDisplay
		if ph.Entries[0].Phase == history.PhaseLoad {
			old = inlineValue(ph.Entries[0].Value)
		}

		newVal := "<removed>"
		if ph.FinalOK {
			newVal = inlineValue(ph.Final)
		}

		rows = append(rows, row{path: ph.Path, old: old, new: newVal})
	}

	var buf strings.Builder
	fmt.Fprintf(&buf, "Changed paths (%s of %d):\n", pluralCount(len(rows), "path"), len(all))
	for _, r := range rows {
		fmt.Fprintf(&buf, "  %-20s %s → %s\n", r.path, r.old, r.new)
	}
	return buf.String()
}

// pluralCount renders "N noun"/"N nouns".
func pluralCount(n int, noun string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, noun)
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
