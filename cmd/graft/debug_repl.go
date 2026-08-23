package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/fivetwenty-io/graft/internal/histdiff"
	"github.com/fivetwenty-io/graft/internal/history"
	"github.com/fivetwenty-io/graft/internal/utils/ansi"
	"github.com/fivetwenty-io/graft/internal/utils/termbg"
	"github.com/fivetwenty-io/graft/log"
	"github.com/fivetwenty-io/graft/pkg/graft"
)

// debugConfigKeys maps the REPL `config` command's dotted key names to the
// real environment variable each one drives (docs/user-guide/cli/debug.md's
// `config vault.addr ...` example).
//
// This is a deliberately narrow set, not an exhaustive one: graft reads
// plenty of other backend settings from a plain os.Getenv, including
// VAULT_SKIP_VERIFY (internal/backends/vault/client.go), AWS_PROFILE/
// AWS_REGION/AWS_ROLE (pkg/graft/operators/op_aws.go's no-target session
// fallback), and NATS_URL plus a dozen NATS_* tuning variables
// (pkg/graft/operators/op_nats.go's parseNatsConfig). Wiring those too is a
// straightforward extension of this map; it is unimplemented, not
// impossible.
//
// `config` only ever affects operators evaluated *after* the change (via
// `eval`/`continue`/`step`); a backend client already constructed and
// pooled from an earlier evaluation may not pick up the new value - the
// REPL has no hook into a backend's client cache to force a rebuild.
// (NATS re-reads its environment on every operator evaluation, so it would
// not have that caveat; Vault, which pools its client, does.)
// debugConfigKeyVaultToken is the one `config` key whose value is a
// credential, which is why it is named rather than spelled out at each use.
const debugConfigKeyVaultToken = "vault.token"

var debugConfigKeys = map[string]string{
	"vault.addr":             "VAULT_ADDR",
	debugConfigKeyVaultToken: "VAULT_TOKEN",
	"vault.namespace":        "VAULT_NAMESPACE",
}

// debugConfigKeyOrder is debugConfigKeys' display order for a bare `config`.
var debugConfigKeyOrder = []string{"vault.addr", debugConfigKeyVaultToken, "vault.namespace"}

// mergedDocPath is the synthetic YamlFile path label the REPL uses when
// re-merging the session's own tree through mergeAllDocs (and as the
// "before" label in histdiff comparisons against it).
const mergedDocPath = "<merged>"

// debugSession holds one `graft debug` REPL's state: the cached source
// files (re-parsed on every step/continue/eval, mirroring
// buildMergeHistorySteps' cachedMergeFile replay approach so evaluation
// side effects like Vault calls stay consistent with a real
// `graft merge`), how far the merge has progressed, and REPL-only state
// (breakpoints, deferred paths) that has no equivalent in a plain merge.
type debugSession struct {
	cached []cachedMergeFile
	opts   *mergeOpts

	loaded bool
	// step is how many of the REPL's totalSteps have run: 0 means only
	// cached[0] (the base document) has been loaded; totalSteps-1 means
	// every file has been merged (raw, unevaluated); totalSteps means the
	// full document has also been evaluated.
	step       int
	totalSteps int

	// tree is the document as of `step`: cached[0..step] raw-merged for
	// step < totalSteps-1... actually step <= totalSteps-1 covers merge
	// steps; at step == totalSteps, tree is the fully evaluated document.
	tree map[string]interface{}

	breakpoints map[string]bool
	// deferred is every path the session has chosen not to evaluate,
	// mapped to why: "" for a manual `defer <path>` (no reason recorded -
	// a human just said so), or the original operator error for a path
	// `autodefer` found and deferred on its own (see cmdAutodefer).
	// applyDeferredWrapping only ever reads the keys; cmdInspect is what
	// surfaces the reasons.
	deferred map[string]string

	out io.Writer

	// styler renders this session's own formatted output in the
	// resolved color/theme (see resolveDebugStyler); it is the zero
	// debugStyler, and so identity, whenever color is off. Nothing
	// calls styler.apply for real output yet - later work threads it
	// through the print sites listed in the category-to-role map.
	styler debugStyler

	// themeName is the session's current theme selection ("auto",
	// "dark", "light", or "mono"), distinct from styler.theme (the
	// palette that selection *resolved* to - see resolveDebugTheme):
	// "auto" always displays the palette it resolved to, e.g. "dark
	// (auto)" (see currentThemeDisplay), and `config theme auto`
	// re-runs that resolution rather than pinning whatever it last
	// picked. Set once at construction (see newDebugSession) and
	// updated only by cmdConfigTheme.
	themeName string

	// detectedBackground is the terminal background handleDebug already
	// detected before constructing this session (see
	// withDetectedBackground/resolveDebugStyler), cached here so a later
	// `config theme auto` (cmdConfigTheme) reuses the same answer
	// instead of re-querying the terminal mid-session. termbg.Unknown
	// for every session that never ran detection (color off, a
	// non-terminal writer, or an explicit non-auto theme).
	detectedBackground termbg.Background

	// reader is the session's own input source, set by handleDebug once
	// it constructs one, so cmdConfigTheme can restyle the live prompt
	// (SetPrompt) after a `config theme <name>` switch without
	// threading the reader through every call site. Nil for a
	// debugSession built directly (as several tests do), in which case
	// a theme switch simply has no prompt to restyle.
	reader debugLineReader
}

// newDebugSession loads and caches every input file's raw bytes (without
// merging), matching the REPL's `load` command: files are read once up
// front so later replays (step/continue/eval all re-run mergeAllDocs on
// fresh YamlFile readers) don't depend on any reader being seekable or
// reusable. ui is resolved once, here, into the session's styler (see
// resolveDebugStyler), against out - never against stderr, which is
// what the package-global ansi flag resolves against.
func newDebugSession(files []string, opts *mergeOpts, out io.Writer, ui debugUIOptions) (*debugSession, error) {
	resolvedOpts := *opts
	resolvedOpts.Files = files
	yamlFiles, err := resolveMergeInputFiles(&resolvedOpts)
	if err != nil {
		return nil, err
	}
	if len(yamlFiles) == 0 {
		return nil, ansi.Errorf("@R{Missing Input}: no files to debug")
	}

	cached := make([]cachedMergeFile, len(yamlFiles))
	for i := range yamlFiles {
		data, readErr := readFile(&yamlFiles[i])
		if readErr != nil {
			return nil, readErr
		}
		cached[i] = cachedMergeFile{Path: yamlFiles[i].Path, Data: data}
	}

	total := len(cached) // (len(cached)-1) merge steps + 1 eval step
	if total < 1 {
		total = 1
	}

	return &debugSession{
		cached:             cached,
		opts:               opts,
		totalSteps:         total,
		breakpoints:        map[string]bool{},
		deferred:           map[string]string{},
		out:                out,
		styler:             resolveDebugStyler(ui, out),
		themeName:          normalizeThemeName(ui.Theme),
		detectedBackground: ui.DetectedBackground,
	}, nil
}

// freshFiles returns fresh, single-use YamlFile readers for cached[0:n].
func (s *debugSession) freshFiles(n int) []YamlFile {
	out := make([]YamlFile, n)
	for i := 0; i < n; i++ {
		out[i] = YamlFile{Path: s.cached[i].Path, Reader: io.NopCloser(strings.NewReader(string(s.cached[i].Data)))}
	}
	return out
}

