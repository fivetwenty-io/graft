package jq_test

import (
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

func TestOverBoundMathIsBounded(t *testing.T) {
	huge := strings.Repeat("9", 100001)
	wantErrContains(t, runErr(t, `. + 0`, huge), "number exceeds 100000 significant digits")
	// A 60,000-digit literal squared passes the bound, and the product does not.
	wantErrContains(t, runErr(t, `. * .`, strings.Repeat("9", 60000)), "number exceeds 100000 significant digits")
	// Parse, Mul, and the result check each reject the exponent, so no
	// assertion here can tell which one ran. The load and decimal tests pin
	// the parse check and the Mul check on their own.
	wantErrContains(t, runErr(t, `. * 2`, "1e1000000000"), "number exponent exceeds 999999999")
}

func TestNULInProgramIsRejected(t *testing.T) {
	_, err := jq.Compile("1 \x00")
	wantErrContains(t, err, "unexpected character")
}
