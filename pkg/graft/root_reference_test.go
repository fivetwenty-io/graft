package graft_test

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// rootDoc is the document the $.-rooted reference tests read from.
const rootDoc = `
key_name: meta
meta:
  a: hello
  b: [x, z]
  m:
    k1: v1
list: [one, two]
jobs:
  postgresql/0:
    ips: [10.0.0.5]
"/x": slash-x
`

// TestRootReferenceReadsFromTheDocumentRoot pins that a reference
// written with a leading "$." names a path from the document root, as
// spruce reads it, so every shape below gives the same result as the
// path without the "$.".
func TestRootReferenceReadsFromTheDocumentRoot(t *testing.T) {
	t.Setenv("GRAFT_ROOT_TEST_KEY", "a")
	t.Setenv("GRAFT_ROOT_TEST_TOP", "meta")

	cases := []struct {
		name string
		expr string
		get  string
		want string
	}{
		{"grab", "(( grab $.meta.a ))", "result", "hello"},
		{"grab a single segment", "(( grab $.list ))", "result", "[one two]"},
		{"grab several arguments", "(( grab $.meta.b $.list ))", "result", "[x z one two]"},
		{"bracket index", "(( grab $.list[1] ))", "result", "two"},
		{"dotted index", "(( grab $.list.0 ))", "result", "one"},
		{"slash path", "(( grab $.jobs.postgresql/0.ips[0] ))", "result", "10.0.0.5"},
		{"slash key at the root", "(( grab $./x ))", "result", "slash-x"},
		{"left of ||", `(( grab $.meta.a || "fallback" ))`, "result", "hello"},
		{"right of ||", "(( grab meta.missing || $.meta.a ))", "result", "hello"},
		{"both sides of ||", "(( grab $.meta.missing || $.meta.a ))", "result", "hello"},
		{"concat", `(( concat $.meta.a "-x" ))`, "result", "hello-x"},
		{"nested call", `(( concat "v=" (grab $.meta.a) ))`, "result", "v=hello"},
		{"join", `(( join "," $.meta.b ))`, "result", "x,z"},
		{"keys", "(( keys $.meta.m ))", "result", "[k1]"},
		// inject replaces its own key with the keys of the map it reads,
		// so the injected k1 lands at the top level.
		{"inject", "(( inject $.meta.m ))", "k1", "v1"},
		{"ternary branch", `'(( true ? $.meta.a : "no" ))'`, "result", "hello"},
		{"comparison", `(( $.meta.a == "hello" ))`, "result", "true"},
		{"bare arithmetic", "(( $.meta.b.0 == meta.b.0 ))", "result", "true"},
		{"environment variable segment", "(( grab $.meta.$GRAFT_ROOT_TEST_KEY ))", "result", "hello"},
		{"environment variable first segment", "(( grab $.$GRAFT_ROOT_TEST_TOP.a ))", "result", "hello"},
		{"dynamic bracket at the root", "(( grab $[key_name].a ))", "result", "hello"},
		{"bare reference stays in the output", "(( $.meta.a ))", "result", "(( $.meta.a ))"},
		{"defer writes the path without the root", "(( defer grab $.meta.a ))", "result", "(( grab meta.a ))"},
	}

	for _, mode := range slashModes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				d, err := evalSlashDoc(t, mode.opts, rootDoc+"result: "+tc.expr+"\n")
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				got, err := d.Get(tc.get)
				if err != nil {
					t.Fatalf("failed to read %s: %v", tc.get, err)
				}
				if fmt.Sprint(got) != tc.want {
					t.Fatalf("%s = %v (%T), want %q", tc.get, got, got, tc.want)
				}
			})
		}
	}
}

