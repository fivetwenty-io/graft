package eval

import (
	"sync"

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
	return ev.eval(rest, ev.set.bindLink(env, link), in, p, emit)
}

// instanceSet holds the definitions of the modules a program loaded,
// built once for one base environment. A module's definitions see the
// base environment, the module's own imports, and the definitions before
// them, and nothing of the program that imported them. Concurrent runs of
// one program share a set, and a run that passes another base gets a new
// set.
type instanceSet struct {
	base   *Env
	mu     sync.RWMutex
	tables map[*Module]map[funcKey]*Closure
}

// instances returns the set for runs on base, building an empty one when
// the last run used another base.
func (p *Program) instances(base *Env) *instanceSet {
	if s := p.insts.Load(); s != nil && s.base == base {
		return s
	}
	s := &instanceSet{base: base, tables: map[*Module]map[funcKey]*Closure{}}
	p.insts.Store(s)
	return s
}

// bindLink returns env with what one import or include binds. An import
// binds a single entry that names the module's table of definitions, and
// an include binds a single entry holding the table, so the cost of a link
// does not grow with the size of the module.
func (s *instanceSet) bindLink(env *Env, l Link) *Env {
	if l.Data {
		return env.Bind(l.Alias+"::"+l.Alias, l.Mod.Data).Bind(l.Alias, l.Mod.Data)
	}
	t := s.table(l.Mod)
	if l.Include {
		return &Env{parent: env, kind: bindFuncs, fns: t}
	}
	return &Env{parent: env, kind: bindNS, name: l.Alias, fns: t}
}

// table returns the definitions of m by name and arity, building them the
// first time they are asked for.
func (s *instanceSet) table(m *Module) map[funcKey]*Closure {
	s.mu.RLock()
	t, ok := s.tables[m]
	s.mu.RUnlock()
	if ok {
		return t
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.build(m)
}

func (s *instanceSet) build(m *Module) map[funcKey]*Closure {
	if t, ok := s.tables[m]; ok {
		return t
	}
	env := s.base
	for _, l := range m.Links {
		switch {
		case l.Data:
			env = env.Bind(l.Alias+"::"+l.Alias, l.Mod.Data).Bind(l.Alias, l.Mod.Data)
		case l.Include:
			env = &Env{parent: env, kind: bindFuncs, fns: s.build(l.Mod)}
		default:
			env = &Env{parent: env, kind: bindNS, name: l.Alias, fns: s.build(l.Mod)}
		}
	}
	t := make(map[funcKey]*Closure, len(m.Defs))
	for _, d := range m.Defs {
		fn := &Closure{Name: d.Name, Params: d.Params, Body: d.Body}
		env = env.Def(fn)
		t[funcKey{d.Name, len(d.Params)}] = fn
	}
	s.tables[m] = t
	return t
}
