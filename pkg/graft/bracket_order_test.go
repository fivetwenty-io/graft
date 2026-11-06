package graft_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/fivetwenty-io/graft/pkg/graft"
	_ "github.com/fivetwenty-io/graft/pkg/graft/operators"
)

// bracketCase is one document and the value one of its paths must hold
// after a merge.
type bracketCase struct {
	name string
	doc  string
	path string
	want string
}

// mergeBracketDoc merges doc on its own with the sequential or the
// parallel evaluator and returns the result.
func mergeBracketDoc(t *testing.T, parallel bool, doc string) (graft.Document, error) {
	t.Helper()
	opts := []graft.EngineOption{}
	if parallel {
		opts = append(opts, graft.WithParallel(true))
	}
	eng, err := graft.NewEngine(opts...)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := eng.ParseYAML([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	return eng.Merge(context.Background(), parsed).Execute()
}

// runBracketCases merges every case in both evaluator modes and checks
// the value at its path.
func runBracketCases(t *testing.T, cases []bracketCase) {
	t.Helper()
	for _, mode := range leakModes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				got, err := mergeBracketDoc(t, mode.parallel, tc.doc)
				if err != nil {
					t.Fatalf("Execute: %v", err)
				}
				v, err := got.Get(tc.path)
				if err != nil {
					t.Fatal(err)
				}
				if s := fmt.Sprint(v); s != tc.want {
					t.Fatalf("%s is %q, want %q", tc.path, s, tc.want)
				}
			})
		}
	}
}

// TestGrabBracketWaitsForKeyAndTarget checks that a (( grab )) with a
// bracket key runs after the operator that computes the key, and, when
// the key already holds its value, after the operator that computes the
// value the path reaches. Each key or target sorts after the grab, so
// only a dependency edge can order them.
func TestGrabBracketWaitsForKeyAndTarget(t *testing.T) {
	runBracketCases(t, []bracketCase{
		{"computed key", `aa: (( grab meta[z] ))
meta:
  ab: 1
z: (( concat "a" "b" ))
`, "aa", "1"},
		{"computed key from the root", `aa: (( grab $.meta[z] ))
meta:
  ab: 1
z: (( concat "a" "b" ))
`, "aa", "1"},
		{"computed key at the root", `aa: (( grab $[z] ))
ab: 1
z: (( concat "a" "b" ))
`, "aa", "1"},
		{"computed target", `aa: (( grab tgt[k] ))
k: x
tgt:
  x: (( concat "computed" "-value" ))
`, "aa", "computed-value"},
		{"computed target sorted first", `k: x
tgt:
  x: (( concat "computed" "-value" ))
zz: (( grab tgt[k] ))
`, "zz", "computed-value"},
		{"computed target past the key", `aa: (( grab tgt[k].v ))
k: x
tgt:
  x:
    v: (( concat "deep" "-value" ))
`, "aa", "deep-value"},
		{"two keys", `aa: (( grab tgt[k][j] ))
j: w
k: x
tgt:
  x:
    w: (( concat "two" "-keys" ))
`, "aa", "two-keys"},
		{"key under the same parent", `meta:
  a: (( grab meta[k] ))
  b: 2
k: b
`, "meta.a", "2"},
		{"computed key under the same parent", `meta:
  a: (( grab meta[k] ))
  b: 2
k: (( concat "b" "" ))
`, "meta.a", "2"},
		{"a sibling reads the grab", `meta:
  a: (( grab meta[k] ))
  b: 2
  c: (( grab meta.a ))
k: b
`, "meta.c", "2"},
		{"a fallback with a bracket", `aa: (( grab nope[z] || tgt[k] ))
k: x
tgt:
  x: (( concat "or" "-value" ))
z: (( concat "q" "" ))
`, "aa", "or-value"},
		{"inside concat", `aa: (( concat "v=" (grab tgt[k]) ))
k: x
tgt:
  x: (( concat "inner" "" ))
`, "aa", "v=inner"},
		{"list index from a computed key", `idx: (( calc "0 + 1" ))
items:
- (( grab items[idx] ))
- two
`, "items.0", "two"},
	})
}

