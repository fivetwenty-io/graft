package jq_test

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/fivetwenty-io/graft/internal/query/jq"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// runOpts compiles prog with opts and runs it on the JSON text in.
func runOpts(t testing.TB, opts jq.CompileOptions, prog, in string) ([]string, error) {
	t.Helper()
	p, err := jq.CompileWith(prog, opts)
	if err != nil {
		return nil, err
	}
	ctx := runtime.NewCtx(jq.New(), runtime.NumbersExact)
	ctx.Stderr = io.Discard
	ctx.Modules = jq.NewModuleResolver(opts)
	var out []string
	err = p.Run(ctx, p.Env, []*value.Value{mustJSON(t, in)}, func(v *value.Value, _ value.Path) error {
		out = append(out, runtime.JSON(v))
		return nil
	})
	return out, err
}

func wantOutputsOpts(t *testing.T, opts jq.CompileOptions, prog, in string, want ...string) {
	t.Helper()
	got, err := runOpts(t, opts, prog, in)
	if err != nil {
		t.Errorf("%s: unexpected error: %v", prog, err)
		return
	}
	if len(got)+len(want) > 0 && !reflect.DeepEqual(got, want) {
		t.Errorf("%s:\n got %.200q\nwant %.200q", prog, got, want)
	}
}

// wantErrorOpts checks that prog fails, at compile time or run time, with
// a message that contains msg.
func wantErrorOpts(t *testing.T, opts jq.CompileOptions, prog, in, msg string) {
	t.Helper()
	out, err := runOpts(t, opts, prog, in)
	if err == nil {
		t.Errorf("%s: want error %q, got outputs %q", prog, msg, out)
		return
	}
	if got := message(err); !strings.Contains(got, msg) {
		t.Errorf("%s:\n got error %q\nwant error containing %q", prog, got, msg)
	}
}

// modDir writes files, named relative to a new directory, and returns the
// options that search it.
func modDir(t *testing.T, files map[string]string) jq.CompileOptions {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return jq.CompileOptions{SearchPaths: []string{dir}}
}

func TestModulesImportIncludeData(t *testing.T) {
	opts := jq.CompileOptions{SearchPaths: []string{"testdata/mods"}}
	wantOutputsOpts(t, opts, `import "a" as a; a::f`, `null`, `1`, `2`)
	wantOutputsOpts(t, opts, `include "a"; f`, `null`, `1`, `2`)
	wantOutputsOpts(t, opts, `import "data" as $d; $d::d`, `null`, `[{"k":1}]`)
	wantOutputsOpts(t, opts, `import "data" as $d; $d`, `null`, `[{"k":1}]`)
	wantOutputsOpts(t, opts, `import "b/c" as c; c::g`, `null`, `"c"`)
	wantOutputsOpts(t, opts, `"a" | modulemeta | .deps, .defs`, `null`, `[]`, `["f/0"]`)
	wantOutputsOpts(t, opts, `get_search_list`, `null`, `["testdata/mods"]`)
	wantErrorOpts(t, opts, `import "nope" as n; 1`, `null`, `module not found: nope`)
}

func TestModulesScoping(t *testing.T) {
	opts := modDir(t, map[string]string{
		"a.jq":      `def f: 1, 2;`,
		"inner.jq":  `import "a" as a; def twice: [a::f, a::f]; def f: "inner";`,
		"inc.jq":    `import "a" as hidden; def seen: hidden::f;`,
		"shadow.jq": `def length: "mine"; def uses: length;`,
		"deep.jq":   `include "a"; def both: [f];`,
	})
	wantOutputsOpts(t, opts, `import "inner" as i; i::twice, i::f`, `null`, `[1,2,1,2]`, `"inner"`)
	wantOutputsOpts(t, opts, `import "a" as a; def f: 5; [f, a::f]`, `null`, `[5,1,2]`)
	wantOutputsOpts(t, opts, `import "a" as a; import "a" as b; [a::f, b::f]`, `null`, `[1,2,1,2]`)
	wantOutputsOpts(t, opts, `include "inc"; seen`, `null`, `1`, `2`)
	wantOutputsOpts(t, opts, `import "shadow" as s; [1,2] | length, s::uses`, `null`, `2`, `"mine"`)
	wantOutputsOpts(t, opts, `import "deep" as d; d::both`, `null`, `[1,2]`)
	// An import is private to the module that makes it, and an include
	// does not reach the program that imports the includer.
	wantErrorOpts(t, opts, `import "inc" as i; hidden::f`, `null`, `hidden::f/0 is not defined`)
	wantErrorOpts(t, opts, `import "inc" as i; i::hidden::f`, `null`, `i::hidden::f/0 is not defined`)
	wantErrorOpts(t, opts, `import "deep" as d; d::f`, `null`, `d::f/0 is not defined`)
	wantErrorOpts(t, opts, `import "a" as a; f`, `null`, `f/0 is not defined`)
	wantErrorOpts(t, opts, `import "a" as a; a::f(1)`, `null`, `a::f/1 is not defined`)
}