func (s *debugSession) printf(format string, args ...interface{}) {
	_, _ = fmt.Fprintf(s.out, format, args...)
}

// style renders str in role r through the session's own resolved
// styler - identity (str unchanged) whenever color is off, so every
// existing bytes.Buffer test's plain-mode assertions keep matching
// byte for byte (see debugStyler.apply). Call sites style the label
// parts of a line, not the whole formatted line, so punctuation such as
// the colon after a heading stays outside the escape span.
func (s *debugSession) style(r debugRole, str string) string {
	return s.styler.apply(r, str)
}

// rawMergeOpts returns a copy of the session's own opts (so --go-patch and
// --fallback-append, the two merge flags meaningful to a raw structural
// merge, are honored) with SkipEval forced on and any --prune/--cherry-pick
// cleared, for the session's own raw (unevaluated) merge steps: load's base
// document, each step's raw merge, and diff's recomputed base. Before this
// helper existed those call sites used a bare &mergeOpts{SkipEval: true},
// silently discarding EnableGoPatch/FallbackAppend even when the CLI/REPL
// caller had set them (F14).
func (s *debugSession) rawMergeOpts() *mergeOpts {
	opts := *s.opts
	opts.SkipEval = true
	opts.Prune = nil
	opts.CherryPick = nil
	return &opts
}

// cmdLoad implements the `load` REPL command: parses every file
// individually (not merged) to report its own top-level key count, and
// establishes cached[0] (the first file, unmerged) as the starting
// document - matching docs/user-guide/cli/debug.md's session transcript,
// where `step` begins by merging the *second* file onto an already-loaded
// first one.
func (s *debugSession) cmdLoad() {
	s.printf("%s\n", s.style(roleHeading, fmt.Sprintf("Loaded %s:", pluralCount(len(s.cached), "document"))))
	for i, c := range s.cached {
		singleOpts := *s.opts
		singleOpts.Prune = nil
		singleOpts.CherryPick = nil
		singleOpts.SkipEval = true
		data, _, err := mergeAllDocs([]YamlFile{{Path: c.Path, Reader: io.NopCloser(strings.NewReader(string(c.Data)))}}, &singleOpts)
		keyCount := 0
		if err == nil {
			keyCount = len(data)
		}
		s.printf("  %s %s %s\n",
			s.style(roleCounter, fmt.Sprintf("[%d]", i)),
			s.style(roleFile, c.Path),
			s.style(roleMuted, fmt.Sprintf("(%s)", pluralCount(keyCount, "key"))))
	}

	base, _, err := mergeAllDocs(s.freshFiles(1), s.rawMergeOpts())
	if err != nil {
		s.printf("%s %s: %s\n",
			s.style(roleError, "Error loading"),
			s.style(roleFile, s.cached[0].Path),
			ansi.StripEscapes(err.Error()))
		return
	}
	s.tree = base
	s.step = 0
	s.loaded = true
}

// stepOnce runs exactly one step (a merge of the next file, or, on the
// final step, full evaluation), printing its progress line and any changed
// paths, and reports whether a breakpoint fired. It is shared by `step`
// (one call) and `continue` (a loop of calls).
// stepOnce's second return value, failed, is F19's fix: a failed merge or
// evaluation prints its error and rewinds s.step (so a subsequent manual
// `step` retries the same step, matching the pre-fix behavior plain `step`
// already had), but the caller must know a failure happened rather than
// inferring it from the step counter - the counter alone is ambiguous
// (rewound-after-failure looks identical to "hasn't started yet") and
// cmdContinue's loop condition (`s.step < s.totalSteps`) doesn't change
// when a step fails, so without an explicit failure signal it would retry
// and re-fail the same step forever.
func (s *debugSession) stepOnce() (hitBreakpoint, failed bool) {
	s.step++

	if s.step < s.totalSteps {
		fileIdx := s.step
		s.printf("%s Merging %s...\n",
			s.style(roleCounter, fmt.Sprintf("[%d/%d]", s.step, s.totalSteps)),
			s.style(roleFile, s.cached[fileIdx].Path))

		newTree, _, err := mergeAllDocs(s.freshFiles(fileIdx+1), s.rawMergeOpts())
		if err != nil {
			s.printf("%s: %s\n",
				s.style(roleError, "Merge failed"),
				ansi.StripEscapes(err.Error()))
			s.step--
			return false, true
		}

		changes, cmpErr := histdiff.Compare("before", s.tree, s.cached[fileIdx].Path, newTree)
		if cmpErr == nil {
			for _, c := range changes {
				s.printf("  %s: %s %s %s\n",
					s.style(rolePath, c.Path),
					s.styleChangeValue(roleValueOld, changeOldDisplay(c)),
					s.style(roleMuted, "→"),
					s.styleChangeValue(roleValueNew, changeNewDisplay(c)))
			}
		}
		s.tree = newTree

		for _, c := range changes {
			if s.breakpoints[c.Path] {
				s.printf("%s %s\n  Current: %s\n",
					s.style(roleBreak, "Breakpoint hit:"),
					s.style(rolePath, c.Path),
					s.styleChangeValue(roleValueNew, changeNewDisplay(c)))
				// The path itself is already printed above; the
				// caller only needs to know the loop must stop.
				return true, false
			}
		}
		return false, false
	}

	// Final step: evaluate. Deferred paths are protected by rewriting their
	// still-unevaluated "(( op ... ))" value to "(( defer op ... ))" (the
	// real spruce-compatible defer operator, pkg/graft/operators/op_defer.go)
	// before evaluation, so the evaluator reconstructs the literal
	// expression instead of resolving it - the same mechanism a hand-authored
	// "(( defer ... ))" in the source YAML would trigger, not a REPL-only
	// simulation of deferral.
	s.printf("%s Evaluating operators...\n", s.style(roleCounter, fmt.Sprintf("[%d/%d]", s.step, s.totalSteps)))

	deferredTree := applyDeferredWrapping(s.tree, s.deferred)
	evalOpts := *s.opts
	evalOpts.SkipEval = false
	evalOpts.Prune = nil
	evalOpts.CherryPick = nil
	evaluated, _, err := mergeAllDocs([]YamlFile{{Path: mergedDocPath, Reader: io.NopCloser(strings.NewReader(mustYAML(deferredTree)))}}, &evalOpts)
	if err != nil {
		s.printf("%s: %s\n",
			s.style(roleError, "Evaluation failed"),
			ansi.StripEscapes(err.Error()))
		s.step--
		return false, true
	}

	changes, cmpErr := histdiff.Compare(mergedDocPath, s.tree, "<evaluated>", evaluated)
	s.tree = evaluated

	if cmpErr == nil {
		for _, c := range changes {
			if s.breakpoints[c.Path] {
				s.printf("%s %s\n  Current: %s\n",
					s.style(roleBreak, "Breakpoint hit:"),
					s.style(rolePath, c.Path),
					s.styleChangeValue(roleValueNew, changeNewDisplay(c)))
				return true, false
			}
		}
	}
	return false, false
}

