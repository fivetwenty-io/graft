package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/fivetwenty-io/graft/internal/history"
	"github.com/fivetwenty-io/graft/internal/utils/ansi"
	"github.com/fivetwenty-io/graft/log"
	"github.com/fivetwenty-io/graft/pkg/jq"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// The two query dialects a session can speak. jq is the default.
const (
	debugDialectJQ = "jq"
	debugDialectYQ = "yq"
)

// errDebugNotLoaded is evalQuery's answer before `load` has built a tree.
var errDebugNotLoaded = errors.New("no documents loaded")

// debugYQUnavailable is what every yq entry point says until the yq
// dialect is wired in.
const debugYQUnavailable = "yq is not available until a later release"

// debugWatch is one expression the session re-evaluates after every step,
// with the rendering it produced the last time.
type debugWatch struct{ Dialect, Expr, Last string }

// debugInit is what `graft debug --jq`, `graft debug --yq`, and
// `graft jq --repl` hand the session before its prompt appears: a query to
// run once against the fully merged inputs.
type debugInit struct {
	Dialect string
	Query   string
}

// debugInitFor is the startup query the --jq and --yq flags ask for.
func debugInitFor(jqProgram, yqExpr string) debugInit {
	if yqExpr != "" {
		return debugInit{Dialect: debugDialectYQ, Query: yqExpr}
	}
	return debugInit{Dialect: debugDialectJQ, Query: jqProgram}
}

// debugQueryPrefix matches the `jq:` or `yq:` marker at the start of a path
// argument. Nothing else is a query, so a key such as `ns:x` stays a path.
var debugQueryPrefix = regexp.MustCompile(`^(jq|yq):`)

// debugAnyPrefix is any two letters before a colon, which resolveDebugPaths
// refuses by name when it is asked to resolve one that is not `jq:` or
// `yq:`.
var debugAnyPrefix = regexp.MustCompile(`^([A-Za-z]{2}):`)

// hasQueryPrefix reports whether a path argument names a query.
func hasQueryPrefix(arg string) bool {
	return debugQueryPrefix.MatchString(strings.TrimSpace(arg))
}

// dialectName is the session's dialect, jq until `config dialect` says
// otherwise.
func (s *debugSession) dialectName() string {
	if s.dialect == "" {
		return debugDialectJQ
	}
	return s.dialect
}

// evalQuery runs expr in dialect against the session's current tree and
// returns every output. The tree is converted again only when it has
// changed since the last call.
func (s *debugSession) evalQuery(dialect, expr string) ([]*value.Value, error) {
	if s.tree == nil {
		return nil, errDebugNotLoaded
	}
	if s.root == nil || s.rootGen != s.treeGen {
		root, err := value.FromGo(s.tree)
		if err != nil {
			return nil, err
		}
		s.root, s.rootGen = root, s.treeGen
		s.rootBuilds++
	}
	root := s.root
	switch dialect {
	case debugDialectJQ:
		q, err := jq.Compile(expr)
		if err != nil {
			return nil, err
		}
		return q.RunAll(context.Background(), root)
	case debugDialectYQ:
		return nil, errors.New(debugYQUnavailable)
	}
	return nil, fmt.Errorf("unknown dialect %q (jq or yq)", dialect)
}

// evalPaths returns the paths expr selects, as typed paths.
func (s *debugSession) evalPaths(dialect, expr string) ([]value.Path, error) {
	var program string
	switch dialect {
	case debugDialectJQ:
		program = "path(" + expr + ")"
	case debugDialectYQ:
		program = expr + " | path"
	default:
		return nil, fmt.Errorf("unknown dialect %q (jq or yq)", dialect)
	}
	outs, err := s.evalQuery(dialect, program)
	if err != nil {
		return nil, err
	}
	paths := make([]value.Path, 0, len(outs))
	for _, o := range outs {
		if o.Kind() != value.KindArray {
			return nil, fmt.Errorf("path expression gave %v, not a path", o.Kind())
		}
		p := make(value.Path, 0, o.Len())
		for _, c := range o.Elems() {
			if c.Kind() != value.KindString && c.Kind() != value.KindNumber {
				return nil, errors.New("a path with a slice cannot name one value")
			}
			p = append(p, c)
		}
		paths = append(paths, p)
	}
	return paths, nil
}

