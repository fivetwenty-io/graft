package controlflow

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/fivetwenty-io/graft/pkg/graft"
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

// tree returns the flattened map an expression should be evaluated against:
// the prescan scope with current bindings overlaid.
func (e *env) tree() map[string]interface{} {
	if len(e.bindings) == 0 {
		return e.scope
	}
	merged := make(map[string]interface{}, len(e.scope)+len(e.bindings))
	for k, v := range e.scope {
		merged[k] = v
	}
	for k, v := range e.bindings {
		merged[k] = v
	}
	return merged
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

// bareIdentifierRe matches a condition/iterable/subject that is nothing but
// a single unqualified identifier, e.g. "services", "cloud_provider".
//
// This needs special handling because of how graft's own parser treats a
// bare word at the very first token of "(( ... ))" (spec §3 (A6), decision
// B-1): an *unregistered* bare identifier there is deliberately left as
// unparsed literal text (so a not-yet-evaluated "(( some_key ))" value can
// survive a merge pass unresolved), and a bare identifier that *does*
// collide with a registered operator name is parsed as a call to that
// operator (H1). Neither reading is useful for a control-flow
// condition/iterable/subject, which the docs consistently write as a bare
// document key (`for svc in services`, `(( case cloud_provider ))`) and
// which must always resolve to that key's value (spec decision C-1: "both
// [bare and `grab`] work, shown producing the same output"). So a bare
// identifier here is evaluated as an explicit "(( grab <name> ))" — the
// same reference resolution a dotted path already gets for free by lexing
// as a single TokenReference — sidestepping both the literal-passthrough
// and operator-name-collision cases entirely.
var bareIdentifierRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

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
	if bareIdentifierRe.MatchString(raw) {
		raw = "grab " + raw
	}

	op, err := graft.ParseOpcallWithParserForEngine(e.engine, graft.EvalPhase, "(( "+raw+" ))")
	if err != nil {
		return nil, fmt.Errorf("$.%s: invalid expression %q: %w", location, raw, err)
	}
	if op == nil {
		return nil, fmt.Errorf("$.%s: invalid expression %q", location, raw)
	}
	if cursor, cerr := tree.ParseCursor(location); cerr == nil {
		op.SetWhere(cursor)
	}

	ev := &graft.Evaluator{Tree: e.tree()}
	ev.SetEngine(e.engine)
	resp, err := op.Run(ev)
	if err != nil {
		return nil, err // already "$.<location>: msg" via Opcall.Run
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
		return false, fmt.Errorf("$.%s: condition %q did not evaluate to a boolean (got %T)", location, raw, val)
	}
	return b, nil
}