// cmdStep implements the `step` REPL command.
func (s *debugSession) cmdStep() {
	if !s.loaded {
		s.printf("%s\n", s.style(roleWarn, "No documents loaded. Run 'load' first."))
		return
	}
	if s.step >= s.totalSteps {
		s.printf("%s\n", s.style(roleSuccess, "Merge complete. Nothing more to step."))
		return
	}
	s.stepOnce()
}

// cmdContinue implements the `continue` REPL command: repeats stepOnce
// until every step has run, a breakpoint fires, or a step fails. A failure
// stops the loop immediately (stepOnce has already printed the error and
// rewound s.step to the failing step) rather than retrying it - without
// this check the loop condition alone (`s.step < s.totalSteps`) never
// changes on a failure, so continue would re-run and re-fail the same step
// forever (F19).
func (s *debugSession) cmdContinue() {
	if !s.loaded {
		s.printf("%s\n", s.style(roleWarn, "No documents loaded. Run 'load' first."))
		return
	}
	if s.step >= s.totalSteps {
		s.printf("%s\n", s.style(roleSuccess, "Merge complete. Nothing more to run."))
		return
	}
	for s.step < s.totalSteps {
		hit, failed := s.stepOnce()
		if hit || failed {
			return
		}
	}
	s.printf("%s\n", s.style(roleSuccess, "Evaluation complete."))
}

// cmdBreak/cmdUnbreak/cmdBreaks implement `break <path>`, `unbreak <path>`,
// and `breaks`.
func (s *debugSession) cmdBreak(path string) {
	if path == "" {
		s.printf("%s\n", s.style(roleWarn, "Usage: break <path>"))
		return
	}
	s.breakpoints[path] = true
	s.printf("%s\n", s.style(roleSuccess, fmt.Sprintf("Breakpoint set on %s", path)))
}

func (s *debugSession) cmdUnbreak(path string) {
	if path == "" {
		s.printf("%s\n", s.style(roleWarn, "Usage: unbreak <path>"))
		return
	}
	if !s.breakpoints[path] {
		s.printf("%s\n", s.style(roleWarn, fmt.Sprintf("No breakpoint on %s", path)))
		return
	}
	delete(s.breakpoints, path)
	s.printf("%s\n", s.style(roleSuccess, "Breakpoint removed"))
}

