package jq_test

import (
	"errors"
	"testing"

	"github.com/fivetwenty-io/graft/internal/query/jq"
	"github.com/fivetwenty-io/graft/pkg/query"
)

func TestParseChecksSyntaxAlone(t *testing.T) {
	if _, err := jq.Parse(`$undefined | nosuchfn`); err != nil {
		t.Errorf("names are not resolved by Parse, got %v", err)
	}
	_, err := jq.Parse("1 |\n .[")
	var ce *query.CompileError
	if !errors.As(err, &ce) {
		t.Fatalf("got %T %v, want a *query.CompileError", err, err)
	}
	wantEqual(t, ce.Line, 2)
}

// A program must have an expression to run. One that is blank, only
// comments, or only directives and definitions fails before any module is
// looked up, as it does in jq.
func TestParseWantsATopLevelProgram(t *testing.T) {
	for _, src := range []string{"", "  \n\t", "# a comment\n", "#c \\\n.", "def f: 1;", "def f: 1; def g: 2;", `module {a: 1};`, `import "nosuch" as x;`, `include "nosuch";`} {
		_, err := jq.Parse(src)
		if err == nil || err.Error() != `Top-level program not given (try ".")` {
			t.Errorf("Parse(%q) = %v, want the top-level program error", src, err)
		}
		if _, err := jq.Compile(src); err == nil || err.Error() != `Top-level program not given (try ".")` {
			t.Errorf("Compile(%q) = %v, want the top-level program error", src, err)
		}
	}
	for _, src := range []string{".", "def f: 1; f", "# c\n.", `module {a: 1}; .`} {
		if _, err := jq.Parse(src); err != nil {
			t.Errorf("Parse(%q): %v", src, err)
		}
	}
}
