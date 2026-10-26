package jq

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/fivetwenty-io/graft/internal/query/ast"
	"github.com/fivetwenty-io/graft/internal/query/eval"
	"github.com/fivetwenty-io/graft/internal/query/load"
	"github.com/fivetwenty-io/graft/internal/query/parser"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// The limits on what a program can make the compiler read. A module or a
// data file is read whole, and a program that imports its way through
// more files than maxModules is stopped.
const (
	maxModuleBytes = 64 << 20
	maxModules     = 1024
)

// CompileOptions are what Compile needs to find modules. SearchPaths is
// the list -L builds, and nil means the compiler reads no module files at
// all, so every import fails. A caller that wants jq's own default list
// passes it, as the jq command does. ProgramOrigin is
// the directory of the program file, and empty means the current
// directory. JQOrigin is what $ORIGIN stands for, and empty means the
// directory of the running executable.
type CompileOptions struct {
	SearchPaths   []string
	ProgramOrigin string
	JQOrigin      string
	// AutoInclude is the path of a library file whose definitions the
	// program sees as if it began with an include of that file. Empty
	// means none.
	AutoInclude string
}

// ModuleResolver finds module files by jq's rules. It implements
// runtime.ModuleLibrary for the builtins that describe modules.
type ModuleResolver struct {
	SearchPaths []string
	Origin      string
	JQDir       string
}

var _ runtime.ModuleLibrary = (*ModuleResolver)(nil)

// NewModuleResolver returns the resolver that opts describe.
func NewModuleResolver(opts CompileOptions) *ModuleResolver {
	return &ModuleResolver{SearchPaths: opts.SearchPaths, Origin: opts.ProgramOrigin, JQDir: opts.JQOrigin}
}

// SearchList returns the library directories as they were given.
func (r *ModuleResolver) SearchList() []string {
	return append([]string{}, r.SearchPaths...)
}

// ProgOrigin returns the directory of the main program as an absolute
// path.
func (r *ModuleResolver) ProgOrigin() string {
	abs, err := filepath.Abs(r.progDir())
	if err != nil {
		return r.progDir()
	}
	return abs
}

// JQOrigin returns the directory that $ORIGIN stands for.
func (r *ModuleResolver) JQOrigin() string {
	if r.JQDir != "" {
		return r.JQDir
	}
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe)
	}
	return "."
}

func (r *ModuleResolver) progDir() string {
	if r.Origin == "" {
		return "."
	}
	return r.Origin
}

// Resolve finds the file for the module relpath as the main program sees
// it, which is a .jq file or, when isData is set, a .json file.
func (r *ModuleResolver) Resolve(relpath string, isData bool) (string, error) {
	return r.find(relpath, isData, nil, r.progDir())
}

// find looks for relpath along the chain that search and libOrigin
// build. search is the "search" key of the importing directive's
// metadata, or nil when the directive has none.
func (r *ModuleResolver) find(relpath string, isData bool, search *value.Value, libOrigin string) (string, error) {
	if err := validateRelPath(relpath); err != nil {
		return "", err
	}
	if r.SearchPaths == nil {
		return "", errors.New("module not found: " + relpath)
	}
	suffix := ".jq"
	if isData {
		suffix = ".json"
	}
	if !strings.HasPrefix(relpath, "/") {
		for _, dir := range r.chain(search, libOrigin) {
			base := filepath.Join(dir, relpath)
			for _, cand := range []string{base + suffix, filepath.Join(base, filepath.Base(relpath)+suffix)} {
				if info, err := os.Stat(cand); err == nil && info.Mode().IsRegular() {
					return cand, nil
				}
			}
		}
	}
	return "", errors.New("module not found: " + relpath)
}

