package yaml

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/fivetwenty-io/graft/internal/yamldiff"
	"github.com/fivetwenty-io/graft/internal/yamlnode"
)

// TestDiffPathCostIsLinear spells one path for every key of a wide
// mapping, as a diff does when every key changed, and checks that 4n
// paths cost well under 16 times what n paths do. A scan of the parent
// mapping for each path costs about 16 times as much.
func TestDiffPathCostIsLinear(t *testing.T) {
	if testing.Short() {
		t.Skip("spells 40000 paths")
	}
	const n, limit = 2000, 8
	run := func(count int, stop time.Duration) time.Duration {
		var src strings.Builder
		for i := range count {
			fmt.Fprintf(&src, "key%d: v\n", i)
		}
		docs, err := yamlnode.Parse([]byte(src.String()))
		if err != nil {
			t.Fatal(err)
		}
		roots := []diffRoot{{node: docs[0].Content[0], from: true}}
		paths := make([][]yamldiff.PathElement, count)
		for i := range count {
			name := fmt.Sprintf("key%d", i)
			p := yamldiff.NewPathWithNamedElement(yamldiff.Path{}, name)
			paths[i] = p.PathElements
		}
		least := time.Duration(1<<63 - 1)
		for range 5 {
			ix := yamldiff.NewKeyIndex()
			start := time.Now()
			for _, elements := range paths {
				if p := diffPath(elements, roots, ix); strings.HasPrefix(p, "]") {
					t.Fatalf("path %q was refused", p)
				}
			}
			least = min(least, time.Since(start))
			if least <= stop {
				break
			}
		}
		return least
	}
	small := run(n, 0)
	large := run(4*n, limit*small)
	t.Logf("%d paths: %v, %d paths: %v", n, small, 4*n, large)
	if large > limit*small {
		t.Errorf("%d paths took %v, more than %d times the %v that "+
			"%d take", 4*n, large, limit, small, n)
	}
}
