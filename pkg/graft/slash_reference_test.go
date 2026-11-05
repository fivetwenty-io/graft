package graft_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
	_ "github.com/fivetwenty-io/graft/pkg/graft/operators"
)

// slashModes runs a merge through both evaluator modes: the library's
// sequential default and the parallel mode the graft CLI uses.
var slashModes = []struct {
	name string
	opts []graft.EngineOption
}{
	{"sequential", nil},
	{"parallel", []graft.EngineOption{graft.WithParallel(true)}},
}

func evalSlashDoc(t *testing.T, opts []graft.EngineOption, src string) (graft.Document, error) {
	t.Helper()
	engine, err := graft.NewEngine(opts...)
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	doc, err := engine.ParseYAML([]byte(src))
	if err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}
	return engine.Evaluate(context.Background(), doc)
}

// slashDoc holds keys with a '/' in them. jobs.count and jobs.count/2
// sit side by side, so reading count/2 as division would give 4 rather
// than the path's value.
const slashDoc = `
jobs:
  count: 8
  count/2: path-value
  postgresql/0:
    ips: [10.0.0.5]
meta:
  a/b/c: abc
postgresql/0:
  ips: [10.0.0.9]
`

// TestSlashInReferenceIsPartOfThePath pins that a '/' with no whitespace
// before it is part of the reference, wherever the expression grammar
// reads a reference, the way spruce reads it.
func TestSlashInReferenceIsPartOfThePath(t *testing.T) {
	paths := []struct {
		path string
		want string
	}{
		{"jobs.postgresql/0.ips[0]", "10.0.0.5"},
		{"jobs.count/2", "path-value"},
		{"meta.a/b/c", "abc"},
		{"postgresql/0.ips[0]", "10.0.0.9"},
	}
	shapes := []struct {
		name   string
		expr   func(path string) string
		result func(want string) string
	}{
		{"grab", func(p string) string { return "(( grab " + p + " ))" }, func(w string) string { return w }},
		{"concat", func(p string) string { return `(( concat "v=" ` + p + " ))" }, func(w string) string { return "v=" + w }},
		{"left of ||", func(p string) string { return `(( grab ` + p + ` || "fallback" ))` }, func(w string) string { return w }},
		{"right of ||", func(p string) string { return "(( grab missing.key || " + p + " ))" }, func(w string) string { return w }},
		{"nested call", func(p string) string { return `(( concat "v=" (grab ` + p + `) ))` }, func(w string) string { return "v=" + w }},
		{"ternary branch", func(p string) string { return `'(( true ? ` + p + ` : "no" ))'` }, func(w string) string { return w }},
	}

	for _, mode := range slashModes {
		for _, sh := range shapes {
			for _, p := range paths {
				t.Run(mode.name+"/"+sh.name+"/"+p.path, func(t *testing.T) {
					src := slashDoc + "result: " + sh.expr(p.path) + "\n"
					doc, err := evalSlashDoc(t, mode.opts, src)
					if err != nil {
						t.Fatalf("unexpected error: %v", err)
					}
					got, err := doc.Get("result")
					if err != nil {
						t.Fatalf("failed to read result: %v", err)
					}
					if fmt.Sprint(got) != sh.result(p.want) {
						t.Fatalf("result = %v (%T), want %q", got, got, sh.result(p.want))
					}
				})
			}
		}
	}
}

