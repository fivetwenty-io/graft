package eval

import (
	"iter"

	"github.com/fivetwenty-io/graft/internal/query/ast"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// assign runs an assignment operator. It collects the paths the left side
// names and hands them to the dialect, which decides how the right side
// changes the value at each one.
func (ev evaluator) assign(n *ast.Assign, env *Env, in *value.Value, p value.Path, emit runtime.Emit) error {
	mode, err := ev.assignMode(n.Op)
	if err != nil {
		return err
	}
	lhs := ev.pathsFor(runtime.IndexUpdate)
	rhs := func(cur *value.Value) iter.Seq2[*value.Value, error] {
		return ev.seq(n.R, env, cur)
	}
	return ev.each(in, p, func(root *value.Value, _ value.Path) error {
		var paths []value.Path
		err := lhs.eval(n.L, env, root, value.Path{}, func(v *value.Value, vp value.Path) error {
			if vp == nil {
				return invalidPathResult(v)
			}
			paths = append(paths, vp)
			return nil
		})
		if err != nil {
			return err
		}
		for out, err := range ev.ctx.Dialect.Assign(ev.ctx, root, paths, rhs, mode) {
			if err != nil {
				return err
			}
			if err := emit(out, nil); err != nil {
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

// seq adapts the outputs of n on cur to a sequence that stops evaluating
// as soon as its consumer stops ranging.
func (ev evaluator) seq(n ast.Node, env *Env, cur *value.Value) iter.Seq2[*value.Value, error] {
	return func(yield func(*value.Value, error) bool) {
		stopped := false
		err := ev.values().eval(n, env, cur, nil, func(v *value.Value, _ value.Path) error {
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