// chain is the list of directories to search. A directive that names
// none searches the current directory and then the library path.
func (r *ModuleResolver) chain(search *value.Value, libOrigin string) []string {
	var list []string
	switch {
	case search == nil:
		list = append([]string{"."}, r.SearchList()...)
	case search.Kind() == value.KindString:
		list = []string{search.Str()}
	case search.Kind() == value.KindArray:
		for _, e := range search.Elems() {
			if e.Kind() == value.KindString {
				list = append(list, e.Str())
			}
		}
	}
	dirs := make([]string, 0, len(list))
	for _, p := range list {
		if dir, ok := r.expand(p, libOrigin); ok {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// expand turns one search entry into a directory. An entry that needs the
// home directory when there is none is skipped.
func (r *ModuleResolver) expand(p, libOrigin string) (string, bool) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		home := os.Getenv("HOME")
		if home == "" {
			return "", false
		}
		p = home + p[1:]
	}
	switch {
	case p == ".":
		return ".", true
	case strings.HasPrefix(p, "$ORIGIN/"):
		return filepath.Join(r.JQOrigin(), p[len("$ORIGIN/"):]), true
	case filepath.IsAbs(p):
		return p, true
	}
	return filepath.Join(libOrigin, p), true
}

// validateRelPath refuses a module name that climbs out of its search
// directory or repeats a component, as jq does.
func validateRelPath(rel string) error {
	parts := strings.Split(rel, "/")
	for i, part := range parts {
		if part == ".." {
			return errors.New("Relative paths to modules may not traverse to parent directories (" + rel + ")")
		}
		if i > 0 && part == parts[i-1] {
			return errors.New("module names must not have equal consecutive components: " + rel)
		}
	}
	return nil
}

// Meta describes the module name as modulemeta does: its own metadata
// followed by its deps and defs. It reads and parses the file and loads
// none of its dependencies.
func (r *ModuleResolver) Meta(name string) (*value.Value, error) {
	path, err := r.Resolve(name, false)
	if err != nil {
		return nil, &query.RuntimeError{Msg: err.Error()}
	}
	lib, err := readLibrary(path)
	if err != nil {
		return nil, &query.RuntimeError{Msg: errMessage(err)}
	}
	var b *value.ObjBuilder
	if lib.meta != nil {
		b = value.NewObjBuilderFrom(lib.meta)
	} else {
		b = value.NewObjBuilder(2)
	}
	deps := make([]*value.Value, 0, len(lib.dirs))
	for _, d := range lib.dirs {
		db := value.NewObjBuilder(3)
		if d.meta != nil {
			db = value.NewObjBuilderFrom(d.meta)
		}
		if d.alias != "" {
			db.Set("as", value.Str(d.alias))
		}
		db.Set("is_data", value.Bool(d.data))
		db.Set("relpath", value.Str(d.rel))
		deps = append(deps, db.Seal())
	}
	defs := make([]*value.Value, 0, len(lib.defs))
	for _, d := range lib.defs {
		defs = append(defs, value.Str(fmt.Sprintf("%s/%d", d.Name, len(d.Params))))
	}
	b.Set("deps", value.Arr(deps...))
	b.Set("defs", value.Arr(defs...))
	return b.Seal(), nil
}

// errMessage is the text of err without a compile error's position.
func errMessage(err error) string {
	var ce *query.CompileError
	if errors.As(err, &ce) {
		return ce.Msg
	}
	return err.Error()
}

// directive is one import or include of a library, with its metadata
// already evaluated.
type directive struct {
	node    ast.Node
	rel     string
	alias   string
	data    bool
	include bool
	meta    *value.Value
}

// library is a module file that has been parsed and not yet linked.
type library struct {
	path string
	src  string
	root ast.Node
	meta *value.Value
	dirs []directive
	defs []*ast.FuncDef
}

// readFile reads a module or data file. It refuses anything but a regular
// file, and one larger than maxModuleBytes, because the path comes from a
// program that may be untrusted.
func readFile(path string) ([]byte, error) {
	f, err := os.Open(path) // #nosec G304 -- the search path and the module name pick the file
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, errors.New("not a regular file: " + path)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxModuleBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxModuleBytes {
		return nil, errors.New("module is too large: " + path)
	}
	return data, nil
}

// readLibrary parses the module file at path and collects its directives
// and definitions.
func readLibrary(path string) (*library, error) {
	data, err := readFile(path)
	if err != nil {
		return nil, &query.CompileError{Msg: err.Error()}
	}
	src := string(data)
	root, err := parser.ParseFile(src, path, Grammar())
	if err != nil {
		var pe *parser.Error
		if errors.As(err, &pe) {
			line, col := position(src, pe.Pos)
			return nil, &query.CompileError{Msg: pe.Msg + " in " + path, Line: line, Col: col}
		}
		return nil, err
	}
	lib := &library{path: path, src: src, root: root}
	for n := root; n != nil; {
		switch x := n.(type) {
		case *ast.Module:
			if lib.meta, err = constMeta(x.Meta); err != nil {
				return nil, err
			}
			n = x.Rest
		case *ast.Import:
			meta, err := constMeta(x.Meta)
			if err != nil {
				return nil, err
			}
			lib.dirs = append(lib.dirs, directive{node: x, rel: x.Path, alias: x.Alias, data: x.Data, meta: meta})
			n = x.Rest
		case *ast.Include:
			meta, err := constMeta(x.Meta)
			if err != nil {
				return nil, err
			}
			lib.dirs = append(lib.dirs, directive{node: x, rel: x.Path, include: true, meta: meta})
			n = x.Rest
		case *ast.FuncDef:
			lib.defs = append(lib.defs, x)
			n = x.Rest
		case *ast.Identity:
			n = nil
		default:
			return nil, &query.CompileError{Msg: "library should only have function definitions, not a main expression"}
		}
	}
	return lib, nil
}

// constMeta evaluates the metadata of a module directive, which must be a
// constant object. A directive with none gives nil.
func constMeta(n ast.Node) (*value.Value, error) {
	if n == nil {
		return nil, nil
	}
	if !isConstant(n) {
		return nil, &query.CompileError{Msg: "Module metadata must be constant"}
	}
	var out *value.Value
	ctx := runtime.NewCtx(New(), runtime.NumbersExact)
	err := eval.Run(ctx, n, nil, []*value.Value{value.Null()}, func(v *value.Value, _ value.Path) error {
		out = v
		return nil
	})
	if err != nil {
		return nil, &query.CompileError{Msg: err.Error()}
	}
	if out == nil || out.Kind() != value.KindObject {
		return nil, &query.CompileError{Msg: "Module metadata must be an object"}
	}
	return out, nil
}

// isConstant reports whether n is built from literals alone.
func isConstant(n ast.Node) bool {
	switch n := n.(type) {
	case nil:
		return true
	case *ast.Literal:
		return true
	case *ast.Str:
		for _, p := range n.Parts {
			if _, ok := p.(*ast.Literal); !ok {
				return false
			}
		}
		return true
	case *ast.ArrayCons:
		return isConstant(n.Body)
	case *ast.Comma:
		return isConstant(n.L) && isConstant(n.R)
	case *ast.ObjectCons:
		for _, e := range n.Entries {
			if e.Val == nil || !isConstant(e.Key) || !isConstant(e.Val) {
				return false
			}
		}
		return true
	}
	return false
}

// loaded is a module the linker has read, with the state the resolver
// keeps for it.
type loaded struct {
	lib     *library
	mod     *eval.Module
	loading bool
	deps    []*loaded
	exports []*scope
}

// linker loads the modules a program imports, once each, and records
// which module every directive of the main program loaded.
type linker struct {
	res    *ModuleResolver
	mods   map[ast.Node]*eval.Module
	files  map[string]*loaded
	defMod map[*ast.FuncDef]*loaded
}

func newLinker(opts CompileOptions) *linker {
	return &linker{
		res:    NewModuleResolver(opts),
		mods:   map[ast.Node]*eval.Module{},
		files:  map[string]*loaded{},
		defMod: map[*ast.FuncDef]*loaded{},
	}
}

// load reads the module that a directive names and everything it imports.
// libOrigin is the directory of the module that has the directive.
func (lk *linker) load(rel string, isData bool, meta *value.Value, libOrigin string) (*loaded, error) {
	var search *value.Value
	if meta != nil {
		search, _ = meta.Get("search")
	}
	path, err := lk.res.find(rel, isData, search, libOrigin)
	if err != nil {
		return nil, &query.CompileError{Msg: err.Error()}
	}
	return lk.loadPath(path, isData)
}

// loadPath reads the module or data file at path, once, and loads what
// the module imports.
func (lk *linker) loadPath(path string, isData bool) (*loaded, error) {
	key := path
	if abs, err := filepath.Abs(path); err == nil {
		key = abs
	}
	if isData {
		key += "\x00data"
	}
	if m, ok := lk.files[key]; ok {
		if m.loading {
			return nil, &query.CompileError{Msg: "circular import of " + path}
		}
		return m, nil
	}
	if len(lk.files) >= maxModules {
		return nil, &query.CompileError{Msg: fmt.Sprintf("a program may load at most %d modules", maxModules)}
	}
	if isData {
		return lk.loadData(key, path)
	}
	lib, err := readLibrary(path)
	if err != nil {
		return nil, err
	}
	m := &loaded{lib: lib, mod: &eval.Module{Path: path, Root: lib.root, Defs: lib.defs}, loading: true}
	lk.files[key] = m
	for _, d := range lib.dirs {
		dep, err := lk.load(d.rel, d.data, d.meta, filepath.Dir(path))
		if err != nil {
			return nil, err
		}
		m.deps = append(m.deps, dep)
		m.mod.Links = append(m.mod.Links, eval.Link{Alias: d.alias, Include: d.include, Data: d.data, Mod: dep.mod})
	}
	for _, def := range lib.defs {
		lk.defMod[def] = m
	}
	m.loading = false
	return m, nil
}

// loadData reads a JSON file into the array of its values.
func (lk *linker) loadData(key, path string) (*loaded, error) {
	data, err := readFile(path)
	if err != nil {
		return nil, &query.CompileError{Msg: "error loading data file " + path + ": " + err.Error()}
	}
	rd := load.NewJSONReader(bufio.NewReader(strings.NewReader(string(data))), path, load.LoadOptions{})
	var vals []*value.Value
	for {
		v, _, err := rd.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, &query.CompileError{Msg: "error loading data file " + path + ": " + err.Error()}
		}
		vals = append(vals, v)
	}
	m := &loaded{mod: &eval.Module{Path: path, Data: value.Arr(vals...)}}
	lk.files[key] = m
	return m, nil
}

