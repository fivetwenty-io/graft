package jq

import (
	"errors"
	"strconv"
	"strings"

	"github.com/fivetwenty-io/graft/internal/query/ast"
	"github.com/fivetwenty-io/graft/internal/query/eval"
	"github.com/fivetwenty-io/graft/internal/query/lexer"
	"github.com/fivetwenty-io/graft/internal/query/parser"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// Parse reads a jq program for its syntax alone, without resolving any
// name, and returns its tree or a *query.CompileError that points at the
// first problem.
func Parse(src string) (ast.Node, error) {
	node, err := parser.Parse(src, Grammar())
	if err != nil {
		var pe *parser.Error
		if errors.As(err, &pe) {
			line, col := position(src, pe.Pos)
			return nil, &query.CompileError{Msg: pe.Msg, Line: line, Col: col}
		}
		return nil, err
	}
	return node, nil
}

// Compile parses a jq program and resolves every label, variable, and
// function it can reach, so a name that is not defined is a compile error
// rather than a runtime one. vars names the variables the caller will bind,
// without their $, and $ENV and $ARGS are always defined. The program's
// Env holds the prelude with $ARGS bound to empty arguments, and a caller
// binds its own variables on top of it with Env.Bind.
func Compile(src string, vars ...string) (*eval.Program, error) {
	return CompileWith(src, CompileOptions{}, vars...)
}

// CompileWith is Compile for a program that may import and include
// modules, which it finds along opts.SearchPaths. It reads every module
// the program reaches, so a module that is missing, does not parse, or
// names something undefined is a compile error.
func CompileWith(src string, opts CompileOptions, vars ...string) (*eval.Program, error) {
	r := registry()
	node, err := Parse(src)
	if err != nil {
		return nil, err
	}
	res := &resolver{done: map[*ast.FuncDef]bool{}, natives: map[string]bool{}, lk: newLinker(opts)}
	for _, name := range r.Names() {
		res.natives[name] = true
	}
	top := &scope{kind: scopeVar, name: "ENV"}
	top = &scope{parent: top, kind: scopeVar, name: "ARGS"}
	for _, v := range vars {
		top = &scope{parent: top, kind: scopeVar, name: v}
	}
	res.top = top
	root := node
	if opts.AutoInclude != "" {
		m, err := res.lk.loadPath(opts.AutoInclude, false)
		if err != nil {
			return nil, err
		}
		root = &ast.Include{Rest: node}
		res.lk.mods[root] = m.mod
		top = res.bindLink(top, eval.Link{Include: true}, m)
	}
	if err := res.resolve(node, top); err != nil {
		var ue *undefined
		if errors.As(err, &ue) {
			if ue.mod != nil {
				line, col := locate(ue.mod.lib.src, ue.mod.lib.root, ue)
				return nil, &query.CompileError{Msg: ue.msg() + " in " + ue.mod.lib.path, Line: line, Col: col}
			}
			line, col := locate(src, node, ue)
			return nil, &query.CompileError{Msg: ue.msg(), Line: line, Col: col}
		}
		return nil, err
	}
	args := value.Obj(value.Pair{Key: "positional", Value: value.Arr()}, value.Pair{Key: "named", Value: value.Obj()})
	prog := eval.NewProgram(root, rootEnv.Bind("ARGS", args), r)
	if len(res.lk.mods) > 0 {
		prog.WithModules(res.lk.mods)
	}
	return prog, nil
}

type scopeKind uint8

const (
	scopeVar scopeKind = iota
	scopeFunc
	scopeLabel
)

// scope is one name the resolver can see. A function's scope keeps its
// definition and the scope it was defined in, so its body resolves there
// the first time a call reaches it.
type scope struct {
	parent *scope
	kind   scopeKind
	name   string
	arity  int
	def    *ast.FuncDef
	at     *scope
}

func (s *scope) find(kind scopeKind, name string, arity int) (*scope, bool) {
	for ; s != nil; s = s.parent {
		if s.kind == kind && s.name == name && (kind != scopeFunc || s.arity == arity) {
			return s, true
		}
	}
	return nil, false
}

// undefined is a name the resolver could not find, kept with the node
// that named it so the error can point at its token.
type undefined struct {
	node  ast.Node
	name  string
	arity int
	// mod is the module whose source holds node, or nil for the main
	// program.
	mod *loaded
}

func (u *undefined) Error() string { return u.msg() }

func (u *undefined) msg() string {
	switch u.node.(type) {
	case *ast.Var:
		return "$" + u.name + " is not defined"
	case *ast.Break:
		return "$*label-" + u.name + " is not defined"
	default:
		return u.name + "/" + strconv.Itoa(u.arity) + " is not defined"
	}
}

type resolver struct {
	natives map[string]bool
	done    map[*ast.FuncDef]bool
	lk      *linker
	// top is the scope of the variables every program sees, which a
	// module's own scope starts from.
	top *scope
}

//nolint:gocyclo // one case for each node kind that binds or uses a name
func (r *resolver) resolve(n ast.Node, s *scope) error {
	switch n := n.(type) {
	case nil:
		return nil
	case *ast.Var:
		if _, ok := s.find(scopeVar, n.Name, 0); !ok {
			return &undefined{node: n, name: n.Name}
		}
		return nil
	case *ast.Break:
		if _, ok := s.find(scopeLabel, n.Name, 0); !ok {
			return &undefined{node: n, name: n.Name}
		}
		return nil
	case *ast.Label:
		return r.resolve(n.Body, &scope{parent: s, kind: scopeLabel, name: n.Name})
	case *ast.Call:
		return r.call(n, s)
	case *ast.FuncDef:
		return r.resolve(n.Rest, &scope{parent: s, kind: scopeFunc, name: n.Name, arity: len(n.Params), def: n, at: s})
	case *ast.Bind:
		if err := r.resolve(n.Src, s); err != nil {
			return err
		}
		inner, err := r.patterns(n.Pats, s)
		if err != nil {
			return err
		}
		return r.resolve(n.Body, inner)
	case *ast.Reduce:
		if err := r.resolve(n.Src, s); err != nil {
			return err
		}
		inner, err := r.patterns(n.Pats, s)
		if err != nil {
			return err
		}
		if err := r.resolve(n.Init, s); err != nil {
			return err
		}
		return r.resolve(n.Update, inner)
	case *ast.Foreach:
		if err := r.resolve(n.Src, s); err != nil {
			return err
		}
		inner, err := r.patterns(n.Pats, s)
		if err != nil {
			return err
		}
		for _, c := range []ast.Node{n.Init, n.Update, n.Extract} {
			sc := inner
			if c == n.Init {
				sc = s
			}
			if err := r.resolve(c, sc); err != nil {
				return err
			}
		}
		return nil
	case *ast.Module:
		if _, err := constMeta(n.Meta); err != nil {
			return err
		}
		return r.resolve(n.Rest, s)
	case *ast.Import:
		m, err := r.lk.link(n, n.Path, n.Data, n.Meta)
		if err != nil {
			return err
		}
		return r.resolve(n.Rest, r.bindLink(s, eval.Link{Alias: n.Alias, Data: n.Data}, m))
	case *ast.Include:
		m, err := r.lk.link(n, n.Path, false, n.Meta)
		if err != nil {
			return err
		}
		return r.resolve(n.Rest, r.bindLink(s, eval.Link{Include: true}, m))
	}
	for _, c := range children(n) {
		if err := r.resolve(c, s); err != nil {
			return err
		}
	}
	return nil
}

// call resolves a call to a definition in scope, a path form, a native,
// or a prelude definition. A definition's body resolves the first time a
// call reaches it, in the scope it was defined in, so a definition nothing
// calls may name anything, as in jq.
func (r *resolver) call(n *ast.Call, s *scope) error {
	for _, a := range n.Args {
		if err := r.resolve(a, s); err != nil {
			return err
		}
	}
	if fn, ok := s.find(scopeFunc, n.Name, len(n.Args)); ok {
		if fn.def == nil || r.done[fn.def] {
			return nil
		}
		r.done[fn.def] = true
		body := &scope{parent: fn.at, kind: scopeFunc, name: fn.def.Name, arity: fn.arity, def: fn.def, at: fn.at}
		for _, p := range fn.def.Params {
			if name, ok := strings.CutPrefix(p, "$"); ok {
				body = &scope{parent: body, kind: scopeVar, name: name}
				p = name
			}
			body = &scope{parent: body, kind: scopeFunc, name: p}
		}
		err := r.resolve(fn.def.Body, body)
		var ue *undefined
		if errors.As(err, &ue) && ue.mod == nil {
			ue.mod = r.lk.defMod[fn.def]
		}
		return err
	}
	if n.Name == "path" && len(n.Args) == 1 {
		return nil
	}
	if r.natives[n.Name+"/"+strconv.Itoa(len(n.Args))] {
		return nil
	}
	return &undefined{node: n, name: n.Name, arity: len(n.Args)}
}

// patterns resolves the key expressions of a bind's patterns and returns
// the scope its body sees. A lone pattern's keys see only the scope
// outside it. Under ?// every pattern's variables are bound before any
// destructures, so the keys see them all.
func (r *resolver) patterns(pats []ast.Pattern, s *scope) (*scope, error) {
	inner := s
	for i := range pats {
		for _, name := range patternVars(&pats[i], nil) {
			inner = &scope{parent: inner, kind: scopeVar, name: name}
		}
	}
	keys := s
	if len(pats) > 1 {
		keys = inner
	}
	for i := range pats {
		if err := r.patternKeys(&pats[i], keys); err != nil {
			return nil, err
		}
	}
	return inner, nil
}

func (r *resolver) patternKeys(pat *ast.Pattern, s *scope) error {
	for i := range pat.Elems {
		if err := r.patternKeys(&pat.Elems[i], s); err != nil {
			return err
		}
	}
	for _, e := range pat.Entries {
		if e.Key != nil {
			if err := r.resolve(e.Key, s); err != nil {
				return err
			}
		}
		if e.Val != nil {
			if err := r.patternKeys(e.Val, s); err != nil {
				return err
			}
		}
	}
	return nil
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

// children returns the nodes directly under n that resolve in n's own
// scope.
//
//nolint:gocyclo // one case for each node kind with children
func children(n ast.Node) []ast.Node {
	switch n := n.(type) {
	case *ast.Field:
		return []ast.Node{n.Target}
	case *ast.Index:
		return []ast.Node{n.Target, n.Key}
	case *ast.Slice:
		return []ast.Node{n.Target, n.From, n.To}
	case *ast.Iterate:
		return []ast.Node{n.Target}
	case *ast.Str:
		return n.Parts
	case *ast.ArrayCons:
		return []ast.Node{n.Body}
	case *ast.ObjectCons:
		out := make([]ast.Node, 0, 2*len(n.Entries))
		for _, e := range n.Entries {
			out = append(out, e.Key, e.Val)
		}
		return out
	case *ast.Pipe:
		return []ast.Node{n.L, n.R}
	case *ast.Comma:
		return []ast.Node{n.L, n.R}
	case *ast.Binary:
		return []ast.Node{n.L, n.R}
	case *ast.Unary:
		return []ast.Node{n.X}
	case *ast.Alt:
		return []ast.Node{n.L, n.R}
	case *ast.Assign:
		return []ast.Node{n.L, n.R}
	case *ast.If:
		out := []ast.Node{n.Cond, n.Then}
		for _, e := range n.Elifs {
			out = append(out, e.Cond, e.Then)
		}
		return append(out, n.Else)
	case *ast.Try:
		return []ast.Node{n.Body, n.Catch}
	case *ast.Optional:
		return []ast.Node{n.X}
	case *ast.Module:
		return []ast.Node{n.Rest}
	}
	return nil
}

// position turns a byte offset into a 1-based line and byte column.
func position(src string, off int) (int, int) {
	off = min(max(off, 0), len(src))
	line := 1 + strings.Count(src[:off], "\n")
	return line, off - strings.LastIndexByte(src[:off], '\n')
}

// locate finds the token that named an undefined name. It counts the
// tokens that spell the name, and the places in the tree that spell it in
// source order. When the two counts agree, the error's place in the tree
// picks its token. When they do not, which happens when the name is also
// an object key, the first token stands.
//
//nolint:gocyclo // the token rule for each kind of undefined name
func locate(src string, root ast.Node, u *undefined) (int, int) {
	toks, err := lexer.Tokenize(src, Grammar().Tokens)
	if err != nil {
		return 1, 1
	}
	var cands []int
	for i, t := range toks {
		switch u.node.(type) {
		case *ast.Var:
			if t.Kind == lexer.Variable && t.Text == "$"+u.name {
				cands = append(cands, t.Pos)
			}
		case *ast.Break:
			if t.Kind == lexer.Keyword && t.Text == "break" && i+1 < len(toks) && toks[i+1].Text == "$"+u.name {
				cands = append(cands, t.Pos)
			}
		default:
			if t.Kind == lexer.Ident && t.Text == u.name {
				cands = append(cands, t.Pos)
			}
		}
	}
	if len(cands) == 0 {
		return 1, 1
	}
	spots, at := occurrences(root, u)
	pos := cands[0]
	if len(spots) == len(cands) && at >= 0 {
		pos = cands[at]
	}
	return position(src, pos)
}

// occurrences walks the tree in source order and lists every place that
// spells u's name as the kind of token u was, returning the index of u's
// own node among them.
//
//nolint:gocyclo // one case for each node kind that can name a variable, label, or function
func occurrences(root ast.Node, u *undefined) ([]ast.Node, int) {
	var spots []ast.Node
	at := -1
	add := func(n ast.Node, mine bool) {
		if mine {
			at = len(spots)
		}
		spots = append(spots, n)
	}
	_, isVar := u.node.(*ast.Var)
	_, isBreak := u.node.(*ast.Break)
	isCall := !isVar && !isBreak
	var pats func(p *ast.Pattern)
	var walk func(n ast.Node)
	pats = func(p *ast.Pattern) {
		switch p.Kind {
		case ast.PatVar:
			if isVar && p.Name == u.name {
				add(nil, false)
			}
		case ast.PatArray:
			for i := range p.Elems {
				pats(&p.Elems[i])
			}
		case ast.PatObject:
			for _, e := range p.Entries {
				if isVar && e.KeyVar == u.name {
					add(nil, false)
				}
				walk(e.Key)
				if e.Val != nil {
					pats(e.Val)
				}
			}
		}
	}
	walk = func(n ast.Node) {
		switch n := n.(type) {
		case nil:
		case *ast.Var:
			if isVar && n.Name == u.name {
				add(n, n == u.node)
			}
		case *ast.Break:
			if (isBreak || isVar) && n.Name == u.name {
				add(n, n == u.node)
			}
		case *ast.Label:
			if isVar && n.Name == u.name {
				add(nil, false)
			}
			walk(n.Body)
		case *ast.Call:
			if isCall && n.Name == u.name {
				add(n, n == u.node)
			}
			for _, a := range n.Args {
				walk(a)
			}
		case *ast.FuncDef:
			if isCall && n.Name == u.name {
				add(nil, false)
			}
			for _, p := range n.Params {
				name, dollar := strings.CutPrefix(p, "$")
				if name == u.name && ((dollar && isVar) || (!dollar && isCall)) {
					add(nil, false)
				}
			}
			walk(n.Body)
			walk(n.Rest)
		case *ast.Bind:
			walk(n.Src)
			for i := range n.Pats {
				pats(&n.Pats[i])
			}
			walk(n.Body)
		case *ast.Reduce:
			walk(n.Src)
			for i := range n.Pats {
				pats(&n.Pats[i])
			}
			walk(n.Init)
			walk(n.Update)
		case *ast.Foreach:
			walk(n.Src)
			for i := range n.Pats {
				pats(&n.Pats[i])
			}
			walk(n.Init)
			walk(n.Update)
			walk(n.Extract)
		default:
			for _, c := range children(n) {
				walk(c)
			}
		}
	}
	walk(root)
	return spots, at
}
