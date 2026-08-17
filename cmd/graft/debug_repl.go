package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/fivetwenty-io/graft/internal/histdiff"
	"github.com/fivetwenty-io/graft/internal/history"
	"github.com/fivetwenty-io/graft/internal/utils/ansi"
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
// REPL has no hook into internal/pools' client cache to force a rebuild.
// (NATS re-reads its environment on every operator evaluation, so it would
// not have that caveat; Vault, which pools its client, does.)
var debugConfigKeys = map[string]string{
	"vault.addr":      "VAULT_ADDR",
	"vault.token":     "VAULT_TOKEN",
	"vault.namespace": "VAULT_NAMESPACE",
}

// debugConfigKeyOrder is debugConfigKeys' display order for a bare `config`.
var debugConfigKeyOrder = []string{"vault.addr", "vault.token", "vault.namespace"}

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
	deferred    map[string]bool

	out io.Writer
}

// newDebugSession loads and caches every input file's raw bytes (without
// merging), matching the REPL's `load` command: files are read once up
// front so later replays (step/continue/eval all re-run mergeAllDocs on
// fresh YamlFile readers) don't depend on any reader being seekable or
// reusable.
func newDebugSession(files []string, opts *mergeOpts, out io.Writer) (*debugSession, error) {
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
		cached:      cached,
		opts:        opts,
		totalSteps:  total,
		breakpoints: map[string]bool{},
		deferred:    map[string]bool{},
		out:         out,
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
	s.printf("Loaded %s:\n", pluralCount(len(s.cached), "document"))
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
		s.printf("  [%d] %s (%s)\n", i, c.Path, pluralCount(keyCount, "key"))
	}

	base, _, err := mergeAllDocs(s.freshFiles(1), s.rawMergeOpts())
	if err != nil {
		s.printf("%s\n", ansi.Sprintf("@R{Error loading} @m{%s}: %s", s.cached[0].Path, err.Error()))
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
// stepOnce's third return value, failed, is F19's fix: a failed merge or
// evaluation prints its error and rewinds s.step (so a subsequent manual
// `step` retries the same step, matching the pre-fix behavior plain `step`
// already had), but the caller must know a failure happened rather than
// inferring it from the step counter - the counter alone is ambiguous
// (rewound-after-failure looks identical to "hasn't started yet") and
// cmdContinue's loop condition (`s.step < s.totalSteps`) doesn't change
// when a step fails, so without an explicit failure signal it would retry
// and re-fail the same step forever.
func (s *debugSession) stepOnce() (hitBreakpoint bool, hitPath string, failed bool) {
	s.step++

	if s.step < s.totalSteps {
		fileIdx := s.step
		s.printf("[%d/%d] Merging %s...\n", s.step, s.totalSteps, s.cached[fileIdx].Path)

		newTree, _, err := mergeAllDocs(s.freshFiles(fileIdx+1), s.rawMergeOpts())
		if err != nil {
			s.printf("%s\n", ansi.Sprintf("@R{Merge failed}: %s", err.Error()))
			s.step--
			return false, "", true
		}

		changes, cmpErr := histdiff.Compare("before", s.tree, s.cached[fileIdx].Path, newTree)
		if cmpErr == nil {
			for _, c := range changes {
				s.printf("  %s: %s → %s\n", c.Path, changeOldDisplay(c), changeNewDisplay(c))
			}
		}
		s.tree = newTree

		for _, c := range changes {
			if s.breakpoints[c.Path] {
				s.printf("Breakpoint hit: %s\n  Current: %s\n", c.Path, changeNewDisplay(c))
				return true, c.Path, false
			}
		}
		return false, "", false
	}

	// Final step: evaluate. Deferred paths are protected by rewriting their
	// still-unevaluated "(( op ... ))" value to "(( defer op ... ))" (the
	// real spruce-compatible defer operator, pkg/graft/operators/op_defer.go)
	// before evaluation, so the evaluator reconstructs the literal
	// expression instead of resolving it - the same mechanism a hand-authored
	// "(( defer ... ))" in the source YAML would trigger, not a REPL-only
	// simulation of deferral.
	s.printf("[%d/%d] Evaluating operators...\n", s.step, s.totalSteps)

	deferredTree := applyDeferredWrapping(s.tree, s.deferred)
	evalOpts := *s.opts
	evalOpts.SkipEval = false
	evalOpts.Prune = nil
	evalOpts.CherryPick = nil
	evaluated, _, err := mergeAllDocs([]YamlFile{{Path: "<merged>", Reader: io.NopCloser(strings.NewReader(mustYAML(deferredTree)))}}, &evalOpts)
	if err != nil {
		s.printf("%s\n", ansi.Sprintf("@R{Evaluation failed}: %s", err.Error()))
		s.step--
		return false, "", true
	}

	changes, cmpErr := histdiff.Compare("<merged>", s.tree, "<evaluated>", evaluated)
	s.tree = evaluated

	if cmpErr == nil {
		for _, c := range changes {
			if s.breakpoints[c.Path] {
				s.printf("Breakpoint hit: %s\n  Current: %s\n", c.Path, changeNewDisplay(c))
				return true, c.Path, false
			}
		}
	}
	return false, "", false
}

// cmdStep implements the `step` REPL command.
func (s *debugSession) cmdStep() {
	if !s.loaded {
		s.printf("No documents loaded. Run 'load' first.\n")
		return
	}
	if s.step >= s.totalSteps {
		s.printf("Merge complete. Nothing more to step.\n")
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
		s.printf("No documents loaded. Run 'load' first.\n")
		return
	}
	if s.step >= s.totalSteps {
		s.printf("Merge complete. Nothing more to run.\n")
		return
	}
	for s.step < s.totalSteps {
		hit, _, failed := s.stepOnce()
		if hit || failed {
			return
		}
	}
	s.printf("Evaluation complete.\n")
}

// cmdBreak/cmdUnbreak/cmdBreaks implement `break <path>`, `unbreak <path>`,
// and `breaks`.
func (s *debugSession) cmdBreak(path string) {
	if path == "" {
		s.printf("Usage: break <path>\n")
		return
	}
	s.breakpoints[path] = true
	s.printf("Breakpoint set on %s\n", path)
}

func (s *debugSession) cmdUnbreak(path string) {
	if path == "" {
		s.printf("Usage: unbreak <path>\n")
		return
	}
	if !s.breakpoints[path] {
		s.printf("No breakpoint on %s\n", path)
		return
	}
	delete(s.breakpoints, path)
	s.printf("Breakpoint removed\n")
}

func (s *debugSession) cmdBreaks() {
	if len(s.breakpoints) == 0 {
		s.printf("No breakpoints set.\n")
		return
	}
	paths := make([]string, 0, len(s.breakpoints))
	for p := range s.breakpoints {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	s.printf("Breakpoints:\n")
	for _, p := range paths {
		s.printf("  - %s\n", p)
	}
}

// cmdInspect implements `inspect [path]`: the current value at path (the
// whole document if path is empty), from whatever phase the session is
// currently in (raw merge or evaluated).
func (s *debugSession) cmdInspect(path string) {
	if !s.loaded {
		s.printf("No documents loaded. Run 'load' first.\n")
		return
	}
	value, ok := lookupDottedPath(s.tree, path)
	if !ok {
		s.printf("Path not found: %s\n", path)
		return
	}
	raw, err := graft.MarshalYAML(value)
	if err != nil {
		s.printf("%s\n", ansi.Sprintf("@R{Error rendering value}: %s", err.Error()))
		return
	}
	s.out.Write(raw) //nolint:errcheck // best-effort REPL output
}

// cmdHistory implements `history <path>`: the same per-path entry list
// `merge --history` would show for path, computed from the debug session's
// files under the same opts.
func (s *debugSession) cmdHistory(path string) {
	if path == "" {
		s.printf("Usage: history <path>\n")
		return
	}

	fileOpts := *s.opts
	fileOpts.Files = make([]string, len(s.cached))
	for i, c := range s.cached {
		fileOpts.Files[i] = c.Path
	}
	// buildMergeHistorySteps re-resolves files from opts.Files by path, not
	// from s.cached's in-memory bytes; this matches a plain `graft debug`
	// invocation's files still being present on disk (the same assumption
	// resolveMergeInputFiles already makes for stdin-less invocations).
	steps, _, err := buildMergeHistorySteps(&fileOpts)
	if err != nil {
		s.printf("%s\n", ansi.Sprintf("@R{Error computing history}: %s", err.Error()))
		return
	}
	all, err := history.Track(steps)
	if err != nil {
		s.printf("%s\n", ansi.Sprintf("@R{Error computing history}: %s", err.Error()))
		return
	}
	ph, found := findPathHistory(all, path)
	if !found {
		s.printf("No history recorded for path: %s\n", path)
		return
	}
	var buf strings.Builder
	fmt.Fprintf(&buf, "%s:\n", ph.Path)
	for _, e := range ph.Entries {
		writeHistoryEntryLine(&buf, e)
	}
	writeHistoryFinalLine(&buf, ph, len(ph.Entries) == 1)
	s.out.Write([]byte(buf.String())) //nolint:errcheck // best-effort REPL output
}

// cmdDefer implements `defer <path>`.
func (s *debugSession) cmdDefer(path string) {
	if path == "" {
		s.printf("Usage: defer <path>\n")
		return
	}
	s.deferred[path] = true
	s.printf("Marked %s for deferred evaluation\n", path)
}

// cmdEval implements `eval <path>`: forces immediate evaluation of the
// operator at path (even if it was marked `defer`), independent of the
// session's overall step progress, and updates the session's tree with the
// resolved value at that path.
func (s *debugSession) cmdEval(path string) {
	if !s.loaded {
		s.printf("No documents loaded. Run 'load' first.\n")
		return
	}
	value, ok := lookupDottedPath(s.tree, path)
	if !ok {
		s.printf("Path not found: %s\n", path)
		return
	}
	s.printf("Evaluating: %s\n", inlineValue(value))

	evalOpts := *s.opts
	evalOpts.SkipEval = false
	evalOpts.CherryPick = []string{path}
	evalOpts.Prune = nil
	result, _, err := mergeAllDocs([]YamlFile{{Path: "<merged>", Reader: io.NopCloser(strings.NewReader(mustYAML(s.tree)))}}, &evalOpts)
	if err != nil {
		s.printf("%s\n", ansi.Sprintf("@R{Evaluation failed}: %s", err.Error()))
		return
	}
	resolved, ok := lookupDottedPath(result, path)
	if !ok {
		s.printf("%s\n", ansi.Sprintf("@R{Evaluation did not produce a value at} @m{%s}", path))
		return
	}
	s.printf("Result: %s\n", inlineValue(resolved))
	setDottedPath(s.tree, path, resolved)
}

// cmdConfig implements `config`/`config <key>`/`config <key> <value>`; see
// debugConfigKeys' doc comment for scope.
func (s *debugSession) cmdConfig(args []string) {
	switch len(args) {
	case 0:
		for _, key := range debugConfigKeyOrder {
			s.printf("%s: %s\n", key, envOrNotSet(debugConfigKeys[key]))
		}
	case 1:
		envVar, known := debugConfigKeys[args[0]]
		if !known {
			s.printf("Unknown config key: %s. Known keys: %s\n", args[0], strings.Join(debugConfigKeyOrder, ", "))
			return
		}
		s.printf("Current: %s\n", envOrNotSet(envVar))
	default:
		envVar, known := debugConfigKeys[args[0]]
		if !known {
			s.printf("Unknown config key: %s. Known keys: %s\n", args[0], strings.Join(debugConfigKeyOrder, ", "))
			return
		}
		_ = os.Setenv(envVar, strings.Join(args[1:], " "))
		s.printf("Updated %s\n", args[0])
	}
}

func envOrNotSet(envVar string) string {
	if v := os.Getenv(envVar); v != "" {
		return v
	}
	return "(not set)"
}

// cmdOutput implements `output`: the current document state as YAML.
func (s *debugSession) cmdOutput() {
	if !s.loaded {
		s.printf("No documents loaded. Run 'load' first.\n")
		return
	}
	raw, err := graft.MarshalYAML(s.tree)
	if err != nil {
		s.printf("%s\n", ansi.Sprintf("@R{Error rendering document}: %s", err.Error()))
		return
	}
	s.out.Write(raw) //nolint:errcheck // best-effort REPL output
}

// cmdDiff implements `diff`: changes from the first loaded file to the
// session's current state.
func (s *debugSession) cmdDiff() {
	if !s.loaded {
		s.printf("No documents loaded. Run 'load' first.\n")
		return
	}
	base, _, err := mergeAllDocs(s.freshFiles(1), s.rawMergeOpts())
	if err != nil {
		s.printf("%s\n", ansi.Sprintf("@R{Error computing diff}: %s", err.Error()))
		return
	}
	changes, err := histdiff.Compare(s.cached[0].Path, base, "<current>", s.tree)
	if err != nil {
		s.printf("%s\n", ansi.Sprintf("@R{Error computing diff}: %s", err.Error()))
		return
	}
	s.printf("Changes from %s:\n\n", s.cached[0].Path)
	for _, c := range changes {
		s.printf("  %s: %s → %s\n", c.Path, changeOldDisplay(c), changeNewDisplay(c))
	}
}

// cmdExport implements `export <file>`: writes the current document state
// to file as YAML, or as JSON if file ends in ".json".
func (s *debugSession) cmdExport(path string) {
	if path == "" {
		s.printf("Usage: export <file>\n")
		return
	}
	if !s.loaded {
		s.printf("No documents loaded. Run 'load' first.\n")
		return
	}

	var out []byte
	if strings.HasSuffix(path, ".json") {
		jsonBytes, jsonErr := json.MarshalIndent(s.tree, "", "  ")
		if jsonErr != nil {
			s.printf("%s\n", ansi.Sprintf("@R{Error encoding JSON}: %s", jsonErr.Error()))
			return
		}
		out = jsonBytes
	} else {
		raw, err := graft.MarshalYAML(s.tree)
		if err != nil {
			s.printf("%s\n", ansi.Sprintf("@R{Error rendering document}: %s", err.Error()))
			return
		}
		out = raw
	}

	// #nosec G306 - export output is meant to be readable configuration data
	if err := os.WriteFile(path, out, 0o644); err != nil {
		s.printf("%s\n", ansi.Sprintf("@R{Error writing} @m{%s}: %s", path, err.Error()))
		return
	}
	s.printf("Exported to %s\n", path)
}

// cmdHelp implements `help`/`help <command>`.
func (s *debugSession) cmdHelp(command string) {
	if command == "" {
		s.printf("Available commands:\n")
		for _, c := range debugCommandHelp {
			s.printf("  %-15s %s\n", c.name, c.summary)
		}
		return
	}
	for _, c := range debugCommandHelp {
		if c.name == command {
			s.printf("%s\n\n%s\n", c.usage, c.detail)
			return
		}
	}
	s.printf("No help available for %q.\n", command)
}

type debugHelpEntry struct {
	name, summary, usage, detail string
}

var debugCommandHelp = []debugHelpEntry{
	{"load", "Load all documents", "load", "Parses every input file individually and establishes the first file as the starting document."},
	{"step", "Execute next merge step", "step", "Merges the next file (or evaluates operators, on the final step)."},
	{"continue", "Run to completion", "continue", "Repeats 'step' until the merge and evaluation are complete or a breakpoint is hit."},
	{"break", "Set breakpoint on path", "break <path>", "Sets a breakpoint on a path. The debugger reports when this path changes during a later step or continue.\n\nExample:\n  break database.password"},
	{"unbreak", "Remove breakpoint", "unbreak <path>", "Removes a previously set breakpoint."},
	{"breaks", "List all breakpoints", "breaks", "Lists every breakpoint currently set."},
	{"inspect", "Show current value at path", "inspect <path>", "Shows the current (raw or evaluated, depending on progress) value at path."},
	{"history", "Show change history for path", "history <path>", "Shows the same per-file history 'merge --history' would show for path."},
	{"defer", "Mark path for deferred evaluation", "defer <path>", "Marks path so its operator is left unevaluated (via the real (( defer ... )) operator) when 'step'/'continue' evaluates."},
	{"eval", "Force evaluate operator at path", "eval <path>", "Immediately evaluates the operator at path, regardless of 'defer' or overall step progress."},
	{"config", "View/set configuration", "config [key] [value]", "Views or sets a small set of Vault connection settings (vault.addr, vault.token, vault.namespace) for the rest of the session."},
	{"output", "Show current document state", "output", "Prints the current document state as YAML."},
	{"diff", "Show changes from original", "diff", "Shows the changes between the first loaded file and the current state."},
	{"export", "Export current state to file", "export <file>", "Writes the current document state to file (YAML, or JSON if file ends in .json)."},
	{"help", "Show help", "help [command]", "Lists every command, or shows detailed help for one command."},
	{"quit", "Exit the debugger", "quit", "Exits the debugger without exporting."},
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
// present, or not currently an operator expression, are left alone.
func applyDeferredWrapping(tree map[string]interface{}, deferred map[string]bool) map[string]interface{} {
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
		trimmed := strings.TrimSpace(s)
		if !strings.HasPrefix(trimmed, "((") || !strings.HasSuffix(trimmed, "))") {
			continue
		}
		inner := strings.TrimSpace(trimmed[2 : len(trimmed)-2])
		if strings.HasPrefix(inner, "defer ") {
			continue // already deferred
		}
		setDottedPath(out, path, "(( defer "+inner+" ))")
	}
	return out
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
// docs/user-guide/cli/debug.md documents (see debugCommandHelp).
func handleDebug(files []string, opts *mergeOpts, in io.Reader, out io.Writer) int {
	if len(files) == 0 {
		// Unlike merge/fan/json/vaultinfo, debug can't fall back to reading
		// a document from stdin: stdin is the REPL's own command input
		// stream. resolveMergeInputFiles' stdin-fallback error text talks
		// about piping YAML data, which is the wrong frame here.
		log.PrintStdErrf("%s\n", ansi.Sprintf("@R{Missing Input}: graft debug requires at least one file (e.g. %s)", "graft debug base.yml overlay.yml"))
		return 1
	}

	sess, err := newDebugSession(files, opts, out)
	if err != nil {
		log.PrintStdErrf("%s\n", err.Error())
		return 2
	}

	_, _ = fmt.Fprintf(out, "Welcome to the Graft Debugger\nType 'help' for available commands.\n\n")

	scanner := bufio.NewScanner(in)
	for {
		_, _ = fmt.Fprint(out, "graft> ")
		if !scanner.Scan() {
			_, _ = fmt.Fprintln(out)
			// Scan also stops on a read error or a line longer than
			// bufio.Scanner's 64KiB buffer; without this check that is
			// indistinguishable from a clean EOF and every remaining
			// command is silently dropped.
			if err := scanner.Err(); err != nil {
				log.PrintStdErrf("%s\n", ansi.Sprintf("@R{Error reading debugger input}: %s", err.Error()))
				return 2
			}
			return 0
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		cmd, args := fields[0], fields[1:]

		switch cmd {
		case "load":
			sess.cmdLoad()
		case "step":
			sess.cmdStep()
		case "continue":
			sess.cmdContinue()
		case "break":
			sess.cmdBreak(strings.Join(args, " "))
		case "unbreak":
			sess.cmdUnbreak(strings.Join(args, " "))
		case "breaks":
			sess.cmdBreaks()
		case "inspect":
			sess.cmdInspect(strings.Join(args, " "))
		case "history":
			sess.cmdHistory(strings.Join(args, " "))
		case "defer":
			sess.cmdDefer(strings.Join(args, " "))
		case "eval":
			sess.cmdEval(strings.Join(args, " "))
		case "config":
			sess.cmdConfig(args)
		case "output":
			sess.cmdOutput()
		case "diff":
			sess.cmdDiff()
		case "export":
			sess.cmdExport(strings.Join(args, " "))
		case "help":
			sess.cmdHelp(strings.Join(args, " "))
		case "quit", "exit":
			return 0
		default:
			_, _ = fmt.Fprintf(out, "Unknown command: %s. Type 'help' for available commands.\n", cmd)
		}
	}
}