func TestModulesInputAndVariables(t *testing.T) {
	opts := modDir(t, map[string]string{
		"vars.jq": `def env_home: $ENV | type; def args: $ARGS.positional; def mine: .+1;`,
		"rec.jq":  `def count($n): if $n == 0 then 0 else count($n - 1) end;`,
	})
	wantOutputsOpts(t, opts, `import "vars" as v; v::env_home, v::args`, `null`, `"object"`, `[]`)
	wantOutputsOpts(t, opts, `import "vars" as v; v::mine`, `4`, `5`)
	wantOutputsOpts(t, opts, `import "rec" as r; r::count(50000)`, `null`, `0`)
}

func TestModulesSearchMetadata(t *testing.T) {
	opts := modDir(t, map[string]string{
		"lib/x.jq":     `def f: "x";`,
		"lib/user.jq":  `import "x" as x {search: "./"}; def g: x::f;`,
		"lib/plain.jq": `import "x" as x; def g: x::f;`,
	})
	opts.Unconfined = true
	dir := opts.SearchPaths[0]
	wantOutputsOpts(t, opts, `import "lib/user" as u; u::g`, `null`, `"x"`)
	wantOutputsOpts(t, opts, `import "lib/x" as x {search: "`+dir+`"}; x::f`, `null`, `"x"`)
	wantOutputsOpts(t, opts, `import "x" as x {search: ["nowhere", "`+filepath.Join(dir, "lib")+`"]}; x::f`, `null`, `"x"`)
	wantErrorOpts(t, opts, `import "lib/plain" as p; p::g`, `null`, `module not found: x`)
	wantErrorOpts(t, opts, `import "x" as x {search: "`+dir+`"}; x::f`, `null`, `module not found: x`)
	wantErrorOpts(t, opts, `import "x" as x {search: 5}; x::f`, `null`, `module not found: x`)
	wantErrorOpts(t, opts, `import "lib/x" as x ("a"|ascii_upcase); 1`, `null`, `Module metadata must be constant`)
	wantErrorOpts(t, opts, `import "lib/x" as x [1]; 1`, `null`, `Module metadata must be an object`)
	wantErrorOpts(t, opts, `module 5; 1`, `null`, `Module metadata must be an object`)
	wantOutputsOpts(t, opts, `module {name: "m"}; import "lib/x" as x {k: [1, {"z": null}]}; x::f`, `null`, `"x"`)
}

func TestModulesRelativePaths(t *testing.T) {
	opts := modDir(t, map[string]string{
		"a.jq":     `def f: 1;`,
		"d/e/e.jq": `def f: "dir";`,
	})
	wantOutputsOpts(t, opts, `import "d/e" as e; e::f`, `null`, `"dir"`)
	wantOutputsOpts(t, opts, `import "./a" as a; a::f`, `null`, `1`)
	wantErrorOpts(t, opts, `import "../a" as a; 1`, `null`, `Relative paths to modules may not traverse to parent directories (../a)`)
	wantErrorOpts(t, opts, `import "a/../a" as a; 1`, `null`, `may not traverse`)
	wantErrorOpts(t, opts, `import "a//" as a; 1`, `null`, `module names must not have equal consecutive components: a//`)
	wantErrorOpts(t, opts, `import "/a" as a; 1`, `null`, `module not found: /a`)
	wantErrorOpts(t, opts, `import "a.jq" as a; 1`, `null`, `module not found: a.jq`)
	wantErrorOpts(t, opts, `import "a" as $a; 1`, `null`, `module not found: a`)
}

