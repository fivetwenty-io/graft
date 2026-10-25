package eval

import (
	"github.com/fivetwenty-io/graft/internal/query/ast"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// Closure is a function definition with the environment it was defined
// in. Params spell a value parameter with its $.
type Closure struct {
	Name   string
	Params []string
	Body   ast.Node
	Env    *Env

	// param marks a closure made for a filter argument, whose body is the
	// caller's expression rather than a definition.
	param bool
}

// Env is an immutable chain of variable, function, and label bindings.
// Every method on a nil Env treats it as empty.
type Env struct {
	parent *Env
	kind   bindingKind
	name   string
	arity  int
	val    *value.Value
	fn     *Closure
	label  uintptr
	fns    map[funcKey]*Closure
}

type bindingKind uint8

const (
	bindVar bindingKind = iota
	bindFunc
	bindLabel
	bindFuncs
)

type funcKey struct {
	name  string
	arity int
}

// NewEnv returns an Env holding the definitions in reg's prelude, each of
// which sees the others. It returns nil, the empty Env, for a nil reg or an
// empty prelude.
func NewEnv(reg *runtime.Registry) *Env {
	if reg == nil {
		return nil
	}
	var env *Env
	for _, prog := range reg.Prelude() {
		frame := &Env{parent: env, kind: bindFuncs, fns: map[funcKey]*Closure{}}
		for def, ok := prog.(*ast.FuncDef); ok; def, ok = def.Rest.(*ast.FuncDef) {
			frame.fns[funcKey{def.Name, len(def.Params)}] = &Closure{Name: def.Name, Params: def.Params, Body: def.Body, Env: frame}
		}
		env = frame
	}
	return env
}

// Bind returns env with $name bound to v.
func (env *Env) Bind(name string, v *value.Value) *Env {
	return &Env{parent: env, kind: bindVar, name: name, val: v}
}

// Lookup returns the value bound to $name.
func (env *Env) Lookup(name string) (*value.Value, bool) {
	for e := env; e != nil; e = e.parent {
		if e.kind == bindVar && e.name == name {
			return e.val, true
		}
	}
	return nil, false
}

// Def returns env with fn defined as fn.Name/len(fn.Params). When fn.Env is
// nil, Def sets it to the returned Env, so fn sees itself and everything
// bound before it.
func (env *Env) Def(fn *Closure) *Env {
	out := env.defAs(fn.Name, len(fn.Params), fn)
	if fn.Env == nil {
		fn.Env = out
	}
	return out
}

func (env *Env) defAs(name string, arity int, fn *Closure) *Env {
	return &Env{parent: env, kind: bindFunc, name: name, arity: arity, fn: fn}
}

func (env *Env) lookupFunc(name string, arity int) (*Closure, bool) {
	for e := env; e != nil; e = e.parent {
		switch e.kind {
		case bindFunc:
			if e.name == name && e.arity == arity {
				return e.fn, true
			}
		case bindFuncs:
			if fn, ok := e.fns[funcKey{name, arity}]; ok {
				return fn, true
			}
		case bindVar, bindLabel:
		}
	}
	return nil, false
}

func (env *Env) bindLabel(name string, id uintptr) *Env {
	return &Env{parent: env, kind: bindLabel, name: name, label: id}
}

func (env *Env) lookupLabel(name string) (uintptr, bool) {
	for e := env; e != nil; e = e.parent {
		if e.kind == bindLabel && e.name == name {
			return e.label, true
		}
	}
	return 0, false
}
