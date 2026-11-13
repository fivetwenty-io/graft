package yaml

import (
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/token"
)

// TestStandInSearchCostIsLinear hands out n and then 4n stand-ins from
// one mapping's search and checks that the larger run costs well under
// 16 times the smaller one. A search that resumes where the last one
// ended costs about 4 times as much. One that starts over at the
// largest float and skips the floats it handed out costs about 16
// times as much, which a whole decode hides behind goccy's own cost.
func TestStandInSearchCostIsLinear(t *testing.T) {
	if testing.Short() {
		t.Skip("hands out 20000 stand-ins")
	}
	const n, limit = 4000, 8
	kt := reflect.TypeOf(float64(0))
	k := ast.Float(token.New(".nan", ".nan", &token.Position{}))
	run := func(count int, stop time.Duration) time.Duration {
		least := time.Duration(math.MaxInt64)
		for range 5 {
			w := &siteWalk{}
			stands := &standIns{taken: map[uint64]struct{}{},
				next: math.MaxFloat64}
			start := time.Now()
			for range count {
				if _, _, ok := w.freeFloat(stands, k, kt, 64); !ok {
					t.Fatal("the search found no free float")
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
	t.Logf("%d stand-ins: %v, %d stand-ins: %v", n, small, 4*n, large)
	if large > limit*small {
		t.Errorf("%d stand-ins took %v, more than %d times the %v "+
			"that %d take", 4*n, large, limit, small, n)
	}
}