// TestSlashEdgeCases pins the edge cases of the slash rule. Each
// matches what spruce v1.35.17 does with the same input, except the
// spaced forms, which divide.
func TestSlashEdgeCases(t *testing.T) {
	const doc = `
a: 8
b: 2
"8/2": eight-slash-two
"a/": a-trailing
"a/b": a-slash-b
arr:
  - "/x": slash-x
`
	cases := []struct {
		name string
		expr string
		want string
	}{
		{"numeric segment with a slash is a path", "(( grab 8/2 ))", "eight-slash-two"},
		{"single segment with a slash is a path", "(( grab a/b ))", "a-slash-b"},
		{"trailing slash is part of the path", "(( grab a/ ))", "a-trailing"},
		{"slash then space ends the path", "(( grab a/ b ))", "[a-trailing 2]"},
		{"slash after a bracket is part of the path", "(( grab arr[0]/x ))", "slash-x"},
		{"slash after a bracket inside concat", `(( concat "v=" arr[0]/x ))`, "v=slash-x"},
		{"spaced slash divides", "(( a / b ))", "4"},
		{"slash with a space before it divides", "(( a /b ))", "4"},
		{"spaced slash after a dotted path divides", "(( grab a / b ))", "4"},
		{"calc keeps its own arithmetic", `(( calc "8/2" ))`, "4"},
		{"bare slash path passes through like spruce", "(( a/b ))", "(( a/b ))"},
	}

	for _, mode := range slashModes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				d, err := evalSlashDoc(t, mode.opts, doc+"result: "+tc.expr+"\n")
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				got, err := d.Get("result")
				if err != nil {
					t.Fatalf("failed to read result: %v", err)
				}
				if fmt.Sprint(got) != tc.want {
					t.Fatalf("result = %v (%T), want %q", got, got, tc.want)
				}
			})
		}
	}
}

// TestSlashEdgeCaseErrors pins the slash shapes that fail. A slash
// before a bracket keeps the slash in the segment before the bracket,
// as spruce does.
func TestSlashEdgeCaseErrors(t *testing.T) {
	const doc = `
arr: [x0, x1]
`
	cases := []struct {
		name    string
		expr    string
		wantErr string
	}{
		{"slash before a bracket", "(( grab arr/[0] ))", "`$.arr/` could not be found"},
	}
	for _, mode := range slashModes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				_, err := evalSlashDoc(t, mode.opts, doc+"result: "+tc.expr+"\n")
				if err == nil {
					t.Fatalf("expected an error")
				}
				if !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.wantErr)
				}
			})
		}
	}
}

// TestUnresolvedSlashPathError pins that a slash path that does not
// exist fails with a "could not be found" error naming the whole path,
// and that neither the plain nor the detailed text quotes a value.
func TestUnresolvedSlashPathError(t *testing.T) {
	const doc = `
jobs:
  count: 8675309
`
	assertUnresolvedPathError(t, doc, "jobs.count/3", []string{
		"(( grab jobs.count/3 ))",
		`(( concat "v=" jobs.count/3 ))`,
		`(( grab missing.key || jobs.count/3 ))`,
	})
}

// assertUnresolvedPathError evaluates each expression against doc in
// both modes and checks that it fails with a "could not be found" error
// that names path, in both the plain and the detailed text, and that
// neither text quotes the planted value 8675309.
func assertUnresolvedPathError(t *testing.T, doc, path string, exprs []string) {
	t.Helper()
	for _, mode := range slashModes {
		for _, expr := range exprs {
			t.Run(mode.name+"/"+expr, func(t *testing.T) {
				_, err := evalSlashDoc(t, mode.opts, doc+"result: "+expr+"\n")
				if err == nil {
					t.Fatalf("expected an error")
				}
				for label, text := range map[string]string{
					"Error()":       err.Error(),
					"DetailedError": graft.DetailedError(err),
				} {
					if !strings.Contains(text, "could not be found") {
						t.Errorf("%s %q does not say the path could not be found", label, text)
					}
					if !strings.Contains(text, path) {
						t.Errorf("%s %q does not name the path %s", label, text, path)
					}
					if strings.Contains(text, "8675309") {
						t.Errorf("%s %q quotes a document value", label, text)
					}
				}
			})
		}
	}
}

// TestSlashPathInOtherOperators pins the slash rule for operators beyond
// grab and concat, which read their arguments through the same grammar.
func TestSlashPathInOtherOperators(t *testing.T) {
	cases := []struct {
		name string
		expr string
		get  string
		want string
	}{
		{"join", `(( join "," jobs.postgresql/0.ips ))`, "result", "10.0.0.5"},
		{"keys", "(( keys jobs.postgresql/0 ))", "result", "[ips]"},
		// inject replaces its own key with the keys of the map it reads,
		// so the injected ips list lands at the top level.
		{"inject", "(( inject jobs.postgresql/0 ))", "ips", "[10.0.0.5]"},
		{"bare arithmetic beside a slash path", "(( jobs.count/2 + 1 ))", "result", "4"},
	}
	const doc = `
jobs:
  count: 8
  count/2: 3
  postgresql/0:
    ips: [10.0.0.5]
`
	for _, mode := range slashModes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				d, err := evalSlashDoc(t, mode.opts, doc+"result: "+tc.expr+"\n")
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				got, err := d.Get(tc.get)
				if err != nil {
					t.Fatalf("failed to read %s: %v", tc.get, err)
				}
				if fmt.Sprint(got) != tc.want {
					t.Fatalf("result = %v (%T), want %q", got, got, tc.want)
				}
			})
		}
	}
}

