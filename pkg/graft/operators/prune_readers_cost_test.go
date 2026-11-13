package operators_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// pruneReaderDocs returns two documents for a merge in which n keys of
// the first are pruned by the second, which also holds a join and a
// stringify beside each pruned key. The merger queues every pruned key
// before the dependency graph is built.
func pruneReaderDocs(n int) (string, string) {
	var a, b strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&a, "k%d:\n  a: 1\n  b: 2\n", i)
		fmt.Fprintf(&b, "k%d:\n  b: (( prune ))\n", i)
		fmt.Fprintf(&b, "  s: (( join \",\" k%d.c ))\n", i)
		fmt.Fprintf(&b, "  t: (( stringify k%d.c ))\n", i)
		fmt.Fprintf(&b, "  c: [x, y]\n")
	}
	return a.String(), b.String()
}

// mergeTwoAllocs returns the allocations one merge of first and second
// makes.
func mergeTwoAllocs(t *testing.T, parallel bool, first, second string) float64 {
	t.Helper()
	return testing.AllocsPerRun(3, func() {
		engine, err := graft.NewEngine(graft.WithParallel(parallel))
		if err != nil {
			t.Fatal(err)
		}
		d1, err := engine.ParseYAML([]byte(first))
		if err != nil {
			t.Fatal(err)
		}
		d2, err := engine.ParseYAML([]byte(second))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := engine.Merge(context.Background(), d1, d2).Execute(); err != nil {
			t.Fatal(err)
		}
	})
}

// The cost of ordering join and stringify after the operators above a
// pruned key grows with the number of readers plus the number of pruned
// keys, not with their product. The operators above the pruned keys are
// found once for the phase, and each reader looks them up.
func TestPruneReadersScaleLinearly(t *testing.T) {
	for _, parallel := range []bool{true, false} {
		t.Run(fmt.Sprintf("parallel=%v", parallel), func(t *testing.T) {
			a, b := pruneReaderDocs(200)
			small := mergeTwoAllocs(t, parallel, a, b)
			a, b = pruneReaderDocs(400)
			large := mergeTwoAllocs(t, parallel, a, b)
			if ratio := large / small; ratio > 2.5 {
				t.Fatalf("doubling the readers multiplied allocations "+
					"by %.2f, want at most 2.5", ratio)
			}
		})
	}
}