// resolveDebugPaths turns a path argument into typed paths. A bare dotted
// path gives the one path splitDebugPath reads, `jq:EXPR` and `yq:EXPR`
// give each path the expression selects, and an empty argument gives the
// current selection.
func resolveDebugPaths(s *debugSession, arg string) ([]value.Path, error) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return append([]value.Path(nil), s.selection...), nil
	}
	if m := debugAnyPrefix.FindStringSubmatch(arg); m != nil {
		expr := strings.TrimSpace(arg[len(m[0]):])
		switch m[1] {
		case debugDialectJQ, debugDialectYQ:
			return s.evalPaths(m[1], expr)
		}
		return nil, fmt.Errorf("unknown query prefix %q (jq: or yq:)", m[0])
	}
	segs := splitDebugPath(arg)
	p := make(value.Path, len(segs))
	for i, seg := range segs {
		key, idx, isIdx := decodeSegment(seg)
		if isIdx {
			p[i] = value.Num(float64(idx))
			continue
		}
		p[i] = value.Str(key)
	}
	return []value.Path{p}, nil
}

// dottedPath renders a path for display in the REPL's own form. A key that
// holds a dot, is empty, or is all digits is quoted in brackets, so
// ["a.b"] and ["a", "b"] stay distinct.
func dottedPath(p value.Path) string {
	parts := make([]string, len(p))
	for i, c := range p {
		switch c.Kind() {
		case value.KindNumber:
			parts[i] = "[" + c.Dec().String() + "]"
		case value.KindString:
			parts[i] = displaySegment(c.Str())
		default:
			parts[i] = fmt.Sprintf("[%v]", c.Kind())
		}
	}
	return strings.Join(parts, ".")
}

