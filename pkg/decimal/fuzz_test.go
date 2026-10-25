package decimal

import (
	"errors"
	"strings"
	"testing"
)

func FuzzDecimalParse(f *testing.F) {
	for _, s := range []string{"1e999999999", "-0.0e-5", "NaN", "1.5E+3", "0x10", "", "1e", ".e1", "9999999999999999999999999999999999999999"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		d, err := Parse(s)
		if err == nil && d.String() == "" {
			t.Fatal("empty")
		}
	})
}

func FuzzDecimalArith(f *testing.F) {
	f.Add("1e500000", "1e500000")
	f.Add("1", "3")
	f.Add("1e999999999", "1e-999999999")
	f.Add("-0", "0")
	// Products past the digit bound and past the exponent bound, so the
	// Mul checks run and the result assertions can see a missing one.
	f.Add(strings.Repeat("9", 60_000), strings.Repeat("9", 60_000))
	f.Add("1e999999999", "1e999999999")
	f.Add("1e-999999999", "1e-999999999")
	f.Add("123456789.987654321", "0.000001")
	f.Fuzz(func(t *testing.T, a, b string) {
		x, e1 := Parse(a)
		y, e2 := Parse(b)
		if e1 != nil || e2 != nil {
			return
		}
		results := map[string]func() (Decimal, error){
			"add": func() (Decimal, error) { return x.Add(y) },
			"sub": func() (Decimal, error) { return x.Sub(y) },
			"mul": func() (Decimal, error) { return x.Mul(y) },
			"quo": func() (Decimal, error) { return x.Quo(y, Context128) },
			"rem": func() (Decimal, error) { return x.Rem(y) },
		}
		for name, op := range results {
			r, err := op()
			if err != nil || r.IsNaN() {
				continue
			}
			if inf, _ := r.IsInf(); inf {
				continue
			}
			if n := r.numDigits(); n > defaultMaxDigits {
				t.Fatalf("%s(%q, %q) has %d digits, past the bound", name, a, b, n)
			}
			if e := r.adjusted(); e > defaultMaxExponent || e < -defaultMaxExponent {
				t.Fatalf("%s(%q, %q) has adjusted exponent %d, past the bound", name, a, b, e)
			}
		}
		if x.Cmp(y) != -y.Cmp(x) {
			t.Fatalf("Cmp(%q, %q) is not antisymmetric", a, b)
		}
	})
}

// The Mul bound checks run before the multiplication, and the product is
// checked again afterwards, so a fuzz seed sees the same error whether or
// not the early check exists. What the early check saves is the work, and
// that shows as an allocation.
func TestMulRejectsBeforeMultiplying(t *testing.T) {
	digits := MustParse("1." + strings.Repeat("7", 99_999))
	huge := MustParse("1e999999999")
	wide := MustParse("1." + strings.Repeat("7", 49_998) + "e999999990")
	for _, tc := range []struct {
		name string
		x, y Decimal
		want error
	}{
		{"digits", digits, digits, ErrTooManyDigits},
		{"exponent", huge, huge, ErrExponentRange},
		{"exponent with wide coefficients", wide, wide, ErrExponentRange},
	} {
		if _, err := tc.x.Mul(tc.y); !errors.Is(err, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, err, tc.want)
		}
		if n := testing.AllocsPerRun(5, func() { _, _ = tc.x.Mul(tc.y) }); n != 0 {
			t.Errorf("%s: Mul allocated %v times before rejecting", tc.name, n)
		}
	}
}