func (s *debugSession) cmdBreaks() {
	if len(s.breakpoints) == 0 {
		s.printf("%s\n", s.style(roleMuted, "No breakpoints set."))
		return
	}
	paths := make([]string, 0, len(s.breakpoints))
	for p := range s.breakpoints {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	s.printf("%s\n", s.style(roleHeading, "Breakpoints:"))
	for _, p := range paths {
		s.printf("  - %s\n", s.style(rolePath, p))
	}
}

// cmdInspect implements `inspect [path]`: the current value at path (the
// whole document if path is empty), from whatever phase the session is
// currently in (raw merge or evaluated), followed by the session's full
// deferred-path list (manual `defer` and `autodefer` alike) when it has
// one - regardless of whether the inspected path itself is deferred, so a
// bare `inspect` surfaces the whole picture in one place.
func (s *debugSession) cmdInspect(path string) {
	if !s.loaded {
		s.printf("%s\n", s.style(roleWarn, "No documents loaded. Run 'load' first."))
		return
	}
	value, ok := lookupDottedPath(s.tree, path)
	if !ok {
		s.printf("%s\n", s.style(roleWarn, fmt.Sprintf("Path not found: %s", path)))
		return
	}
	raw, err := graft.MarshalYAML(value)
	if err != nil {
		s.printf("%s: %s\n",
			s.style(roleError, "Error rendering value"),
			ansi.StripEscapes(err.Error()))
		return
	}
	s.writeYAML(raw)

	if len(s.deferred) == 0 {
		return
	}
	paths := make([]string, 0, len(s.deferred))
	for p := range s.deferred {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	s.printf("\n%s\n", s.style(roleHeading, fmt.Sprintf("Deferred %s:", pluralCount(len(paths), "path"))))
	for _, p := range paths {
		reason := s.deferred[p]
		if reason == "" {
			s.printf("  - %s\n", s.style(rolePath, p))
			continue
		}
		s.printf("  - %s: %s\n", s.style(rolePath, p), s.style(roleMuted, reason))
	}
}

// cmdHistory implements `history <path>`: the same per-path entry list
// `merge --history` would show for path, computed from the debug session's
// files under the same opts - except --prune/--cherry-pick, which are
// deliberately excluded (see below), so `history` agrees with `output`/
// `export` throughout stepping. Run `prune-report` once the session is
// fully evaluated to see what --prune/--cherry-pick would additionally
// remove.
func (s *debugSession) cmdHistory(path string) {
	if path == "" {
		s.printf("%s\n", s.style(roleWarn, "Usage: history <path>"))
		return
	}

	fileOpts := *s.opts
	fileOpts.Files = make([]string, len(s.cached))
	for i, c := range s.cached {
		fileOpts.Files[i] = c.Path
	}
	// output/export always show the session's pre-(CLI-flag)-prune tree
	// (stepOnce's own evalOpts strips Prune/CherryPick for the same
	// reason); history must agree; only a --prune/--cherry-pick flag's
	// effect is excluded here, not an operator (( prune )) marker's -
	// that always applies unconditionally, in the engine itself,
	// independent of any CLI flag, so it stays visible in both.
	fileOpts.Prune = nil
	fileOpts.CherryPick = nil
	// buildMergeHistorySteps re-resolves files from opts.Files by path, not
	// from s.cached's in-memory bytes; this matches a plain `graft debug`
	// invocation's files still being present on disk (the same assumption
	// resolveMergeInputFiles already makes for stdin-less invocations).
	//
	// The session's deferred paths are applied to each of those files as
	// they are read, so `defer` covers `history` exactly as it covers
	// `step`/`continue`. Without this, one operator the session cannot
	// resolve - an unreachable Vault path, an unfilled (( param )) - aborts
	// the recompute and makes `history` report that operator's error for
	// every path asked about, including unrelated ones.
	steps, _, err := buildMergeHistorySteps(&fileOpts, s.deferredDocRewriter(), -1)
	if err != nil {
		s.printf("%s: %s\n",
			s.style(roleError, "Error computing history"),
			ansi.StripEscapes(err.Error()))
		return
	}
	all, err := history.Track(steps)
	if err != nil {
		s.printf("%s: %s\n",
			s.style(roleError, "Error computing history"),
			ansi.StripEscapes(err.Error()))
		return
	}
	ph, found := findPathHistory(all, path)
	if !found {
		s.printf("%s %s\n", s.style(roleWarn, "No history recorded for path:"), s.style(rolePath, path))
		return
	}
	s.printf("%s:\n", s.style(rolePath, ph.Path))
	for _, e := range ph.Entries {
		s.writeHistoryLine(entryLineParts(e), false)
	}
	s.writeHistoryLine(finalLineParts(ph, "Final", len(ph.Entries) == 1), true)
}

// writeHistoryLine formats one historyLineParts (an entry row, or the
// trailing Final row when isFinal) through the session's styler,
// matching Category H of the debugger's plan of record: the source
// column - "[N] source" for an entry, "Final" for the trailing row -
// styles roleFile; the arrow between source and value is always
// roleMuted; a genuine removal's value ("<pruned>", from
// history.Entry.Removed or PathHistory.FinalOK false, never from the
// value's own text - see historyLineParts) styles roleMuted; the
// trailing Final row's real value styles roleValueNew; every other
// entry's value stays unstyled ("values default").
//
// The source column is padded to sourceColumnWidth as plain text
// *before* styling (pad-then-style), so the escape bytes roleFile adds
// never count toward %-*s's width and so never throw off the column's
// alignment.
func (s *debugSession) writeHistoryLine(p historyLineParts, isFinal bool) {
	paddedSource := fmt.Sprintf("%-*s", sourceColumnWidth, p.source)
	value := p.value
	switch {
	case p.pruned:
		value = s.style(roleMuted, value)
	case isFinal:
		value = s.style(roleValueNew, value)
	}
	s.printf("  %s %s %s\n",
		s.style(roleFile, paddedSource),
		s.style(roleMuted, "→"),
		value)
}

// deferredDocRewriter returns a historyDocRewriter that wraps the
// session's deferred paths in each document it is handed, or nil when
// nothing is deferred (so `history` costs exactly what it did before).
//
// A document the rewriter cannot make sense of is returned untouched
// rather than reported as an error: a non-map root, most commonly a
// go-patch document, has no dotted path to wrap, and re-marshaling a
// document that gained no wrapping would only risk perturbing input the
// merge engine is about to parse anyway.
func (s *debugSession) deferredDocRewriter() historyDocRewriter {
	if len(s.deferred) == 0 {
		return nil
	}
	return func(data []byte) []byte {
		tree, err := parseYAML(data)
		if err != nil {
			return data
		}
		wrapped := applyDeferredWrapping(tree, s.deferred)
		out, err := graft.MarshalYAML(wrapped)
		if err != nil {
			return data
		}
		return out
	}
}

// cmdDefer implements `defer <path>`.
func (s *debugSession) cmdDefer(path string) {
	if path == "" {
		s.printf("%s\n", s.style(roleWarn, "Usage: defer <path>"))
		return
	}
	s.deferred[path] = "" // manual defer: no root-cause reason recorded
	s.printf("%s\n", s.style(roleSuccess, fmt.Sprintf("Marked %s for deferred evaluation", path)))
}

// cmdAutodefer implements `autodefer`: runs the same defer-on-error retry
// loop `graft merge --defer-on-error`/`--adaptive` uses (runAdaptiveMerge,
// adaptive_merge.go) against the session's current tree, wrapping each
// newly-failing operator in "(( defer ... ))" and retrying to a fixed
// point instead of leaving the session stuck on the first failure.
//
// Composition with a prior manual `defer`: the session's own deferred set
// is applied first (applyDeferredWrapping, the same protection stepOnce's
// final-step evaluation gives it), so a path the user already deferred is
// never re-attempted, never re-fails, and never gets a redundant second
// entry in the summary below - runAdaptiveMerge only ever sees and reports
// genuinely new failures. Every path autodefer newly defers is folded
// into s.deferred (with its root-cause reason), so a later `step`/
// `continue`/`history` protects it exactly as a manual `defer` would, and
// `inspect` can list it (see cmdInspect).
//
// A hard failure (a true cycle, or the loop not converging) leaves the
// session's tree and deferred set exactly as they were before this call -
// runAdaptiveMerge returns before recording anything new on a hard
// failure (see its own doc comment), so there is nothing to roll back.
func (s *debugSession) cmdAutodefer() {
	if !s.loaded {
		s.printf("%s\n", s.style(roleWarn, "No documents loaded. Run 'load' first."))
		return
	}

	deferredTree := applyDeferredWrapping(s.tree, s.deferred)
	autodeferOpts := *s.opts
	autodeferOpts.SkipEval = false
	// Prune/CherryPick deliberately excluded, matching cmdHistory/output's
	// own pre-(--prune/--cherry-pick)-flag convention (Item 5): those
	// flags apply once, to a normal merge's final result, and would
	// otherwise strip data before the loop below (and any later
	// inspect/step) can see it. Use `prune-report` to see what they would
	// remove.
	autodeferOpts.Prune = nil
	autodeferOpts.CherryPick = nil

	engine, docs, err := buildEngineAndDocs(
		[]YamlFile{{Path: mergedDocPath, Reader: io.NopCloser(strings.NewReader(mustYAML(deferredTree)))}},
		&autodeferOpts,
	)
	if err != nil {
		s.printf("%s: %s\n",
			s.style(roleError, "Error preparing autodefer"),
			ansi.StripEscapes(err.Error()))
		return
	}

	result, err := runAdaptiveMerge(context.Background(), engine, docs, adaptiveMergeOptions{
		FallbackAppend: s.opts.FallbackAppend,
	})
	if err != nil {
		s.printf("%s: %s\n",
			s.style(roleError, "Autodefer failed"),
			ansi.StripEscapes(err.Error()))
		return
	}

	if len(result.Deferred) == 0 {
		s.printf("%s\n", s.style(roleMuted, "Autodefer: no failing operators - nothing to defer."))
	} else {
		s.printf("%s\n", s.style(roleHeading, fmt.Sprintf("Autodefer: %s deferred:", pluralCount(len(result.Deferred), "key"))))
		for _, d := range result.Deferred {
			s.printf("  deferred %s: %s\n", s.style(rolePath, "$."+d.Path), s.style(roleMuted, d.Reason))
			s.deferred[d.Path] = d.Reason
		}
	}

	s.tree = result.Tree
	s.step = s.totalSteps
}

// cmdEval implements `eval <path>`: forces immediate evaluation of the
// operator at path (even if it was marked `defer`), independent of the
// session's overall step progress, and updates the session's tree with the
// resolved value at that path.
func (s *debugSession) cmdEval(path string) {
	if !s.loaded {
		s.printf("%s\n", s.style(roleWarn, "No documents loaded. Run 'load' first."))
		return
	}
	value, ok := lookupDottedPath(s.tree, path)
	if !ok {
		s.printf("%s\n", s.style(roleWarn, fmt.Sprintf("Path not found: %s", path)))
		return
	}
	s.printf("%s %s\n", s.style(roleHeading, "Evaluating:"), inlineValue(value))

	evalOpts := *s.opts
	evalOpts.SkipEval = false
	evalOpts.CherryPick = []string{path}
	evalOpts.Prune = nil
	result, _, err := mergeAllDocs([]YamlFile{{Path: mergedDocPath, Reader: io.NopCloser(strings.NewReader(mustYAML(s.tree)))}}, &evalOpts)
	if err != nil {
		s.printf("%s: %s\n",
			s.style(roleError, "Evaluation failed"),
			ansi.StripEscapes(err.Error()))
		return
	}
	resolved, ok := lookupDottedPath(result, path)
	if !ok {
		s.printf("%s %s\n",
			s.style(roleError, "Evaluation did not produce a value at"),
			s.style(rolePath, path))
		return
	}
	s.printf("%s %s\n", s.style(roleSuccess, "Result:"), inlineValue(resolved))
	setDottedPath(s.tree, path, resolved)
}

// debugConfigKeyTheme is `config`'s one non-Vault key: the debugger's
// own color theme (see cmdConfigTheme). It is special-cased ahead of
// the env-backed debugConfigKeys map in every arm below - the bare
// listing loop reads debugConfigKeys[key] (an env var name "theme"
// does not have), and both the get and set arms reject any key absent
// from that map - because, unlike vault.*, it names no environment
// variable and never persists past the session: no config file, no env
// var, session-only (see plans/debugger-colorizing.md's "config theme"
// section).
const debugConfigKeyTheme = "theme"

// debugConfigKnownKeysDisplay renders every recognized `config` key for
// the "Unknown config key" message: debugConfigKeyOrder's own entries
// (the Vault keys, in their display order) plus debugConfigKeyTheme.
// debugConfigKeyOrder itself must stay theme-free - it drives the bare
// `config` listing loop (cmdConfig's zero-argument arm), which already
// prints the theme row on its own line ahead of that loop, so appending
// theme to debugConfigKeyOrder would print it there twice. append onto a
// freshly allocated slice, never onto debugConfigKeyOrder's own backing
// array, so this can never mutate the package-level order slice even if
// its capacity ever exceeds its length.
func debugConfigKnownKeysDisplay() string {
	known := make([]string, 0, len(debugConfigKeyOrder)+1)
	known = append(known, debugConfigKeyOrder...)
	known = append(known, debugConfigKeyTheme)
	return strings.Join(known, ", ")
}

// cmdConfig implements `config`/`config <key>`/`config <key> <value>`; see
// debugConfigKeys' doc comment for the Vault-key scope and
// debugConfigKeyTheme's doc comment for the theme special case.
// cmdConfig's arms never style a value line: the bare listing, the
// single-key "Current:" line, and the theme row all print plain text
// unconditionally, because vault.token's value is a live credential
// (decision 12, plans/debugger-colorizing.md). Only the key-name-only
// confirmation/warning lines below get a role.
func (s *debugSession) cmdConfig(args []string) {
	switch len(args) {
	case 0:
		s.printf("%s: %s\n", debugConfigKeyTheme, s.currentThemeDisplay())
		for _, key := range debugConfigKeyOrder {
			s.printf("%s: %s\n", key, envOrNotSet(debugConfigKeys[key]))
		}
	case 1:
		if args[0] == debugConfigKeyTheme {
			s.printf("Current: %s\n", s.currentThemeDisplay())
			return
		}
		envVar, known := debugConfigKeys[args[0]]
		if !known {
			s.printf("%s\n", s.style(roleWarn, fmt.Sprintf("Unknown config key: %s. Known keys: %s", args[0], debugConfigKnownKeysDisplay())))
			return
		}
		s.printf("Current: %s\n", envOrNotSet(envVar))
	default:
		if args[0] == debugConfigKeyTheme {
			s.cmdConfigTheme(strings.Join(args[1:], " "))
			return
		}
		envVar, known := debugConfigKeys[args[0]]
		if !known {
			s.printf("%s\n", s.style(roleWarn, fmt.Sprintf("Unknown config key: %s. Known keys: %s", args[0], debugConfigKnownKeysDisplay())))
			return
		}
		_ = os.Setenv(envVar, strings.Join(args[1:], " "))
		s.printf("%s\n", s.style(roleSuccess, fmt.Sprintf("Updated %s", args[0])))
	}
}

// cmdConfigTheme implements the set arm of `config theme <name>`: name
// is validated against knownThemeNames (isValidThemeName, debug_theme.go
// - the same table --theme/GRAFT_THEME validate against, so the three
// tiers can never disagree on what a valid name is). An unknown name
// changes nothing and reports the mismatch; a known name updates the
// session's recorded selection and, when color is enabled, re-resolves
// the styler's theme and restyles the live prompt through the session's
// reader (see debugSession.reader/SetPrompt) so later output and the
// prompt agree immediately. Session-only throughout: no config file, no
// environment variable (see docs/user-guide/cli/debug.md's asymmetry
// note against vault.* keys, which do set one).
func (s *debugSession) cmdConfigTheme(name string) {
	if !isValidThemeName(name) {
		s.printf("%s\n", s.style(roleWarn, fmt.Sprintf("Unknown theme: %s. Known themes: %s.", name, knownThemeNamesJoined())))
		return
	}

	s.themeName = name
	if s.styler.enabled {
		s.styler.theme = resolveDebugThemeFor(name, s.detectedBackground)
		if s.reader != nil {
			s.reader.SetPrompt(debugPromptString(s))
		}
	}

	// The resolved theme name is a config value (decision 12: no config
	// output styles a value, in any arm), so only the literal label is
	// styled here - never fmt.Sprintf'd together with the value inside
	// one styled span, the way vault.token's value must never be either.
	s.printf("%s %s\n", s.style(roleSuccess, "Theme set to"), s.currentThemeDisplay())
	if !s.styler.enabled {
		s.printf("%s\n", debugColorDisabledNotice)
	}
}

// currentThemeDisplay renders the session's current theme selection for
// `config`/`config theme`: the resolved palette's own name, with an
// " (auto)" suffix when the selection itself is "auto" - "dark (auto)"
// or "light (auto)" depending on what detection found at startup (see
// detectedBackground), "dark (auto)" for any session that never ran
// detection at all. It never touches the styler: displaying the
// preference costs nothing even when color is disabled and no theme is
// resolved at all.
func (s *debugSession) currentThemeDisplay() string {
	resolved := s.styler.theme
	if resolved == nil {
		resolved = resolveDebugThemeFor(s.themeName, s.detectedBackground)
	}
	if s.themeName == themeNameAuto {
		return resolved.name + " (auto)"
	}
	return resolved.name
}

func envOrNotSet(envVar string) string {
	if v := os.Getenv(envVar); v != "" {
		return v
	}
	return "(not set)"
}

// cmdPruneReport implements `prune-report`: the paths --prune/--cherry-pick
// (given as flags to `graft debug`) would remove from the document, computed
// but never applied to `output`/`export`/`history` - see cmdHistory's doc
// comment for why those stay on the pre-(CLI-flag)-prune tree throughout
// stepping. Only meaningful once the session is fully evaluated: a
// --prune/--cherry-pick path is resolved against the final document the same
// way `merge` itself resolves it (after evaluation), so reporting against a
// partially-merged tree could name paths that do not exist yet, or miss ones
// evaluation has not produced.
func (s *debugSession) cmdPruneReport() {
	if !s.loaded {
		s.printf("%s\n", s.style(roleWarn, "No documents loaded. Run 'load' first."))
		return
	}
	if s.step < s.totalSteps {
		s.printf("%s\n", s.style(roleWarn, "Merge not complete yet. Run 'continue' (or enough 'step's) before 'prune-report'."))
		return
	}
	if len(s.opts.Prune) == 0 && len(s.opts.CherryPick) == 0 {
		s.printf("%s\n", s.style(roleMuted, "No --prune/--cherry-pick flags were given for this session."))
		return
	}

	reportOpts := *s.opts
	reportOpts.SkipEval = true // s.tree is already fully evaluated
	pruned, _, err := mergeAllDocs([]YamlFile{{Path: "<current>", Reader: io.NopCloser(strings.NewReader(mustYAML(s.tree)))}}, &reportOpts)
	if err != nil {
		s.printf("%s: %s\n",
			s.style(roleError, "Error computing prune report"),
			ansi.StripEscapes(err.Error()))
		return
	}

	changes, cmpErr := histdiff.Compare("<current>", s.tree, "<pruned>", pruned)
	if cmpErr != nil {
		s.printf("%s: %s\n",
			s.style(roleError, "Error computing prune report"),
			ansi.StripEscapes(cmpErr.Error()))
		return
	}

	removed := make([]histdiff.Change, 0, len(changes))
	for _, c := range changes {
		if c.Kind == histdiff.Removed {
			removed = append(removed, c)
		}
	}
	if len(removed) == 0 {
		s.printf("%s\n", s.style(roleMuted, "--prune/--cherry-pick would not remove any path from the current document."))
		return
	}
	s.printf("%s\n", s.style(roleHeading, "Paths --prune/--cherry-pick would remove (not applied to 'output'/'export'/'history'):"))
	for _, c := range removed {
		s.printf("  - %s\n", s.style(rolePath, c.Path))
	}
}

// cmdOutput implements `output`: the current document state as YAML.
func (s *debugSession) cmdOutput() {
	if !s.loaded {
		s.printf("%s\n", s.style(roleWarn, "No documents loaded. Run 'load' first."))
		return
	}
	raw, err := graft.MarshalYAML(s.tree)
	if err != nil {
		s.printf("%s: %s\n",
			s.style(roleError, "Error rendering document"),
			ansi.StripEscapes(err.Error()))
		return
	}
	s.writeYAML(raw)
}

// cmdDiff implements `diff`: changes from the first loaded file to the
// session's current state.
func (s *debugSession) cmdDiff() {
	if !s.loaded {
		s.printf("%s\n", s.style(roleWarn, "No documents loaded. Run 'load' first."))
		return
	}
	base, _, err := mergeAllDocs(s.freshFiles(1), s.rawMergeOpts())
	if err != nil {
		s.printf("%s: %s\n",
			s.style(roleError, "Error computing diff"),
			ansi.StripEscapes(err.Error()))
		return
	}
	changes, err := histdiff.Compare(s.cached[0].Path, base, "<current>", s.tree)
	if err != nil {
		s.printf("%s: %s\n",
			s.style(roleError, "Error computing diff"),
			ansi.StripEscapes(err.Error()))
		return
	}
	s.printf("%s %s:\n\n", s.style(roleHeading, "Changes from"), s.style(roleFile, s.cached[0].Path))
	for _, c := range changes {
		s.printf("  %s: %s %s %s\n",
			s.style(rolePath, c.Path),
			s.styleChangeValue(roleValueOld, changeOldDisplay(c)),
			s.style(roleMuted, "→"),
			s.styleChangeValue(roleValueNew, changeNewDisplay(c)))
	}
}

// cmdExport implements `export <file>`: writes the current document state
// to file as YAML, or as JSON if file ends in ".json".
func (s *debugSession) cmdExport(path string) {
	if path == "" {
		s.printf("%s\n", s.style(roleWarn, "Usage: export <file>"))
		return
	}
	if !s.loaded {
		s.printf("%s\n", s.style(roleWarn, "No documents loaded. Run 'load' first."))
		return
	}

	var out []byte
	if strings.HasSuffix(path, ".json") {
		jsonBytes, jsonErr := json.MarshalIndent(s.tree, "", "  ")
		if jsonErr != nil {
			s.printf("%s: %s\n",
				s.style(roleError, "Error encoding JSON"),
				ansi.StripEscapes(jsonErr.Error()))
			return
		}
		out = jsonBytes
	} else {
		raw, err := graft.MarshalYAML(s.tree)
		if err != nil {
			s.printf("%s: %s\n",
				s.style(roleError, "Error rendering document"),
				ansi.StripEscapes(err.Error()))
			return
		}
		out = raw
	}

	// #nosec G306 - export output is meant to be readable configuration data
	if err := os.WriteFile(path, out, 0o644); err != nil {
		s.printf("%s %s: %s\n",
			s.style(roleError, "Error writing"),
			s.style(roleFile, path),
			ansi.StripEscapes(err.Error()))
		return
	}
	s.printf("%s %s\n", s.style(roleSuccess, "Exported to"), s.style(roleFile, path))
}

// cmdHelp implements `help`/`help <command>`.
func (s *debugSession) cmdHelp(command string) {
	if command == "" {
		s.printf("%s\n", s.style(roleHeading, "Available commands:"))
		for _, c := range debugCommandHelp {
			// The name is padded to width before styling, not after,
			// so the escape codes wrapping it never count toward the
			// column width and the summary column stays aligned.
			s.printf("  %s %s\n", s.style(roleCommand, fmt.Sprintf("%-15s", c.name)), c.summary)
		}
		return
	}
	for _, c := range debugCommandHelp {
		if c.name == command {
			s.printf("%s\n\n%s\n", c.usage, c.detail)
			return
		}
	}
	s.printf("%s\n", s.style(roleWarn, fmt.Sprintf("No help available for %q.", command)))
}

type debugHelpEntry struct {
	name, summary, usage, detail string
	// arg is what this command's argument is, for Tab completion. A
	// command with no argument, or one nothing can be offered for, is
	// debugArgNone.
	arg debugArgKind
}

// debugArgKind is the kind of thing a REPL command's argument names.
type debugArgKind int

const (
	debugArgNone debugArgKind = iota
	debugArgPath
	debugArgBreakpoint
	debugArgConfigKey
	debugArgFile
	debugArgCommand
)

// debugArgKindFor is the argument kind of the named command, or
// debugArgNone for a command that takes none (or is not a command).
func debugArgKindFor(name string) debugArgKind {
	for _, c := range debugCommandHelp {
		if c.name == name {
			return c.arg
		}
	}
	return debugArgNone
}

// debugCmdAutodefer/debugCmdPruneReport name the two multi-word REPL
// commands used in both the help table below and the debugCommands
// dispatch map, so the two tables cannot drift apart on the spelling.
const (
	debugCmdAutodefer   = "autodefer"
	debugCmdPruneReport = "prune-report"
)

var debugCommandHelp = []debugHelpEntry{
	{"load", "Load all documents", "load", "Parses every input file individually and establishes the first file as the starting document.", debugArgNone},
	{"step", "Execute next merge step", "step", "Merges the next file (or evaluates operators, on the final step).", debugArgNone},
	{"continue", "Run to completion", "continue", "Repeats 'step' until the merge and evaluation are complete or a breakpoint is hit.", debugArgNone},
	{"break", "Set breakpoint on path", "break <path>", "Sets a breakpoint on a path. The debugger reports when this path changes during a later step or continue.\n\nExample:\n  break database.password", debugArgPath},
	{"unbreak", "Remove breakpoint", "unbreak <path>", "Removes a previously set breakpoint.", debugArgBreakpoint},
	{"breaks", "List all breakpoints", "breaks", "Lists every breakpoint currently set.", debugArgNone},
	{"inspect", "Show current value at path", "inspect <path>", "Shows the current (raw or evaluated, depending on progress) value at path.", debugArgPath},
	{"history", "Show change history for path", "history <path>", "Shows the same per-file history 'merge --history' would show for path.", debugArgPath},
	{"tree", "Show tree of the document at path", treeUsage, "Prints a box-drawing tree of the document (or the subtree at path) as of the session's current step, with map keys in cyan, list indices dim, and still-unevaluated (( ... )) operators in yellow. A leading $ or $. on the path is accepted.\n\nFlags:\n  --depth N, -d N   limit display depth, N >= 1 (deeper containers collapse to {N keys}/[N items], hiding annotations beneath them)\n  --keys, -k        structure only, no leaf values (--annotate overrides it)\n  --annotate, -a    inline each node's history entries, up to the current step\n  --history, -H     append per-path history blocks, up to the current step (requires a path)\n  --no-color        plain output for this command\n\nUnlike 'history', which reports the whole run, --annotate/--history stop at the session's current step; each block ends with an 'As of step N' line. History never descends into lists. Tab completion completes the path when it is the first word after tree.\n\nExample:\n  tree database --annotate", debugArgPath},
	{"defer", "Mark path for deferred evaluation", "defer <path>", "Marks path so its operator is left unevaluated (via the real (( defer ... )) operator) when 'step'/'continue' evaluates.", debugArgPath},
	{debugCmdAutodefer, "Defer every failing operator and retry", debugCmdAutodefer, "Runs the same defer-on-error retry loop 'graft merge --defer-on-error'/'--adaptive' uses against the session's current tree: wraps each failing operator in (( defer ... )) and retries to a fixed point, hard-failing on a true cycle with the original error. Composes with paths already deferred via 'defer' - they are protected, not re-attempted - and every path this discovers is added to the session's deferred set too, so 'output'/'export'/'history'/'inspect' all agree afterward. Prints a summary: how many keys were deferred, each with its root-cause reason.", debugArgNone},
	{"eval", "Force evaluate operator at path", "eval <path>", "Immediately evaluates the operator at path, regardless of 'defer' or overall step progress.", debugArgPath},
	{"config", "View/set configuration", "config [key] [value]", "Views or sets a small set of Vault connection settings (vault.addr, vault.token, vault.namespace) for the rest of the session, or the debugger's own color theme (auto, dark, light, mono) via 'config theme [name]'. Vault keys also set the matching environment variable; the theme choice is session-only and touches no environment variable or config file.", debugArgConfigKey},
	{"output", "Show current document state", "output", "Prints the current document state as YAML. Always shows the pre-(--prune/--cherry-pick)-flag tree, even once fully evaluated; see 'prune-report'.", debugArgNone},
	{debugCmdPruneReport, "Show what --prune/--cherry-pick would remove", debugCmdPruneReport, "Once the session is fully evaluated, reports the paths this session's --prune/--cherry-pick flags would remove. Does not change 'output'/'export'/'history', which always show the pre-flag tree.", debugArgNone},
	{"diff", "Show changes from original", "diff", "Shows the changes between the first loaded file and the current state.", debugArgNone},
	{"export", "Export current state to file", "export <file>", "Writes the current document state to file (YAML, or JSON if file ends in .json).", debugArgFile},
	{"help", "Show help", "help [command]", "Lists every command, or shows detailed help for one command.", debugArgCommand},
	{"quit", "Exit the debugger", "quit", "Exits the debugger without exporting.", debugArgNone},
}

// noneDisplay is the placeholder shown for a side of a change that does
// not exist, matching docs/user-guide/cli/debug.md's "<none> → true"
// convention.
const noneDisplay = "<none>"

// changeOldDisplay/changeNewDisplay render a histdiff.Change's Old/New side
// as the literal text "<none>" when that side is absent (an Added or
// Removed change), matching docs/user-guide/cli/debug.md's
// "<none> → true" convention. Both return an already-rendered display
// string (not a value to pass through inlineValue again).
func changeOldDisplay(c histdiff.Change) string {
	if c.Kind == histdiff.Added {
		return noneDisplay
	}
	return inlineValue(c.Old)
}

func changeNewDisplay(c histdiff.Change) string {
	if c.Kind == histdiff.Removed {
		return noneDisplay
	}
	return inlineValue(c.New)
}

// styleChangeValue renders an already-rendered change-line display
// string (changeOldDisplay/changeNewDisplay's return value) in role,
// except the literal "<none>" placeholder, which always renders
// roleMuted regardless of which side of the change it is on (Category
// E, plans/debugger-colorizing.md).
func (s *debugSession) styleChangeValue(role debugRole, display string) string {
	if display == noneDisplay {
		return s.style(roleMuted, display)
	}
	return s.style(role, display)
}

// lookupDottedPath resolves a dot-joined path (optionally with "[N]" index
// segments, e.g. "list[0].name") against tree. An empty path returns tree
// itself.
func lookupDottedPath(tree map[string]interface{}, path string) (interface{}, bool) {
	if path == "" {
		return tree, true
	}

	var current interface{} = tree
	for _, seg := range splitDebugPath(path) {
		if idx, isIdx := parseIndexSegment(seg); isIdx {
			arr, ok := current.([]interface{})
			if !ok || idx < 0 || idx >= len(arr) {
				return nil, false
			}
			current = arr[idx]
			continue
		}
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil, false
		}
		v, exists := m[seg]
		if !exists {
			return nil, false
		}
		current = v
	}
	return current, true
}

// setDottedPath writes value at path within tree, creating no new
// intermediate structure (the path must already resolve, as it always does
// for cmdEval's use - a path just read via lookupDottedPath).
func setDottedPath(tree map[string]interface{}, path string, value interface{}) {
	segs := splitDebugPath(path)
	if len(segs) == 0 {
		return
	}
	var current interface{} = tree
	for _, seg := range segs[:len(segs)-1] {
		if idx, isIdx := parseIndexSegment(seg); isIdx {
			arr, ok := current.([]interface{})
			if !ok || idx < 0 || idx >= len(arr) {
				return
			}
			current = arr[idx]
			continue
		}
		m, ok := current.(map[string]interface{})
		if !ok {
			return
		}
		current = m[seg]
	}
	last := segs[len(segs)-1]
	if idx, isIdx := parseIndexSegment(last); isIdx {
		if arr, ok := current.([]interface{}); ok && idx >= 0 && idx < len(arr) {
			arr[idx] = value
		}
		return
	}
	if m, ok := current.(map[string]interface{}); ok {
		m[last] = value
	}
}

func splitDebugPath(path string) []string {
	raw := strings.Split(path, ".")
	segs := make([]string, 0, len(raw))
	for _, r := range raw {
		if r != "" {
			segs = append(segs, r)
		}
	}
	return segs
}

func parseIndexSegment(seg string) (int, bool) {
	if !strings.HasPrefix(seg, "[") || !strings.HasSuffix(seg, "]") {
		return 0, false
	}
	n, err := strconv.Atoi(seg[1 : len(seg)-1])
	if err != nil {
		return 0, false
	}
	return n, true
}

// applyDeferredWrapping returns a deep copy of tree with every deferred
// path whose current value is an unevaluated "(( op ... ))" expression
// rewritten to "(( defer op ... ))" (see stepOnce's doc comment). Paths not
// present, or not currently an operator expression, are left alone. Only
// deferred's keys matter here (each path's mapped reason is display-only,
// surfaced by cmdInspect).
func applyDeferredWrapping(tree map[string]interface{}, deferred map[string]string) map[string]interface{} {
	if len(deferred) == 0 {
		return tree
	}
	out := deepCopyTree(tree)
	for path := range deferred {
		value, ok := lookupDottedPath(out, path)
		if !ok {
			continue
		}
		s, isStr := value.(string)
		if !isStr {
			continue
		}
		if wrapped, changed := deferWrapIfOperator(s); changed {
			setDottedPath(out, path, wrapped)
		}
	}
	return out
}

// deferWrapIfOperator rewrites s to "(( defer <inner> ))" when s is an
// unevaluated "(( ... ))" operator call not already wrapped in defer,
// reporting changed=true. Any other string (already defer-wrapped, or
// not an operator call at all - ordinary data) is returned unchanged
// with changed=false. Shared by applyDeferredWrapping's path-keyed
// rewrite above (a human-chosen set of paths, via graft debug's
// cmdDefer) and adaptive_merge.go's deferAllUnevaluatedOperators (every
// still-unevaluated-looking leaf in a tree, via graft merge
// --defer-on-error's retry loop) - both need the exact same "is this
// text an operator call, and if so what do I wrap it as" decision.
func deferWrapIfOperator(s string) (wrapped string, changed bool) {
	trimmed := strings.TrimSpace(s)
	if !strings.HasPrefix(trimmed, "((") || !strings.HasSuffix(trimmed, "))") {
		return s, false
	}
	inner := strings.TrimSpace(trimmed[2 : len(trimmed)-2])
	if strings.HasPrefix(inner, "defer ") {
		return s, false // already deferred
	}
	return "(( defer " + inner + " ))", true
}

func deepCopyTree(v interface{}) map[string]interface{} {
	raw, err := graft.MarshalYAML(v)
	if err != nil {
		if m, ok := v.(map[string]interface{}); ok {
			return m
		}
		return map[string]interface{}{}
	}
	data, err := parseYAML(raw)
	if err != nil {
		if m, ok := v.(map[string]interface{}); ok {
			return m
		}
		return map[string]interface{}{}
	}
	return data
}

func mustYAML(v interface{}) string {
	raw, err := graft.MarshalYAML(v)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

// handleDebug runs the `graft debug` REPL: a line-oriented command loop
// reading from in and writing to out, implementing the commands
// docs/user-guide/cli/debug.md documents (see debugCommandHelp). ui
// carries the session's color/theme choice, resolved by the caller
// (both `graft debug` and `graft merge --interactive`'s RunE closures)
// from --color/--no-color and, from a later phase, --theme/GRAFT_THEME.
func handleDebug(files []string, opts *mergeOpts, in io.Reader, out io.Writer, ui debugUIOptions) int {
	if len(files) == 0 {
		// Unlike merge/fan/json/vaultinfo, debug can't fall back to reading
		// a document from stdin: stdin is the REPL's own command input
		// stream. resolveMergeInputFiles' stdin-fallback error text talks
		// about piping YAML data, which is the wrong frame here.
		log.PrintStdErrf("%s\n", ansi.Sprintf("@R{Missing Input}: graft debug requires at least one file (e.g. %s)", "graft debug base.yml overlay.yml"))
		return 1
	}

	// Background auto-detection, if it runs at all, happens here: before
	// the session (and its banner) exist, and before newDebugLineReader
	// below constructs the readline instance, so no redraw can
	// interleave with the query (see withDetectedBackground).
	ui = withDetectedBackground(ui, in, out)

	sess, err := newDebugSession(files, opts, out, ui)
	if err != nil {
		log.PrintStdErrf("%s\n", err.Error())
		return 2
	}

	sess.printf("%s\n%s\n\n",
		sess.style(roleBanner, "Welcome to the Graft Debugger"),
		sess.style(roleMuted, "Type 'help' for available commands."))

	reader := newDebugLineReader(in, out, debugPromptString(sess), &debugCompleter{sess: sess})
	sess.reader = reader
	defer func() { _ = reader.Close() }()

	for {
		raw, err := reader.ReadLine()
		if err != nil {
			// Ctrl+C abandons the line in hand, nothing more.
			if errors.Is(err, errREPLInterrupted) {
				continue
			}
			if errors.Is(err, io.EOF) {
				return 0
			}
			log.PrintStdErrf("%s\n", ansi.Sprintf("@R{Error reading debugger input}: %s", err.Error()))
			return 2
		}
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		reader.SaveHistory(line)

		fields := strings.Fields(line)
		cmd, args := fields[0], fields[1:]

		if cmd == "quit" || cmd == "exit" {
			return 0
		}

		run, known := debugCommands[cmd]
		if !known {
			sess.printf("%s\n", sess.style(roleWarn, fmt.Sprintf("Unknown command: %s. Type 'help' for available commands.", cmd)))
			continue
		}
		run(sess, args)
	}
}

// debugPromptString builds the REPL prompt: rolePrompt applied to the
// literal "graft>", with the trailing space left outside the styled
// span so mono's reverse video never paints a floating block (Prompt
// and Input Contrast, plans/debugger-colorizing.md). Plain-mode bytes
// are exactly "graft> ", unchanged from before this phase.
func debugPromptString(s *debugSession) string {
	return s.style(rolePrompt, "graft>") + " "
}

// debugCommands maps each REPL command to the session method that runs
// it, less "quit"/"exit", which end the loop itself. Most commands take
// the rest of the line as a single free-form argument - a path, a
// pattern, a filename - so they join their fields back together; only
// config and tree read their arguments as separate words.
var debugCommands = map[string]func(sess *debugSession, args []string){
	"load":              func(sess *debugSession, _ []string) { sess.cmdLoad() },
	"step":              func(sess *debugSession, _ []string) { sess.cmdStep() },
	"continue":          func(sess *debugSession, _ []string) { sess.cmdContinue() },
	"break":             func(sess *debugSession, args []string) { sess.cmdBreak(strings.Join(args, " ")) },
	"unbreak":           func(sess *debugSession, args []string) { sess.cmdUnbreak(strings.Join(args, " ")) },
	"breaks":            func(sess *debugSession, _ []string) { sess.cmdBreaks() },
	"inspect":           func(sess *debugSession, args []string) { sess.cmdInspect(strings.Join(args, " ")) },
	"history":           func(sess *debugSession, args []string) { sess.cmdHistory(strings.Join(args, " ")) },
	"tree":              func(sess *debugSession, args []string) { sess.cmdTree(args) },
	"defer":             func(sess *debugSession, args []string) { sess.cmdDefer(strings.Join(args, " ")) },
	debugCmdAutodefer:   func(sess *debugSession, _ []string) { sess.cmdAutodefer() },
	"eval":              func(sess *debugSession, args []string) { sess.cmdEval(strings.Join(args, " ")) },
	"config":            func(sess *debugSession, args []string) { sess.cmdConfig(args) },
	"output":            func(sess *debugSession, _ []string) { sess.cmdOutput() },
	debugCmdPruneReport: func(sess *debugSession, _ []string) { sess.cmdPruneReport() },
	"diff":              func(sess *debugSession, _ []string) { sess.cmdDiff() },
	"export":            func(sess *debugSession, args []string) { sess.cmdExport(strings.Join(args, " ")) },
	"help":              func(sess *debugSession, args []string) { sess.cmdHelp(strings.Join(args, " ")) },
}
