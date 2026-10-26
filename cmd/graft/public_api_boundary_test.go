package main

import (
	"go/ast"
	"go/build"
	"go/doc"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const modulePath = "github.com/fivetwenty-io/graft"

// publicPackages are the query engine's public packages, by directory
// under the module root. pkg/yq joins them when it exists.
var publicPackages = []string{
	"pkg/query",
	"pkg/query/value",
	"pkg/jq",
	"pkg/regex",
	"pkg/decimal",
	"pkg/sortkeys",
	"pkg/keyorder",
}

// publicStandIns are packages the boundary walker treats as public so it
// looks through their own types, which is what lets a fixture prove the
// walker finds what it should.
var publicStandIns = []string{modulePath + "/internal/query/apitest"}

// moduleRoot returns the directory holding go.mod, two levels above this
// package.
func moduleRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("no go.mod at %s: %v", root, err)
	}
	return root
}

// goImports returns the import paths of the production files in dir.
func goImports(t *testing.T, dir string) []string {
	t.Helper()
	pkg, err := build.ImportDir(dir, 0)
	if err != nil {
		t.Fatal(err)
	}
	return pkg.Imports
}

func TestCLIUsesOnlyPublicPackages(t *testing.T) {
	for _, imp := range goImports(t, ".") {
		if strings.HasPrefix(imp, modulePath+"/internal/query") {
			t.Errorf("cmd/graft imports %s; use pkg/jq, pkg/yq, pkg/query, pkg/regex, or pkg/sortkeys", imp)
		}
	}
}

// typeCheck loads the package at the import path with go/types, from
// source, so the check needs no compiled export data.
func typeCheck(t *testing.T, path string) *types.Package {
	t.Helper()
	fset := token.NewFileSet()
	imp := importer.ForCompiler(fset, "source", nil)
	pkg, err := imp.Import(path)
	if err != nil {
		t.Fatalf("type-checking %s: %v", path, err)
	}
	return pkg
}

// exportedScope returns the exported objects the package declares.
func exportedScope(t *testing.T, path string) map[string]types.Object {
	t.Helper()
	scope := typeCheck(t, path).Scope()
	objs := map[string]types.Object{}
	for _, name := range scope.Names() {
		if obj := scope.Lookup(name); obj.Exported() {
			objs[name] = obj
		}
	}
	return objs
}

// isInternal reports whether a package path names a package the public
// surface must not mention.
func isInternal(path string) bool {
	return strings.Contains(path+"/", "/internal/") && !slices.Contains(publicStandIns, path)
}

// internalReferences returns every internal named type typ reaches through
// exported fields, method sets, signatures, and type-parameter
// constraints, each once. seen breaks cycles.
func internalReferences(typ types.Type, seen map[types.Type]bool) []string {
	if typ == nil || seen[typ] {
		return nil
	}
	seen[typ] = true
	var leaks []string
	walk := func(t types.Type) { leaks = append(leaks, internalReferences(t, seen)...) }
	switch t := typ.(type) {
	case *types.Named:
		obj := t.Obj()
		if obj.Pkg() != nil && isInternal(obj.Pkg().Path()) {
			return []string{obj.Pkg().Path() + "." + obj.Name()}
		}
		if obj.Pkg() == nil {
			return nil // error and comparable belong to no package
		}
		for i := range t.TypeArgs().Len() {
			walk(t.TypeArgs().At(i))
		}
		for i := range t.TypeParams().Len() {
			walk(t.TypeParams().At(i).Constraint())
		}
		walk(t.Underlying())
		for _, recv := range []types.Type{t, types.NewPointer(t)} {
			ms := types.NewMethodSet(recv)
			for i := range ms.Len() {
				if m := ms.At(i).Obj(); m.Exported() {
					walk(m.Type())
				}
			}
		}
	case *types.Alias:
		walk(types.Unalias(t))
	case *types.Pointer:
		walk(t.Elem())
	case *types.Slice:
		walk(t.Elem())
	case *types.Array:
		walk(t.Elem())
	case *types.Map:
		walk(t.Key())
		walk(t.Elem())
	case *types.Chan:
		walk(t.Elem())
	case *types.Struct:
		for i := range t.NumFields() {
			if f := t.Field(i); f.Exported() || f.Embedded() {
				walk(f.Type())
			}
		}
	case *types.Interface:
		for i := range t.NumMethods() {
			if m := t.Method(i); m.Exported() {
				walk(m.Type())
			}
		}
		for i := range t.NumEmbeddeds() {
			walk(t.EmbeddedType(i))
		}
	case *types.Union:
		for i := range t.Len() {
			walk(t.Term(i).Type())
		}
	case *types.Signature:
		for i := range t.TypeParams().Len() {
			walk(t.TypeParams().At(i).Constraint())
		}
		walk(t.Params())
		walk(t.Results())
	case *types.Tuple:
		for i := range t.Len() {
			walk(t.At(i).Type())
		}
	case *types.TypeParam:
		walk(t.Constraint())
	}
	return leaks
}