// TestSlashSegmentGluesPlusName pins that a '+' followed by a name
// continues a segment after a '/', the same way it does after a '.',
// for genesis vaultified keys such as "haproxy_ssl+certificate". A '+'
// before a digit still adds.
func TestSlashSegmentGluesPlusName(t *testing.T) {
	const doc = `
m:
  x: 10
  x/b: 6
  x/b+c: plus-key
c: 1
`
	cases := []struct {
		name string
		expr string
		want string
	}{
		{"plus name joins the slash segment", "(( grab m.x/b+c ))", "plus-key"},
		{"plus digit after a slash segment adds", "(( m.x/b+1 ))", "7"},
	}
	for _, mode := range slashModes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				d, err := evalSlashDoc(t, mode.opts, doc+"result: "+tc.expr+"\n")
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				got, err := d.Get("result")
				if err != nil {
					t.Fatalf("failed to read result: %v", err)
				}
				if fmt.Sprint(got) != tc.want {
					t.Fatalf("result = %v (%T), want %q", got, got, tc.want)
				}
			})
		}
	}
}

// TestSlashSegmentKeepsDollarName pins that a '$' name after a '/' is
// part of the path, as it is after a '.'. The segment "a/$X" does not
// start with '$', so it is a literal key, as spruce reads it, and the
// environment variable is not substituted into it.
func TestSlashSegmentKeepsDollarName(t *testing.T) {
	t.Setenv("GRAFT_SLASH_VAR", "zzz")
	const doc = `
"a/": [trailing]
"a/$GRAFT_SLASH_VAR": dollar
"a/zzz": substituted
m:
  "x/$GRAFT_SLASH_VAR":
    y: nested-dollar
`
	cases := []struct {
		name string
		expr string
		want string
	}{
		{"dollar name after a slash is one key", "(( grab a/$GRAFT_SLASH_VAR ))", "dollar"},
		{"dollar name after a slash mid-path", "(( grab m.x/$GRAFT_SLASH_VAR.y ))", "nested-dollar"},
	}
	for _, mode := range slashModes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				d, err := evalSlashDoc(t, mode.opts, doc+"result: "+tc.expr+"\n")
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				got, err := d.Get("result")
				if err != nil {
					t.Fatalf("failed to read result: %v", err)
				}
				if fmt.Sprint(got) != tc.want {
					t.Fatalf("result = %v (%T), want %q", got, got, tc.want)
				}
			})
		}
	}
}

// TestSlashDirectlyAfterDot pins that a '/' right after a '.' starts a
// segment whose key begins with '/', as spruce reads it. defer writes a
// bracket index followed by a slash segment in that dotted form, so
// graft must be able to read its own deferred output back.
func TestSlashDirectlyAfterDot(t *testing.T) {
	const doc = `
m:
  "/x": m-slash-x
arr:
  - "/x": slash-x
`
	cases := []struct {
		name string
		expr string
		want string
	}{
		{"slash after a dot", "(( grab m./x ))", "m-slash-x"},
		{"slash after a dotted index", "(( grab arr.0./x ))", "slash-x"},
		{"defer writes the dotted form", "(( defer grab arr[0]/x ))", "(( grab arr.0./x ))"},
	}
	for _, mode := range slashModes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				d, err := evalSlashDoc(t, mode.opts, doc+"result: "+tc.expr+"\n")
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				got, err := d.Get("result")
				if err != nil {
					t.Fatalf("failed to read result: %v", err)
				}
				if fmt.Sprint(got) != tc.want {
					t.Fatalf("result = %v (%T), want %q", got, got, tc.want)
				}
			})
		}

		t.Run(mode.name+"/deferred output reads back", func(t *testing.T) {
			first, err := evalSlashDoc(t, mode.opts, doc+"result: (( defer grab arr[0]/x ))\n")
			if err != nil {
				t.Fatalf("unexpected error on the first pass: %v", err)
			}
			deferred, err := first.GetString("result")
			if err != nil {
				t.Fatalf("failed to read the deferred text: %v", err)
			}
			second, err := evalSlashDoc(t, mode.opts, doc+"result: "+deferred+"\n")
			if err != nil {
				t.Fatalf("unexpected error reading %q back: %v", deferred, err)
			}
			got, err := second.Get("result")
			if err != nil {
				t.Fatalf("failed to read result: %v", err)
			}
			if fmt.Sprint(got) != "slash-x" {
				t.Fatalf("result = %v (%T), want %q", got, got, "slash-x")
			}
		})
	}
}

