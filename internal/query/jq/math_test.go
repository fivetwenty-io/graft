package jq_test

import (
	"strings"
	"testing"
)

// libmOneArg is every one-argument libm name that computes in float64.
const libmOneArg = `acos acosh asin asinh atan atanh cbrt cos cosh erf erfc exp exp10 exp2 expm1
gamma j0 j1 lgamma log log10 log1p log2 logb nearbyint rint significand sin sinh sqrt tan tanh tgamma y0 y1`

func TestIsfinite(t *testing.T) {
	// jq counts NaN as finite, and a non-number is not finite.
	wantOutputs(t, `[1, infinite, -infinite, nan, "a", null] | map(isfinite)`, `null`, `[true,false,false,true,false,false]`)
}

// The expected last digits below come from Go's math package. A float64
// libm builtin may differ from the platform libm in the last place, and
// gamma is lgamma here as in glibc, where macOS's gamma is tgamma.
func TestLibmNamesAndEdges(t *testing.T) {
	for _, n := range strings.Fields(libmOneArg) {
		wantOutputs(t, n+" | type", `1`, `"number"`)
	}
	wantOutputs(t, `[pow(2; 10), atan2(1; 1), fma(2; 3; 4), ldexp(1; 3), (8.5 | frexp), (8.5 | modf), (8.5 | lgamma_r)]`, `null`,
		`[1024,0.7853981633974483,10,8,[0.53125,4],[0.5,8],[9.549267257300997,1]]`)
	wantOutputs(t, `[infinite, -infinite, nan] | map(isinfinite, isnan)`, `null`, `[true,false,true,false,false,true]`)
	wantOutputs(t, `significand, logb, gamma, drem(5; 3)`, `8`, `1`, `3`, `8.525161361065413`, `-1`)
	wantOutputs(t, `nearbyint, rint, trunc, round`, `2.5`, `2`, `2`, `2`, `3`)
	wantOutputs(t, `[sqrt, pow(.; 10), (. * 0.1 | . * 10), (log | exp)]`, `4`, `[2,1048576,4,4]`)
	wantOutputs(t, `floor`, `100000000000000000001`, `100000000000000000001`)
}

func TestLibmTwoAndThreeArg(t *testing.T) {
	wantOutputs(t, `[scalb(.; 2), scalbln(.; 2), nexttoward(.; 4), nextafter(.; 4), ldexp(.; 0.5)]`, `3`,
		`[12,12,3.0000000000000004,3.0000000000000004,3]`)
	wantOutputs(t, `[jn(2; .), yn(2; .), j0, j1, y0, y1]`, `1`,
		`[0.11490348493190049,-1.6506826068162543,0.7651976865579666,0.4400505857449335,0.08825696421567697,-0.7812128213002887]`)
	wantOutputs(t, `[remainder(.; 3), fmod(.; 3), fdim(.; 9), fdim(9; .), copysign(.; -1), hypot(.; 12)]`, `5`, `[-1,2,0,4,-5,13]`)
	wantOutputs(t, `nan | [fmin(.; 3), fmax(.; 3)]`, `null`, `[3,3]`)
}

func TestLibmArrayResultsAndEdges(t *testing.T) {
	wantOutputs(t, `frexp, modf`, `-5.5`, `[-0.6875,3]`, `[-0.5,-5]`)
	wantOutputs(t, `nan | frexp, modf`, `null`, `[null,0]`, `[null,null]`)
	wantOutputs(t, `frexp, logb, significand`, `0`, `[0,0]`, `-1.7976931348623157e+308`, `0`)
	wantOutputs(t, `lgamma_r, tgamma`, `-3`, `[1.7976931348623157e+308,1]`, `null`)
	wantOutputs(t, `[pow(.; 2), pow(2; .)]`, `3`, `[9,8]`)
}

func TestLibmErrors(t *testing.T) {
	wantError(t, `sqrt`, `"a"`, `string ("a") number required`)
	wantError(t, `ldexp(1; "a")`, `null`, `string ("a") number required`)
	wantError(t, `frexp`, `"x"`, `string ("x") number required`)
	wantError(t, `fma(1; 2; "z")`, `null`, `string ("z") number required`)
}
