package eval

import (
	"github.com/fivetwenty-io/graft/internal/query/ast"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// extend returns p with c appended, always in a new array so that sibling
// paths never share one.
func extend(p value.Path, c ...*value.Value) value.Path {
	out := make(value.Path, 0, len(p)+len(c))
	out = append(out, p...)
	return append(out, c...)
}

// place returns the path of the output found at keys under v, whose own
// path is vp. In path mode that is vp extended. Tracking cursors outside
// path mode, a value with no path of its own is the root its outputs are
// placed under. Otherwise outputs carry no path.
func (ev evaluator) place(v *value.Value, vp value.Path, keys ...*value.Value) value.Path {
	switch {
	case ev.paths:
		return extend(vp, keys...)
	case !ev.track:
		return nil
	case vp == nil:
		return runtime.Rooted(v, 0, keys)
	}
	return extend(vp, keys...)
}

// setCursor points the context's cursor at in, whose tracked path is p.
// The path carries the root and the document, and a value with no path is
// a root of its own that belongs to no document.
func (ev evaluator) setCursor(in *value.Value, p value.Path) {
	root, doc, rel := runtime.Unroot(p)
	if root == nil {
		root, doc, rel = in, runtime.Detached, nil
	}
	ev.ctx.Cursor = &runtime.Cursor{Root: root, Path: rel, Doc: doc}
}

// target runs a postfix node's target, which is the input itself when the
// node has none.
func (ev evaluator) target(t ast.Node, env *Env, in *value.Value, p value.Path, emit runtime.Emit) error {
	if t == nil {
		return emit(in, p)
	}
	return ev.eval(t, env, in, p, emit)
}

// field runs Target.name. A target sees the candidate set itself, so one
// that does not read its input, such as a variable, runs once.
func (ev evaluator) field(n *ast.Field, env *Env, in *value.Value, p value.Path, emit runtime.Emit) error {
	key := value.Str(n.Name)
	look := func(v *value.Value, vp value.Path) error {
		return ev.lookup(v, vp, key, n.Opt, emit)
	}
	if n.Target != nil {
		return ev.eval(n.Target, env, in, p, look)
	}
	return ev.each(in, p, look)
}

// index runs Target[Key]. The key reads the node's own input and varies
// slower than the target.
func (ev evaluator) index(n *ast.Index, env *Env, in *value.Value, p value.Path, emit runtime.Emit) error {
	return ev.each(in, p, func(in *value.Value, p value.Path) error {
		return ev.values().eval(n.Key, env, in, nil, func(key *value.Value, _ value.Path) error {
			return ev.target(n.Target, env, in, p, func(v *value.Value, vp value.Path) error {
				return ev.lookup(v, vp, key, n.Opt, emit)
			})
		})
	})
}

// slice runs Target[From:To] as an index by the object {start, end}. The
// start varies slowest and the target fastest.
func (ev evaluator) slice(n *ast.Slice, env *Env, in *value.Value, p value.Path, emit runtime.Emit) error {
	vev := ev.values()
	bound := func(b ast.Node, in *value.Value, k func(*value.Value) error) error {
		if b == nil {
			return k(value.Null())
		}
		return vev.eval(b, env, in, nil, func(v *value.Value, _ value.Path) error { return k(v) })
	}
	return ev.each(in, p, func(in *value.Value, p value.Path) error {
		return bound(n.From, in, func(from *value.Value) error {
			return bound(n.To, in, func(to *value.Value) error {
				key := value.Obj(value.Pair{Key: "start", Value: from}, value.Pair{Key: "end", Value: to})
				return ev.target(n.Target, env, in, p, func(v *value.Value, vp value.Path) error {
					return ev.lookup(v, vp, key, n.Opt, emit)
				})
			})
		})
	})
}

// lookup indexes v by key through the dialect. With opt set it drops the
// errors the index itself raises, but never one from emit.
func (ev evaluator) lookup(v *value.Value, vp value.Path, key *value.Value, opt bool, emit runtime.Emit) error {
	if ev.paths && vp == nil {
		if opt {
			return nil
		}
		return invalidPathAccess(key, v)
	}
	mode := ev.mode
	if opt && mode == runtime.IndexRead {
		mode = runtime.IndexOptional
	}
	out, err := ev.ctx.Dialect.Index(ev.ctx, v, key, mode)
	if err != nil {
		if opt && catchable(err) {
			return nil
		}
		return err
	}
	if out == nil {
		return nil
	}
	if ev.derives() && v.Kind() != value.KindObject {
		// A slice is a new value that takes v's place rather than a
		// place in v.
		if _, _, ok := value.IsSliceComponent(key); ok {
			return emit(out, derived(v, vp, out))
		}
	}
	return emit(out, ev.place(v, vp, key))
}

// iterate runs Target[], with the same rule for opt as lookup and the
// same rule for the target as field.
func (ev evaluator) iterate(n *ast.Iterate, env *Env, in *value.Value, p value.Path, emit runtime.Emit) error {
	it := func(v *value.Value, vp value.Path) error {
		if ev.paths && vp == nil {
			if n.Opt {
				return nil
			}
			return invalidPathIterate(v)
		}
		var downstream error
		err := ev.ctx.Dialect.Iterate(v, func(k, e *value.Value) error {
			if err := ev.ctx.Tick(); err != nil {
				downstream = err
				return err
			}
			if err := emit(e, ev.place(v, vp, k)); err != nil {
				downstream = err
				return err
			}
			return nil
		})
		if err != nil && downstream == nil && n.Opt && catchable(err) {
			return nil
		}
		return err
	}
	if n.Target != nil {
		return ev.eval(n.Target, env, in, p, it)
	}
	return ev.each(in, p, it)
}

// recurse runs "..", which emits in and everything under it, depth first.
// With keys set it is yq's "...", which also emits map keys.
func (ev evaluator) recurse(in *value.Value, p value.Path, keys bool, emit runtime.Emit) error {
	return ev.ctx.Dialect.Recurse(in, keys, func(v *value.Value, rel value.Path) error {
		if ev.paths && p == nil {
			return emit(v, nil)
		}
		if n := len(rel); ev.paths && n > 0 && rel[n-1] == runtime.KeyMark {
			// A path names the key's value, as a path cannot name a key.
			rel = rel[:n-1]
		}
		return emit(v, ev.place(in, p, rel...))
	})
}

// pathOf runs path(f), which emits the path of every output of f.
func (ev evaluator) pathOf(f ast.Node, env *Env, in *value.Value, p value.Path, emit runtime.Emit) error {
	pev := ev.pathsFor(runtime.IndexRead)
	return ev.each(in, p, func(in *value.Value, _ value.Path) error {
		return pev.eval(f, env, in, value.Path{}, func(v *value.Value, vp value.Path) error {
			if vp == nil {
				return invalidPathResult(v)
			}
			return emit(value.Arr(vp...), nil)
		})
	})
}

// getpathOf runs getpath(f) in path mode, where its outputs extend the
// input's path by each path f produces. The native runs first, so its own
// errors about the path or the input come before an invalid path.
func (ev evaluator) getpathOf(f ast.Node, env *Env, in *value.Value, p value.Path, emit runtime.Emit) error {
	return ev.each(in, p, func(in *value.Value, p value.Path) error {
		return ev.values().eval(f, env, in, nil, func(path *value.Value, _ value.Path) error {
			return ev.native("getpath", in, []runtime.Thunk{constThunk{path}}, func(v *value.Value, _ value.Path) error {
				if p == nil {
					return invalidPathResult(in)
				}
				if path.Kind() != value.KindArray {
					return emit(v, nil)
				}
				return emit(v, extend(p, path.Elems()...))
			})
		})
	})
}
