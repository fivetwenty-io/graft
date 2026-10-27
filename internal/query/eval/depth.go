package eval

import (
	"strconv"

	"github.com/fivetwenty-io/graft/internal/query/ast"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// notDefined is the error for a call that names no function.
func notDefined(name string, arity int) error {
	return RuntimeError(name + "/" + strconv.Itoa(arity) + " is not defined")
}

// call runs a function call. A definition in scope comes first, then the
// path forms the evaluator implements itself, then the dialect's natives.
func (ev evaluator) call(n *ast.Call, env *Env, in *value.Value, p value.Path, emit runtime.Emit) error {
	if err := ev.ctx.Tick(); err != nil {
		return err
	}
	arity := len(n.Args)
	if fn, ok := env.lookupFunc(n.Name, arity); ok {
		if fn.param {
			return ev.invokeParam(fn, in, p, emit)
		}
		if in == nil && len(ev.roots) > 1 {
			// A definition runs once for each candidate, as yq's
			// operators do.
			return ev.each(in, p, func(in *value.Value, p value.Path) error {
				return ev.invoke(n, fn, env, in, p, emit)
			})
		}
		return ev.invoke(n, fn, env, in, p, emit)
	}
	switch {
	case n.Name == "path" && arity == 1:
		return ev.pathOf(n.Args[0], env, in, p, emit)
	case n.Name == "getpath" && arity == 1 && ev.paths:
		return ev.getpathOf(n.Args[0], env, in, p, emit)
	}
	return ev.callNative(n, env, in, p, emit)
}

// callNative runs a call to one of the dialect's natives on in, or on each
// candidate.
func (ev evaluator) callNative(n *ast.Call, env *Env, in *value.Value, p value.Path, emit runtime.Emit) error {
	arity := len(n.Args)
	native, ok := ev.reg.Lookup(n.Name, arity)
	if !ok {
		return notDefined(n.Name, arity)
	}
	return ev.each(in, p, func(in *value.Value, p value.Path) error {
		args := make([]runtime.Thunk, arity)
		for i, a := range n.Args {
			args[i] = argThunk{ev: ev, node: a, env: env, in: in, p: p}
		}
		if ev.track {
			ev.setCursor(in, p)
		}
		return native(ev.ctx, in, args, ev.derive(in, p, emit))
	})
}

// invoke calls a definition. A call in tail position of the definition's
// own body, whose guards each have one output here, hands its frame back
// to runBody instead of nesting, so it takes no depth. Every other call
// counts one level against the depth limit.
func (ev evaluator) invoke(n *ast.Call, fn *Closure, env *Env, in *value.Value, p value.Path, emit runtime.Emit) error {
	if site, ok := ev.tails[n]; ok && site.body == fn.Body && in != nil && ev.guarded(site.guards, env) {
		if tc, ok, err := ev.tailFrame(n, fn, env, in, p); ok || err != nil {
			if tc == nil {
				return err
			}
			return tc
		}
	}
	if err := ev.ctx.Enter(); err != nil {
		return err
	}
	defer ev.ctx.Leave()
	return ev.frames(n, fn, env, in, func(frame *Env) error {
		return ev.runBody(fn, frame, in, p, emit)
	})
}

// invokeParam runs a filter argument in the environment of the call that
// passed it.
func (ev evaluator) invokeParam(fn *Closure, in *value.Value, p value.Path, emit runtime.Emit) error {
	if err := ev.ctx.Enter(); err != nil {
		return err
	}
	defer ev.ctx.Leave()
	return ev.eval(fn.Body, fn.Env, in, p, emit)
}

// frames binds a call's arguments to fn's parameters and runs k with each
// resulting environment. A filter parameter binds a closure over the
// caller's environment. A value parameter binds every output of its
// argument, with the first parameter varying slowest. It also defines the
// filter of the same name as the argument itself, as jq's "def f($a)"
// means "def f(a): a as $a". The filter is left out when the body never
// calls it, so a tail-recursive loop does not keep every earlier frame
// alive through it.
func (ev evaluator) frames(n *ast.Call, fn *Closure, env *Env, in *value.Value, k func(*Env) error) error {
	var bind func(i int, frame *Env) error
	bind = func(i int, frame *Env) error {
		if i == len(fn.Params) {
			return k(frame)
		}
		name := fn.Params[i]
		if name == "" || name[0] != '$' {
			return bind(i+1, frame.defAs(name, 0, ev.argClosure(n.Args[i], env)))
		}
		name = name[1:]
		var arg *Closure
		if ev.calls(fn.Body, name) {
			arg = ev.argClosure(n.Args[i], env)
		}
		return ev.values().eval(n.Args[i], env, in, nil, func(v *value.Value, _ value.Path) error {
			frame := frame.Bind(name, v)
			if arg != nil {
				frame = frame.defAs(name, 0, arg)
			}
			return bind(i+1, frame)
		})
	}
	return bind(0, fn.Env)
}

// argClosure wraps a filter argument. An argument that is itself a bare
// filter parameter passes through unwrapped, so a parameter handed down a
// recursion does not grow a chain of closures.
func (ev evaluator) argClosure(arg ast.Node, env *Env) *Closure {
	if c, ok := arg.(*ast.Call); ok && len(c.Args) == 0 {
		if fn, ok := env.lookupFunc(c.Name, 0); ok && fn.param {
			return fn
		}
	}
	return &Closure{Body: arg, Env: env, param: true}
}

// argThunk is a native's argument. Evaluated on the input of the call that
// passed it, it keeps tracking paths or cursors when the call does, so a
// native such as first passes its argument's paths through.
type argThunk struct {
	ev   evaluator
	node ast.Node
	env  *Env
	in   *value.Value
	p    value.Path
}

// Each runs the argument on input.
func (t argThunk) Each(input *value.Value, emit runtime.Emit) error {
	if (t.ev.paths || t.ev.track) && input == t.in {
		return t.ev.eval(t.node, t.env, input, t.p, emit)
	}
	return t.ev.values().eval(t.node, t.env, input, nil, emit)
}

// calls reports whether body calls a filter called name with no
// arguments anywhere inside it.
func (ev evaluator) calls(body ast.Node, name string) bool {
	key := callKey{body, name}
	if used, ok := ev.used[key]; ok {
		return used
	}
	used := false
	walk(body, func(n ast.Node) {
		if c, ok := n.(*ast.Call); ok && c.Name == name && len(c.Args) == 0 {
			used = true
		}
	})
	ev.used[key] = used
	return used
}

// callKey names a filter called within a body.
type callKey struct {
	body ast.Node
	name string
}