func TestPublicPackagesExposeNoInternalTypes(t *testing.T) {
	for _, pkg := range publicPackages {
		objs := exportedScope(t, modulePath+"/"+pkg)
		names := make([]string, 0, len(objs))
		for name := range objs {
			names = append(names, name)
		}
		slices.Sort(names)
		for _, name := range names {
			for _, leak := range internalReferences(objs[name].Type(), map[types.Type]bool{}) {
				t.Errorf("%s.%s reaches %s", pkg, name, leak)
			}
		}
	}
}

// typeOf returns the type of the exported name in the package at dir.
func typeOf(t *testing.T, dir, name string) types.Type {
	t.Helper()
	obj := typeCheck(t, modulePath+"/"+dir).Scope().Lookup(name)
	if obj == nil {
		t.Fatalf("%s declares no %s", dir, name)
	}
	return obj.Type()
}

func TestBoundaryCheckerSeesHiddenLeaks(t *testing.T) {
	leaks := internalReferences(typeOf(t, "internal/query/apitest", "Leaky"), map[types.Type]bool{})
	slices.Sort(leaks)
	wantEqual(t, leaks, []string{
		modulePath + "/internal/query/eval.Program",
		modulePath + "/internal/query/load.Document",
		modulePath + "/internal/query/runtime.Ctx",
	})
}

// packageDoc reads the package at dir, test files included, so its
// examples attach to the names they show.
func packageDoc(t *testing.T, dir string) *doc.Package {
	t.Helper()
	fset := token.NewFileSet()
	full := filepath.Join(moduleRoot(t), dir)
	entries, err := os.ReadDir(full)
	if err != nil {
		t.Fatal(err)
	}
	var files []*ast.File
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") {
			continue
		}
		f, err := parser.ParseFile(fset, filepath.Join(full, e.Name()), nil, parser.ParseComments)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, f)
	}
	p, err := doc.NewFromFiles(fset, files, modulePath+"/"+dir)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// missingExamples returns the exported functions, types, and methods in p
// that no Example shows.
func missingExamples(p *doc.Package) []string {
	var missing []string
	check := func(name string, examples []*doc.Example) {
		if len(examples) == 0 {
			missing = append(missing, name)
		}
	}
	for _, f := range p.Funcs {
		check(f.Name, f.Examples)
	}
	for _, typ := range p.Types {
		check(typ.Name, typ.Examples)
		for _, f := range typ.Funcs {
			check(f.Name, f.Examples)
		}
		for _, m := range typ.Methods {
			check(typ.Name+"."+m.Name, m.Examples)
		}
	}
	return missing
}

func TestEveryExportHasAnExample(t *testing.T) {
	for _, pkg := range publicPackages {
		for _, name := range missingExamples(packageDoc(t, pkg)) {
			t.Errorf("%s.%s has no Example", pkg, name)
		}
	}
}

func TestPublicPackagesStateTheirStability(t *testing.T) {
	for _, pkg := range publicPackages {
		if !strings.Contains(packageDoc(t, pkg).Doc, "# Stability\n") {
			t.Errorf("%s has no Stability section in its package comment", pkg)
		}
	}
}
