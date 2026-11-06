package graft

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/errtext"
	"github.com/fivetwenty-io/graft/internal/literal"
	"github.com/fivetwenty-io/graft/internal/pending"
)

const testLoopPath = "__graft_loop.l0123456789ab_1.0.svc"

// loopTree is a tree that holds loop bindings, so withoutLoopNames
// scrubs the errors an evaluator over it returns.
func loopTree() *Evaluator {
	return &Evaluator{Tree: map[string]interface{}{loopBindingKey: map[string]interface{}{}}}
}

// TestWithoutLoopNamesKeepsTypedErrors scrubs each kind of typed error
// that can carry a loop binding path and checks that the plain and
// detailed texts lose the prefix and gain nothing, and that errors.Is
// and errors.As still match what they matched before.
func TestWithoutLoopNamesKeepsTypedErrors(t *testing.T) {
	notEvaluated := &pending.NotEvaluatedError{Grab: "out.0", Target: testLoopPath + ".l.0"}
	refused := fmt.Errorf("`%s.expr`: %w", testLoopPath, literal.Refuse("calc", "an expression"))
	valued := errtext.WithPlain(fmt.Errorf("`%s.name` holds SECRETv4lue", testLoopPath), "`"+testLoopPath+".name` holds a value")
	cycle := &CycleError{Inputs: []string{"a.yml"}, Nodes: []CycleNode{
		{Path: "out.0.b", Expr: "(( concat (grab " + testLoopPath + ".n) (grab out.0.c) ))"},
		{Path: "out.0.c", Expr: `(( concat "w" (grab out.0.b) ))`},
	}}
	multi := MultiError{Errors: []error{
		&PathError{Path: "out.0", Cause: fmt.Errorf("nested operator grab failed: %w", notEvaluated)},
		&PathError{Path: "out.1", Cause: refused},
		&PathError{Path: "out.2", Cause: valued},
		cycle,
		&PathError{Path: "plain", Cause: errors.New("untouched")},
	}}

	got := loopTree().withoutLoopNames(multi)

	for name, text := range map[string]string{"Error()": got.Error(), "DetailedError": DetailedError(got)} {
		if strings.Contains(text, loopBindingKey) {
			t.Errorf("%s still names the loop binding tree:\n%s", name, text)
		}
		want := strings.ReplaceAll(map[string]string{"Error()": multi.Error(), "DetailedError": DetailedError(multi)}[name], testLoopPath+".", "svc.")
		if text != want {
			t.Errorf("%s = %q,\nwant the original text with only the prefix removed, %q", name, text, want)
		}
	}
	if !strings.Contains(DetailedError(got), "SECRETv4lue") || strings.Contains(got.Error(), "SECRETv4lue") {
		t.Errorf("the scrub changed which text quotes a value:\nError() %q\nDetailedError %q", got.Error(), DetailedError(got))
	}

	scrubbed, ok := got.(MultiError) //nolint:errorlint // the scrub keeps the type at the top
	if !ok {
		t.Fatalf("withoutLoopNames(MultiError) returned %T", got)
	}
	if !errors.Is(got, ErrNotEvaluated) || !errors.Is(got, ErrLiteralRefused) || !errors.Is(got, ErrDependencyCycle) {
		t.Errorf("errors.Is lost a sentinel: NotEvaluated %v, LiteralRefused %v, DependencyCycle %v",
			errors.Is(got, ErrNotEvaluated), errors.Is(got, ErrLiteralRefused), errors.Is(got, ErrDependencyCycle))
	}

	var pe *PathError
	if !errors.As(scrubbed.Errors[0], &pe) || pe.Path != "out.0" || strings.Contains(pe.Cause.Error(), loopBindingKey) {
		t.Errorf("errors.As(*PathError) = %#v", pe)
	}
	var ne *pending.NotEvaluatedError
	if !errors.As(got, &ne) || ne.Target != "svc.l.0" || ne.Grab != "out.0" {
		t.Errorf("errors.As(*NotEvaluatedError) = %#v, want Target svc.l.0", ne)
	}
	var re *literal.RefusedError
	if !errors.As(scrubbed.Errors[1], &re) || re.Operator != "calc" {
		t.Errorf("errors.As(*RefusedError) = %#v", re)
	}
	var ce *CycleError
	if !errors.As(got, &ce) || strings.Contains(ce.Nodes[0].Expr, loopBindingKey) || ce.Nodes[0].Expr != "(( concat (grab svc.n) (grab out.0.c) ))" {
		t.Errorf("errors.As(*CycleError) = %#v", ce)
	}
	if !sameError(scrubbed.Errors[4], multi.Errors[4]) {
		t.Errorf("an error with no loop path was replaced: %#v", scrubbed.Errors[4])
	}
	if notEvaluated.Target != testLoopPath+".l.0" || cycle.Nodes[0].Expr == ce.Nodes[0].Expr {
		t.Error("the scrub changed the original errors in place")
	}
}

// TestWithoutLoopNamesNeedsLoopBindings checks that an error passes
// through unchanged when the tree holds no loop bindings, and that a
// nil error stays nil.
func TestWithoutLoopNamesNeedsLoopBindings(t *testing.T) {
	err := &PathError{Path: "out", Cause: errors.New("`" + testLoopPath + "` could not be found")}
	ev := &Evaluator{Tree: map[string]interface{}{"out": 1}}
	if got := ev.withoutLoopNames(err); !sameError(got, err) {
		t.Errorf("withoutLoopNames changed an error from a tree with no loop bindings: %v", got)
	}
	if got := loopTree().withoutLoopNames(nil); got != nil {
		t.Errorf("withoutLoopNames(nil) = %v", got)
	}
}

// sameError reports whether got is the very error want, not a scrubbed
// copy of it.
func sameError(got, want error) bool {
	pe, ok := got.(*PathError) //nolint:errorlint // the very value, not one down the chain
	return ok && pe == want    //nolint:errorlint // identity, not a match down the chain
}
