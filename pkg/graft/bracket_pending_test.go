package graft_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/pending"
	"github.com/fivetwenty-io/graft/pkg/graft"
)

// pendingCase is a document in which a grab reads a path whose operator
// has not run when the grab runs, and no dependency edge can order them.
type pendingCase struct {
	name   string
	doc    string
	grab   string
	target string
}

// pendingCases are the documents in which a grab would otherwise return
// the operator text it finds at its target.
var pendingCases = []pendingCase{
	{"a sibling grab sorted later", `k: (( concat "x" "" ))
k2: (( concat "p" "2" ))
meta:
  x: (( concat "v" "1" ))
  p1: (( grab meta[k2] ))
  p2: (( grab meta[k] ))
`, "meta.p1", "meta.p2"},
	{"an operator that reads the grab", `k: (( concat "a" "" ))
meta:
  a: (( concat "q" (grab meta.pick) ))
  pick: (( grab meta[k] ))
`, "meta.pick", "meta.a"},
	{"a fallback is not taken", `k: (( concat "x" "" ))
k2: (( concat "p" "2" ))
meta:
  x: (( concat "v" "1" ))
  p1: (( grab meta[k2] || "fallback" ))
  p2: (( grab meta[k] ))
`, "meta.p1", "meta.p2"},
	{"inside concat", `k: (( concat "x" "" ))
k2: (( concat "p" "2" ))
meta:
  x: (( concat "v" "1" ))
  p1: (( concat "got=" (grab meta[k2]) ))
  p2: (( grab meta[k] ))
`, "meta.p1", "meta.p2"},
	{"a list entry flattened into a grab of two paths", `aa: (( grab b.list c ))
b:
  list:
  - (( concat "a" (grab aa.2) ))
  - 2
c: 3
`, "aa", "b.list.0"},
}

// TestGrabPendingTargetFails checks that a grab whose target still holds
// an operator that has not run fails, in both modes, with an error that
// names the grab and the target, quotes no value, and comes through the
// operator's own path error.
func TestGrabPendingTargetFails(t *testing.T) {
	for _, mode := range leakModes {
		for _, tc := range pendingCases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				got, err := mergeBracketDoc(t, mode.parallel, tc.doc)
				if err == nil {
					v, _ := got.Get(tc.grab)
					t.Fatalf("Execute succeeded, %s is %q", tc.grab, fmt.Sprint(v))
				}
				if !errors.Is(err, pending.ErrNotEvaluated) {
					t.Fatalf("error does not match pending.ErrNotEvaluated: %v", err)
				}
				var nerr *pending.NotEvaluatedError
				if !errors.As(err, &nerr) {
					t.Fatalf("error is not a *pending.NotEvaluatedError: %v", err)
				}
				if nerr.Grab != tc.grab || nerr.Target != tc.target {
					t.Fatalf("error names grab %q and target %q, want %q and %q", nerr.Grab, nerr.Target, tc.grab, tc.target)
				}
				var perr *graft.PathError
				if !errors.As(err, &perr) || perr.Path != tc.grab {
					t.Fatalf("error does not come through the path error for %s: %v", tc.grab, err)
				}
				msg := err.Error()
				for _, want := range []string{"$." + tc.grab, "$." + tc.target} {
					if !strings.Contains(msg, want) {
						t.Errorf("error %q does not name %s", msg, want)
					}
				}
				// The error names the operator it comes from, so it holds
				// "(( grab ))", but no operator text found at either path
				// and no value.
				for _, value := range []string{"concat", "(( grab meta", "(grab", "meta[k", "v1", "fallback", `"ab"`} {
					if strings.Contains(msg, value) {
						t.Errorf("error %q quotes %q", msg, value)
					}
				}
			})
		}
	}
}

// TestGrabOperatorTextAsData checks that text which looks like an
// operator but is data once evaluation reaches it is grabbed like any
// other value.
func TestGrabOperatorTextAsData(t *testing.T) {
	runBracketCases(t, []bracketCase{
		{"the output of defer", `d: (( defer grab y ))
y: 1
z: (( grab d ))
`, "z", "(( grab y ))"},
		{"an unspaced placeholder", `a: 4
b: 2
p: (( a/b ))
z: (( grab p ))
`, "z", "(( a/b ))"},
		{"a quoted string", `q: 'say "((" here'
z: (( grab q ))
`, "z", `say "((" here`},
		{"the output of concat", `r: (( concat "((" " grab y ))" ))
y: 1
zr: (( grab r ))
`, "zr", "(( grab y ))"},
		{"a map whose operator runs later", `a: (( grab meta ))
meta:
  x: (( concat "a" "b" ))
`, "a.x", "ab"},
	})

	for _, mode := range leakModes {
		t.Run(mode.name+"/a literal", func(t *testing.T) {
			opts := []graft.EngineOption{}
			if mode.parallel {
				opts = append(opts, graft.WithParallel(true))
			}
			eng, err := graft.NewEngine(opts...)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := eng.ParseYAML([]byte("z: (( grab params.code ))\n"))
			if err != nil {
				t.Fatal(err)
			}
			user := graft.NewDocument(map[string]interface{}{
				"params": graft.NewLiteral(map[string]interface{}{"code": `(( concat "a" "b" ))`}),
			})
			got, err := eng.Merge(context.Background(), plan, user).Execute()
			if err != nil {
				t.Fatalf("Execute: %v", err)
			}
			v, err := got.Get("z")
			if err != nil {
				t.Fatal(err)
			}
			if s := fmt.Sprint(v); s != `(( concat "a" "b" ))` {
				t.Fatalf("z is %q, want the literal's text", s)
			}
		})
	}
}