// TestRootReferenceErrors pins the "$"-rooted shapes that fail. A "$"
// or a "$." with no path after it names the whole document, which
// spruce reports as a self-referencing dataset, so graft refuses it
// with a parse error. A root index that is not in the document fails
// the way spruce fails it.
func TestRootReferenceErrors(t *testing.T) {
	cases := []struct {
		name    string
		expr    string
		wantErr string
	}{
		{"dollar dot alone", "(( grab $. ))", "needs a path after it"},
		{"dollar dot before a space", "(( grab $. meta.a ))", "needs a path after it"},
		{"dollar dot inside ||", "(( grab meta.missing || $. ))", "needs a path after it"},
		{"dollar alone", "(( grab $ ))", "invalid environment variable syntax"},
		{"index at the root after a dot", "(( grab $.[0] ))", "`$.0` could not be found"},
		{"index at the root", "(( grab $[0] ))", "`$.0` could not be found"},
		{"missing key", "(( grab $.meta.missing ))", "`$.meta.missing` could not be found"},
	}
	for _, mode := range slashModes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				_, err := evalSlashDoc(t, mode.opts, rootDoc+"result: "+tc.expr+"\n")
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

// TestRootReferenceOrdersAfterTheOperatorItReads pins that a "$." path
// that points at an operator, or at a map that holds one, depends on
// that operator, so the operator runs first in both evaluator modes.
// Every reader sorts before the operator it reads, so evaluating in key
// order without the dependency would read the unevaluated operator text.
func TestRootReferenceOrdersAfterTheOperatorItReads(t *testing.T) {
	const doc = `
a_grab: (( grab $.z_source.value ))
b_concat: (( concat $.z_source.value "-x" ))
c_subtree: (( grab $.z_source ))
d_chain: (( grab $.a_grab ))
meta:
  base: hi
z_source:
  value: (( grab meta.base ))
`
	want := map[string]string{
		"a_grab":    "hi",
		"b_concat":  "hi-x",
		"c_subtree": "map[value:hi]",
		"d_chain":   "hi",
	}
	for _, mode := range slashModes {
		t.Run(mode.name, func(t *testing.T) {
			d, err := evalSlashDoc(t, mode.opts, doc)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for key, w := range want {
				got, err := d.Get(key)
				if err != nil {
					t.Fatalf("failed to read %s: %v", key, err)
				}
				if fmt.Sprint(got) != w {
					t.Errorf("%s = %v, want %q", key, got, w)
				}
			}
		})
	}
}

// TestRootReferenceHasTheSameDependencies pins that a "$." path and the
// same path without the "$." give the dependency scan the same cursors,
// which is what both evaluator modes order operators by.
func TestRootReferenceHasTheSameDependencies(t *testing.T) {
	pairs := [][2]string{
		{"(( grab $.z.value ))", "(( grab z.value ))"},
		{`(( concat $.z.value "x" ))`, `(( concat z.value "x" ))`},
		{"(( grab $.z.missing || $.z.value ))", "(( grab z.missing || z.value ))"},
		{"(( grab $.jobs.pg/0.ips[0] ))", "(( grab jobs.pg/0.ips[0] ))"},
		{"(( keys $.z ))", "(( keys z ))"},
		{"(( $.z.n + 1 ))", "(( z.n + 1 ))"},
	}
	ev := &graft.Evaluator{Tree: map[string]interface{}{}}
	deps := func(t *testing.T, src string) string {
		t.Helper()
		op, err := graft.ParseOpcallWithParser(graft.EvalPhase, src)
		if err != nil || op == nil {
			t.Fatalf("parse %s: op=%v err=%v", src, op, err)
		}
		var out []string
		for _, c := range op.Dependencies(ev, []*tree.Cursor{}) {
			out = append(out, c.String())
		}
		sort.Strings(out)
		return strings.Join(out, " ")
	}
	for _, p := range pairs {
		t.Run(p[0], func(t *testing.T) {
			rooted, plain := deps(t, p[0]), deps(t, p[1])
			if rooted != plain {
				t.Fatalf("dependencies differ:\n %s -> %q\n %s -> %q", p[0], rooted, p[1], plain)
			}
			if plain == "" {
				t.Fatalf("no dependencies found for %s", p[1])
			}
		})
	}
}

// TestRootReferenceInTheDependencyGraph pins that the dataflow graph
// gives a "$." reader the same edges as a reader of the plain path, so
// the dataflow output and the parallel evaluator's waves agree with the
// sequential order.
func TestRootReferenceInTheDependencyGraph(t *testing.T) {
	const doc = `
rooted: (( grab $.z_source.value ))
rooted_subtree: (( concat $.z_source.value $[key_name].value ))
plain: (( grab z_source.value ))
key_name: z_source
meta:
  base: hi
z_source:
  value: (( grab meta.base ))
`
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	de, ok := engine.(*graft.DefaultEngine)
	if !ok {
		t.Fatalf("engine is %T, not *graft.DefaultEngine", engine)
	}
	parsed, err := engine.ParseYAML([]byte(doc))
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}
	g, err := de.BuildDependencyGraph(parsed, graft.EvalPhase)
	if err != nil {
		t.Fatalf("BuildDependencyGraph: %v", err)
	}
	want := fmt.Sprint(g.Dependencies("plain"))
	if want != "[z_source.value]" {
		t.Fatalf("plain depends on %s, want [z_source.value]", want)
	}
	for _, path := range []string{"rooted", "rooted_subtree"} {
		if got := fmt.Sprint(g.Dependencies(path)); got != want {
			t.Errorf("%s depends on %s, want %s", path, got, want)
		}
	}
}