func TestModulesLoadErrors(t *testing.T) {
	opts := modDir(t, map[string]string{
		"syntax.jq":  `def f: ;`,
		"undef.jq":   `def f: nothing_here; def ok: 1;`,
		"expr.jq":    `def f: 1; 5`,
		"cyc1.jq":    `import "cyc2" as c; def f: 1;`,
		"cyc2.jq":    `import "cyc1" as c; def f: 2;`,
		"self.jq":    `import "self" as s; def f: 1;`,
		"bad.json":   `{bad`,
		"multi.json": "1 2 3",
		"empty.json": "",
	})
	wantErrorOpts(t, opts, `import "syntax" as s; 1`, `null`, `unexpected ';' in `)
	wantErrorOpts(t, opts, `import "undef" as u; u::f`, `null`, `nothing_here/0 is not defined in `)
	wantOutputsOpts(t, opts, `import "undef" as u; u::ok`, `null`, `1`)
	wantOutputsOpts(t, opts, `import "undef" as u; 1`, `null`, `1`)
	wantErrorOpts(t, opts, `import "expr" as e; 1`, `null`, `library should only have function definitions, not a main expression`)
	wantErrorOpts(t, opts, `import "cyc1" as c; 1`, `null`, `circular import of `)
	wantErrorOpts(t, opts, `import "self" as s; 1`, `null`, `circular import of `)
	wantErrorOpts(t, opts, `import "bad" as $b; 1`, `null`, `error loading data file `)
	wantOutputsOpts(t, opts, `import "multi" as $m; $m`, `null`, `[1,2,3]`)
	wantOutputsOpts(t, opts, `import "empty" as $m; $m`, `null`, `[]`)
}

func TestModulesRefuseIrregularFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "dir.jq"), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("/dev/zero", filepath.Join(dir, "zero.jq")); err != nil {
		t.Fatal(err)
	}
	for _, unconfined := range []bool{false, true} {
		opts := jq.CompileOptions{SearchPaths: []string{dir}, Unconfined: unconfined}
		wantErrorOpts(t, opts, `import "dir" as d; 1`, `null`, `module not found: dir`)
		wantErrorOpts(t, opts, `import "zero" as z; 1`, `null`, `module not found: zero`)
		wantErrorOpts(t, opts, `import "zero" as $z; 1`, `null`, `module not found: zero`)
	}
}

func TestModulesRefuseBadNames(t *testing.T) {
	opts := jq.CompileOptions{SearchPaths: []string{t.TempDir()}}
	wantErrorOpts(t, opts, `import "a\\b" as x; 1`, `null`, `Modules must be named by relative paths using '/', not '\' (a\b)`)
	wantErrorOpts(t, opts, `import "a\u0000b" as x; 1`, `null`, `Module path contains a NUL byte`)
	wantErrorOpts(t, opts, `"a\\b" | modulemeta`, `null`, `Modules must be named by relative paths`)
}

func TestModuleDataErrorsNameOnlyPositions(t *testing.T) {
	opts := modDir(t, map[string]string{
		"lit.json":  `{"a": nullsecretword}`,
		"open.json": "[1,\n",
	})
	for _, name := range []string{"lit", "open"} {
		_, err := runOpts(t, opts, `import "`+name+`" as $d; $d`, `null`)
		if err == nil {
			t.Fatalf("%s: want an error", name)
		}
		got := message(err)
		if strings.Contains(got, "secretword") {
			t.Errorf("%s: the error quotes the file: %q", name, got)
		}
		if strings.Count(got, name+".json") != 1 || !strings.Contains(got, "error loading data file") || !strings.Contains(got, "at line") {
			t.Errorf("%s: want one path and a position, got %q", name, got)
		}
	}
}

func TestModuleMetaListsRedefinedNamesOnce(t *testing.T) {
	opts := modDir(t, map[string]string{"dup.jq": `def a: 1; def b: 2; def a: 3; def c: 4;`})
	wantOutputsOpts(t, opts, `"dup" | modulemeta | .defs`, `null`, `["a/0","b/0","c/0"]`)
}

func TestModuleMetaFoldsLikeJQAtTheEdges(t *testing.T) {
	opts := modDir(t, map[string]string{"loc.jq": `module {$__loc__, a: 1};`})
	wantOutputsOpts(t, opts, `"loc" | modulemeta | .__loc__.line, .a`, `null`, `1`, `1`)
	for _, meta := range []string{`(1, 2)`, `("a\(1)")`} {
		o := modDir(t, map[string]string{"bad.jq": `module {a: ` + meta + `};`})
		wantErrorOpts(t, o, `"bad" | modulemeta`, `null`, `Module metadata must be constant`)
	}
}

