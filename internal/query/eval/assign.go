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
// the value it is given, and an assignment below a document's root runs
// on the document, as assignIn describes.
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
	return ev.each(in, p, func(root *value.Value, rp value.Path) error {
		if ev.derives() {
			if top, doc, rel := runtime.Unroot(rp); top != nil && len(rel) > 0 {
				if base, ok := ev.docFor(top, doc, rel, root); ok {
					return ev.assignIn(n, env, mode, lhs, src, root, rp, base, emit)
				}
			}
		}
		return ev.assignOn(n, env, mode, lhs, src, root, rp, emit)
	})
}

// assignOn runs an assignment on root, whose path is rp, as a value of its
// own.
func (ev evaluator) assignOn(n *ast.Assign, env *Env, mode runtime.AssignMode, lhs, src evaluator, root *value.Value, rp value.Path, emit runtime.Emit) error {
	f := func(cur *value.Value, _ value.Path) iter.Seq2[*value.Value, error] {
		return src.seq(n.R, env, cur, nil)
	}
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
		// Tracking cursors, the result stands in for its root, so it is a
		// root of its own rather than a value built from nothing.
		var op value.Path
		if ev.derives() {
			op = runtime.Rooted(out, doc, nil)
		}
		if err := emit(out, op); err != nil {
			return err
		}
	}
	return nil
}

// docVersion is the latest version of a document that assignments below
// its root changed, and the version that lineage started from.
type docVersion struct {
	origin, latest *value.Value
}

// latest returns the version of document doc an assignment whose input
// sits in top should change. That is the latest version when top is one
// that lineage started from or reached, so sibling assignments build on
// each other's changes as yq's shared nodes do, and top itself otherwise.
func (ev evaluator) latest(top *value.Value, doc int) *value.Value {
	if d, ok := ev.changed[doc]; ok && (top == d.origin || top == d.latest) {
		return d.latest
	}
	return top
}

// docFor returns the document an assignment whose input root sits at rel
// in top should change. That is the latest version of it, with root put
// in place when root is a value computed there rather than top's own
// node. It reports false when rel cannot hold root.
func (ev evaluator) docFor(top *value.Value, doc int, rel value.Path, root *value.Value) (*value.Value, bool) {
	base := ev.latest(top, doc)
	put, ok := replaceAt(base, rel, root)
	if !ok || (runtime.Cursor{Root: top, Path: rel}).Node() != root {
		return put, ok
	}
	return base, true
}

// assignIn runs an assignment whose input root sits below the root of a
// document, as yq's do: base is that document with root in its place,
// the left side runs at root's place in it and may name a node anywhere
// in it, such as parent.d, and the change is made to the document. The
// output is root as the change left it, at its place in the changed
// document. An output already emitted keeps the document as it was then,
// where yq, printing after it evaluates, shows the final one.
func (ev evaluator) assignIn(n *ast.Assign, env *Env, mode runtime.AssignMode, lhs, src evaluator, root *value.Value, rp value.Path, base *value.Value, emit runtime.Emit) error {
	top, doc, rel := runtime.Unroot(rp)
	var paths []value.Path
	err := lhs.eval(n.L, env, root, rp, func(v *value.Value, vp value.Path) error {
		if vp == nil {
			if ev.ctx.Lax {
				return nil
			}
			return invalidPathResult(v)
		}
		t, _, abs := runtime.Unroot(vp)
		if t == nil {
			abs = extend(rel, vp...)
		}
		// A map key is no place a value can be put, so it changes
		// nothing.
		if len(abs) > 0 && abs[len(abs)-1] == runtime.KeyMark {
			return nil
		}
		paths = append(paths, abs)
		return nil
	})
	if err != nil {
		return err
	}
	// The right side of "=" runs on root, and that of the other forms on
	// the value at each path.
	f := func(cur *value.Value, at value.Path) iter.Seq2[*value.Value, error] {
		if at == nil {
			return src.seq(n.R, env, root, rp)
		}
		return src.seq(n.R, env, cur, runtime.Rooted(base, doc, at))
	}
	origin := top
	if d, ok := ev.changed[doc]; ok && top == d.latest {
		origin = d.origin
	}
	for changed, err := range ev.ctx.Dialect.Assign(ev.ctx, base, paths, f, mode) {
		if err != nil {
			return err
		}
		ev.changed[doc] = docVersion{origin: origin, latest: changed}
		out := runtime.Cursor{Root: changed, Path: rel}.Node()
		if out == nil {
			out = value.Null()
		}
		if err := emit(out, runtime.Rooted(changed, doc, rel)); err != nil {
			return err
		}
	}
	return nil
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

// replaceAt returns root with v in place of the value at path, adding a
// key a map or null on the way lacks. It reports false for a path it
// cannot follow, such as one past the end of a sequence or to a key.
func replaceAt(root *value.Value, path value.Path, v *value.Value) (*value.Value, bool) {
	if len(path) == 0 {
		return v, true
	}
	k := path[0]
	if k == runtime.KeyMark {
		return nil, false
	}
	switch {
	case root.Kind() == value.KindNull && k.Kind() == value.KindString:
		c, ok := replaceAt(value.Null(), path[1:], v)
		if !ok {
			return nil, false
		}
		return value.Obj(value.Pair{Key: k.Str(), Value: c}), true
	case root.Kind() == value.KindObject && k.Kind() == value.KindString:
		cur, found := root.Get(k.Str())
		if !found {
			cur = value.Null()
		}
		c, ok := replaceAt(cur, path[1:], v)
		if !ok {
			return nil, false
		}
		return root.Set(k.Str(), c), true
	case root.Kind() == value.KindArray && k.Kind() == value.KindNumber:
		i := int(k.Float())
		if i < 0 {
			i += root.Len()
		}
		cur, found := root.Index(i)
		if !found {
			return nil, false
		}
		c, ok := replaceAt(cur, path[1:], v)
		if !ok {
			return nil, false
		}
		return root.SetIndex(i, c), true
	default:
	}
	return nil, false
}
