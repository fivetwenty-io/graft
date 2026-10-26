package eval

import (
	"iter"

	"github.com/fivetwenty-io/graft/internal/query/ast"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// assign runs an assignment operator. It collects the paths the left side
// names and hands them to the dialect, which decides how the right side
// changes the value at each one. The right side of "|=" reads as any
// expression does, and that of "=" and the arithmetic forms reads in
// IndexSource mode. Tracking cursors, the right side runs at the place of
// the value it is given.
func (ev evaluator) assign(n *ast.Assign, env *Env, in *value.Value, p value.Path, emit runtime.Emit) error {
	mode, err := ev.assignMode(n.Op)
	if err != nil {
		return err
	}
	lhs := ev.pathsFor(runtime.IndexUpdate)
	src := evaluator{state: ev.state, mode: runtime.IndexSource}
	if n.Op == "|=" {
		src.mode = runtime.IndexRead
	}
	rhs := func(cur *value.Value, _ value.Path) iter.Seq2[*value.Value, error] {
		return src.seq(n.R, env, cur, nil)
	}
	return ev.each(in, p, func(root *value.Value, rp value.Path) error {
		f := rhs
		var doc int
		if ev.derives() {
			_, doc, _ = runtime.Unroot(rp)
			f = func(cur *value.Value, at value.Path) iter.Seq2[*value.Value, error] {
				if rp == nil {
					return src.seq(n.R, env, cur, runtime.Rooted(root, 0, at))
				}
				return src.seq(n.R, env, cur, extend(rp, at...))
			}
		}
		var paths []value.Path
		err := lhs.eval(n.L, env, root, value.Path{}, func(v *value.Value, vp value.Path) error {
			if vp == nil {
				if ev.ctx.Lax {
					return nil
				}
				return invalidPathResult(v)
			}
			paths = append(paths, vp)
			return nil
		})
		if err != nil {
			return err
		}
		for out, err := range ev.ctx.Dialect.Assign(ev.ctx, root, paths, f, mode) {
			if err != nil {
				return err
			}
			// Tracking cursors, the result stands in for its root, so it
			// is a root of its own rather than a value built from nothing.
			var op value.Path
			if ev.derives() {
				op = runtime.Rooted(out, doc, nil)
			}
			if err := emit(out, op); err != nil {
				return err
			}
		}
		return nil
	})
}

// assignMode returns the dialect mode for an assignment operator. The
// arithmetic forms combine values through the same natives as the binary
// operators, and "//=" keeps a value that is already truthy.
func (ev evaluator) assignMode(op string) (runtime.AssignMode, error) {
	switch op {
	case "=":
		return runtime.AssignSet, nil
	case "|=":
		return runtime.AssignUpdate, nil
	case "//=":
		return runtime.AssignArith(op, func(cur, rhs *value.Value) (*value.Value, error) {
			if ev.ctx.Dialect.Truthy(cur) {
				return cur, nil
			}
			return rhs, nil
		}), nil
	}
	name, ok := arithmetic[op[:len(op)-1]]
	if !ok {
		return runtime.AssignMode{}, RuntimeError("unknown assignment operator " + op)
	}
	return runtime.AssignArith(op, func(cur, rhs *value.Value) (*value.Value, error) {
		out, err := ev.first(name, value.Null(), []runtime.Thunk{constThunk{cur}, constThunk{rhs}})
		if err == nil && out == nil {
			out = value.Null()
		}
		return out, err
	}), nil
}

// seqStop is the signal a sequence raises inside the evaluator when its
// consumer stops ranging over it.
type seqStop struct{}

func (*seqStop) Error() string { return "sequence stopped" }

// seq adapts the outputs of n on cur, whose path is at, to a sequence that
// stops evaluating as soon as its consumer stops ranging. ev is a values
// evaluator.
func (ev evaluator) seq(n ast.Node, env *Env, cur *value.Value, at value.Path) iter.Seq2[*value.Value, error] {
	return func(yield func(*value.Value, error) bool) {
		stopped := false
		err := ev.eval(n, env, cur, at, func(v *value.Value, _ value.Path) error {
			if !yield(v, nil) {
				stopped = true
				return &seqStop{}
			}
			return nil
		})
		if err != nil && !stopped {
			yield(nil, unwrapDownstream(err))
		}
	}
}
