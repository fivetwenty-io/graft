package main

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/fivetwenty-io/graft/internal/history"
	"github.com/fivetwenty-io/graft/internal/utils/ansi"
)

// treeUsage is the one-line usage string cmdTree prints after a flag
// parsing error (and for -h/--help), and the usage column of tree's
// help entry.
const treeUsage = "tree [path] [--depth|-d N] [--keys|-k] [--annotate|-a] [--history|-H] [--no-color]"

// treeOptions is the parsed form of one `tree` REPL command line.
type treeOptions struct {
	path string
	// depth limits how many levels below the root are expanded; 0 means
	// unlimited (the flag itself requires N >= 1). Containers at the
	// cutoff collapse to a "{N keys}"/"[N items]" summary.
	depth       int
	keysOnly    bool
	annotate    bool
	historyList bool
	noColor     bool
	help        bool
}

// treeBoolFlags maps each boolean flag spelling to its effect on the
// parsed options. --history's short form is a capital -H because -h
// reads as help everywhere else in graft (cobra auto-registers it on
// every CLI command); --annotate keeps the house-convention lowercase
// -a, which nothing conflicts with.
var treeBoolFlags = map[string]func(*treeOptions){
	"--keys":     func(o *treeOptions) { o.keysOnly = true },
	"-k":         func(o *treeOptions) { o.keysOnly = true },
	"--annotate": func(o *treeOptions) { o.annotate = true },
	"-a":         func(o *treeOptions) { o.annotate = true },
	"--history":  func(o *treeOptions) { o.historyList = true },
	"-H":         func(o *treeOptions) { o.historyList = true },
	"--no-color": func(o *treeOptions) { o.noColor = true },
	"--help":     func(o *treeOptions) { o.help = true },
	"-h":         func(o *treeOptions) { o.help = true },
}

// parseTreeArgs parses `tree`'s REPL arguments. Non-flag tokens are
// joined with single spaces to form the path (matching how every other
// REPL command treats the rest of its line as one free-form argument).
func parseTreeArgs(args []string) (treeOptions, error) {
	var opts treeOptions
	var pathParts []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case treeBoolFlags[arg] != nil:
			treeBoolFlags[arg](&opts)
		case arg == "--depth" || arg == "-d":
			val, ok := treeArgAt(args, i)
			if !ok {
				return opts, fmt.Errorf("--depth requires a number")
			}
			n, err := parseTreeDepth(val)
			if err != nil {
				return opts, err
			}
			opts.depth = n
			i++
		case strings.HasPrefix(arg, "--depth=") || strings.HasPrefix(arg, "-d="):
			n, err := parseTreeDepth(arg[strings.Index(arg, "=")+1:])
			if err != nil {
				return opts, err
			}
			opts.depth = n
		case strings.HasPrefix(arg, "-"):
			return opts, fmt.Errorf("unknown flag %q", arg)
		default:
			pathParts = append(pathParts, arg)
		}
	}
	opts.path = normalizeTreePath(strings.Join(pathParts, " "))
	if opts.annotate {
		// Annotation is about values; it overrides --keys.
		opts.keysOnly = false
	}
	if opts.historyList && opts.path == "" && !opts.help {
		return opts, fmt.Errorf("--history requires a path (it prints one block per tracked path)")
	}
	return opts, nil
}

// treeArgAt returns args[i+1] (the value following a flag at index i),
// or false if there is no next argument.
func treeArgAt(args []string, i int) (string, bool) {
	if i+1 >= len(args) {
		return "", false
	}
	return args[i+1], true
}

// parseTreeDepth parses a --depth value; N must be at least 1 (depth 0
// is the internal spelling of "unlimited", which is the default).
func parseTreeDepth(raw string) (int, error) {
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 0, fmt.Errorf("--depth requires a positive number, got %q", raw)
	}
	return n, nil
}

// normalizeTreePath strips an optional leading "$"/"$." so paths copied
// from other debugger output (autodefer prints $.database.password)
// paste directly.
func normalizeTreePath(path string) string {
	if path == "$" {
		return ""
	}
	return strings.TrimPrefix(path, "$.")
}

