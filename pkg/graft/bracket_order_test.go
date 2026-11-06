package graft_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

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
