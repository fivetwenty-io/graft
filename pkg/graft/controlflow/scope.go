package controlflow

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/fivetwenty-io/graft/internal/errtext"
	"github.com/fivetwenty-io/graft/pkg/graft"
	"github.com/fivetwenty-io/graft/pkg/graft/interfaces"
	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// env is the evaluation environment a condition, iterable, or case subject
// is evaluated against: the prescan scope (§8.3 step 2) extended by
// whatever enclosing loop variables are currently bound (§8.3 step 3). scope
// is shared and never mutated; each nested for-loop iteration derives a new
// env via withBinding, so sibling and outer iterations are unaffected
// (spec's "inner loops shadow outer loops" and "binding does not escape the
// body" rules fall out of this by construction).
type env struct {
	scope    map[string]interface{}
	bindings map[string]interface{}

	// engine is the engine performing the parse that this control-flow
	// expansion is part of (see Expand's doc comment). Every expression
	// evaluated against this env resolves operators through it, so a
	// custom operator registered on engine is visible inside
	// conditions/iterables/subjects the same way it is everywhere else. A
	// nil engine resolves identically to before engine-local registration
	// existed.
	engine graft.Engine
}

// newEnv creates the root environment for a document: the prescan scope,
// with no loop bindings yet.
func newEnv(scope map[string]interface{}, engine graft.Engine) *env {
	return &env{scope: scope, engine: engine}
}

// withBinding returns a new environment with name bound to value, shadowing
// any scope key or outer binding of the same name for evaluations performed
// against the returned env. The receiver is left unmodified.
func (e *env) withBinding(name string, value interface{}) *env {
	next := make(map[string]interface{}, len(e.bindings)+1)
	for k, v := range e.bindings {
		next[k] = v
	}
	next[name] = value
	return &env{scope: e.scope, bindings: next, engine: e.engine}
}

// view is what one header expression is parsed from and evaluated
// against. text is the expression to parse, tree is the evaluator's
// tree, and alias, when not empty, is the extra top-level key that text
// reads the prescan scope through. rewritten holds, for each "$." path
// that viewFor sent through the alias, the path as written after the
// "$.", which is what withoutAlias looks for.
type view struct {
	text      string
	tree      map[string]interface{}
	alias     string
	rewritten []string
}

// viewFor returns the view raw is evaluated in. The tree is the prescan
// scope with the current bindings laid over its top level, so a bound
// name shadows the document key of the same name.
//
// pathNameSpans finds the names that raw reads with the expression
// lexer, so a name inside a quoted string is never counted.
//
// A "$." path always names the document root. When raw reads a bound
// name only through "$." paths, that binding is left out of the tree,
// and those paths read the document key as written. viewFor builds the
// tree for raw alone, so whether a binding stays in it depends only on
// how raw itself reads the name.
//
// When raw reads one bound name both ways, as in "$.x.kind == x.kind",
// one tree cannot serve both. The name stays bound, and each "$." path
// that starts with it reads the scope through an alias, a top-level key
// that no document key or binding holds. The view records each path it
// sent through the alias, and evalExpr takes the alias back out of an
// error only where it starts one of those paths. A message then names
// the path as written, while a path that an operator built from a
// document value keeps the text it was built with. The alias is never a
// key that raw mentions, so no path the user wrote can reach it.
func (e *env) viewFor(raw string) view {
	if len(e.bindings) == 0 {
		return view{text: raw, tree: e.scope}
	}
	spans := pathNameSpans(raw, e.engine)
	rooted, plain := e.boundReads(raw, spans)
	merged := make(map[string]interface{}, len(e.scope)+len(e.bindings)+1)
	for k, v := range e.scope {
		merged[k] = v
	}
	both := false
	for k, v := range e.bindings {
		if rooted[k] && plain[k] {
			both = true
		}
		if rooted[k] && !plain[k] {
			continue
		}
		merged[k] = v
	}
	if !both {
		return view{text: raw, tree: merged}
	}
	alias := unusedKey(merged, raw, "__graft_root")
	merged[alias] = e.scope
	var out strings.Builder
	var rewritten []string
	last := 0
	for _, sp := range spans {
		if !sp.rooted || !plain[raw[sp.start:sp.end]] {
			continue
		}
		out.WriteString(raw[last:sp.start])
		out.WriteString(alias + ".")
		rewritten = append(rewritten, raw[sp.start:sp.tokenEnd])
		last = sp.start
	}
	out.WriteString(raw[last:])
	return view{text: out.String(), tree: merged, alias: alias, rewritten: rewritten}
}

