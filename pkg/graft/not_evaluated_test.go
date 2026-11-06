package graft_test

import (
	"errors"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// TestErrNotEvaluatedFromThePublicPackage checks that library code can
// tell a grab of an operator that has not run apart from every other
// failure with graft.ErrNotEvaluated, through the error a merge returns
// when it holds one failure and when it holds several.
func TestErrNotEvaluatedFromThePublicPackage(t *testing.T) {
	docs := map[string]string{
		"one failure": `aaa: (( join "," (grab zmeta.l) ))
zmeta:
  l:
  - (( concat "1" (grab aaa) ))
`,
		"several failures": `aaa: (( join "," (grab zmeta.l) ))
bad: (( grab nowhere ))
zmeta:
  l:
  - (( concat "1" (grab aaa) ))
`,
	}
	for _, mode := range leakModes {
		for name, doc := range docs {
			t.Run(mode.name+"/"+name, func(t *testing.T) {
				_, err := mergeBracketDoc(t, mode.parallel, doc)
				if err == nil {
					t.Fatal("Execute succeeded, want an error")
				}
				if !errors.Is(err, graft.ErrNotEvaluated) {
					t.Fatalf("error does not match graft.ErrNotEvaluated: %v", err)
				}
				if errors.Is(err, graft.ErrLiteralRefused) {
					t.Fatalf("error matches graft.ErrLiteralRefused: %v", err)
				}
			})
		}
	}

	_, err := mergeBracketDoc(t, false, "bad: (( grab nowhere ))\n")
	if err == nil || errors.Is(err, graft.ErrNotEvaluated) {
		t.Fatalf("a path that does not resolve matches graft.ErrNotEvaluated: %v", err)
	}
}
