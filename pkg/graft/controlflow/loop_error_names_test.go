package controlflow

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/pending"
	"github.com/fivetwenty-io/graft/internal/utils/ansi"
	"github.com/fivetwenty-io/graft/pkg/graft"
)

// runLoopMergeErr merges src on an engine built with opts and returns
// the error the merge fails with.
func runLoopMergeErr(t *testing.T, opts []graft.EngineOption, src string) error {
	t.Helper()
	engine, err := graft.NewEngine(opts...)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	doc, err := engine.ParseYAML([]byte(src))
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}
	_, err = engine.Merge(context.Background(), doc).Execute()
	if err == nil {
		t.Fatal("the merge succeeded, want an error")
	}
	return err
}

// assertNoLoopNames fails when any text a caller can reach from err
// names the internal loop binding tree: Error(), DetailedError, and
// the plain and detailed text of each *PathError in it, which is how
// the graft CLI reads a failing path.
func assertNoLoopNames(t *testing.T, err error) {
	t.Helper()
	texts := map[string]string{
		"Error()":       err.Error(),
		"DetailedError": graft.DetailedError(err),
	}
	var multi graft.MultiError
	if errors.As(err, &multi) {
		for _, item := range multi.Errors {
			var pe *graft.PathError
			if errors.As(item, &pe) {
				texts["PathError.Path "+pe.Path] = pe.Path
				texts["PathError.Cause of "+pe.Path] = pe.Cause.Error()
				texts["detailed PathError.Cause of "+pe.Path] = graft.DetailedError(pe.Cause)
			}
		}
	}
	for where, text := range texts {
		if strings.Contains(text, "__graft_loop") {
			t.Errorf("%s names the loop binding tree:\n%s", where, text)
		}
	}
}

const loopErrorItems = `items:
- {name: A, tags: [x, y], n: 3}
out:
(( for svc in items ))
`

// TestLoopBodyErrorsNameTheWrittenPath fails a loop body in the ways
// that used to name the internal binding path, and checks that every
// message names the path the user wrote instead, in both evaluator
// modes, while errors.Is and errors.As still see the typed errors.
func TestLoopBodyErrorsNameTheWrittenPath(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{"missing key", "  - (( grab svc.missing ))\n",
			[]string{"$.out.0: unable to resolve `svc.missing`: `$.svc.missing` could not be found in the datastructure"}},
		{"missing index", "  - (( grab svc.tags.5 ))\n",
			[]string{"unable to resolve `svc.tags.5`"}},
		{"fallback", "  - (( grab svc.missing || svc.alsomissing ))\n",
			[]string{"unable to resolve `svc.alsomissing`"}},
		{"bracket index", "  - (( grab svc.tags[svc.n] ))\n",
			[]string{"unable to resolve `svc.tags.3`"}},
		{"concat of a list", "  - (( concat svc.tags \"x\" ))\n",
			[]string{"tried to concat svc.tags, which is not a string scalar"}},
		{"concat of a map", "  - (( concat svc \"x\" ))\n",
			[]string{"tried to concat svc, which is not a string scalar"}},
		{"keys of a scalar", "  - (( keys svc.name ))\n",
			[]string{"svc.name is not a map"}},
		{"inject of a scalar", "  - (( inject svc.name ))\n",
			[]string{"svc.name is not a map"}},
	}
	for _, mode := range rootRefModes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				err := runLoopMergeErr(t, mode.opts, loopErrorItems+tc.body+"(( done ))\n")
				assertNoLoopNames(t, err)
				for _, want := range tc.want {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("Error() lacks %q:\n%s", want, err.Error())
					}
					if !strings.Contains(ansi.StripEscapes(graft.DetailedError(err)), want) {
						t.Errorf("DetailedError lacks %q:\n%s", want, graft.DetailedError(err))
					}
				}
				var pe *graft.PathError
				if !errors.As(err, &pe) || pe.Path != "out.0" {
					t.Errorf("errors.As found no *PathError at out.0: %#v", pe)
				}
			})
		}
	}
}

