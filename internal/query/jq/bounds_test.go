package jq_test

import (
	"strings"
	"testing"
	"time"

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

func TestOverBoundMathIsBounded(t *testing.T) {
	start := time.Now()
	huge := strings.Repeat("9", 100001)
	wantErrContains(t, runErr(t, `. + 0`, huge), "number exceeds 100000 significant digits")
	wantErrContains(t, runErr(t, `. * 2`, "1e1000000000"), "number exponent exceeds 999999999")
	// A 60,000-digit literal squared passes the bound, and the product does not.
	wantErrContains(t, runErr(t, `. * .`, strings.Repeat("9", 60000)), "number exceeds 100000 significant digits")
	if el := time.Since(start); el > time.Second {
		t.Fatalf("the bounded math took %v", el)
	}
}

func TestNULInProgramIsRejected(t *testing.T) {
	_, err := jq.Compile("1 \x00")
	wantErrContains(t, err, "unexpected character")
}
