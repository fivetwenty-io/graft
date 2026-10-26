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
		// One step finishes before the next begins, so the steps share
		// their continuations rather than allocate them each time.
		state := v0
		var last *value.Value
		keep := runtime.Emit(func(v *value.Value, _ value.Path) error {
			last = v
			return nil
		})
		// An owner lets the update change the state in place, from the
		// first copy it takes, as ownsState describes.
		var owner *value.Owner
		if ev.ownsState(n) {
			owner = &value.Owner{}
		}
		step := func(env *Env) error {
			last = nil
			outer := ev.ctx.Owner
			ev.ctx.Owner = owner
			err := ev.eval(n.Update, env, state, ev.statePath(state, v0, p0), keep)
			ev.ctx.Owner = outer
			if err != nil {
				return err
			}
			if last == nil {
				last = value.Null()
			}
			state = last
			return nil
		}
		err := ev.values().eval(n.Src, env, in, nil, func(x *value.Value, _ value.Path) error {
			if err := ev.ctx.Tick(); err != nil {
				return err
			}
			return ev.bindPats(n.Pats, x, env, step)
		})
		if owner != nil {
			owner.Release()
		}
		if err != nil {
			return err
		}
		return emit(state, ev.statePath(state, v0, p0))
	})
}

// foreach is reduce that emits every output of each update, or of Extract
// applied to it.
//
// Each output of the update becomes the state before it goes downstream,
// as in jq 1.8.2, so a "?//" pattern that an error moves past leaves the
// state its update set for the next pattern.
func (ev evaluator) foreach(n *ast.Foreach, env *Env, in *value.Value, p value.Path, emit runtime.Emit) error {
	return ev.eval(n.Init, env, in, p, func(v0 *value.Value, p0 value.Path) error {
		state := v0
		return ev.values().eval(n.Src, env, in, nil, func(x *value.Value, _ value.Path) error {
			if err := ev.ctx.Tick(); err != nil {
				return err
			}
			return ev.bindPats(n.Pats, x, env, func(env *Env) error {
				got := false
				err := ev.eval(n.Update, env, state, ev.statePath(state, v0, p0), func(v *value.Value, vp value.Path) error {
					state, got = v, true
					if n.Extract == nil {
						return emit(v, vp)
					}
					return ev.eval(n.Extract, env, v, vp, emit)
				})
				if err != nil {
					return err
				}
				if !got {
					state = value.Null()
				}
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
// the value it destructures. Tracking cursors, a variable bound whole
// keeps the place of the value it stands for.
func (ev evaluator) bind(n *ast.Bind, env *Env, in *value.Value, p value.Path, emit runtime.Emit) error {
	if n.Ref {
		return ev.bindRef(n, env, in, p, emit)
	}
	if ev.derives() && len(n.Pats) == 1 && n.Pats[0].Kind == ast.PatVar {
		name := n.Pats[0].Name
		return ev.values().eval(n.Src, env, in, p, func(x *value.Value, xp value.Path) error {
			return ev.eval(n.Body, env.BindAt(name, x, xp), in, p, emit)
		})
	}
	return ev.values().eval(n.Src, env, in, nil, func(x *value.Value, _ value.Path) error {
		return ev.bindPats(n.Pats, x, env, func(env *Env) error {
			return ev.eval(n.Body, env, in, p, emit)
		})
	})
}

// bindPats destructures x by pats and runs body with the bindings. With
// "?//" alternatives every variable starts out null, and an error or a
// break raised while a pattern's bindings are live moves on to the next
// pattern. That covers the destructuring, the body, and everything
// downstream of the body's outputs, even past a try, as in jq 1.8.2. The
// last pattern's error stands as it is.
func (ev evaluator) bindPats(pats []ast.Pattern, x *value.Value, env *Env, body func(*Env) error) error {
	if len(pats) == 1 {
		return ev.destructure(&pats[0], x, env, keyScope{outer: env}, body)
	}
	for i := range pats {
		for _, name := range patternVars(&pats[i], nil) {
			env = env.Bind(name, value.Null())
		}
	}
	last := len(pats) - 1
	for i := range last {
		if err := ev.destructure(&pats[i], x, env, keyScope{alt: true}, body); err == nil || !catchable(unwrapDownstream(err)) {
			return err
		}
	}
	return ev.destructure(&pats[last], x, env, keyScope{alt: true}, body)
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

// keyScope is the environment an object pattern's key expressions run in.
// It is the environment outside the pattern, except under ?//, where every
// pattern's variables are bound before any of them destructures and keys
// see the bindings made so far.
type keyScope struct {
	outer *Env
	alt   bool
}

// of returns the environment for a key, given the bindings made so far.
func (s keyScope) of(env *Env) *Env {
	if s.alt {
		return env
	}
	return s.outer
}

// destructure binds pat against v and runs k once for every set of
// bindings, since an object pattern's key may have several outputs. Key
// expressions read v, the value at their own level, in scope.
func (ev evaluator) destructure(pat *ast.Pattern, v *value.Value, env *Env, scope keyScope, k func(*Env) error) error {
	switch pat.Kind {
	case ast.PatVar:
		return k(env.Bind(pat.Name, v))
	case ast.PatArray:
		return ev.destructureArray(pat.Elems, 0, v, env, scope, k)
	default:
		return ev.destructureObject(pat.Entries, v, env, scope, k)
	}
}

func (ev evaluator) destructureArray(elems []ast.Pattern, i int, v *value.Value, env *Env, scope keyScope, k func(*Env) error) error {
	if i == len(elems) {
		return k(env)
	}
	x, err := ev.element(v, value.Num(float64(i)))
	if err != nil {
		return err
	}
	return ev.destructure(&elems[i], x, env, scope, func(env *Env) error {
		return ev.destructureArray(elems, i+1, v, env, scope, k)
	})
}

func (ev evaluator) destructureObject(entries []ast.PatEntry, v *value.Value, env *Env, scope keyScope, k func(*Env) error) error {
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
			return ev.destructureObject(rest, v, env, scope, k)
		}
		return ev.destructure(e.Val, x, env, scope, func(env *Env) error {
			return ev.destructureObject(rest, v, env, scope, k)
		})
	}
	if e.KeyVar != "" {
		return next(value.Str(e.KeyVar), env)
	}
	return ev.values().eval(e.Key, scope.of(env), v, nil, func(key *value.Value, _ value.Path) error {
		return next(key, env)
	})
}

// element indexes v by key for destructuring, where a missing value is
// null.
func (ev evaluator) element(v, key *value.Value) (*value.Value, error) {
	x, err := ev.ctx.Dialect.Index(ev.ctx, v, key, runtime.IndexRead)
	if err != nil {
		return nil, err
	}
	if x == nil {
		return value.Null(), nil
	}
	return x, nil
}

// bindRef runs "Src ref $x | Body". The variable is bound to every place
// Src names, in the document the bind's input sits in, and the body runs
// once for each, reading that place, or all of them when it names a
// place to change. It reads those places in the version of the document it
// is read in, so it follows changes the body makes, an assignment to it
// changes every place, and a place the document lacks gives nothing.
func (ev evaluator) bindRef(n *ast.Bind, env *Env, in *value.Value, p value.Path, emit runtime.Emit) error {
	name := n.Pats[0].Name
	pev := ev.pathsFor(runtime.IndexRead)
	return ev.each(in, p, func(in *value.Value, p value.Path) error {
		var places []value.Path
		err := pev.eval(n.Src, env, in, value.Path{}, func(v *value.Value, vp value.Path) error {
			if vp == nil {
				return invalidPathResult(v)
			}
			places = append(places, vp)
			return nil
		})
		if err != nil {
			return err
		}
		for i := range places {
			inner := env.BindRef(name, p, places, i)
			if err := ev.eval(n.Body, inner, in, p, emit); err != nil {
				return err
			}
		}
		return nil
	})
}

// refVar reads a variable bound to places. Each place is below the bound
// input's own place, read in the version of its document that in sits in
// when it sits in one, and in the latest version otherwise.
func (ev evaluator) refVar(base value.Path, places []value.Path, in *value.Value, p value.Path, emit runtime.Emit) error {
	return ev.each(in, p, func(in *value.Value, p value.Path) error {
		btop, bdoc, brel := runtime.Unroot(base)
		top, doc, _ := runtime.Unroot(p)
		root, rooted := in, false
		switch {
		case btop == nil:
		case top != nil && doc == bdoc:
			root, rooted = top, true
		default:
			root, rooted = ev.latest(btop, bdoc), true
		}
		for _, rel := range places {
			abs := extend(brel, rel...)
			if !rooted {
				abs = rel
			}
			v, ok := existingAt(root, abs)
			if !ok {
				continue
			}
			switch {
			case !ev.track && !ev.paths:
				if err := emit(v, nil); err != nil {
					return err
				}
			case rooted && (!ev.paths || top != nil):
				if err := emit(v, runtime.Rooted(root, bdoc, abs)); err != nil {
					return err
				}
			default:
				if err := emit(v, ev.place(in, p, rel...)); err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// existingAt is the value at path in v, and false when v lacks the place.
func existingAt(v *value.Value, path value.Path) (*value.Value, bool) {
	for _, k := range path {
		var ok bool
		switch {
		case v.Kind() == value.KindObject && k.Kind() == value.KindString:
			v, ok = v.Get(k.Str())
		case v.Kind() == value.KindObject:
			for _, e := range v.Entries() {
				if value.Equal(e.Key, k) {
					v, ok = e.Value, true
				}
			}
		case v.Kind() == value.KindArray && k.Kind() == value.KindNumber:
			v, ok = v.Index(int(k.Float()))
		}
		if !ok {
			return nil, false
		}
	}
	return v, true
}