// link loads the module that directive n of the main program names.
func (lk *linker) link(n ast.Node, rel string, isData bool, metaNode ast.Node) (*loaded, error) {
	meta, err := constMeta(metaNode)
	if err != nil {
		return nil, err
	}
	m, err := lk.load(rel, isData, meta, lk.res.progDir())
	if err != nil {
		return nil, err
	}
	lk.mods[n] = m.mod
	return m, nil
}

// bindLink returns s with the names that one import or include of m
// brings into scope.
func (r *resolver) bindLink(s *scope, l eval.Link, m *loaded) *scope {
	if l.Data {
		s = &scope{parent: s, kind: scopeVar, name: l.Alias + "::" + l.Alias}
		return &scope{parent: s, kind: scopeVar, name: l.Alias}
	}
	for _, e := range r.exports(m) {
		name := e.name
		if !l.Include {
			name = l.Alias + "::" + name
		}
		s = &scope{parent: s, kind: scopeFunc, name: name, arity: e.arity, def: e.def, at: e.at}
	}
	return s
}

// exports lists the definitions that m offers, each with the scope its
// body resolves in. That scope is the module's own: the program's
// variables, the module's imports, and the definitions before it.
func (r *resolver) exports(m *loaded) []*scope {
	if m.exports != nil {
		return m.exports
	}
	s := r.top
	for i, d := range m.lib.dirs {
		s = r.bindLink(s, eval.Link{Alias: d.alias, Include: d.include, Data: d.data}, m.deps[i])
	}
	out := make([]*scope, 0, len(m.lib.defs))
	for _, def := range m.lib.defs {
		s = &scope{parent: s, kind: scopeFunc, name: def.Name, arity: len(def.Params), def: def, at: s}
		out = append(out, s)
	}
	m.exports = out
	return out
}