// renderDebugTree renders value (the subtree opts.path resolves to) as a
// box-drawing tree, through st (the session's styler, or an identity
// override for --no-color - see cmdTree). ann, when non-nil, maps
// internal/history flattened paths (historyKeyForPath's spelling) to
// that path's entries; each node whose path has entries gets them
// printed roleMuted and indented beneath it.
func renderDebugTree(value interface{}, opts treeOptions, ann map[string][]history.Entry, st debugStyler) string {
	var buf strings.Builder
	label := opts.path
	if label == "" {
		label = "$"
	}
	// A root inside a list has no history path of its own; treating the
	// truncated key as valid would borrow the enclosing list's entries.
	rootKey, insideList := historyKeyForPath(opts.path)
	rootOK := !insideList

	if isTreeContainer(value) {
		buf.WriteString(st.apply(rolePath, label) + "\n")
		writeTreeAnnotations(&buf, "", ann, rootKey, rootOK, st)
		writeTreeChildren(&buf, "", value, 1, opts, ann, rootKey, rootOK, st)
		return buf.String()
	}

	if opts.keysOnly {
		buf.WriteString(st.apply(rolePath, label) + "\n")
	} else {
		buf.WriteString(st.apply(rolePath, label) + ": " + treeValueDisplay(value, st) + "\n")
	}
	writeTreeAnnotations(&buf, "", ann, rootKey, rootOK, st)
	return buf.String()
}

// writeTreeChildren writes one container's children (sorted keys for a
// map, index order for a list), recursing through writeTreeEntry.
func writeTreeChildren(buf *strings.Builder, prefix string, value interface{}, depth int, opts treeOptions, ann map[string][]history.Entry, histKey string, histOK bool, st debugStyler) {
	switch v := value.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for i, k := range keys {
			writeTreeEntry(buf, prefix, st.apply(rolePath, k), v[k], i == len(keys)-1, depth, opts, ann, joinHistKey(histKey, k), histOK, st)
		}
	case []interface{}:
		for i, item := range v {
			// History never descends into lists, so no path below this
			// point has entries; histOK false suppresses every lookup
			// beneath (a lookup here could otherwise collide with an
			// unrelated top-level path's key).
			writeTreeEntry(buf, prefix, st.apply(roleCounter, fmt.Sprintf("[%d]", i)), item, i == len(v)-1, depth, opts, ann, "", false, st)
		}
	}
}

// writeTreeEntry writes one node line (with its annotation lines when
// the node's path has entries), recursing into containers.
func writeTreeEntry(buf *strings.Builder, prefix, label string, value interface{}, last bool, depth int, opts treeOptions, ann map[string][]history.Entry, histKey string, histOK bool, st debugStyler) {
	connector, childPrefix := "├─ ", prefix+"│  "
	if last {
		connector, childPrefix = "└─ ", prefix+"   "
	}

	if isTreeContainer(value) {
		if opts.depth > 0 && depth >= opts.depth {
			// A collapsed container hides everything beneath it,
			// annotations included (documented in the user guide).
			fmt.Fprintf(buf, "%s%s%s %s\n", prefix, connector, label, st.apply(roleMuted, collapsedSummary(value)))
			return
		}
		fmt.Fprintf(buf, "%s%s%s\n", prefix, connector, label)
		// A non-empty list is tracked at its own path (history never
		// descends into it), so a container can carry entries of its own.
		writeTreeAnnotations(buf, childPrefix, ann, histKey, histOK, st)
		writeTreeChildren(buf, childPrefix, value, depth+1, opts, ann, histKey, histOK, st)
		return
	}

	if opts.keysOnly {
		fmt.Fprintf(buf, "%s%s%s\n", prefix, connector, label)
	} else {
		fmt.Fprintf(buf, "%s%s%s: %s\n", prefix, connector, label, treeValueDisplay(value, st))
	}
	writeTreeAnnotations(buf, childPrefix, ann, histKey, histOK, st)
}