func displaySegment(seg string) string {
	if seg == "" || strings.Contains(seg, ".") || isAllDigits(seg) {
		return "[" + strconv.Quote(seg) + "]"
	}
	return seg
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// lookupPath finds the value a typed path names in the session's tree.
func (s *debugSession) lookupPath(p value.Path) (interface{}, bool) {
	var current interface{} = s.tree
	for _, c := range p {
		switch c.Kind() {
		case value.KindString:
			m, ok := current.(map[string]interface{})
			if !ok {
				return nil, false
			}
			v, exists := m[c.Str()]
			if !exists {
				return nil, false
			}
			current = v
		case value.KindNumber:
			arr, ok := current.([]interface{})
			idx, isInt := c.Dec().Int64()
			if !ok || !isInt || idx < 0 || idx >= int64(len(arr)) {
				return nil, false
			}
			current = arr[idx]
		default:
			return nil, false
		}
	}
	return current, true
}

// eachPath applies fn to each path arg names, spelled by conv (dottedPath
// when nil). A bare dotted path, or no
// argument with no selection, reaches fn untouched, so the verbs behave
// exactly as they did before queries existed.
func (s *debugSession) eachPath(arg string, conv func(value.Path) string, fn func(path string)) {
	if arg == "" && len(s.selection) == 0 || arg != "" && !hasQueryPrefix(arg) {
		fn(arg)
		return
	}
	paths, err := resolveDebugPaths(s, arg)
	if err != nil {
		s.printf("%s\n", s.style(roleWarn, ansi.StripEscapes(err.Error())))
		return
	}
	if len(paths) == 0 {
		s.printf("%s\n", s.style(roleWarn, fmt.Sprintf("No paths matched: %s", arg)))
		return
	}
	if conv == nil {
		conv = dottedPath
	}
	for _, p := range paths {
		fn(conv(p))
	}
}

// cmdInspectArg is `inspect`: the legacy dotted form, or the value at each
// path a query or the selection names.
func (s *debugSession) cmdInspectArg(arg string) {
	if arg == "" && len(s.selection) == 0 || arg != "" && !hasQueryPrefix(arg) {
		s.cmdInspect(arg)
		return
	}
	if !s.loaded {
		s.printf("%s\n", s.style(roleWarn, "No documents loaded. Run 'load' first."))
		return
	}
	paths, err := resolveDebugPaths(s, arg)
	if err != nil {
		s.printf("%s\n", s.style(roleWarn, ansi.StripEscapes(err.Error())))
		return
	}
	if len(paths) == 0 {
		s.printf("%s\n", s.style(roleWarn, fmt.Sprintf("No paths matched: %s", arg)))
		return
	}
	for _, p := range paths {
		v, ok := s.lookupPath(p)
		if !ok {
			s.printf("%s\n", s.style(roleWarn, fmt.Sprintf("Path not found: %s", dottedPath(p))))
			continue
		}
		s.printf("%s:\n", s.style(rolePath, dottedPath(p)))
		s.writeValue(v)
	}
	s.printDeferred()
}

// stripQueryFlags removes the jq flags the query verbs accept from the
// front of their argument. -r and -c are accepted for jq habit and change
// nothing, because the REPL always prints YAML with a string bare.
func stripQueryFlags(arg string) string {
	for {
		arg = strings.TrimSpace(arg)
		tok, rest, _ := strings.Cut(arg, " ")
		switch tok {
		case "-r", "--raw-output", "-c", "--compact-output":
			arg = rest
		default:
			return arg
		}
	}
}

// renderQuery encodes outputs as the jq command prints YAML, except that a
// top-level string prints bare, as 'inspect' prints one.
func renderQuery(outs []*value.Value) ([][]byte, error) {
	docs := make([][]byte, 0, len(outs))
	for _, o := range outs {
		var buf bytes.Buffer
		enc := jq.NewEncoder(&buf, jq.EncodeOptions{Format: jq.FormatYAML, Raw: true})
		if err := enc.Encode(o); err != nil {
			return nil, err
		}
		if err := enc.Flush(); err != nil {
			return nil, err
		}
		docs = append(docs, buf.Bytes())
	}
	return docs, nil
}

// runQuery evaluates expr and prints each output as a YAML document.
func (s *debugSession) runQuery(dialect, expr string) {
	outs, err := s.evalQuery(dialect, expr)
	if err != nil {
		s.printQueryError(err)
		return
	}
	docs, err := renderQuery(outs)
	if err != nil {
		s.printQueryError(err)
		return
	}
	for _, o := range outs {
		traceOutput(o)
	}
	for i, d := range docs {
		if i > 0 {
			s.printf("---\n")
		}
		s.writeYAML(d)
	}
}

func (s *debugSession) printQueryError(err error) {
	if errors.Is(err, errDebugNotLoaded) {
		s.printf("%s\n", s.style(roleWarn, "No documents loaded. Run 'load' first."))
		return
	}
	msg := ansi.StripEscapes(err.Error())
	var ce *query.CompileError
	phrase := "Query failed"
	if errors.As(err, &ce) {
		phrase = "Compile error"
	}
	s.printf("%s: %s\n", s.style(roleError, phrase), msg)
}

// cmdJQ implements `jq [-r] PROGRAM`. Without a program it runs the one
// the session started with, or the last one it ran.
func (s *debugSession) cmdJQ(arg string) {
	s.cmdQueryVerb(debugDialectJQ, arg)
}

// cmdYQ implements `yq [-r] EXPR`.
func (s *debugSession) cmdYQ(arg string) {
	s.cmdQueryVerb(debugDialectYQ, arg)
}

func (s *debugSession) cmdQueryVerb(dialect, arg string) {
	expr := stripQueryFlags(arg)
	if expr == "" {
		expr = s.query
	}
	if expr == "" {
		s.printf("%s\n", s.style(roleWarn, fmt.Sprintf("Usage: %s <expression>", dialect)))
		return
	}
	s.query = expr
	s.runQuery(dialect, expr)
}

// cmdSelect implements `select EXPR`: the paths EXPR names become the
// selection that the path verbs use when given no argument.
func (s *debugSession) cmdSelect(arg string) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		if len(s.selection) == 0 {
			s.printf("%s\n", s.style(roleMuted, "Nothing selected."))
			return
		}
		s.printSelection()
		return
	}
	paths, err := s.evalPaths(s.dialectName(), arg)
	if err != nil {
		s.printQueryError(err)
		return
	}
	s.selection = paths
	s.selectionNames = make([]string, len(paths))
	for i, p := range paths {
		s.selectionNames[i] = s.histPath(p)
	}
	s.printSelection()
}

func (s *debugSession) printSelection() {
	names := make([]string, len(s.selection))
	for i, p := range s.selection {
		names[i] = dottedPath(p)
	}
	line := "selected " + pluralCount(len(names), "path")
	if len(names) > 0 {
		line += ": " + strings.Join(names, ", ")
	}
	s.printf("%s\n", s.style(roleSuccess, line))
}

// cmdUnselect implements `unselect`.
func (s *debugSession) cmdUnselect() {
	s.selection = nil
	s.selectionNames = nil
	s.printf("%s\n", s.style(roleSuccess, "Selection cleared"))
}