// TestSlashPathDependencyOrdering pins that the dependency graph orders
// an operator behind the operator its slash path reads. a1 sorts before
// z, and z.pg/0.ips is itself a grab of another grab, so a1 resolves to
// the address only when the graph sees the slash path as one reference.
// The parallel scheduler gets several runs because ordering mistakes
// there show up only some of the time.
func TestSlashPathDependencyOrdering(t *testing.T) {
	const doc = `
a1: (( grab z.pg/0.ips[0] ))
a2: (( concat "ip=" z.pg/0.ips[1] ))
a3: (( grab z.pg/0+meta.name ))
base:
  ips: [10.0.0.7, 10.0.0.8]
  name: pg
meta:
  ips: (( grab base.ips ))
z:
  pg/0:
    ips: (( grab meta.ips ))
  pg/0+meta:
    name: (( grab base.name ))
`
	want := map[string]string{"a1": "10.0.0.7", "a2": "ip=10.0.0.8", "a3": "pg"}
	for _, mode := range slashModes {
		runs := 1
		if mode.name == "parallel" {
			runs = 20
		}
		t.Run(mode.name, func(t *testing.T) {
			for run := 0; run < runs; run++ {
				d, err := evalSlashDoc(t, mode.opts, doc)
				if err != nil {
					t.Fatalf("run %d: unexpected error: %v", run, err)
				}
				for key, w := range want {
					got, err := d.Get(key)
					if err != nil {
						t.Fatalf("run %d: failed to read %s: %v", run, key, err)
					}
					if fmt.Sprint(got) != w {
						t.Fatalf("run %d: %s = %v (%T), want %q", run, key, got, got, w)
					}
				}
			}
		})
	}
}

// cornerDoc holds keys for the leading-slash, exponent, and "$name+name"
// shapes. "a/zzz+y" sits beside "a/$GRAFT_CORNER_VAR+y", so substituting
// the environment variable into the segment would give the wrong value.
const cornerDoc = `
a: 8
b: 2
c: cee
"/b": slash-b
"/b/c": slash-b-c
"/8": slash-eight
"1e+5/2": exp-plus
"1E5/2": exp-upper
"1":
  "5e3/2": exp-frac
"x/1e+5": slash-exp
"a/$GRAFT_CORNER_VAR+y": dollar-plus
"a/zzz+y": substituted
m:
  "x/$GRAFT_CORNER_VAR+y":
    k: nested-dollar-plus
`