// boundReads reports which bound names raw reads through a "$." path
// and which it reads without one.
func (e *env) boundReads(raw string, spans []nameSpan) (rooted, plain map[string]bool) {
	rooted, plain = map[string]bool{}, map[string]bool{}
	for _, sp := range spans {
		name := raw[sp.start:sp.end]
		if _, bound := e.bindings[name]; !bound {
			continue
		}
		if sp.rooted {
			rooted[name] = true
		} else {
			plain[name] = true
		}
	}
	return rooted, plain
}

// unusedKey returns base, or base with the lowest numeric suffix that
// makes it a key m does not hold and text never mentions. The first
// keeps an alias from shadowing a document key or a binding. The second
// keeps a path the user wrote from reaching the alias, so a key the
// document lacks still fails to resolve.
func unusedKey(m map[string]interface{}, text, base string) string {
	key := base
	for i := 1; ; i++ {
		if _, taken := m[key]; !taken && !strings.Contains(text, key) {
			return key
		}
		key = fmt.Sprintf("%s_%d", base, i)
	}
}

// withoutAlias returns err with the alias taken back out of each path
// that viewFor rewrote, in both its plain and its detailed text, so the
// message names the "$." path the user wrote. Only a path in backticks
// that starts with the alias, after a "`" or a "`$.", and goes on as one
// of v.rewritten or a leading part of one is changed. Text that holds
// the alias's name for any other reason, such as a path built from a
// document value, is left as it is.
func withoutAlias(err error, v view) error {
	if err == nil || v.alias == "" {
		return err
	}
	plain, detail := err.Error(), errtext.Detail(err)
	newPlain, newDetail := v.stripAlias(plain), v.stripAlias(detail)
	if newPlain == plain && newDetail == detail {
		return err
	}
	return errtext.Wrap(err, newPlain, newDetail)
}

// stripAlias is withoutAlias for one text.
func (v view) stripAlias(text string) string {
	prefix := v.alias + "."
	var out strings.Builder
	rest := text
	for {
		i := strings.Index(rest, prefix)
		if i < 0 {
			out.WriteString(rest)
			return out.String()
		}
		head, after := rest[:i], rest[i+len(prefix):]
		out.WriteString(head)
		if !v.wrotePath(head, after) {
			out.WriteString(prefix)
		}
		rest = after
	}
}

// trailingColor matches the terminal color codes at the end of a text,
// which a colored message puts between a backtick and the path in it.
var trailingColor = regexp.MustCompile(`(\x1b\[[0-9;]*m)+$`)

// wrotePath reports whether the alias that sits between head and after
// starts a path that viewFor wrote. head must end with "`" or "`$.",
// with any color codes around the "$." set aside, and the path, which
// runs from the alias to the next "`" or color code, must be one of
// v.rewritten or a leading part of one that ends where a node does.
func (v view) wrotePath(head, after string) bool {
	head = trailingColor.ReplaceAllString(head, "")
	if rest, ok := strings.CutSuffix(head, "$."); ok {
		head = trailingColor.ReplaceAllString(rest, "")
	}
	if !strings.HasSuffix(head, "`") {
		return false
	}
	end := strings.IndexAny(after, "`\x1b")
	if end < 0 {
		return false
	}
	path := after[:end]
	for _, r := range v.rewritten {
		if r == path || strings.HasPrefix(r, path+".") || strings.HasPrefix(r, path+"[") {
			return true
		}
	}
	return false
}