// watchRender is the one-line rendering a watch compares and shows: each
// output as compact JSON, joined with commas.
func (s *debugSession) watchRender(dialect, expr string) string {
	outs, err := s.evalQuery(dialect, expr)
	if err != nil {
		return "error: " + ansi.StripEscapes(err.Error())
	}
	parts := make([]string, len(outs))
	for i, o := range outs {
		var buf bytes.Buffer
		enc := jq.NewEncoder(&buf, jq.EncodeOptions{Compact: true})
		if enc.Encode(o) != nil || enc.Flush() != nil {
			parts[i] = "?"
			continue
		}
		parts[i] = strings.TrimSpace(buf.String())
	}
	if len(parts) == 0 {
		return "<empty>"
	}
	return strings.Join(parts, ", ")
}

// cmdWatch implements `watch EXPR`: after every step the session prints
// the expression's value when it changed.
func (s *debugSession) cmdWatch(arg string) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		if len(s.watches) == 0 {
			s.printf("%s\n", s.style(roleMuted, "No watches set."))
			return
		}
		for _, w := range s.watches {
			s.printf("  - (%s): %s\n", w.Expr, w.Last)
		}
		return
	}
	if !s.loaded {
		s.printf("%s\n", s.style(roleWarn, "No documents loaded. Run 'load' first."))
		return
	}
	w := debugWatch{Dialect: s.dialectName(), Expr: arg}
	w.Last = s.watchRender(w.Dialect, w.Expr)
	s.watches = append(s.watches, w)
	s.printf("%s\n", s.style(roleSuccess, fmt.Sprintf("watching (%s): %s", w.Expr, w.Last)))
}

// cmdUnwatch implements `unwatch [EXPR]`: one watch, or all of them.
func (s *debugSession) cmdUnwatch(arg string) {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		s.watches = nil
		s.printf("%s\n", s.style(roleSuccess, "All watches removed"))
		return
	}
	kept := s.watches[:0]
	removed := false
	for _, w := range s.watches {
		if w.Expr == arg {
			removed = true
			continue
		}
		kept = append(kept, w)
	}
	s.watches = kept
	if !removed {
		s.printf("%s\n", s.style(roleWarn, fmt.Sprintf("No watch on (%s)", arg)))
		return
	}
	s.printf("%s\n", s.style(roleSuccess, "Watch removed"))
}

// reportWatches re-evaluates every watch and prints the ones whose value
// changed since the last report.
func (s *debugSession) reportWatches() {
	for i := range s.watches {
		w := &s.watches[i]
		now := s.watchRender(w.Dialect, w.Expr)
		if now == w.Last {
			continue
		}
		s.printf("%s %s -> %s\n", s.style(roleHeading, fmt.Sprintf("watch (%s):", w.Expr)), s.style(roleValueOld, w.Last), s.style(roleValueNew, now))
		w.Last = now
	}
}

// cmdConfigDialect implements `config dialect [jq|yq]`.
func (s *debugSession) cmdConfigDialect(args []string) {
	if len(args) == 0 {
		s.printf("dialect: %s\n", s.dialectName())
		return
	}
	switch name := strings.Join(args, " "); name {
	case debugDialectJQ, debugDialectYQ:
		s.dialect = name
		s.printf("%s\n", s.style(roleSuccess, "Updated dialect"))
	default:
		s.printf("%s\n", s.style(roleWarn, fmt.Sprintf("Unknown dialect: %s. Known dialects: jq, yq.", name)))
	}
}

// runStartupQuery merges every input, evaluates the operators, and runs
// the query once against the result, the way `graft jq` would run it. The
// session is left as `load` would find it.
func (s *debugSession) runStartupQuery(init debugInit) {
	evalOpts := *s.opts
	evalOpts.SkipEval = false
	evalOpts.Prune = nil
	evalOpts.CherryPick = nil
	merged, _, err := mergeAllDocs(s.freshFiles(len(s.cached)), &evalOpts)
	if err != nil {
		s.printf("%s: %s\n", s.style(roleError, "Merge failed"), ansi.StripEscapes(err.Error()))
		return
	}
	prevTree, prevDialect := s.tree, s.dialect
	s.setTree(merged)
	s.dialect, s.query = init.Dialect, init.Query
	log.DEBUG("debug query: %s", init.Query)
	s.runQuery(init.Dialect, init.Query)
	s.setTree(prevTree)
	s.dialect = prevDialect
}

