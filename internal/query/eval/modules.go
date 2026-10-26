package eval

import (
	"github.com/fivetwenty-io/graft/internal/query/ast"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// Module is a library a compile step has loaded and linked. A code module
// holds its imports and includes in order, and its top-level definitions.
// A data module holds the array of the values in its JSON file.
type Module struct {
	Path  string
	Root  ast.Node
	Links []Link
	Defs  []*ast.FuncDef
	Data  *value.Value
}

// Link is one import or include of a module. A data import binds Alias as
// a variable, an import binds each definition as Alias::name, and an
// include binds each definition under its own name.
type Link struct {
	Alias   string
	Include bool
	Data    bool
	Mod     *Module
}

// WithModules tells p which module each import and include of its program
// loaded, and returns p. The modules' own definitions join the tail-call
// analysis, so a module function that recurses in tail position loops as
// the program's own do.
func (p *Program) WithModules(mods map[ast.Node]*Module) *Program {
	p.mods = mods
	seen := map[*Module]bool{}
	var roots []ast.Node
	var add func(m *Module)
	add = func(m *Module) {
		if seen[m] {
			return
		}
		seen[m] = true
		if m.Root != nil {
			roots = append(roots, m.Root)
		}
		for _, l := range m.Links {
			add(l.Mod)
		}
	}
	for _, m := range mods {
		add(m)
	}
	for site, tail := range tailSites(roots...) {
		p.tails[site] = tail
	}
	return p
}

// linked runs the rest of a program under the bindings that an import or
// include of the module loaded for n makes.
func (ev evaluator) linked(n, rest ast.Node, env *Env, in *value.Value, p value.Path, emit func(*value.Value, value.Path) error) error {
	link := Link{Mod: ev.mods[n]}
	switch n := n.(type) {
	case *ast.Import:
		link.Alias, link.Data = n.Alias, n.Data
	case *ast.Include:
		link.Include = true
	}
	if rest == nil {
		return ev.each(in, p, emit)
	}
	return ev.eval(rest, ev.bindLink(env, link), in, p, emit)
}

// bindLink returns env with what one import or include binds.
func (ev evaluator) bindLink(env *Env, l Link) *Env {
	if l.Data {
		return env.Bind(l.Alias+"::"+l.Alias, l.Mod.Data).Bind(l.Alias, l.Mod.Data)
	}
	for _, fn := range ev.instance(l.Mod) {
		name := fn.Name
		if !l.Include {
			name = l.Alias + "::" + name
		}
		env = env.defAs(name, len(fn.Params), fn)
	}
	return env
}

// instance builds a code module's definitions once for the run. They see
// the program's base environment, the module's own imports, and the
// definitions before them, and nothing of the program that imported them.
func (ev evaluator) instance(m *Module) []*Closure {
	if fns, ok := ev.insts[m]; ok {
		return fns
	}
	env := ev.base
	for _, l := range m.Links {
		env = ev.bindLink(env, l)
	}
	fns := make([]*Closure, 0, len(m.Defs))
	for _, d := range m.Defs {
		fn := &Closure{Name: d.Name, Params: d.Params, Body: d.Body}
		env = env.Def(fn)
		fns = append(fns, fn)
	}
	if ev.insts == nil {
		ev.insts = map[*Module][]*Closure{}
	}
	ev.insts[m] = fns
	return fns
}