// buildPrescanScope implements spec §8.3 step 2: strip every top-level
// control-flow block (and, by construction, everything nested inside it)
// from source, YAML-parse the remainder, and evaluate its operators
// best-effort — an operator that fails to resolve leaves its key absent
// rather than failing the whole scope. This is what every condition,
// iterable, and case subject in the document is evaluated against, unless
// a loop currently in scope has bound a shadowing name (see env).
//
// engine, when non-nil, both parses the remainder and evaluates its
// operators — so a custom operator registered on engine resolves in the
// prescan scope, not only in condition/iterable/subject expressions
// evaluated later against that scope. A nil engine (only reachable when a
// caller invokes this package directly rather than through
// DefaultEngine.ParseYAML) falls back to a throwaway default engine for
// parsing only, matching pre-P0-1 behavior.
func buildPrescanScope(lines []scanLine, topLevel []item, engine graft.Engine) (map[string]interface{}, error) {
	remainder := stripBlockLines(lines, topLevel)

	scopeEngine := engine
	if scopeEngine == nil {
		var err error
		scopeEngine, err = graft.NewEngine()
		if err != nil {
			return nil, fmt.Errorf("control flow: building prescan scope: %w", err)
		}
	}
	doc, err := scopeEngine.ParseYAML([]byte(remainder))
	if err != nil {
		return nil, fmt.Errorf("control flow: prescan scope is not valid YAML once control-flow blocks are removed: %w", err)
	}
	if doc == nil {
		return map[string]interface{}{}, nil
	}
	data, ok := doc.RawData().(map[string]interface{})
	if !ok {
		return map[string]interface{}{}, nil
	}

	evaluateBestEffort(data, engine)
	return data, nil
}

// stripBlockLines reconstructs source text with every top-level
// control-flow block's lines (marker and body, at every depth inside it)
// removed, leaving only the lines that were never part of a block. Only
// top-level items are consulted because a nested block's lines are already
// entirely contained within its enclosing top-level block's line range.
func stripBlockLines(lines []scanLine, topLevel []item) string {
	keep := make([]bool, len(lines))
	for i := range keep {
		keep[i] = true
	}
	for idx := range topLevel {
		it := &topLevel[idx]
		if it.kind == itemRaw {
			continue
		}
		for i := it.startLine; i <= it.endLine && i < len(keep); i++ {
			keep[i] = false
		}
	}

	out := make([]string, 0, len(lines))
	for i, l := range lines {
		if keep[i] {
			out = append(out, l.text)
		}
	}
	return strings.Join(out, "\n")
}

// evaluateBestEffort runs the normal operator dataflow over data and
// applies every operator that resolves successfully, exactly like a normal
// evaluation pass. An operator that errors is left unresolved and its key
// is deleted from data rather than aborting the pass — spec §8.3 step 2's
// "an unresolvable operator leaves the key absent". A structural dataflow
// failure (a genuine dependency cycle, vanishingly unlikely in a scope that
// has already had every control-flow block stripped out of it) leaves data
// as plain, unevaluated YAML rather than erroring the whole preprocessor:
// conditions referencing an unresolved literal "(( ... ))" string will
// themselves fail to resolve later and be treated as absent by the same
// mechanism callers already use for missing keys.
func evaluateBestEffort(data map[string]interface{}, engine graft.Engine) {
	ev := &graft.Evaluator{Tree: data, DataflowOrder: "alphabetical"}
	ev.SetEngine(engine)
	if err := graft.SetupOperators(graft.EvalPhase); err != nil {
		return
	}
	ops, err := ev.DataFlow(graft.EvalPhase)
	if err != nil {
		return
	}
	for _, op := range ops {
		if runErr := ev.RunOp(op); runErr != nil {
			if where := op.Where(); where != nil {
				_ = where.Delete(data)
			}
		}
	}
}

