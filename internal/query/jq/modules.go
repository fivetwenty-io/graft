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
// passes it, as the jq command does. ProgramOrigin is the directory of the
// program file, and empty means the current directory. JQOrigin is what
// $ORIGIN stands for, and empty means the directory of the running
// executable.
//
// Unless Unconfined is set, a program reads only files that sit under the
// directories of SearchPaths. Such a search ignores the search key of a
// directive's metadata, does not look in the current directory unless it
// is listed, does not expand ~ or $ORIGIN, takes a relative entry from
// ProgramOrigin, and refuses a file whose real path, after symbolic links,
// leaves every listed directory. Unconfined gives jq's own rules, which
// let a program name any directory.
type CompileOptions struct {
	SearchPaths   []string
	ProgramOrigin string
	JQOrigin      string
	Unconfined    bool
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
	Unconfined  bool
}

var _ runtime.ModuleLibrary = (*ModuleResolver)(nil)

// NewModuleResolver returns the resolver that opts describe.
func NewModuleResolver(opts CompileOptions) *ModuleResolver {
	return &ModuleResolver{SearchPaths: opts.SearchPaths, Origin: opts.ProgramOrigin, JQDir: opts.JQOrigin, Unconfined: opts.Unconfined}
}

// SearchList returns the library directories as they were given.
func (r *ModuleResolver) SearchList() []string {
	return append([]string{}, r.SearchPaths...)
}

// ProgOrigin returns the directory of the main program as an absolute
// path.
func (r *ModuleResolver) ProgOrigin() string {
	return realPath(r.progDir())
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
		dirs := r.chain(search, libOrigin)
		var roots []string
		if !r.Unconfined {
			roots = realDirs(dirs)
		}
		for _, dir := range dirs {
			base := filepath.Join(dir, relpath)
			for _, cand := range []string{base + suffix, filepath.Join(base, filepath.Base(relpath)+suffix)} {
				info, err := os.Stat(cand)
				if err != nil || !info.Mode().IsRegular() {
					continue
				}
				resolved := realPath(cand)
				if !r.Unconfined && !underAny(resolved, roots) {
					continue
				}
				return resolved, nil
			}
		}
	}
	return "", errors.New("module not found: " + relpath)
}

