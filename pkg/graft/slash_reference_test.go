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
// spaced forms, which divide, and the leading slash, which stays an
// operator token and so fails to parse.
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
// as spruce does, and a leading slash is still an operator token.
func TestSlashEdgeCaseErrors(t *testing.T) {
	const doc = `
arr: [x0, x1]
"/b": slash-b
`
	cases := []struct {
		name    string
		expr    string
		wantErr string
	}{
		{"slash before a bracket", "(( grab arr/[0] ))", "`$.arr/` could not be found"},
		{"leading slash", "(( grab /b ))", "no arguments specified"},
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
	for _, mode := range slashModes {
		for _, expr := range []string{
			"(( grab jobs.count/3 ))",
			`(( concat "v=" jobs.count/3 ))`,
			`(( grab missing.key || jobs.count/3 ))`,
		} {
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
					if !strings.Contains(text, "jobs.count/3") {
						t.Errorf("%s %q does not name the path jobs.count/3", label, text)
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