func evalCornerResult(t *testing.T, opts []graft.EngineOption, expr string) string {
	t.Helper()
	d, err := evalSlashDoc(t, opts, cornerDoc+"result: "+expr+"\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := d.Get("result")
	if err != nil {
		t.Fatalf("failed to read result: %v", err)
	}
	return fmt.Sprint(got)
}

// TestSlashCornerShapesArePaths pins three shapes that read as one
// path, the way spruce v1.35.17 reads them. A '/' with no left operand
// before it begins a reference. A number in exponent form followed
// directly by a '/' is a path, as "8/2" is. A '+' followed by a name
// continues a "$name" segment after a '/', as it does after a name.
func TestSlashCornerShapesArePaths(t *testing.T) {
	t.Setenv("GRAFT_CORNER_VAR", "zzz")
	paths := []struct {
		path string
		want string
	}{
		{"/b", "slash-b"},
		{"/b/c", "slash-b-c"},
		{"/8", "slash-eight"},
		{"1e+5/2", "exp-plus"},
		{"1E5/2", "exp-upper"},
		{"1.5e3/2", "exp-frac"},
		{"x/1e+5", "slash-exp"},
		{"a/$GRAFT_CORNER_VAR+y", "dollar-plus"},
		{"m.x/$GRAFT_CORNER_VAR+y.k", "nested-dollar-plus"},
	}
	shapes := []struct {
		name   string
		expr   func(path string) string
		result func(want string) string
	}{
		{"grab", func(p string) string { return "(( grab " + p + " ))" }, func(w string) string { return w }},
		// The path comes first, because a string before a leading '/'
		// is a left operand, and the '/' would divide it.
		{"concat", func(p string) string { return "(( concat " + p + ` "=v" ))` }, func(w string) string { return w + "=v" }},
		{"left of ||", func(p string) string { return `(( grab ` + p + ` || "fallback" ))` }, func(w string) string { return w }},
		{"right of ||", func(p string) string { return "(( grab missing.key || " + p + " ))" }, func(w string) string { return w }},
	}

	for _, mode := range slashModes {
		for _, sh := range shapes {
			for _, p := range paths {
				t.Run(mode.name+"/"+sh.name+"/"+p.path, func(t *testing.T) {
					if got := evalCornerResult(t, mode.opts, sh.expr(p.path)); got != sh.result(p.want) {
						t.Fatalf("result = %q, want %q", got, sh.result(p.want))
					}
				})
			}
		}
	}
}

// TestLeadingSlashPositions pins the leading-slash rule in each place
// that has no left operand, and pins that a '/' after an operand still
// divides, with or without whitespace between them.
func TestLeadingSlashPositions(t *testing.T) {
	cases := []struct {
		name string
		expr string
		want string
	}{
		{"first argument of concat", `(( concat /b "-x" ))`, "slash-b-x"},
		{"after a comma", `(( concat "v=", /b ))`, "v=slash-b"},
		{"bare expression left of ||", "(( /b || c ))", "slash-b"},
		{"bare expression right of ||", "(( missing.key || /b ))", "slash-b"},
		{"nested call", `(( concat "v=" (grab /b) ))`, "v=slash-b"},
		{"ternary branch", "'(( true ? /b : c ))'", "slash-b"},
		{"operand with a space before the slash divides", "(( a /b ))", "4"},
		{"spaced slash divides", "(( a / b ))", "4"},
		{"spaced slash between numbers divides", "(( 10 / 4 ))", "2.5"},
		{"grouped operand with a space before the slash divides", `(( concat "v=" (a /b) ))`, "v=4"},
		{"grab operand with a space before the slash divides", "(( grab a /b ))", "4"},
		{"spaced exponent divides", "(( 1e+5 / b ))", "50000"},
	}

	for _, mode := range slashModes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				if got := evalCornerResult(t, mode.opts, tc.expr); got != tc.want {
					t.Fatalf("result = %q, want %q", got, tc.want)
				}
			})
		}
	}
}

// TestLeadingSlashAfterStringDivides pins the documented difference
// from spruce. Spruce reads (( concat "v=" /b )) as two arguments, but
// graft has infix arithmetic, so the string is a left operand and the
// '/' divides. The division takes the whole call as its left side, so
// concat gets one argument and fails, as (( concat a /b )) always has.
func TestLeadingSlashAfterStringDivides(t *testing.T) {
	for _, mode := range slashModes {
		t.Run(mode.name, func(t *testing.T) {
			_, err := evalSlashDoc(t, mode.opts, cornerDoc+`result: (( concat "v=" /b ))`+"\n")
			if err == nil {
				t.Fatalf("expected an error")
			}
			if !strings.Contains(err.Error(), "concat operator requires at least two arguments") {
				t.Fatalf("error %q does not say concat got one argument", err.Error())
			}
		})
	}
}

// TestUnresolvedLeadingSlashPathError pins that a leading-slash path
// that does not exist fails with a "could not be found" error naming
// the whole path, and that neither the plain nor the detailed text
// quotes a value.
func TestUnresolvedLeadingSlashPathError(t *testing.T) {
	const doc = `
"/count": 8675309
`
	assertUnresolvedPathError(t, doc, "/count/3", []string{
		"(( grab /count/3 ))",
		`(( concat /count/3 "=v" ))`,
		`(( grab missing.key || /count/3 ))`,
	})
}
