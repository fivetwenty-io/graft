package graft_test

import (
	"errors"
	"fmt"
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
				msg := err.Error()
				if strings.Contains(msg, "concat") || strings.Contains(msg, "join") {
					t.Fatalf("error %q quotes operator text", msg)
				}
				if n := strings.Count(msg, "$."+tc.grab+":"); n != 1 || strings.Contains(msg, "at $.") {
					t.Fatalf("error %q names the grab %d times, want once", msg, n)
				}
			})
		}
	}
}

// TestGrabOfOwnAncestorFailsAsACycle checks that a grab of a map or
// list that holds the grab itself fails in both modes with an error that
// names the grab and its target, whether the grab is written to the tree
// or nested in another call. The target would otherwise end up holding
// itself, or hand on the text of the call that reads it.
func TestGrabOfOwnAncestorFailsAsACycle(t *testing.T) {
	cases := []struct {
		name, doc, grab, target string
	}{
		{"alone", `meta:
  a: (( grab meta ))
`, "meta.a", "meta"},
		{"beside another operator", `meta:
  a: (( grab meta ))
  b: (( concat "x" "y" ))
`, "meta.a", "meta"},
		{"nested in another call", `meta:
  b: (( concat "x" "y" ))
  a: (( join "," (grab meta) ))
`, "meta.a", "meta"},
		{"a list that holds the reader itself", `meta:
  l:
  - 1
  - (( join "," (grab meta.l) ))
`, "meta.l.1", "meta.l"},
	}
	for _, mode := range leakModes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				_, err := mergeBracketDoc(t, mode.parallel, tc.doc)
				if err == nil {
					t.Fatal("Execute succeeded, want a cycle error")
				}
				msg := err.Error()
				want := "cycle detected: $." + tc.grab + " grabs $." + tc.target + ", which contains it"
				if !strings.Contains(msg, want) {
					t.Fatalf("error %q does not say %q", msg, want)
				}
				if strings.Contains(msg, "concat") || strings.Contains(msg, "join") || strings.Contains(msg, "xy") {
					t.Fatalf("error %q quotes input", msg)
				}
			})
		}
	}
}