// isOneReference reports whether raw, a condition, iterable, or case
// subject, lexes with the expression parser's own rules to exactly one
// identifier or reference token and nothing else, such as "services",
// "deployment.type", "jobs.postgresql/0.ips", "$.meta.list", "/list",
// "$[key]", "lists[0]", or "1e+5/2".
//
// This needs special handling because of how graft's own parser treats a
// whole-expression bare word or reference inside "(( ... ))" (spec §3 (A6),
// decision B-1, and the BOSH-placeholder pass-through): both are
// deliberately left as unparsed literal text (so a not-yet-evaluated
// "(( some_key ))" or "((var.subkey))" placeholder can survive a merge
// pass unresolved), and a bare identifier that *does* collide with a
// registered operator name is parsed as a call to that operator (H1).
// Neither reading is useful for a control-flow
// condition/iterable/subject, which the docs consistently write as a bare
// document key (`for svc in services`, `(( case cloud_provider ))`) and
// which must always resolve to that key's value (spec decision C-1: "both
// [bare and `grab`] work, shown producing the same output"). So a subject
// that is one reference is evaluated as an explicit "(( grab <ref> ))",
// sidestepping the literal-passthrough and operator-name-collision cases
// entirely. Asking the lexer, and not a pattern of its own, keeps this
// check in step with how the same text reads anywhere else in a
// document. Anything else, such as a number, a boolean, a string, or an
// expression with an operator in it, is parsed as written.
func isOneReference(raw string, engine graft.Engine) bool {
	tz := newExprLexer(raw, engine)
	tok := tz.NextToken()
	if tok.Type != interfaces.TokenIdentifier && tok.Type != interfaces.TokenReference {
		return false
	}
	if tok.Pos.Offset != 0 || len(tok.Literal) != len(raw) {
		return false
	}
	return tz.NextToken().Type == interfaces.TokenEOF
}

// evalExpr evaluates raw graft expression text (no surrounding "(( ))")
// against env's current tree and returns its Go value. location is used
// only to build a Genesis-contract-shaped "$.<location>: message" error
// (spec §1.2's " - $.path: msg" format) if evaluation fails; it does not
// need to be a real document path.
func evalExpr(raw string, e *env, location string) (interface{}, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("$.%s: empty expression", location)
	}
	if isOneReference(raw, e.engine) {
		raw = "grab " + raw
	}
	v := e.viewFor(raw)

	op, err := graft.ParseOpcallWithParserForEngine(e.engine, graft.EvalPhase, "(( "+v.text+" ))")
	if err != nil {
		return nil, withoutAlias(errtext.WithPlain(fmt.Errorf("$.%s: invalid expression %q: %w", location, raw, err),
			fmt.Sprintf("$.%s: invalid expression: %s", location, err)), v)
	}
	if op == nil {
		return nil, errtext.WithPlain(fmt.Errorf("$.%s: invalid expression %q", location, raw), fmt.Sprintf("$.%s: invalid expression", location))
	}
	if cursor, cerr := tree.ParseCursor(location); cerr == nil {
		op.SetWhere(cursor)
	}

	ev := &graft.Evaluator{Tree: v.tree}
	ev.SetEngine(e.engine)
	resp, err := op.Run(ev)
	if err != nil {
		return nil, withoutAlias(err, v) // already "$.<location>: msg" via Opcall.Run
	}
	return resp.Value, nil
}

// evalTruthy evaluates raw as a condition and returns its truthiness. It
// delegates the actual truthy/falsy judgment to graft's own "!" operator
// (via double negation) instead of duplicating operators/operator_utils.go's
// isTruthy table here, so control-flow conditions can never drift from
// what (( ! )), (( && )), and (( ?: )) consider true or false elsewhere in
// the same document.
func evalTruthy(raw string, e *env, location string) (bool, error) {
	val, err := evalExpr(fmt.Sprintf("! ! (%s)", strings.TrimSpace(raw)), e, location)
	if err != nil {
		return false, err
	}
	b, ok := val.(bool)
	if !ok {
		return false, errtext.WithPlain(fmt.Errorf("$.%s: condition %q did not evaluate to a boolean (got %T)", location, raw, val),
			fmt.Sprintf("$.%s: condition did not evaluate to a boolean (got %T)", location, val))
	}
	return b, nil
}
