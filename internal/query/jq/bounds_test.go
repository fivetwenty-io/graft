package jq_test

import (
	"runtime"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/query/jq"
)

func wantErrContains(t *testing.T, err error, sub string) {
	t.Helper()
	if err == nil {
		t.Fatalf("got no error, want one containing %q", sub)
	}
	if !strings.Contains(err.Error(), sub) {
		t.Errorf("error %q does not contain %q", err, sub)
	}
}

// allocated runs f and returns the bytes the process allocated meanwhile.
func allocated(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}

func TestOverBoundMathIsBounded(t *testing.T) {
	huge := strings.Repeat("9", 100001)
	wantErrContains(t, runErr(t, `. + 0`, huge), "number exceeds 100000 significant digits")
	// A 60,000-digit literal squared passes the bound, and the product does not.
	wantErrContains(t, runErr(t, `. * .`, strings.Repeat("9", 60000)), "number exceeds 100000 significant digits")
	// Without the exponent check, scaling to 1e1000000000 would build a
	// number of a billion digits.
	const limit = 32 << 20
	var err error
	if n := allocated(func() { err = runErr(t, `. * 2`, "1e1000000000") }); n > limit {
		t.Errorf("the exponent rejection allocated %d bytes, want under %d", n, limit)
	}
	wantErrContains(t, err, "number exponent exceeds 999999999")
}

func TestNULInProgramIsRejected(t *testing.T) {
	_, err := jq.Compile("1 \x00")
	wantErrContains(t, err, "unexpected character")
}
