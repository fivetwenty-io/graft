package operators

import (
	"fmt"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// GrabOperator handles nested operator calls.
type GrabOperator struct{}

// Setup ...
func (GrabOperator) Setup() error {
	return nil
}

// Phase ...
func (GrabOperator) Phase() OperatorPhase {
	return EvalPhase
}

// Dependencies ...
func (GrabOperator) Dependencies(ev *Evaluator, args []*Expr, locs []*tree.Cursor, auto []*tree.Cursor) []*tree.Cursor {
	deps := make([]*tree.Cursor, 0, len(auto))
	deps = append(deps, auto...)

	for _, arg := range args {
		if arg != nil {
			argDeps := arg.Dependencies(ev, locs)
			deps = append(deps, argDeps...)
		}
	}

	return deps
}

// resolveGrabArgValue resolves and post-processes one (( grab ... ))
// argument: a bare reference is resolved directly, a nested-call's
// computed string result is re-interpreted as a grab path and resolved
// against the tree, and a literal/env-var/other value is used as-is.
//
// A LogicalOr argument (a `||` fallback nested inside grab's own argument,
// e.g. `grab (concat "a." "b") || fallback`) recurses into this same
// dispatch for whichever side is being tried, instead of resolving the raw
// sub-expression once and using it as-is: that is what applies the
// reference/computed-path/literal dispatch below to whichever side
// actually produced the value, and what lets a computed path that fails
// to resolve — not just a sub-expression that errors outright — fall
// through to the right side.
func resolveGrabArgValue(ev *Evaluator, arg *Expr) (interface{}, error) {
	if arg == nil {
		return nil, nil
	}

	if arg.Type == LogicalOr {
		if val, err := resolveGrabArgValue(ev, arg.Left); err == nil {
			return val, nil
		}
		return resolveGrabArgValue(ev, arg.Right)
	}

	// Substitute any bracket-notation dynamic key segments (e.g.
	// "key[lookup]") before resolving the reference itself, so both the
	// resolution below and ResolveOperatorArgument see the substituted
	// path.
	if arg.Type == Reference {
		if err := resolveGrabDynamicBrackets(arg, ev); err != nil {
			return nil, err
		}
	}

	// Use ResolveOperatorArgument to handle nested expressions.
	val, err := ResolveOperatorArgument(ev, arg)
	if err != nil {
		return nil, err
	}

	if arg.Type == Reference {
		// Direct reference, resolve it normally.
		resolved, err := arg.Reference.Resolve(ev.Tree)
		if err != nil {
			return nil, fmt.Errorf("unable to resolve `%s`: %w", arg.Reference, err)
		}
		return resolved, nil
	}

	// A nested (( grab ... )) call's result is already grab's own final,
	// fully-resolved answer, not a computed path fragment like a nested
	// concat's is — the documented `grab a.b || grab c.d` fallback
	// pattern needs the right side's grab result used as-is, not re-grabbed.
	//
	// A nested (( prune )) call is excluded for a different reason:
	// prune's own Run (op_prune.go) marks ev.Here for deletion as a side
	// effect the moment it runs, and the key is dropped in post-processing
	// regardless of what value ends up written here — but mid-evaluation
	// that return value is ev.Here's own not-yet-replaced content, i.e.
	// this marker's own raw source text, which is not a path. Using it
	// as-is (like the grab case above) instead of trying to re-resolve it
	// is what keeps `grab X || (( prune ))` working.
	isPathExempt := arg.Type == OperatorCall && (arg.Op() == "grab" || arg.Op() == "prune")

	if pathStr, ok := val.(string); ok && arg.Type != Literal && arg.Type != EnvVar && !isPathExempt {
		// If the resolved value is a string from an expression (not a
		// literal or env var), it might be a reference path.
		cursor, cerr := tree.ParseCursor(pathStr)
		if cerr != nil {
			// Not a valid path, use the string value as-is.
			return pathStr, nil
		}
		// It's a valid path, try to resolve it.
		resolved, rerr := cursor.Resolve(ev.Tree)
		if rerr != nil {
			return nil, fmt.Errorf("unable to resolve `%s`: %w", pathStr, rerr)
		}
		return resolved, nil
	}

	// For literals and other non-string values, use them directly.
	return val, nil
}

// Run ...
//
//nolint:gocyclo // grab operator handles multiple argument formats including nested expressions
func (GrabOperator) Run(ev *Evaluator, args []*Expr) (*Response, error) {
	DEBUG("running (( grab ... )) operation at $.%s", ev.Here)
	defer DEBUG("done with (( grab ... )) operation at $%s\n", ev.Here)

	var vals []interface{}

	for i, arg := range args {
		val, err := resolveGrabArgValue(ev, arg)
		if err != nil {
			DEBUG("     [%d]: resolution failed\n    error: %s", i, err)
			return nil, err
		}

		// Allow nil values to pass through - they are valid values
		if val == nil {
			DEBUG("     [%d]: resolved to nil (allowed)", i)
		} else {
			DEBUG("  arg[%d]: resolved to value (type: %T)", i, val)
		}
		vals = append(vals, val)
		DEBUG("")
	}

	switch len(args) {
	case 0:
		DEBUG("  no arguments supplied to (( grab ... )) operation.  oops.")
		return nil, ansi.Errorf("no arguments specified to @c{(( grab ... ))}")

	case 1:
		DEBUG("  called with only one argument; returning value as-is")
		return &Response{
			Type:  Replace,
			Value: vals[0],
		}, nil

	default:
		DEBUG("  called with more than one arguments; flattening top-level lists into a single list")
		flat := []interface{}{}
		for i, lst := range vals {
			switch v := lst.(type) {
			case []interface{}:
				DEBUG("    [%d]: value is a list; flattening it out", i)
				flat = append(flat, v...)
			default:
				DEBUG("    [%d]: value is not a list; appending it as-is", i)
				flat = append(flat, lst)
			}
		}
		DEBUG("")

		return &Response{
			Type:  Replace,
			Value: flat,
		}, nil
	}
}

//nolint:gochecknoinits // Operator registration must happen at package load time
func init() {
	RegisterOp("grab", GrabOperator{})
}
