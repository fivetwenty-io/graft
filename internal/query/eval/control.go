package eval

import (
	"github.com/fivetwenty-io/graft/internal/query/ast"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// downstream marks every error emit returns, so a construct that catches
// errors can tell its own body's errors from its consumer's.
func downstream(emit runtime.Emit) runtime.Emit {
	return func(v *value.Value, p value.Path) error {
		if err := emit(v, p); err != nil {
			return &DownstreamError{Err: err}
		}
		return nil
	}
}

// ownError sorts the error a body wrapped in downstream returned. It
// reports mine for the body's own error, and otherwise returns the
// consumer's error with one marker removed.
func ownError(err error) (mine bool, out error) {
	if d, ok := err.(*DownstreamError); ok { //nolint:errorlint // only the marker this construct added is removed
		return false, d.Err
	}
	return true, err
}

// alt runs L // R. It emits every truthy output of L, and runs R only when
// L has none. Errors from L are not suppressed, as in jq 1.8.2.
func (ev evaluator) alt(n *ast.Alt, env *Env, in *value.Value, p value.Path, emit runtime.Emit) error {
	found := false
	err := ev.eval(n.L, env, in, p, func(v *value.Value, vp value.Path) error {
		if !ev.ctx.Dialect.Truthy(v) {
			return nil
		}
		found = true
		return emit(v, vp)
	})
	if err != nil || found {
		return err
	}
	return ev.eval(n.R, env, in, p, emit)
}

// cond runs an if from its i-th condition, where 0 is the if and each
// later one is an elif.
func (ev evaluator) cond(n *ast.If, i int, env *Env, in *value.Value, p value.Path, emit runtime.Emit) error {
	if i > len(n.Elifs) {
		if n.Else == nil {
			return ev.each(in, p, emit)
		}
		return ev.eval(n.Else, env, in, p, emit)
	}
	test, then := n.Cond, n.Then
	if i > 0 {
		test, then = n.Elifs[i-1].Cond, n.Elifs[i-1].Then
	}
	return ev.values().eval(test, env, in, nil, func(c *value.Value, _ value.Path) error {
		if ev.ctx.Dialect.Truthy(c) {
			return ev.eval(then, env, in, p, emit)
		}
		return ev.cond(n, i+1, env, in, p, emit)
	})
}

// try runs body and hands a runtime error or a break raised inside it to
// catch, or drops it when catch is nil. Errors from the consumer, stops,
// halts, and cancellations pass through.
func (ev evaluator) try(body, catch ast.Node, env *Env, in *value.Value, p value.Path, emit runtime.Emit) error {
	err := ev.eval(body, env, in, p, downstream(emit))
	if err == nil {
		return nil
	}
	mine, err := ownError(err)
	if !mine || !catchable(err) {
		return err
	}
	if catch == nil {
		return nil
	}
	return ev.eval(catch, env, caught(err), nil, emit)
}

// caught is the value a catch receives for err.
func caught(err error) *value.Value {
	switch err := err.(type) { //nolint:errorlint // catchable has already matched the exact type
	case *query.RuntimeError:
		if err.Value != nil {
			return err.Value
		}
		return value.Str(err.Msg)
	case *BreakError:
		return value.Obj(value.Pair{Key: "__jq", Value: value.Num(float64(err.Label))})
	default:
		return value.Str(err.Error())
	}
}

// label runs body and ends it quietly at a break to this label.
func (ev evaluator) label(n *ast.Label, env *Env, in *value.Value, p value.Path, emit runtime.Emit) error {
	id := ev.labels
	ev.labels++
	err := ev.eval(n.Body, env.bindLabel(n.Name, id), in, p, emit)
	if b, ok := err.(*BreakError); ok && b.Label == id { //nolint:errorlint // a break reaches its label unwrapped
		return nil
	}
	return err
}

// reduce folds every output of Src into a state that starts at each output
// of Init. An update with no output leaves null, and one with several
// leaves the last. In path mode the state keeps the path of Init's output
// only while it is that very value, which is how jq 1.8.2 tracks it.
func (ev evaluator) reduce(n *ast.Reduce, env *Env, in *value.Value, p value.Path, emit runtime.Emit) error {
	return ev.eval(n.Init, env, in, p, func(v0 *value.Value, p0 value.Path) error {
		state := v0
		err := ev.values().eval(n.Src, env, in, nil, func(x *value.Value, _ value.Path) error {
			return ev.bindPats(n.Pats, x, env, func(env *Env) error {
				var last *value.Value
				err := ev.eval(n.Update, env, state, ev.statePath(state, v0, p0), func(v *value.Value, _ value.Path) error {
					last = v
					return nil
				})
				if err != nil {
					return err
				}
				if last == nil {
					last = value.Null()
				}
				state = last
				return nil
			})
		})
		if err != nil {
			return err
		}
		return emit(state, ev.statePath(state, v0, p0))
	})
}

// foreach is reduce that emits every output of each update, or of Extract
// applied to it.
func (ev evaluator) foreach(n *ast.Foreach, env *Env, in *value.Value, p value.Path, emit runtime.Emit) error {
	if len(n.Pats) > 1 {
		emit = downstream(emit)
	}
	return ev.eval(n.Init, env, in, p, func(v0 *value.Value, p0 value.Path) error {
		state := v0
		return ev.values().eval(n.Src, env, in, nil, func(x *value.Value, _ value.Path) error {
			return ev.bindPats(n.Pats, x, env, func(env *Env) error {
				var last *value.Value
				err := ev.eval(n.Update, env, state, ev.statePath(state, v0, p0), func(v *value.Value, vp value.Path) error {
					last = v
					if n.Extract == nil {
						return emit(v, vp)
					}
					return ev.eval(n.Extract, env, v, vp, emit)
				})
				if err != nil {
					return err
				}
				if last == nil {
					last = value.Null()
				}
				state = last
				return nil
			})
		})
	})
}

// statePath is the path a reduce or foreach state carries in path mode.
func (ev evaluator) statePath(state, v0 *value.Value, p0 value.Path) value.Path {
	if ev.paths && state == v0 {
		return p0
	}
	return nil
}

// bind runs "Src as Pats | Body". The body reads the bind's own input, not
// the value it destructures.
func (ev evaluator) bind(n *ast.Bind, env *Env, in *value.Value, p value.Path, emit runtime.Emit) error {
	if len(n.Pats) > 1 {
		emit = downstream(emit)
	}
	return ev.values().eval(n.Src, env, in, nil, func(x *value.Value, _ value.Path) error {
		return ev.bindPats(n.Pats, x, env, func(env *Env) error {
			return ev.eval(n.Body, env, in, p, emit)
		})
	})
}

// bindPats destructures x by pats and runs body with the bindings. With
// "?//" alternatives every variable starts out null, and an error that the
// destructuring or the body raises moves on to the next pattern. The last
// pattern's error stands. Callers with alternatives wrap their emit in
// downstream, so a consumer's error never counts as the body's.
func (ev evaluator) bindPats(pats []ast.Pattern, x *value.Value, env *Env, body func(*Env) error) error {
	if len(pats) == 1 {
		return ev.destructure(&pats[0], x, env, body)
	}
	for i := range pats {
		for _, name := range patternVars(&pats[i], nil) {
			env = env.Bind(name, value.Null())
		}
	}
	var err error
	for i := range pats {
		err = ev.destructure(&pats[i], x, env, body)
		if err == nil {
			return nil
		}
		var mine bool
		if mine, err = ownError(err); !mine || !catchable(err) {
			return err
		}
	}
	return err
}

// patternVars appends the names a pattern binds.
func patternVars(pat *ast.Pattern, names []string) []string {
	switch pat.Kind {
	case ast.PatVar:
		names = append(names, pat.Name)
	case ast.PatArray:
		for i := range pat.Elems {
			names = patternVars(&pat.Elems[i], names)
		}
	case ast.PatObject:
		for _, e := range pat.Entries {
			if e.KeyVar != "" {
				names = append(names, e.KeyVar)
			}
			if e.Val != nil {
				names = patternVars(e.Val, names)
			}
		}
	}
	return names
}

// destructure binds pat against v and runs k once for every set of
// bindings, since an object pattern's key may have several outputs. Key
// expressions read v, the value at their own level.
func (ev evaluator) destructure(pat *ast.Pattern, v *value.Value, env *Env, k func(*Env) error) error {
	switch pat.Kind {
	case ast.PatVar:
		return k(env.Bind(pat.Name, v))
	case ast.PatArray:
		return ev.destructureArray(pat.Elems, 0, v, env, k)
	default:
		return ev.destructureObject(pat.Entries, v, env, k)
	}
}

func (ev evaluator) destructureArray(elems []ast.Pattern, i int, v *value.Value, env *Env, k func(*Env) error) error {
	if i == len(elems) {
		return k(env)
	}
	x, err := ev.element(v, value.Num(float64(i)))
	if err != nil {
		return err
	}
	return ev.destructure(&elems[i], x, env, func(env *Env) error {
		return ev.destructureArray(elems, i+1, v, env, k)
	})
}

func (ev evaluator) destructureObject(entries []ast.PatEntry, v *value.Value, env *Env, k func(*Env) error) error {
	if len(entries) == 0 {
		return k(env)
	}
	e, rest := entries[0], entries[1:]
	next := func(key *value.Value, env *Env) error {
		x, err := ev.element(v, key)
		if err != nil {
			return err
		}
		if e.KeyVar != "" {
			env = env.Bind(e.KeyVar, x)
		}
		if e.Val == nil {
			return ev.destructureObject(rest, v, env, k)
		}
		return ev.destructure(e.Val, x, env, func(env *Env) error {
			return ev.destructureObject(rest, v, env, k)
		})
	}
	if e.KeyVar != "" {
		return next(value.Str(e.KeyVar), env)
	}
	return ev.values().eval(e.Key, env, v, nil, func(key *value.Value, _ value.Path) error {
		return next(key, env)
	})
}

// element indexes v by key for destructuring, where a missing value is
// null.
func (ev evaluator) element(v, key *value.Value) (*value.Value, error) {
	x, err := ev.ctx.Dialect.Index(v, key, runtime.IndexRead)
	if err != nil {
		return nil, err
	}
	if x == nil {
		return value.Null(), nil
	}
	return x, nil
}
