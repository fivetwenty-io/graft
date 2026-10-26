package eval

import "github.com/fivetwenty-io/graft/internal/query/ast"

// ownsState reports whether a reduce may hold its state in a value.Owner
// while its update runs, so that the update's assignments change the state
// in place. That is safe when nothing but the reduce can ever reach the
// state, which holds when the update is a pipe of assignments whose every
// part gives at most one value and builds nothing that could contain the
// state: then the state flows only from one assignment to the next, and
// the right side's value, which may be the state itself, is let go before
// the Editor stores it.
//
// Paths and cursors carry the state's identity elsewhere, so a run that
// tracks either never holds it. Nor does a reduce with "?//" patterns,
// because an error in its update retries the update on the state as the
// failed attempt left it.
func (ev evaluator) ownsState(n *ast.Reduce) bool {
	return !ev.paths && !ev.derives() && len(n.Pats) == 1 && inPlace(n.Update)
}

// inPlace reports whether n is a pipe of assignments that may change their
// input in place.
func inPlace(n ast.Node) bool {
	switch n := n.(type) {
	case *ast.Pipe:
		return inPlace(n.L) && inPlace(n.R)
	case *ast.Assign:
		switch n.Op {
		case "=", "|=", "+=", "-=", "*=", "/=", "%=", "//=":
			return place(n.L) && plain(n.R)
		}
	}
	return false
}

// place reports whether n names one path of fields and indexes below its
// input, with keys that plain allows.
func place(n ast.Node) bool {
	switch n := n.(type) {
	case nil, *ast.Identity:
		return true
	case *ast.Field:
		return !n.Opt && place(n.Target)
	case *ast.Index:
		return !n.Opt && place(n.Target) && plain(n.Key)
	}
	return false
}

// plain reports whether n gives at most one value and keeps no reference
// to its input other than by returning it or a part of it. Such an
// expression reads its input through fields, indexes, and slices,
// combines what it reads with operators, and calls nothing, so nothing it
// runs can store the input anywhere or change it.
//
//nolint:gocyclo // one case for each kind of node it allows
func plain(n ast.Node) bool {
	switch n := n.(type) {
	case nil, *ast.Identity, *ast.Literal, *ast.Var:
		return true
	case *ast.Field:
		return !n.Opt && plain(n.Target)
	case *ast.Index:
		return !n.Opt && plain(n.Target) && plain(n.Key)
	case *ast.Slice:
		return !n.Opt && plain(n.Target) && plain(n.From) && plain(n.To)
	case *ast.Str:
		for _, part := range n.Parts {
			if !plain(part) {
				return false
			}
		}
		return true
	case *ast.Binary:
		return plain(n.L) && plain(n.R)
	case *ast.Unary:
		return plain(n.X)
	case *ast.Alt:
		return plain(n.L) && plain(n.R)
	case *ast.Pipe:
		return plain(n.L) && plain(n.R)
	case *ast.If:
		for _, e := range n.Elifs {
			if !plain(e.Cond) || !plain(e.Then) {
				return false
			}
		}
		return plain(n.Cond) && plain(n.Then) && plain(n.Else)
	}
	return false
}
