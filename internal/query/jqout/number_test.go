package jqout

import (
	"math"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/query/value"
)

func TestNumberPrinting(t *testing.T) {
	// Literals print in decNumber canonical form, pinned against jq 1.8.2.
	for in, want := range map[string]string{
		`1.0`: `1.0`, `100000000000000000000`: `100000000000000000000`, `1e1000`: `1E+1000`,
		`0.1`: `0.1`, `1e17`: `1E+17`, `1e18`: `1E+18`, `3.0e2`: `3.0E+2`, `-0`: `-0`,
		`0.000001`: `0.000001`, `1.5e-7`: `1.5E-7`,
	} {
		wantEqual(t, FormatNumber(lit(in)), want)
	}
	wantEqual(t, FormatNumber(value.Num(1e18)), `1000000000000000000`) // computed results print plainly up to 21 digits
	wantEqual(t, FormatNumber(dec("100000000000000000001")), `100000000000000000001`)
	wantEqual(t, FormatNumber(dec("1e21")), `1e+21`)
	wantEqual(t, FormatNumber(dec("1.50")), `1.5`) // results are reduced
	wantEqual(t, FormatNumber(dec("0.000001")), `0.000001`)
	wantEqual(t, FormatNumber(dec("1e-7")), `1e-07`)
	wantEqual(t, FormatNumber(value.Num(math.Inf(1))), `1.7976931348623157e+308`)
	wantEqual(t, FormatNumber(value.Num(math.Inf(-1))), `-1.7976931348623157e+308`)
	wantEqual(t, FormatNumber(value.Num(math.NaN())), `null`)
	wantEqual(t, FormatNumber(value.Num(math.Copysign(0, -1))), `-0`)
}

func TestFloatNumberPrinting(t *testing.T) {
	wantEqual(t, FormatNumberFloat(value.Num(0.30000000000000004)), `0.30000000000000004`)
	wantEqual(t, FormatNumberFloat(value.Num(1e17)), `1e+17`)
	wantEqual(t, FormatNumberFloat(value.Num(1e16)), `1e+16`) // jvp_dtoa_fmt places the exponent at n+15
	wantEqual(t, FormatNumberFloat(value.Num(1.5e16)), `15000000000000000`)
	wantEqual(t, FormatNumberFloat(value.Num(0.00001)), `1e-05`)
	wantEqual(t, FormatNumberFloat(lit("1e17")), `1E+17`) // literals keep their canonical form under the flag too
	wantEqual(t, FormatNumberFloat(value.Num(math.Inf(-1))), `-1.7976931348623157e+308`)
	wantEqual(t, FormatNumberFloat(value.Num(math.NaN())), `null`)
	wantEqual(t, FormatNumberFloat(value.Num(math.Copysign(0, -1))), `-0`)
}

func TestDigitBoundLiteralPrintsCanonically(t *testing.T) {
	text := strings.Repeat("1", 100001) + "e5"
	for _, got := range []string{FormatNumber(value.NumText(text)), FormatNumberFloat(value.NumText(text))} {
		if !strings.HasSuffix(got, "E+100005") || got != value.CanonicalLiteral(text) {
			t.Errorf("got a %d-byte text ending %q, want the canonical form ending E+100005", len(got), got[len(got)-10:])
		}
	}
}

func TestOverBoundLiteralKeepsItsText(t *testing.T) {
	text := "1e1000000000000"
	wantEqual(t, FormatNumber(value.NumText(text)), text)
	wantEqual(t, FormatNumberFloat(value.NumText(text)), text)
}