// writeTreeAnnotations prints one node's history entries (roleMuted,
// indented two spaces past the node's children) when the node has a
// valid history path. histOK is false for everything at or below a list
// element.
func writeTreeAnnotations(buf *strings.Builder, childPrefix string, ann map[string][]history.Entry, histKey string, histOK bool, st debugStyler) {
	if !histOK {
		return
	}
	for _, e := range ann[histKey] {
		fmt.Fprintf(buf, "%s  %s\n", childPrefix, st.apply(roleMuted, historyEntryLine(e)))
	}
}

// isTreeContainer reports whether v renders as an expandable tree node.
// Empty maps/lists render as leaves ("{}"/"[]" via inlineValue).
func isTreeContainer(v interface{}) bool {
	switch c := v.(type) {
	case map[string]interface{}:
		return len(c) > 0
	case []interface{}:
		return len(c) > 0
	}
	return false
}

// collapsedSummary is the stand-in for a container hidden by --depth.
func collapsedSummary(v interface{}) string {
	switch c := v.(type) {
	case map[string]interface{}:
		return "{" + pluralCount(len(c), "key") + "}"
	case []interface{}:
		return "[" + pluralCount(len(c), "item") + "]"
	}
	return ""
}

// treeValueDisplay renders a leaf value inline, highlighting a
// still-unevaluated "(( ... ))" operator expression roleWarn (the role
// that already flags text needing the reader's attention, same as the
// usage and guard messages every other REPL command uses it for).
func treeValueDisplay(v interface{}, st debugStyler) string {
	s := inlineValue(v)
	if looksLikeOperator(v) {
		return st.apply(roleWarn, s)
	}
	return s
}

// historyKeyForPath converts a REPL dotted path (lookupDottedPath's
// syntax) to internal/history's flattened-path spelling: dot-joined map
// keys, each quoted when it contains "." or "[" (the history package's
// EscapePathSegment). History flattening never descends into lists, so
// a path with an index segment stops at the list and reports
// insideList=true - no history exists below that point.
func historyKeyForPath(path string) (key string, insideList bool) {
	var parts []string
	for _, seg := range splitDebugPath(path) {
		if _, isIdx := parseIndexSegment(seg); isIdx {
			return strings.Join(parts, "."), true
		}
		parts = append(parts, history.EscapePathSegment(seg))
	}
	return strings.Join(parts, "."), false
}

// joinHistKey extends a history-flattened path by one map key.
func joinHistKey(prefix, key string) string {
	seg := history.EscapePathSegment(key)
	if prefix == "" {
		return seg
	}
	return prefix + "." + seg
}

// cmdTree implements the `tree` REPL command: a box-drawing tree of the
// subtree at a path, from the session's current tree (so it reflects step
// progress exactly like `inspect`). --annotate/--history add per-path
// history truncated to the session's current step - unlike `history`,
// which always reports the full run.
func (s *debugSession) cmdTree(args []string) {
	// Flag feedback needs no documents, so -h and parse errors work
	// before load; only rendering requires a loaded session.
	opts, err := parseTreeArgs(args)
	if err != nil {
		s.printf("%s\nUsage: %s\n", err.Error(), treeUsage)
		return
	}
	if opts.help {
		s.printf("Usage: %s\nSee 'help tree' for details.\n", treeUsage)
		return
	}
	if !s.loaded {
		s.printf("%s\n", s.style(roleWarn, "No documents loaded. Run 'load' first."))
		return
	}
	value, ok := lookupDottedPath(s.tree, opts.path)
	if !ok {
		s.printf("%s\n", s.style(roleWarn, fmt.Sprintf("Path not found: %s", opts.path)))
		return
	}

	// --no-color overrides only this render, independent of the
	// session's own color/theme resolution: an identity styler when the
	// flag is set (matching the session's own color-off identity
	// behavior, but scoped to this one command), or the session's
	// styler unchanged otherwise. This replaces the previous save/flip/
	// restore of the package-global ansi flag, which this command was
	// the last site in the debugger to still touch.
	st := s.styler
	if opts.noColor {
		st = debugStyler{}
	}

	// Compute history first (annotations render inline), but never let a
	// history failure suppress the tree itself: render, then explain.
	phs, ann, histNote := s.treeHistoryData(opts)

	s.printf("%s", renderDebugTree(value, opts, ann, st))

	if histNote != "" {
		s.printf("\nNote: %s\n", histNote)
		return
	}
	if opts.historyList {
		s.printTreeHistoryList(phs)
	}
}

