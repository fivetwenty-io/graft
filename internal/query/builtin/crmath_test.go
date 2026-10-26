package builtin

import (
	"math"
	"testing"
)

func TestCorrectlyRoundedLibm(t *testing.T) {
	inf, nan := math.Inf(1), math.NaN()
	cases := []struct {
		name    string
		f       func(float64) float64
		in, out float64
	}{
		// Go's math is one ulp away from each of these four.
		{"atanh", atanh, 0.5, 0.5493061443340549},
		{"expm1", expm1, 1, 1.7182818284590453},
		{"exp2", exp2, 0.5, 1.4142135623730951},
		{"cbrt", cbrt, 6.046602879796196, 1.8218130786768638},

		{"atanh", atanh, 0.75, 0.9729550745276566},
		{"atanh", atanh, -0.999, -3.8002011672501994},
		{"atanh", atanh, 1e-20, 1e-20},
		{"atanh", atanh, 1, inf},
		{"atanh", atanh, -1, -inf},
		{"atanh", atanh, 1.5, nan},
		{"atanh", atanh, nan, nan},

		{"expm1", expm1, 1e-10, 1.00000000005e-10},
		{"expm1", expm1, -0.75, -0.5276334472589853},
		{"expm1", expm1, 20, 485165194.4097903},
		{"expm1", expm1, -50, -1},
		{"expm1", expm1, 709.7, 1.6549840276802644e+308},
		{"expm1", expm1, 710, inf},
		{"expm1", expm1, -inf, -1},
		{"expm1", expm1, inf, inf},
		{"expm1", expm1, nan, nan},

		{"exp2", exp2, 10, 1024},
		{"exp2", exp2, -1074, 5e-324},
		{"exp2", exp2, -1075, 0},
		{"exp2", exp2, -1074.5, 5e-324},
		{"exp2", exp2, -1022.5, 1.5733648139913585e-308},
		{"exp2", exp2, 1023.5, 1.2711610061536464e+308},
		{"exp2", exp2, 1024, inf},
		{"exp2", exp2, 1023.25, 1.068914733284906e+308},
		{"exp2", exp2, -inf, 0},
		{"exp2", exp2, inf, inf},
		{"exp2", exp2, nan, nan},

		{"cbrt", cbrt, 27, 3},
		{"cbrt", cbrt, -8, -2},
		{"cbrt", cbrt, 5e-324, 1.7031839360032603e-108},
		{"cbrt", cbrt, inf, inf},
		{"cbrt", cbrt, -inf, -inf},
		{"cbrt", cbrt, nan, nan},
	}
	for _, c := range cases {
		got := c.f(c.in)
		if math.Float64bits(got) != math.Float64bits(c.out) && !(math.IsNaN(got) && math.IsNaN(c.out)) {
			t.Errorf("%s(%v) = %v, want %v", c.name, c.in, got, c.out)
		}
	}
	for _, f := range []func(float64) float64{atanh, expm1, cbrt} {
		if z := f(math.Copysign(0, -1)); z != 0 || !math.Signbit(z) {
			t.Errorf("negative zero gave %v", z)
		}
	}
	if exp2(math.Copysign(0, -1)) != 1 {
		t.Error("exp2(-0) is not 1")
	}
}