func TestModuleMeta(t *testing.T) {
	opts := modDir(t, map[string]string{
		"m.jq": `module {name: "m", version: 2};
import "a" as a {search: "s", k: 1};
import "data" as $d;
include "b/c" {q: 2};
def h: 1; def h(x): x; def g($a; b): 3;`,
		"plain.jq": `def f: 1;`,
	})
	wantOutputsOpts(t, opts, `"m" | modulemeta`, `null`,
		`{"name":"m","version":2,"deps":[{"search":"s","k":1,"as":"a","is_data":false,"relpath":"a"},{"as":"d","is_data":true,"relpath":"data"},{"q":2,"is_data":false,"relpath":"b/c"}],"defs":["h/0","h/1","g/2"]}`)
	wantOutputsOpts(t, opts, `"plain" | modulemeta`, `null`, `{"deps":[],"defs":["f/0"]}`)
	wantErrorOpts(t, opts, `modulemeta`, `1`, `modulemeta input module name must be a string`)
	wantErrorOpts(t, opts, `"nope" | modulemeta`, `null`, `module not found: nope`)
	wantErrorOpts(t, opts, `"../m" | modulemeta`, `null`, `may not traverse`)
}

func TestModuleOrigins(t *testing.T) {
	opts := jq.CompileOptions{ProgramOrigin: "/some/where", JQOrigin: "/opt/jq/bin"}
	wantOutputsOpts(t, opts, `get_prog_origin, get_jq_origin`, `null`, `"/some/where"`, `"/opt/jq/bin"`)
	wantOutputsOpts(t, opts, `get_search_list`, `null`, `[]`)
	wantOutputsOpts(t, jq.CompileOptions{SearchPaths: []string{"x", "y"}}, `get_search_list`, `null`, `["x","y"]`)
}

