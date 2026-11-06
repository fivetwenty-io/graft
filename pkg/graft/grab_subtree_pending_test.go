package graft_test

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/pending"
)

// TestGrabSubtreeWaitsForOperatorsUnder checks that a grab whose value
// another operator reads straight away runs after the operators under
// its target, for a static path, a held bracket key, and a computed
// bracket key, so the reader never sees their operator text.
func TestGrabSubtreeWaitsForOperatorsUnder(t *testing.T) {
	runBracketCases(t, []bracketCase{
		{"a static path inside join", `aaa: (( join "," (grab zmeta.l) ))
zmeta:
  l:
  - (( concat "1" "2" ))
  - "3"
`, "aaa", "12,3"},
		{"a held bracket key inside join", `ccc: (( join "," (grab zmeta2[k]) ))
k: t
zmeta2:
  t:
  - (( concat "1" "2" ))
`, "ccc", "12"},
		{"a computed bracket key inside join", `ccc: (( join "," (grab zmeta2[k]) ))
k: (( concat "t" "" ))
zmeta2:
  t:
  - (( concat "1" "2" ))
`, "ccc", "12"},
		{"a grab of a grab inside join", `aaa: (( join "," (grab bbb) ))
bbb: (( grab zmeta.l ))
zmeta:
  l:
  - (( concat "1" "2" ))
  - "3"
`, "aaa", "12,3"},
		{"a static path under a grab of many paths", `aaa: (( grab zmeta.l other ))
other: 4
zmeta:
  l:
  - (( concat "1" "2" ))
  - "3"
`, "aaa", "[12 3 4]"},
		{"a grab whose target holds the operator that waits on it", `out: (( grab meta ))
meta:
  x: 1
  v: (( grab out.x ))
`, "out.v", "1"},
	})
}

// TestGrabSubtreePendingFails checks that a grab whose value another
// operator reads straight away fails, in both modes, when an operator
// under its target has not run and no edge can order the grab after it.
// That holds for a static path, a bracket key, a computed path, and a
// path that reaches the operator through another grab's value.
func TestGrabSubtreePendingFails(t *testing.T) {
	cases := []pendingCase{
		{"a static path whose entry reads the grab", `aaa: (( join "," (grab zmeta.l) ))
zmeta:
  l:
  - (( concat "1" (grab aaa) ))
  - "3"
`, "aaa", "zmeta.l.0"},
		{"a computed path", `aaa: (( join "," (grab (concat "zmeta." k)) ))
k: l
zmeta:
  l:
  - (( concat "1" "2" ))
  - "3"
`, "aaa", "zmeta.l.0"},
		{"an entry reached through another grab", `x: (( join "," (grab aaa) ))
aaa: (( grab zmeta.l ))
zmeta:
  l:
  - (( concat "1" (grab aaa.1) ))
  - "2"
`, "x", "zmeta.l.0"},
		{"a list that holds the reader itself", `meta:
  l:
  - 1
  - (( join "," (grab meta.l) ))
`, "meta.l.1", "meta.l.1"},
		{"a bracket path that reaches the grab itself", `k: (( concat "t" "" ))
meta:
  t: (( grab out ))
out:
  v: (( grab meta[k].v ))
`, "out.v", "out.v"},
	}
	for _, mode := range leakModes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				got, err := mergeBracketDoc(t, mode.parallel, tc.doc)
				if err == nil {
					v, _ := got.Get(tc.grab)
					t.Fatalf("Execute succeeded, %s is %q", tc.grab, fmt.Sprint(v))
				}
				var nerr *pending.NotEvaluatedError
				if !errors.As(err, &nerr) {
					t.Fatalf("error is not a *pending.NotEvaluatedError: %v", err)
				}
				if nerr.Grab != tc.grab || nerr.Target != tc.target {
					t.Fatalf("error names grab %q and target %q, want %q and %q", nerr.Grab, nerr.Target, tc.grab, tc.target)
				}
				if msg := err.Error(); strings.Contains(msg, "concat") || strings.Contains(msg, "join") {
					t.Fatalf("error %q quotes operator text", msg)
				}
			})
		}
	}
}

// TestGrabOfOwnAncestorKeepsBehavior checks that a grab of a map that
// holds the grab itself still gives the map, which then holds itself, as
// it did before graft ordered grabs after the operators under their
// targets. Neither the soft edges nor the pending check turn it into an
// error.
func TestGrabOfOwnAncestorKeepsBehavior(t *testing.T) {
	docs := map[string]string{
		"alone": `meta:
  a: (( grab meta ))
`,
		"beside another operator": `meta:
  a: (( grab meta ))
  b: (( concat "x" "y" ))
`,
	}
	for _, mode := range leakModes {
		for name, doc := range docs {
			t.Run(mode.name+"/"+name, func(t *testing.T) {
				got, err := mergeBracketDoc(t, mode.parallel, doc)
				if err != nil {
					t.Fatalf("Execute: %v", err)
				}
				v, err := got.Get("meta")
				if err != nil {
					t.Fatal(err)
				}
				meta, ok := v.(map[string]interface{})
				if !ok {
					t.Fatalf("meta is %T, want a map", v)
				}
				inner, ok := meta["a"].(map[string]interface{})
				if !ok || reflect.ValueOf(inner).Pointer() != reflect.ValueOf(meta).Pointer() {
					t.Fatalf("meta.a is not meta itself")
				}
				if b, ok := meta["b"]; ok && b != "xy" {
					t.Fatalf("meta.b is %v, want xy", b)
				}
			})
		}
	}
}
