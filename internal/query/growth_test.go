package query_test

import (
	"fmt"
	"runtime"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/jq"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// TestGrowthChainsAllocateLinearly runs loops that grow one container a
// step at a time, at n and at 2n steps. Doubling the steps must about
// double the bytes allocated, where a copy of the container per step
// quadruples them.
func TestGrowthChainsAllocateLinearly(t *testing.T) {
	progs := []string{
		`reduce range(%d) as $i ({}; setpath(["k\($i)"]; $i)) | length`,
		`reduce range(%d) as $i ({}; .["k\($i)"] = $i) | length`,
		`reduce range(%d) as $i ({}; . + {"k\($i)": $i}) | length`,
		`reduce range(%d) as $i ([]; .[$i] = $i) | length`,
		`reduce range(%d) as $i ([]; . + [$i]) | length`,
		`reduce range(%d) as $i ([]; . += [$i]) | length`,
	}
	const n = 4000
	for _, p := range progs {
		small, large := allocated(t, fmt.Sprintf(p, n), n), allocated(t, fmt.Sprintf(p, 2*n), 2*n)
		if ratio := float64(large) / float64(small); ratio > 3 {
			t.Errorf("%s: %d steps allocate %.1f times what %d do", p, 2*n, ratio, n)
		}
	}
}

// TestReplacementChainsAllocateLinearly runs reduce loops that change
// keys or elements their state already holds, at n and at 2n steps, which
// must about double the bytes allocated as the growth chains do.
func TestReplacementChainsAllocateLinearly(t *testing.T) {
	progs := []struct {
		prog string
		want func(n int) int
	}{
		{`reduce range(%[1]d) as $i ({}; .["k\($i %% %[2]d)"] += 1) | length`, half},
		{`reduce range(%[1]d) as $i ({}; .["k\($i %% %[2]d)"] |= . + 1) | length`, half},
		{`reduce range(%[1]d) as $i ({}; .count += 1 | .["k\($i)"] = $i) | .count`, same},
		{`reduce range(%[1]d) as $i ([range(%[1]d)]; .[$i] = 0) | length`, same},
		{`reduce range(%[1]d) as $i ({}; .a = $i | .["k\($i)"] = .a | .b //= 0) | length`, plusTwo},
	}
	const n = 4000
	for _, p := range progs {
		small := allocated(t, fmt.Sprintf(p.prog, n, n/2), p.want(n))
		large := allocated(t, fmt.Sprintf(p.prog, 2*n, n), p.want(2*n))
		if ratio := float64(large) / float64(small); ratio > 3 {
			t.Errorf("%s: %d steps allocate %.1f times what %d do", p.prog, 2*n, ratio, n)
		}
	}
}

func half(n int) int    { return n / 2 }
func same(n int) int    { return n }
func plusTwo(n int) int { return n + 2 }

// allocated returns the bytes that one run of prog allocates, after it
// checks that the run gives want.
func allocated(t *testing.T, prog string, want int) uint64 {
	t.Helper()
	q, err := jq.Compile(prog)
	if err != nil {
		t.Fatal(err)
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	out, err := q.RunAll(t.Context(), value.Null())
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].Float() != float64(want) {
		t.Fatalf("%s gave %v, want %d", prog, out, want)
	}
	return after.TotalAlloc - before.TotalAlloc
}
