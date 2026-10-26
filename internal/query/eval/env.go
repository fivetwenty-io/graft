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
	at     value.Path
	fn     *Closure
	// ref marks a variable bound to a place, which at holds relative to
	// the input the variable is read against.
	ref   bool
	label uintptr
	fns   map[funcKey]*Closure
}

type bindingKind uint8

const (
	bindVar bindingKind = iota
	bindFunc
	bindLabel
	bindFuncs
	// bindNS binds the table in fns under name::, which is what an import
	// makes of a module.
	bindNS
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

// BindAt returns env with $name bound to v, which sits at path at, so a
// reference to $name keeps v's place while the run tracks cursors.
func (env *Env) BindAt(name string, v *value.Value, at value.Path) *Env {
	return &Env{parent: env, kind: bindVar, name: name, val: v, at: at}
}

// BindRef returns env with $name bound to the place at, which is
// relative to the input $name is read against.
func (env *Env) BindRef(name string, at value.Path) *Env {
	return &Env{parent: env, kind: bindVar, name: name, at: at, ref: true}
}

// LookupRef returns the place $name is bound to, and false when it is
// bound to a value or not at all.
func (env *Env) LookupRef(name string) (value.Path, bool) {
	for e := env; e != nil; e = e.parent {
		if e.kind == bindVar && e.name == name {
			return e.at, e.ref
		}
	}
	return nil, false
}

// Lookup returns the value bound to $name.
func (env *Env) Lookup(name string) (*value.Value, bool) {
	v, _, ok := env.LookupAt(name)
	return v, ok
}

// LookupAt returns the value bound to $name and the path it was bound at,
// which is nil for a binding made with Bind.
func (env *Env) LookupAt(name string) (*value.Value, value.Path, bool) {
	for e := env; e != nil; e = e.parent {
		if e.kind == bindVar && e.name == name {
			return e.val, e.at, true
		}
	}
	return nil, nil, false
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
		case bindNS:
			if len(name) > len(e.name)+2 && name[:len(e.name)] == e.name && name[len(e.name):len(e.name)+2] == "::" {
				if fn, ok := e.fns[funcKey{name[len(e.name)+2:], arity}]; ok {
					return fn, true
				}
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