func TestModuleResolverSearchOrder(t *testing.T) {
	root := t.TempDir()
	write := func(rel string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("def f: 1;"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("one/m.jq")
	write("two/m.jq")
	write("two/n/n.jq")
	write("home/.jq/h.jq")
	write("exe/lib/e.jq")
	t.Setenv("HOME", filepath.Join(root, "home"))
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	r := &jq.ModuleResolver{
		SearchPaths: []string{filepath.Join(root, "one"), filepath.Join(root, "two")},
		JQDir:       filepath.Join(root, "exe", "bin"),
	}
	for rel, want := range map[string]string{
		"m": filepath.Join(root, "one", "m.jq"),
		"n": filepath.Join(root, "two", "n", "n.jq"),
	} {
		got, err := r.Resolve(rel, false)
		if err != nil || got != want {
			t.Errorf("Resolve(%q) = %q, %v; want %q", rel, got, err, want)
		}
	}
	def := &jq.ModuleResolver{JQDir: filepath.Join(root, "exe", "bin"), Origin: root}
	for _, rel := range []string{"h", "e", "one/m"} {
		if got, err := def.Resolve(rel, false); err == nil {
			t.Errorf("Resolve(%q) with no search paths found %q", rel, got)
		}
	}
	listed := &jq.ModuleResolver{
		SearchPaths: []string{"~/.jq", "$ORIGIN/../lib"},
		JQDir:       filepath.Join(root, "exe", "bin"),
		Unconfined:  true,
	}
	for rel, want := range map[string]string{
		"h": filepath.Join(root, "home", ".jq", "h.jq"),
		"e": filepath.Join(root, "exe", "lib", "e.jq"),
	} {
		got, err := listed.Resolve(rel, false)
		if err != nil || got != want {
			t.Errorf("listed Resolve(%q) = %q, %v; want %q", rel, got, err, want)
		}
	}
	if _, err := r.Resolve("m", true); err == nil {
		t.Error("Resolve of a data module found a .jq file")
	}
}

func TestModulesNeedSearchPaths(t *testing.T) {
	bare := jq.CompileOptions{ProgramOrigin: "testdata/mods"}
	wantErrorOpts(t, bare, `import "a" as a; 1`, `null`, `module not found: a`)
	wantErrorOpts(t, bare, `include "a"; 1`, `null`, `module not found: a`)
	wantErrorOpts(t, jq.CompileOptions{}, `import "a" as a {search: "testdata/mods"}; 1`, `null`, `module not found: a`)
	wantErrorOpts(t, bare, `"a" | modulemeta`, `null`, `module not found: a`)
}

func TestModulesAutoInclude(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	good := write("good.jq", `def hello: "hi"; def twice(f): f | f;`)
	opts := jq.CompileOptions{AutoInclude: good}
	wantOutputsOpts(t, opts, `hello`, `null`, `"hi"`)
	wantOutputsOpts(t, opts, `1 | twice(. + 1)`, `null`, `3`)
	wantOutputsOpts(t, opts, `def hello: "mine"; hello`, `null`, `"mine"`)
	wantOutputsOpts(t, opts, `1`, `null`, `1`)
	wantErrorOpts(t, jq.CompileOptions{}, `hello`, `null`, `hello/0 is not defined`)

	wantErrorOpts(t, jq.CompileOptions{AutoInclude: write("bad.jq", `def x: ;`)}, `1`, `null`, `bad.jq`)
	wantErrorOpts(t, jq.CompileOptions{AutoInclude: write("main.jq", `def f: 1; f`)}, `1`, `null`, `library should only have function definitions`)
	wantErrorOpts(t, jq.CompileOptions{AutoInclude: filepath.Join(dir, "missing.jq")}, `1`, `null`, `missing.jq`)

	write("dep.jq", `def dep: "d";`)
	withImport := write("imp.jq", `import "dep" as d; def viaDep: d::dep;`)
	wantOutputsOpts(t, jq.CompileOptions{AutoInclude: withImport, SearchPaths: []string{dir}}, `viaDep`, `null`, `"d"`)
	wantErrorOpts(t, jq.CompileOptions{AutoInclude: withImport, SearchPaths: []string{dir}}, `d::dep`, `null`, `d::dep/0 is not defined`)
}

func TestModuleMetaFoldsConstants(t *testing.T) {
	opts := modDir(t, map[string]string{
		"sum.jq":    `module {a: (1 + 1), b: [2 * 3, 7 % 4], c: ("a" + "b"), d: (1 < 2)};`,
		"nested.jq": `module {a: ((1 + 1) + 1), b: ({"x": 1} + {"y": 2}), c: ([1, 2] - [1]), d: (1 + (2 * 3)), e: ("a,b" / ",")};`,
		"loose.jq":  `module {a: (null + 1), b: (1 == 1), c: (2 >= 3), d: (6 / 4)};`,
	})
	wantOutputsOpts(t, opts, `"sum" | modulemeta | .a, .b, .c, .d`, `null`, `2`, `[6,3]`, `"ab"`, `true`)
	wantOutputsOpts(t, opts, `"nested" | modulemeta | .a, .b, .c, .d, .e`, `null`, `3`, `{"x":1,"y":2}`, `[2]`, `7`, `["a","b"]`)
	wantOutputsOpts(t, opts, `"loose" | modulemeta | .a, .b, .c, .d`, `null`, `1`, `true`, `false`, `1.5`)
	wantOutputsOpts(t, opts, `import "sum" as s {k: (1 + 2)}; 1`, `null`, `1`)

	for _, meta := range []string{`(-1)`, `(true and false)`, `(null // 1)`, `([1] | .[0])`, `((1, 2) + 1)`, `(1 / 0)`, `("x" - "y")`, `(1 + "a")`, `(.a + 1)`, `(length + 1)`} {
		o := modDir(t, map[string]string{"bad.jq": `module {a: ` + meta + `};`})
		wantErrorOpts(t, o, `"bad" | modulemeta`, `null`, `Module metadata must be constant`)
	}
}

// bigImports returns options that find a module of defs definitions and a
// program that imports it imports times.
func bigImports(t *testing.T, defs, imports int) (jq.CompileOptions, string) {
	t.Helper()
	var mod, prog strings.Builder
	for i := range defs {
		mod.WriteString("def f" + strconv.Itoa(i) + ": . + " + strconv.Itoa(i) + ";\n")
	}
	for i := range imports {
		prog.WriteString(`import "big" as a` + strconv.Itoa(i) + ";\n")
	}
	prog.WriteString("a0::f1 | a" + strconv.Itoa(imports-1) + "::f2")
	return modDir(t, map[string]string{"big.jq": mod.String()}), prog.String()
}

func TestModuleImportCostDoesNotGrowWithDefinitions(t *testing.T) {
	opts, prog := bigImports(t, 300, 60)
	p, err := jq.CompileWith(prog, opts)
	if err != nil {
		t.Fatal(err)
	}
	ctx := runtime.NewCtx(jq.New(), runtime.NumbersExact)
	in := []*value.Value{value.Num(1)}
	run := func() {
		if err := p.Run(ctx, p.Env, in, func(*value.Value, value.Path) error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	run()
	// Binding a module costs the same however many definitions it holds,
	// so one run allocates a few times per import and not once per
	// definition of each.
	if got := testing.AllocsPerRun(5, run); got > 60*10 {
		t.Errorf("a run allocated %.0f times for 60 imports of a 300-definition module", got)
	}
	if got := testing.AllocsPerRun(1, func() {
		if _, err := jq.CompileWith(prog, opts); err != nil {
			t.Fatal(err)
		}
	}); got > 20000 {
		t.Errorf("compiling allocated %.0f times for 60 imports of a 300-definition module", got)
	}
}

func TestModuleRunsShareInstancesConcurrently(t *testing.T) {
	opts, prog := bigImports(t, 20, 5)
	p, err := jq.CompileWith(prog, opts)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx := runtime.NewCtx(jq.New(), runtime.NumbersExact)
			for range 50 {
				var got []string
				err := p.Run(ctx, p.Env, []*value.Value{value.Num(1)}, func(v *value.Value, _ value.Path) error {
					got = append(got, runtime.JSON(v))
					return nil
				})
				if err != nil || len(got) != 1 || got[0] != "4" {
					t.Errorf("got %q, %v", got, err)
					return
				}
			}
		}()
	}
	wg.Wait()
}

// confinement lays out a listed directory, files beside it that a program
// has no business reading, and a home directory and executable directory
// that jq would search.
func confinement(t *testing.T) (root string, opts jq.CompileOptions) {
	t.Helper()
	root = t.TempDir()
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	write := func(rel, body string) {
		t.Helper()
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("allowed/ok.jq", `def ok: "ok";`)
	write("outside/o.jq", `def o: "outside";`)
	write("outside/d.json", `{"secret":1}`)
	write("home/.jq/h.jq", `def h: "home";`)
	write("exe/lib/e.jq", `def e: "exe";`)
	if err := os.Symlink(filepath.Join(root, "outside", "o.jq"), filepath.Join(root, "allowed", "link.jq")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "outside"), filepath.Join(root, "allowed", "dir")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Chdir(filepath.Join(root, "outside"))
	return root, jq.CompileOptions{
		SearchPaths:   []string{filepath.Join(root, "allowed")},
		ProgramOrigin: filepath.Join(root, "allowed"),
		JQOrigin:      filepath.Join(root, "exe", "bin"),
	}
}

func TestConfinedModulesStayInListedDirectories(t *testing.T) {
	root, opts := confinement(t)
	wantOutputsOpts(t, opts, `import "ok" as m; m::ok`, `null`, `"ok"`)
	notFound := func(prog string) {
		t.Helper()
		wantErrorOpts(t, opts, prog, `null`, `module not found`)
	}
	notFound(`import "o" as m {search: "` + filepath.Join(root, "outside") + `"}; m::o`)
	notFound(`import "o" as m {search: "../outside"}; m::o`)
	notFound(`import "h" as m {search: "~/.jq"}; m::h`)
	notFound(`import "e" as m {search: "$ORIGIN/../lib"}; m::e`)
	notFound(`import "o" as m; m::o`)
	notFound(`import "d" as $d; $d`)
	notFound(`import "link" as m; m::o`)
	notFound(`import "dir/o" as m; m::o`)
	notFound(`"o" | modulemeta`)

	tilde := opts
	tilde.SearchPaths = []string{"~/.jq", "$ORIGIN/../lib", "."}
	for _, prog := range []string{`import "h" as m; m::h`, `import "e" as m; m::e`, `import "o" as m; m::o`} {
		wantErrorOpts(t, tilde, prog, `null`, `module not found`)
	}
}

func TestUnconfinedModulesKeepJQSearchRules(t *testing.T) {
	root, opts := confinement(t)
	opts.Unconfined = true
	wantOutputsOpts(t, opts, `import "o" as m {search: "`+filepath.Join(root, "outside")+`"}; m::o`, `null`, `"outside"`)
	wantOutputsOpts(t, opts, `import "o" as m {search: "../outside"}; m::o`, `null`, `"outside"`)
	wantOutputsOpts(t, opts, `import "h" as m {search: "~/.jq"}; m::h`, `null`, `"home"`)
	wantOutputsOpts(t, opts, `import "e" as m {search: "$ORIGIN/../lib"}; m::e`, `null`, `"exe"`)
	wantOutputsOpts(t, opts, `import "o" as m; m::o`, `null`, `"outside"`)
	wantOutputsOpts(t, opts, `import "link" as m; m::o`, `null`, `"outside"`)
	wantOutputsOpts(t, opts, `import "dir/o" as m; m::o`, `null`, `"outside"`)
}
