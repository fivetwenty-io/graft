package eval

import (
	"iter"
	"slices"
	"strings"

	"github.com/fivetwenty-io/graft/internal/query/ast"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// assign runs an assignment operator. It collects the paths the left side
// names and hands them to the dialect, which decides how the right side
// changes the value at each one. The right side of "|=", and of a named
// form such as yq's "style|=", reads as any expression does, and that of
// "=" and the other forms reads in IndexSource mode. Tracking cursors, the
// right side runs at the place of the value it is given, and an assignment
// below a document's root runs on the document, as assignIn describes.
func (ev evaluator) assign(n *ast.Assign, env *Env, in *value.Value, p value.Path, emit runtime.Emit) error {
	mode, err := ev.assignMode(n.Op)
	if err != nil {
		return err
	}
	lhs := ev.pathsFor(runtime.IndexUpdate)
	src := evaluator{state: ev.state, mode: runtime.IndexSource}
	if strings.HasSuffix(strings.TrimRight(n.Op, "c"), "|=") {
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
	start := value.Path{}
	if ev.derives() {
		// The left side runs at root's place, so an operator that reads
		// the cursor, such as key, answers for the node it is given.
		start = runtime.Rooted(root, doc, nil)
	}
	err := lhs.eval(n.L, env, root, start, func(v *value.Value, vp value.Path) error {
		if vp == nil {
			if ev.ctx.Lax {
				return nil
			}
			return invalidPathResult(v)
		}
		if t, _, abs := runtime.Unroot(vp); t != nil {
			vp = abs
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
		at := renamedPath(base, changed, rel, paths)
		out := runtime.Cursor{Root: changed, Path: at}.Node()
		if out == nil {
			out = value.Null()
		}
		if err := emit(out, runtime.Rooted(changed, doc, at)); err != nil {
			return err
		}
	}
	return nil
}

// renamedPath is rel, the place of the node an assignment ran at, after
// the assignment renamed any map key on the way to it. A key it renamed
// stands at the same position in the map as before.
func renamedPath(base, changed *value.Value, rel value.Path, paths []value.Path) value.Path {
	var keys []value.Path
	for _, p := range paths {
		if n := len(p); n > 1 && p[n-1] == runtime.KeyMark && n-1 <= len(rel) {
			keys = append(keys, p[:n-1])
		}
	}
	if len(keys) == 0 {
		return rel
	}
	slices.SortStableFunc(keys, func(a, b value.Path) int { return len(a) - len(b) })
	out := slices.Clone(rel)
	for _, k := range keys {
		j := len(k) - 1
		if !slices.EqualFunc(k, rel[:j+1], func(a, b *value.Value) bool { return a.Kind() == b.Kind() && value.Compare(a, b) == 0 }) {
			continue
		}
		before := runtime.Cursor{Root: base, Path: k[:j]}.Node()
		after := runtime.Cursor{Root: changed, Path: out[:j]}.Node()
		if before == nil || after == nil || before.Kind() != value.KindObject || after.Kind() != value.KindObject {
			continue
		}
		for i, e := range before.Entries() {
			if e.Key.Kind() == value.KindString && k[j].Kind() == value.KindString && e.Key.Str() == k[j].Str() && i < after.Len() {
				if nk := after.Entries()[i].Key; nk.Kind() == value.KindString {
					out[j] = value.Str(nk.Str())
				}
				break
			}
		}
	}
	return out
}

// assignMode returns the dialect mode for an assignment operator. The
// arithmetic forms combine values through the same natives as the binary
// operators, and "//=" keeps a value that is already truthy. Flags after
// the operator, such as yq's "=c" and "*=+", stay in the mode's Op for
// the dialect, and "*=" passes its flags on to the merge as "*" does.
func (ev evaluator) assignMode(op string) (runtime.AssignMode, error) {
	switch strings.TrimRight(op, "c") {
	case "=":
		return runtime.AssignSet.As(op), nil
	case "|=":
		return runtime.AssignUpdate.As(op), nil
	case "//=":
		return runtime.AssignArith(op, func(cur, rhs *value.Value) (*value.Value, error) {
			if ev.ctx.Dialect.Truthy(cur) {
				return cur, nil
			}
			return rhs, nil
		}), nil
	}
	base, after, _ := strings.Cut(op, "=")
	name, flags, ok := arithmeticOp(base + after)
	if !ok && named(op) {
		return runtime.AssignNamed(op), nil
	}
	if !ok {
		return runtime.AssignMode{}, RuntimeError("unknown assignment operator " + op)
	}
	return runtime.AssignArith(op, func(cur, rhs *value.Value) (*value.Value, error) {
		args := []runtime.Thunk{constThunk{cur}, constThunk{rhs}}
		if flags != "" {
			args = append(args, constThunk{value.Str(flags)})
		}
		out, err := ev.first(name, value.Null(), args)
		if err == nil && out == nil {
			out = value.Null()
		}
		return out, err
	}), nil
}

// named reports whether op is an assignment that a name leads, such as
// "style=" or "line_comment|=".
func named(op string) bool {
	c := op[0]
	return c == '_' || 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z'
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