// chain is the list of directories to search. A directive that names
// none searches the current directory and then the library path.
func (r *ModuleResolver) chain(search *value.Value, libOrigin string) []string {
	if !r.Unconfined {
		return r.confinedChain()
	}
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

// confinedChain is the list of directories a confined search reads: each
// listed entry as given, taken from the program's directory when it is
// relative.
func (r *ModuleResolver) confinedChain() []string {
	dirs := make([]string, 0, len(r.SearchPaths))
	for _, p := range r.SearchPaths {
		if !filepath.IsAbs(p) {
			p = filepath.Join(r.progDir(), p)
		}
		dirs = append(dirs, p)
	}
	return dirs
}

// realPath returns the absolute path of p with its symbolic links
// resolved, or p when that cannot be done.
func realPath(p string) string {
	abs, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}

// realDirs returns the real path of each directory that exists.
func realDirs(dirs []string) []string {
	out := make([]string, 0, len(dirs))
	for _, d := range dirs {
		abs, err := filepath.Abs(d)
		if err != nil {
			continue
		}
		if resolved, err := filepath.EvalSymlinks(abs); err == nil {
			out = append(out, resolved)
		}
	}
	return out
}

// underAny reports whether path is inside one of the directories.
func underAny(path string, dirs []string) bool {
	for _, d := range dirs {
		rel, err := filepath.Rel(d, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return false
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
	if strings.IndexByte(rel, 0) >= 0 {
		return errors.New("Module path contains a NUL byte") //nolint:staticcheck // jq's own message
	}
	if strings.Contains(rel, `\`) {
		return errors.New(`Modules must be named by relative paths using '/', not '\' (` + rel + ")")
	}
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
	seen := make(map[string]bool, len(lib.defs))
	for _, d := range lib.defs {
		name := fmt.Sprintf("%s/%d", d.Name, len(d.Params))
		if !seen[name] {
			seen[name] = true
			defs = append(defs, value.Str(name))
		}
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
	f, err := openNoBlock(path)
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
	count := 0
	ctx := runtime.NewCtx(New(), runtime.NumbersExact)
	err := eval.Run(ctx, n, nil, []*value.Value{value.Null()}, func(v *value.Value, _ value.Path) error {
		out = v
		count++
		return nil
	})
	if err == nil && count > 1 {
		err = errors.New("more than one value")
	}
	if err != nil {
		// Folding failed, as in 1 / 0, and jq refuses what it cannot fold.
		return nil, &query.CompileError{Msg: "Module metadata must be constant"}
	}
	if out == nil || out.Kind() != value.KindObject {
		return nil, &query.CompileError{Msg: "Module metadata must be an object"}
	}
	return out, nil
}

// foldedOps are the binary operators jq folds when both operands are
// constants. The logical operators, alternation, pipes, and a negated
// literal are not folded.
var foldedOps = map[string]bool{
	"+": true, "-": true, "*": true, "/": true, "%": true,
	"==": true, "!=": true, "<": true, "<=": true, ">": true, ">=": true,
}

// isConstOperand reports whether n is a constant that stands for one
// value, which is what an operand of a folded operator must be.
func isConstOperand(n ast.Node) bool {
	if _, ok := n.(*ast.Comma); ok {
		return false
	}
	return isConstant(n)
}

// isConstant reports whether n is built from literals, folded operators
// over constants, and the collections that hold them.
func isConstant(n ast.Node) bool {
	switch n := n.(type) {
	case nil:
		return true
	case *ast.Literal, *ast.Loc:
		return true
	case *ast.Str:
		return isConstStr(n)
	case *ast.ArrayCons:
		return isConstant(n.Body)
	case *ast.Comma:
		return isConstant(n.L) && isConstant(n.R)
	case *ast.Binary:
		return foldedOps[n.Op] && isConstOperand(n.L) && isConstOperand(n.R)
	case *ast.ObjectCons:
		return isConstObject(n)
	}
	return false
}

// isConstStr reports whether a string has no interpolation.
func isConstStr(n *ast.Str) bool {
	for _, p := range n.Parts {
		if lit, ok := p.(*ast.Literal); !ok || !isText(lit) {
			return false
		}
	}
	return true
}

// isText reports whether a literal is a run of string text. The parser
// makes the same node of text and of an interpolated constant, so this
// refuses an interpolated number or null, and it takes an interpolated
// string, which jq refuses, as text.
func isText(lit *ast.Literal) bool {
	_, ok := lit.Value.(string)
	return ok
}

// isConstObject reports whether every entry of an object construction has
// a constant key and a constant value.
func isConstObject(n *ast.ObjectCons) bool {
	for _, e := range n.Entries {
		if e.Val == nil || !isConstant(e.Key) || !isConstant(e.Val) {
			return false
		}
	}
	return true
}

// loaded is a module the linker has read, with the state the resolver
// keeps for it.
type loaded struct {
	lib     *library
	mod     *eval.Module
	loading bool
	deps    []*loaded
	exports map[exportKey]*scope
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
	key := realPath(path)
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
			return nil, &query.CompileError{Msg: "error loading data file " + path + ": " + dataErrorText(err)}
		}
		vals = append(vals, v)
	}
	m := &loaded{mod: &eval.Module{Path: path, Data: value.Arr(vals...)}}
	lk.files[key] = m
	return m, nil
}

// dataErrorText describes a failure to read a data file by its position
// alone. The reader's own message can quote the file, and a program that
// imports a file it should not see must not learn what is in it.
func dataErrorText(err error) string {
	var se *load.SyntaxError
	if errors.As(err, &se) && se.Line > 0 {
		return fmt.Sprintf("Invalid JSON at line %d, column %d", se.Line, se.Col)
	}
	return "Invalid JSON"
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
	if l.Include {
		return &scope{parent: s, kind: scopeFuncs, table: r.exports(m)}
	}
	return &scope{parent: s, kind: scopeNS, name: l.Alias, table: r.exports(m)}
}

// exports returns the functions a module defines, by name and arity. A
// name defined twice is the later definition.
func (r *resolver) exports(m *loaded) map[exportKey]*scope {
	if m.exports != nil {
		return m.exports
	}
	s := r.top
	for i, d := range m.lib.dirs {
		s = r.bindLink(s, eval.Link{Alias: d.alias, Include: d.include, Data: d.data}, m.deps[i])
	}
	out := make(map[exportKey]*scope, len(m.lib.defs))
	for _, def := range m.lib.defs {
		s = &scope{parent: s, kind: scopeFunc, name: def.Name, arity: len(def.Params), def: def, at: s}
		out[exportKey{def.Name, len(def.Params)}] = s
	}
	m.exports = out
	return out
}