// runJQRepl is `graft jq --repl PROGRAM FILE...`: the debugger over the
// files, started with the program already run once.
func runJQRepl(opts jqOptions, stdin io.Reader, stdout io.Writer) query.Status {
	if len(opts.Files) == 0 {
		return query.Status{Code: 1, Message: colored("R", "Missing Input") + ": graft jq --repl requires at least one file (e.g. graft jq --repl .a base.yml)\n"}
	}
	init := debugInit{Dialect: debugDialectJQ}
	if opts.HasProgram {
		init.Query = opts.Program
	}
	rc := handleDebugWith(opts.Files, &mergeOpts{}, stdin, stdout, debugUIOptions{Theme: opts.Theme}, init)
	return query.Status{Code: rc}
}

// histPath spells a typed path the way the step diff names it, which is
// with a list entry's name where the entry has one, as merge's history
// names entries, and [N] where it does not.
func (s *debugSession) histPath(p value.Path) string {
	return s.namePath(p, false)
}

// trackPath is histPath in the form the history tracker keys its paths by,
// which quotes a key that holds a dot or a bracket.
func (s *debugSession) trackPath(p value.Path) string {
	return s.namePath(p, true)
}

func (s *debugSession) namePath(p value.Path, quote bool) string {
	var cur interface{} = s.tree
	var out strings.Builder
	add := func(seg string) {
		if out.Len() > 0 {
			out.WriteByte('.')
		}
		out.WriteString(seg)
	}
	for _, c := range p {
		switch c.Kind() {
		case value.KindString:
			seg := c.Str()
			if quote {
				seg = history.EscapePathSegment(seg)
			}
			add(seg)
			m, _ := cur.(map[string]interface{})
			cur = m[c.Str()]
		case value.KindNumber:
			var next interface{}
			arr, _ := cur.([]interface{})
			named := false
			if idx, ok := c.Dec().Int64(); ok && idx >= 0 && idx < int64(len(arr)) {
				next = arr[idx]
				if m, isMap := next.(map[string]interface{}); isMap {
					if name, isStr := m["name"].(string); isStr {
						add(name)
						named = true
					}
				}
			}
			if !named {
				out.WriteString("[" + c.Dec().String() + "]")
			}
			cur = next
		}
	}
	return out.String()
}

// cmdBreakArg is `break`: a plain path sets one exact breakpoint, as it
// always has, and a query sets one on each path it names. A query's
// breakpoint covers everything beneath the path it names, so one on a job
// fires when any field of the job changes.
func (s *debugSession) cmdBreakArg(arg string) {
	if arg == "" && len(s.selection) == 0 || arg != "" && !hasQueryPrefix(arg) {
		s.cmdBreak(arg)
		return
	}
	s.eachPath(arg, s.histPath, func(path string) {
		s.cmdBreak(path)
		s.coverBreaks[path] = true
	})
}

// cmdUnbreakArg is `unbreak`, with the same two forms as `break`.
func (s *debugSession) cmdUnbreakArg(arg string) {
	if arg == "" && len(s.selection) == 0 || arg != "" && !hasQueryPrefix(arg) {
		s.cmdUnbreak(arg)
		return
	}
	s.eachPath(arg, s.histPath, s.cmdUnbreak)
}

// breakpointHit reports whether a change at path trips a breakpoint.
func (s *debugSession) breakpointHit(path string) bool {
	if s.breakpoints[path] {
		return true
	}
	for bp := range s.coverBreaks {
		if s.breakpoints[bp] && strings.HasPrefix(path, bp+".") {
			return true
		}
	}
	return false
}

// inSelection reports whether a change at path lies under a selected path.
// With nothing selected every change does.
func (s *debugSession) inSelection(path string) bool {
	if len(s.selectionNames) == 0 {
		return true
	}
	for _, sel := range s.selectionNames {
		if path == sel || strings.HasPrefix(path, sel+".") || strings.HasPrefix(path, sel+"[") {
			return true
		}
	}
	return false
}

// setTree replaces the session's tree and marks the query root stale.
func (s *debugSession) setTree(t map[string]interface{}) {
	s.tree = t
	s.treeGen++
}

// traceOutput logs one query output under -D, as compact JSON.
func traceOutput(v *value.Value) {
	if !log.DebugOn {
		return
	}
	var buf bytes.Buffer
	enc := jq.NewEncoder(&buf, jq.EncodeOptions{Compact: true})
	if enc.Encode(v) != nil || enc.Flush() != nil {
		return
	}
	log.DEBUG("output: %s", strings.TrimSpace(buf.String()))
}
