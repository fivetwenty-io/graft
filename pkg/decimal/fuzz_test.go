package decimal

import "testing"

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
	f.Add("123456789.987654321", "0.000001")
	f.Fuzz(func(t *testing.T, a, b string) {
		x, e1 := Parse(a)
		y, e2 := Parse(b)
		if e1 != nil || e2 != nil {
			return
		}
		_, _ = x.Add(y)
		_, _ = x.Sub(y)
		_, _ = x.Mul(y)
		_, _ = x.Quo(y, Context128)
		_, _ = x.Rem(y)
		_ = x.Cmp(y)
	})
}