// TestNestedLoopErrorNamesTheWrittenPath checks a type error inside a
// nested loop, whose path runs through the inner loop's binding.
func TestNestedLoopErrorNamesTheWrittenPath(t *testing.T) {
	src := `items: [{n: 1}]
out:
(( for x in items ))
(( for y in x ))
  - (( grab y.z ))
(( done ))
(( done ))
`
	for _, mode := range rootRefModes {
		t.Run(mode.name, func(t *testing.T) {
			err := runLoopMergeErr(t, mode.opts, src)
			assertNoLoopNames(t, err)
			if want := "unable to resolve `y.z`: $.y is a scalar"; !strings.Contains(err.Error(), want) {
				t.Errorf("Error() lacks %q:\n%s", want, err.Error())
			}
			if want := "unable to resolve `y.z`: $.y [=1] is a scalar"; !strings.Contains(ansi.StripEscapes(graft.DetailedError(err)), want) {
				t.Errorf("DetailedError lacks %q:\n%s", want, graft.DetailedError(err))
			}
		})
	}
}

// TestLoopBodyCycleShowsTheWrittenSource checks the cycle report for a
// cycle through a loop body. It shows each operator as the user wrote
// it, and errors.Is and errors.As still see the cycle.
func TestLoopBodyCycleShowsTheWrittenSource(t *testing.T) {
	src := `items: [{n: 1}]
out:
(( for x in items ))
  - b: (( concat (grab x.n) (grab out.0.c) ))
    c: (( concat "w" (grab out.0.b) ))
(( done ))
`
	for _, mode := range rootRefModes {
		t.Run(mode.name, func(t *testing.T) {
			err := runLoopMergeErr(t, mode.opts, src)
			assertNoLoopNames(t, err)
			if !errors.Is(err, graft.ErrDependencyCycle) {
				t.Errorf("errors.Is(err, ErrDependencyCycle) = false: %v", err)
			}
			var cycle *graft.CycleError
			if !errors.As(err, &cycle) {
				t.Fatalf("errors.As found no *CycleError: %v", err)
			}
			for _, n := range cycle.Nodes {
				if strings.Contains(n.Path, "__graft_loop") || strings.Contains(n.Expr, "__graft_loop") {
					t.Errorf("cycle node names the loop binding tree: %#v", n)
				}
			}
			if want := `out.0.b: (( concat (grab x.n) (grab out.0.c) ))`; !strings.Contains(ansi.StripEscapes(graft.DetailedError(err)), want) {
				t.Errorf("DetailedError lacks %q:\n%s", want, graft.DetailedError(err))
			}
		})
	}
}

// TestLoopBodyNotEvaluatedNamesTheWrittenPath checks the error for a
// grab that reads an operator yet to run, where the target lies inside
// the loop binding. The message names the path as written, and the
// error still matches graft.ErrNotEvaluated.
func TestLoopBodyNotEvaluatedNamesTheWrittenPath(t *testing.T) {
	src := `aaa: (( join "," (grab out.0) ))
src:
- l:
  - (( concat "1" (grab aaa) ))
out:
(( for svc in src ))
  - (( grab svc.l ))
(( done ))
`
	for _, mode := range rootRefModes {
		t.Run(mode.name, func(t *testing.T) {
			err := runLoopMergeErr(t, mode.opts, src)
			assertNoLoopNames(t, err)
			if !errors.Is(err, graft.ErrNotEvaluated) {
				t.Fatalf("errors.Is(err, ErrNotEvaluated) = false: %v", err)
			}
			var ne *pending.NotEvaluatedError
			if !errors.As(err, &ne) {
				t.Fatalf("errors.As found no *NotEvaluatedError: %v", err)
			}
			if strings.Contains(ne.Error(), "__graft_loop") || strings.Contains(ne.Target, "__graft_loop") {
				t.Errorf("NotEvaluatedError names the loop binding tree: %#v", ne)
			}
		})
	}
}