// treeHistoryData computes cmdTree's per-path history when --annotate or
// --history was requested.
// phs is the filtered PathHistory list; ann maps each path to its entries
// for inline annotation, populated only for --annotate; histNote is a
// history-computation error to report without suppressing the tree
// render itself.
func (s *debugSession) treeHistoryData(opts treeOptions) (phs []history.PathHistory, ann map[string][]history.Entry, histNote string) {
	if !opts.annotate && !opts.historyList {
		return nil, nil, ""
	}
	var histErr error
	phs, histErr = s.subtreeHistories(opts.path)
	if histErr != nil {
		return nil, nil, ansi.StripEscapes(histErr.Error())
	}
	if opts.annotate {
		ann = make(map[string][]history.Entry, len(phs))
		for _, ph := range phs {
			ann[ph.Path] = ph.Entries
		}
	}
	return phs, ann, ""
}

// printTreeHistoryList prints cmdTree's --history tail: one block per
// path in phs, or a message when nothing was tracked under the path.
func (s *debugSession) printTreeHistoryList(phs []history.PathHistory) {
	if len(phs) == 0 {
		s.printf("\nNo history recorded under this path.\n")
		return
	}
	var buf strings.Builder
	for i, ph := range phs {
		if i > 0 {
			buf.WriteString("\n")
		}
		writeTreeHistoryBlock(&buf, ph, s.step)
	}
	s.printf("\n%s", buf.String())
}

// subtreeHistories computes per-path history truncated to the session's
// current step and filtered to paths at or under path. It mirrors
// cmdHistory's setup: files re-resolved from disk, --prune/--cherry-pick
// excluded, and the session's deferred paths applied to every replay.
func (s *debugSession) subtreeHistories(path string) ([]history.PathHistory, error) {
	prefix, insideList := historyKeyForPath(path)
	if insideList {
		return nil, fmt.Errorf("history does not descend into lists; ask for the list's own path instead")
	}

	fileOpts := *s.opts
	fileOpts.Files = make([]string, len(s.cached))
	for i, c := range s.cached {
		fileOpts.Files[i] = c.Path
	}
	fileOpts.Prune = nil
	fileOpts.CherryPick = nil
	steps, _, err := buildMergeHistorySteps(&fileOpts, s.deferredDocRewriter(), s.step)
	if err != nil {
		return nil, err
	}
	all, err := history.Track(steps)
	if err != nil {
		return nil, err
	}
	return filterPathHistories(all, prefix), nil
}

// filterPathHistories keeps only the histories at or under prefix (a
// history-flattened path; "" keeps everything).
func filterPathHistories(all []history.PathHistory, prefix string) []history.PathHistory {
	if prefix == "" {
		return all
	}
	var out []history.PathHistory
	for _, ph := range all {
		if ph.Path == prefix || strings.HasPrefix(ph.Path, prefix+".") {
			out = append(out, ph)
		}
	}
	return out
}

// writeTreeHistoryBlock prints one path's history in the exact format
// `history <path>` uses, with the final line labeled "As of step N"
// instead of "Final": the tracked steps stop at the session's current
// step, and a targeted `eval <path>` can move the live tree ahead of
// that step, so the label names the step rather than claiming currency.
func writeTreeHistoryBlock(buf *strings.Builder, ph history.PathHistory, step int) {
	fmt.Fprintf(buf, "%s:\n", ph.Path)
	for _, e := range ph.Entries {
		writeHistoryEntryLine(buf, e)
	}
	writeHistoryFinalLine(buf, ph, fmt.Sprintf("As of step %d", step), len(ph.Entries) == 1)
}