// TestGrabBracketDependencies checks the hard dependency edges a bracket
// grab reports: its key's path, and the operator its path reaches once
// every key already holds its value.
func TestGrabBracketDependencies(t *testing.T) {
	cases := []struct {
		name string
		doc  string
		path string
		want []string
	}{
		{"held key", `aa: (( grab tgt[k] ))
k: x
tgt:
  x: (( concat "a" "" ))
  y: (( concat "b" "" ))
`, "aa", []string{"tgt.x"}},
		{"computed key", `aa: (( grab meta[z] ))
meta:
  ab: 1
z: (( concat "a" "b" ))
`, "aa", []string{"z"}},
		{"held key past an operator", `aa: (( grab tgt[k].v ))
k: x
tgt:
  x: (( grab src ))
src:
  v: 1
`, "aa", []string{"tgt.x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := bracketGraph(t, tc.doc).Dependencies(tc.path)
			if strings.Join(got, ",") != strings.Join(tc.want, ",") {
				t.Fatalf("%s depends on %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

// bracketGraph builds the EvalPhase dependency graph of doc.
func bracketGraph(t *testing.T, doc string) *graft.DependencyGraph {
	t.Helper()
	eng, err := graft.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := eng.ParseYAML([]byte(doc))
	if err != nil {
		t.Fatal(err)
	}
	de, ok := eng.(*graft.DefaultEngine)
	if !ok {
		t.Fatal("not a *DefaultEngine")
	}
	g, err := de.BuildDependencyGraph(parsed, graft.EvalPhase)
	if err != nil {
		t.Fatal(err)
	}
	return g
}

// TestGrabBracketWaitsForComputedTarget checks that a bracket grab whose
// key an operator computes still runs after the operator that computes
// the value its path reaches. The target is unknown until the key runs,
// so the grab waits on the operators under the path before the key.
func TestGrabBracketWaitsForComputedTarget(t *testing.T) {
	runBracketCases(t, []bracketCase{
		{"computed key and a target that waits", `aa: (( grab tgt[z] ))
src:
  v: (( concat "chained" "-value" ))
tgt:
  x: (( grab src.v ))
z: (( concat "x" "" ))
`, "aa", "chained-value"},
		{"computed key at the root and a target that waits", `aa: (( grab $[k] ))
k: (( concat "t" "gt" ))
src: (( concat "root" "-value" ))
tgt: (( grab src ))
`, "aa", "root-value"},
		{"computed key and target", `aa: (( grab tgt[z] ))
tgt:
  x: (( concat "computed" "-value" ))
z: (( concat "x" "" ))
`, "aa", "computed-value"},
		{"computed key and target past the key", `k: (( concat "x" "" ))
a:
  b:
    c: (( grab a.b[k].v ))
    x:
      v: (( concat "v" "1" ))
`, "a.b.c", "v1"},
		{"computed key and target at the root", `aa: (( grab $[k] ))
k: (( concat "t" "gt" ))
tgt: (( concat "computed" "-value" ))
`, "aa", "computed-value"},
		{"computed key at the root over data", `aa: (( grab $[w] ))
ab: 7
w: (( concat "a" "b" ))
`, "aa", "7"},
		{"a sibling reads the grab", `key: (( concat "a" "" ))
meta:
  a: 1
  pick1: (( grab meta[key] ))
  use: (( concat "got=" meta.pick1 ))
`, "meta.use", "got=1"},
		{"key computed under the same parent", `meta:
  k: (( concat "a" "b" ))
  ab: 1
  pick: (( grab meta[meta.k] ))
`, "meta.pick", "1"},
		{"a sibling grab sorted first", `k: (( concat "x" "" ))
k2: (( concat "p" "1" ))
meta:
  x: (( concat "v" "1" ))
  p1: (( grab meta[k] ))
  p2: (( grab meta[k2] ))
`, "meta.p2", "v1"},
	})
}

// TestGrabBracketSiblingsNeverCycle checks that grabs under one parent,
// each waiting on the others because each key is computed, still
// evaluate as they did before those waits were added. A wait that would
// close a cycle is dropped.
func TestGrabBracketSiblingsNeverCycle(t *testing.T) {
	cases := []bracketCase{
		{"two siblings with one key", `key: (( concat "a" "" ))
meta:
  a: 1
  pick1: (( grab meta[key] ))
  pick2: (( grab meta[key] ))
`, "meta.pick2", "1"},
		{"list entries with one index", `idx: (( calc "0 + 1" ))
items:
- (( grab items[idx] ))
- two
- (( grab items[idx] ))
`, "items.2", "two"},
		{"a fallback beside a plain grab", `sel: (( concat "a" "" ))
cfg:
  a: 1
out: (( grab cfg[sel] ))
other2: (( grab cfg[sel] || "n" ))
`, "other2", "1"},
	}

	var mesh strings.Builder
	mesh.WriteString("meta:\n")
	for i := 0; i < 10; i++ {
		fmt.Fprintf(&mesh, "  d%d: (( concat \"value-\" \"%d\" ))\n", i, i)
	}
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&mesh, "  g%02d: (( grab meta[k%02d] ))\n", i, i)
		fmt.Fprintf(&mesh, "k%02d: (( concat \"d\" \"%d\" ))\n", i, i%10)
	}
	cases = append(cases, bracketCase{"a mesh of siblings", meshDoc(mesh.String()), "meta.g13", "value-3"})

	var chain strings.Builder
	chain.WriteString("meta:\n  d: (( concat \"chain\" \"-end\" ))\n  g00: (( grab meta[k00] ))\nk00: (( concat \"d\" \"\" ))\n")
	for i := 1; i < 15; i++ {
		fmt.Fprintf(&chain, "meta:\n  g%02d: (( grab meta[k%02d] ))\n", i, i)
		fmt.Fprintf(&chain, "k%02d: (( concat \"g\" \"%02d\" ))\n", i, i-1)
	}
	cases = append(cases, bracketCase{"a chain of siblings", meshDoc(chain.String()), "meta.g14", "chain-end"})

	runBracketCases(t, cases)
}

// meshDoc folds the repeated top-level "meta:" headers a generated
// document writes into one map, so each line can name its parent.
func meshDoc(doc string) string {
	var meta, top strings.Builder
	meta.WriteString("meta:\n")
	for _, line := range strings.Split(strings.TrimSuffix(doc, "\n"), "\n") {
		switch {
		case line == "meta:":
		case strings.HasPrefix(line, "  "):
			meta.WriteString(line + "\n")
		default:
			top.WriteString(line + "\n")
		}
	}
	return meta.String() + top.String()
}

// TestGrabBracketSoftDependencies checks the edges a bracket grab with a
// computed key reports in the dependency graph: its key, and each other
// operator under the path before the key, unless that operator already
// waits on the grab.
func TestGrabBracketSoftDependencies(t *testing.T) {
	g := bracketGraph(t, `aa: (( grab tgt[z] ))
tgt:
  x: (( concat "computed" "-value" ))
  y: plain
z: (( concat "x" "" ))
`)
	if got := strings.Join(g.Dependencies("aa"), ","); got != "tgt.x,z" {
		t.Fatalf("aa depends on %s, want tgt.x,z", got)
	}

	g = bracketGraph(t, `key: (( concat "a" "" ))
meta:
  a: 1
  pick1: (( grab meta[key] ))
  pick2: (( grab meta[key] ))
`)
	if got := strings.Join(g.Dependencies("meta.pick1"), ","); got != "key" {
		t.Fatalf("meta.pick1 depends on %s, want key", got)
	}
	if got := strings.Join(g.Dependencies("meta.pick2"), ","); got != "key,meta.pick1" {
		t.Fatalf("meta.pick2 depends on %s, want key,meta.pick1", got)
	}
	if cycles := g.DetectCycles(); len(cycles) != 0 {
		t.Fatalf("graph has cycles %v", cycles)
	}
}

// largeDocLimit is the time a large document may take to evaluate. The
// race detector slows evaluation several times over, so the bound under
// it only catches a return to quadratic behavior.
func largeDocLimit() time.Duration {
	if raceEnabled {
		return 20 * time.Second
	}
	return time.Second
}

// timeBracketDoc merges doc in each mode, checks that path holds want,
// and fails when the merge takes longer than largeDocLimit.
func timeBracketDoc(t *testing.T, doc, path string, want interface{}) {
	t.Helper()
	limit := largeDocLimit()
	for _, mode := range leakModes {
		t.Run(mode.name, func(t *testing.T) {
			start := time.Now()
			got, err := mergeBracketDoc(t, mode.parallel, doc)
			elapsed := time.Since(start)
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			v, err := got.Get(path)
			if err != nil {
				t.Fatal(err)
			}
			if fmt.Sprint(v) != fmt.Sprint(want) {
				t.Fatalf("%s is %v, want %v", path, v, want)
			}
			t.Logf("evaluation took %v", elapsed)
			if elapsed > limit {
				t.Fatalf("evaluation took %v, want under %v", elapsed, limit)
			}
		})
	}
}

// TestGrabBracketLargeDocument checks that a document with many bracket
// grabs at the root, each of which waits on every other operator in the
// document, still evaluates quickly in both modes.
func TestGrabBracketLargeDocument(t *testing.T) {
	var doc strings.Builder
	doc.WriteString("k: (( concat \"v\" \"0\" ))\n")
	for i := 0; i < 1500; i++ {
		fmt.Fprintf(&doc, "v%d: (( concat \"x\" \"%d\" ))\n", i, i)
	}
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&doc, "g%d: (( grab $[k] ))\n", i)
	}
	timeBracketDoc(t, doc.String(), "g39", "x0")
}

// TestGrabBracketManyComputedKeys checks that a document with 1600
// computed-key grabs, 600 of them a mesh of siblings that each wait on
// every other operator in their map, still evaluates quickly in both
// modes.
func TestGrabBracketManyComputedKeys(t *testing.T) {
	var doc strings.Builder
	doc.WriteString("k: (( concat \"x\" \"\" ))\nsrc:\n  x: 1\n")
	for i := 0; i < 1000; i++ {
		fmt.Fprintf(&doc, "g%d: (( grab src[k] ))\n", i)
	}
	doc.WriteString("meta:\n")
	for i := 0; i < 600; i++ {
		fmt.Fprintf(&doc, "  k%d: (( concat \"v\" \"%d\" ))\n  v%d: %d\n  m%d: (( grab meta[meta.k%d] ))\n", i, i, i, i, i, i)
	}
	timeBracketDoc(t, doc.String(), "meta.m599", 599)
}

// TestGrabBracketComputedKeyOverManyOperators checks that 1600 grabs
// with a computed key, over a map that holds 1600 operators, evaluate
// quickly. Each grab waits on every operator in the map, so the graph
// must not hold one edge for each grab and operator pair.
func TestGrabBracketComputedKeyOverManyOperators(t *testing.T) {
	var doc strings.Builder
	doc.WriteString("src:\n")
	for i := 0; i < 1600; i++ {
		fmt.Fprintf(&doc, "  x%d: (( concat \"a\" \"%d\" ))\n", i, i)
	}
	doc.WriteString("kk: (( concat \"x\" \"5\" ))\n")
	for i := 0; i < 1600; i++ {
		fmt.Fprintf(&doc, "g%d: (( grab src[kk] ))\n", i)
	}
	timeBracketDoc(t, doc.String(), "g1599", "a5")
}
