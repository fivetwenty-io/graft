package operators

import (
	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// locsUnder returns every loc below the path that ref names. An
// operator that reads the whole value at a path depends on each of
// them, so the operators inside that value run before it reads it, and
// a read of the operator's own ancestor closes a cycle in the data-flow
// graph instead of handing back operator text. The path itself needs
// no entry here, because the automatic dependencies already hold it.
func locsUnder(ev *Evaluator, ref *tree.Cursor, locs []*tree.Cursor) []*tree.Cursor {
	if ref == nil {
		return nil
	}
	canon, err := ref.Canonical(ev.Tree)
	if err != nil {
		return nil
	}
	var deps []*tree.Cursor
	for _, other := range locs {
		if other.Under(canon) {
			deps = append(deps, other)
		}
	}
	return deps
}

// readDeps returns locsUnder for each reference whose value arg can
// hand to its operator whole: arg itself when it is a reference, and
// the operands of a || chain in arg. With fallbacks false, only the
// left operand of each || is followed, for an operator that refuses a
// map or a list either way and only needs the cycle to be reported. A
// nested operator call is left to that operator's own dependencies.
func readDeps(ev *Evaluator, arg *Expr, locs []*tree.Cursor, fallbacks bool) []*tree.Cursor {
	if arg == nil {
		return nil
	}
	switch arg.Type {
	case Reference:
		return locsUnder(ev, arg.Reference, locs)
	case LogicalOr:
		deps := readDeps(ev, arg.Left, locs, fallbacks)
		if fallbacks {
			deps = append(deps, readDeps(ev, arg.Right, locs, fallbacks)...)
		}
		return deps
	default:
		return nil
	}
}
