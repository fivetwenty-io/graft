package query_test

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/jq"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// TestArithmeticLoopAllocations bounds what one step of an arithmetic
// reduce allocates, counted as the difference between n and 2n steps so
// that compiling and starting the run drop out.
func TestArithmeticLoopAllocations(t *testing.T) {
	const n = 20000
	for _, c := range []struct {
		prog  string
		float bool
	}{
		{`reduce range(%d) as $i (0; . + $i / 7) | 0`, false},
		{`reduce range(%d) as $i (0; . + $i / 7) | 0`, true},
	} {
		small, large := mallocs(t, fmt.Sprintf(c.prog, n), c.float), mallocs(t, fmt.Sprintf(c.prog, 2*n), c.float)
		if per := float64(large-small) / n; per > 20 {
			t.Errorf("%s with float numbers %v: %.1f allocations a step", c.prog, c.float, per)
		}
	}
}

// mallocs returns the number of allocations one run of prog makes.
func mallocs(t *testing.T, prog string, float bool) uint64 {
	t.Helper()
	var opts []jq.Option
	if float {
		opts = append(opts, jq.WithFloatNumbers())
	}
	q, err := jq.Compile(prog, opts...)
	if err != nil {
		t.Fatal(err)
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err = q.RunAll(t.Context(), value.Null())
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	return after.Mallocs - before.Mallocs
}
