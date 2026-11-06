package graft_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/pending"
	"github.com/fivetwenty-io/graft/pkg/graft"
)

// computedPathDoc grabs a path that concat computes, with no bracket
// key, so the dependency graph cannot know the target before the grab
// runs and has no edge to order the grab after meta.t.
const computedPathDoc = `k: t
meta:
  t: (( concat "a" "b" ))
out: (( grab (concat "meta." k) ))
`

// TestGrabComputedPathSequential checks that the sequential evaluator
// resolves a grab of a computed path whose target sorts before it,
// because it runs the operators of a phase one at a time in sorted
// order and meta.t has run by the time out does.
func TestGrabComputedPathSequential(t *testing.T) {
	got, err := mergeBracketDoc(t, false, computedPathDoc)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	v, err := got.Get("out")
	if err != nil {
		t.Fatal(err)
	}
	if v != "ab" {
		t.Fatalf("out is %v, want ab", v)
	}
}

// TestGrabComputedPathParallel checks that the parallel evaluator, which
// runs meta.t and out in the same wave, fails the grab with a
// NotEvaluatedError rather than returning the text of meta.t.
func TestGrabComputedPathParallel(t *testing.T) {
	_, err := mergeBracketDoc(t, true, computedPathDoc)
	if err == nil {
		t.Fatal("Execute succeeded, want a NotEvaluatedError")
	}
	var nerr *pending.NotEvaluatedError
	if !errors.As(err, &nerr) {
		t.Fatalf("error is not a *pending.NotEvaluatedError: %v", err)
	}
	if nerr.Grab != "out" || nerr.Target != "meta.t" {
		t.Fatalf("error names grab %q and target %q, want out and meta.t", nerr.Grab, nerr.Target)
	}
	if strings.Contains(err.Error(), "concat") {
		t.Fatalf("error %q quotes operator text", err)
	}
}

// TestGrabUnderPendingAncestor checks that a grab whose bracket path
// runs through an operator that has not run yet, and that reads the
// grab, fails with the error for a path that does not resolve. The
// operator's text is a string, which holds no entry for the rest of the
// path, so no operator text reaches the grab's value.
func TestGrabUnderPendingAncestor(t *testing.T) {
	doc := `k: (( concat "t" "" ))
meta:
  t: (( concat "x" (grab out) ))
out: (( grab meta[k].v ))
`
	for _, mode := range leakModes {
		t.Run(mode.name, func(t *testing.T) {
			_, err := mergeBracketDoc(t, mode.parallel, doc)
			if err == nil {
				t.Fatal("Execute succeeded, want an error at $.out")
			}
			if errors.Is(err, pending.ErrNotEvaluated) {
				t.Fatalf("error matches ErrNotEvaluated: %v", err)
			}
			var perr *graft.PathError
			if !errors.As(err, &perr) || perr.Path != "out" {
				t.Fatalf("error does not come through the path error for out: %v", err)
			}
			msg := err.Error()
			if !strings.Contains(msg, "unable to resolve") {
				t.Fatalf("error %q does not say the path is unable to resolve", msg)
			}
			if strings.Contains(msg, "concat") {
				t.Fatalf("error %q quotes operator text", msg)
			}
		})
	}
}
