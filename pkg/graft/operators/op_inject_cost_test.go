package operators_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// injectDoc returns a document with n injects, each of a map that holds
// an inject of its own, so every inject reads a path with an operator
// under it.
func injectDoc(n int) string {
	var b strings.Builder
	b.WriteString("base:\n  k: v\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "m%d:\n  a: %d\n  sub:\n    _: (( inject base ))\n", i, i)
		fmt.Fprintf(&b, "x%d:\n  _: (( inject m%d ))\n", i, i)
	}
	return b.String()
}

// mergeAllocs returns the allocations one merge of src makes.
func mergeAllocs(t *testing.T, parallel bool, src string) float64 {
	t.Helper()
	return testing.AllocsPerRun(3, func() {
		engine, err := graft.NewEngine(graft.WithParallel(parallel))
		if err != nil {
			t.Fatal(err)
		}
		doc, err := engine.ParseYAML([]byte(src))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := engine.Merge(context.Background(), doc).Execute(); err != nil {
			t.Fatal(err)
		}
	})
}

// The cost of ordering injects grows with the number of injects, not
// with its square. Each inject finds the operators under the map it
// reads through the index the evaluator builds for the phase, rather
// than by comparing its path with every operator's path.
func TestInjectDependenciesScaleLinearly(t *testing.T) {
	for _, parallel := range []bool{true, false} {
		t.Run(fmt.Sprintf("parallel=%v", parallel), func(t *testing.T) {
			small := mergeAllocs(t, parallel, injectDoc(200))
			large := mergeAllocs(t, parallel, injectDoc(400))
			if ratio := large / small; ratio > 2.5 {
				t.Fatalf("doubling the injects multiplied allocations "+
					"by %.2f, want at most 2.5", ratio)
			}
		})
	}
}
