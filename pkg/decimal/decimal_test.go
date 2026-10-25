package decimal

import (
	"errors"
	"math"
	"math/big"
	"math/rand/v2"
	"strings"
	"testing"
	"time"
)

func TestParseIsExactAndCanonical(t *testing.T) {
	for in, want := range map[string]string{
		"0.1": "0.1", "100000000000000000000": "100000000000000000000", "100000000000000000001": "100000000000000000001",
		"1e1000": "1e+1000", "1E-7": "1e-07", "3.0e2": "300", "-0": "-0", "+5": "5", "0.000001": "0.000001", "1e21": "1e+21",
	} {
		wantEqual(t, MustParse(in).String(), want)
	}
	wantEqual(t, MustParse("3.0e2").Scientific(), "3.0E+2")
	wantEqual(t, MustParse("1e1000").Scientific(), "1E+1000")
	wantEqual(t, MustParse("100000000000000000000").Scientific(), "100000000000000000000")
	for _, bad := range []string{"", "1.", ".5", "1e", "0x10", "1_000", "--1", "NaN"} {
		if _, err := Parse(bad); err == nil {
			t.Errorf("Parse(%q) accepted", bad)
		}
	}
}

func TestArithmeticIsExact(t *testing.T) {
	wantEqual(t, add(t, "0.1", "0.2").String(), "0.3")
	wantEqual(t, add(t, "100000000000000000000", "1").String(), "100000000000000000001")
	wantEqual(t, sub(t, "9007199254740993", "9007199254740992").String(), "1")
	wantEqual(t, mul(t, "1.5", "2").String(), "3")
	wantEqual(t, add(t, "1e18", "0").String(), "1000000000000000000")
	wantEqual(t, add(t, "1e1000", "0").String(), "1e+1000")
	wantEqual(t, mul(t, "0", "-1").String(), "-0")
	wantEqual(t, add(t, "-0", "0").String(), "0")
	wantEqual(t, rem(t, "5", "3").String(), "2")
	wantEqual(t, rem(t, "-5", "3").String(), "-2") // truncated, as jq
	wantEqual(t, MustParse("-1.5").Round().String(), "-2")
	wantEqual(t, MustParse("2.5").Floor().String(), "2")
	wantEqual(t, MustParse("1.50").Reduce().String(), "1.5")
}

func TestDivisionRoundsToContext(t *testing.T) {
	wantEqual(t, quo(t, "1", "4").String(), "0.25")
	wantEqual(t, quo(t, "1", "3").String(), "0.3333333333333333333333333333333333")
	wantEqual(t, quo(t, "2", "3").String(), "0.6666666666666666666666666666666667")                             // an ordinary round up, not a tie
	wantEqual(t, quo(t, "1"+strings.Repeat("0", 32)+"25", "10").String(), "1."+strings.Repeat("0", 32)+"2e+33") // exact tie at digit 35 rounds to even, down
	wantEqual(t, quo(t, "1"+strings.Repeat("0", 32)+"35", "10").String(), "1."+strings.Repeat("0", 32)+"4e+33") // exact tie rounds to even, up
	wantEqual(t, quo(t, "1"+strings.Repeat("0", 32)+"24", "10").String(), "1."+strings.Repeat("0", 32)+"2e+33") // below the tie rounds down
	wantEqual(t, quo(t, strings.Repeat("9", 35), "10").String(), "1e+34")                                       // a round up that carries through every digit
	wantEqual(t, quo(t, "10", "4").String(), "2.5")
	wantEqual(t, quo(t, "1e1000", "1e-1000").String(), "1e+2000")
	_, err := MustParse("1").Quo(Zero, Context128)
	if !errors.Is(err, ErrDivisionByZero) {
		t.Fatal(err)
	}
}

func TestCompareAndSpecialValues(t *testing.T) {
	wantEqual(t, MustParse("100000000000000000000").Cmp(MustParse("100000000000000000001")), -1)
	wantEqual(t, MustParse("-0").Cmp(Zero), 0)
	wantEqual(t, NaN().Cmp(MustParse("-1e1000")), -1) // NaN sorts below every number, as jq orders it
	wantEqual(t, NaN().Cmp(NaN()), -1)                // a NaN sorts below everything, itself included, which is jq's order
	wantEqual(t, MustParse("1").Cmp(NaN()), 1)
	if NaN().Equal(NaN()) {
		t.Fatal("NaN never equals")
	}
	d, _ := Inf(1).Sub(Inf(1))
	if !d.IsNaN() {
		t.Fatal("inf - inf must be NaN")
	}
	p, _ := Inf(1).Add(MustParse("1"))
	isInf, sign := p.IsInf()
	if !isInf || sign != 1 {
		t.Fatal("inf + 1 must be +inf")
	}
	wantEqual(t, FromFloat64(0.1).String(), "0.1")
	x, y := 0.1, 0.2 // variables, because Go folds the constant expression 0.1+0.2 to an exact 0.3
	wantEqual(t, FromFloat64(x+y).FloatString(), "0.30000000000000004")
	wantEqual(t, FromFloat64(1e17).FloatString(), "1e+17") // jq 1.8.2 prints 1e17 + 0 this way
	// jq 1.8.2 prints 1e16 + 0 as 1e+16 and 1.5e16 + 0 as 15000000000000000,
	// because jvp_dtoa_fmt's switch point depends on the digit count.
	wantEqual(t, FromFloat64(1e16).FloatString(), "1e+16")
	wantEqual(t, FromFloat64(1.5e16).FloatString(), "15000000000000000")
	wantEqual(t, FromFloat64(0.00001).FloatString(), "1e-05")
	wantEqual(t, FromFloat64(math.Inf(1)).Float64(), math.Inf(1))
	wantEqual(t, MustParse("1e400").Float64(), math.Inf(1))
	if _, ok := MustParse("9223372036854775808").Int64(); ok {
		t.Fatal("past int64 must report !ok")
	}
	i, ok := MustParse("-9223372036854775808").Int64()
	wantEqual(t, ok, true)
	wantEqual(t, i, int64(math.MinInt64))
}

func TestBoundsAreEnforcedQuickly(t *testing.T) {
	start := time.Now()
	if _, err := Parse(strings.Repeat("9", 100001)); !errors.Is(err, ErrTooManyDigits) {
		t.Fatalf("got %v", err)
	}
	if _, err := Parse("1e1000000000"); !errors.Is(err, ErrExponentRange) {
		t.Fatalf("got %v", err)
	}
	huge := MustParse("1e999999999")
	if _, err := huge.Mul(huge); !errors.Is(err, ErrExponentRange) {
		t.Fatalf("got %v", err)
	}
	wide := MustParse(strings.Repeat("9", 60000))
	if _, err := wide.Mul(wide); !errors.Is(err, ErrTooManyDigits) {
		t.Fatalf("got %v", err)
	}
	full := MustParse(strings.Repeat("9", 100000))
	if p, err := full.Mul(MustParse("1")); err != nil || p.String() != full.String() {
		t.Fatalf("a number at the bound times 1 must succeed: %v", err)
	}
	if s, err := huge.Add(Zero); err != nil || s.String() != "1e+999999999" {
		t.Fatalf("adding zero never scales: %v", err)
	}
	if _, err := MustParse("9").Mul(MustParse("9")); err != nil {
		t.Fatal(err)
	}
	if _, err := MustParse("1").Add(MustParse("1e-100001")); !errors.Is(err, ErrTooManyDigits) {
		t.Fatalf("got %v", err)
	}
	if q, err := MustParse("1").Quo(MustParse("1e-999999999"), Context128); err != nil || q.String() != "1e+999999999" {
		t.Fatalf("1e999999999 is inside the exponent range: %v", err)
	}
	if _, err := MustParse("1e-999999999").Quo(MustParse("1e999999999"), Context128); !errors.Is(err, ErrExponentRange) {
		t.Fatalf("got %v", err)
	}
	// An exponent of four million digits costs seconds when it is built as
	// a big.Int, and milliseconds when it is kept as text.
	vast := "1.5e-" + strings.Repeat("7", 4000000)
	d, err := Parse(vast)
	if !errors.Is(err, ErrExponentRange) {
		t.Fatalf("got %v", err)
	}
	if d.Scientific() != strings.ToUpper(vast) || d.String() != vast {
		t.Fatal("a vast exponent must print as written")
	}
	wantEqual(t, d.Cmp(MustParse("-1")), 1)
	wantEqual(t, d.Neg().Cmp(d), -1)
	if el := time.Since(start); el > time.Second {
		t.Fatalf("bounds took %v, so a bound is checked after allocating", el)
	}
}

func TestBoundsCoverResultsAndOtherOperations(t *testing.T) {
	big9 := MustParse("9e999999999")
	if _, err := big9.Add(big9); !errors.Is(err, ErrExponentRange) {
		t.Fatalf("a sum past the exponent bound: got %v", err)
	}
	if _, err := MustParse("1.5e-999999999").Sub(MustParse("1.4e-999999999")); !errors.Is(err, ErrExponentRange) {
		t.Fatalf("a difference below the exponent bound: got %v", err)
	}
	if _, err := MustParse("1e999999999").Rem(MustParse("3")); !errors.Is(err, ErrTooManyDigits) {
		t.Fatalf("a remainder whose alignment crosses the digit bound: got %v", err)
	}
	if r, err := MustParse("2").Rem(MustParse("1e999999999")); err != nil || r.String() != "2" {
		t.Fatalf("a dividend smaller than the divisor is its own remainder: %v %v", r, err)
	}
	if _, err := MustParse("1e999999999").Quo(MustParse("0.1"), Context128); !errors.Is(err, ErrExponentRange) {
		t.Fatalf("a quotient past the exponent bound: got %v", err)
	}
	nines := strings.Repeat("9", 100000)
	if _, err := MustParse(nines).Add(MustParse("1")); !errors.Is(err, ErrTooManyDigits) {
		t.Fatalf("a carry past the digit bound: got %v", err)
	}
	q, err := MustParse("1").Quo(MustParse(nines), Context128)
	if err != nil || q.Scientific() != "1.000000000000000000000000000000000E-100000" {
		t.Fatalf("a quotient by a divisor at the digit bound: %v %v", q, err)
	}
	small := Context{Precision: 5, MaxDigits: 10, MaxExponent: 20}
	if q, err := MustParse("2").Quo(MustParse("3"), small); err != nil || q.String() != "0.66667" {
		t.Fatalf("a narrower context: %v %v", q, err)
	}
	if _, err := MustParse("1e15").Quo(MustParse("1e-15"), small); !errors.Is(err, ErrExponentRange) {
		t.Fatalf("a narrower context's exponent bound: got %v", err)
	}
	if q, err := MustParse("2").Quo(MustParse("3"), Context{}); err != nil || q.String() != "0.6666666666666666666666666666666667" {
		t.Fatalf("a zero context takes Context128's settings: %v %v", q, err)
	}
}

func TestZeroLiteralsClampTheirExponent(t *testing.T) {
	d, err := Parse("0e1000000000")
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, d.Scientific(), "0E+999999999") // jq 1.8.2 prints the same
	wantEqual(t, d.String(), "0")
	d, err = Parse("-0.0e-99999999999999999999999")
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, d.Scientific(), "-0E-999999999")
	if p, err := MustParse("0e999999999").Mul(MustParse("0e999999999")); err != nil || p.Scientific() != "0E+999999999" {
		t.Fatalf("a zero product clamps its exponent: %v %v", p, err)
	}
}

func TestOverBoundLiteralsKeepTheirText(t *testing.T) {
	long := "1." + strings.Repeat("2", 100001)
	d, err := Parse(long)
	if !errors.Is(err, ErrTooManyDigits) || !errors.Is(d.Err(), ErrTooManyDigits) {
		t.Fatalf("got %v and %v", err, d.Err())
	}
	wantEqual(t, d.Scientific(), long)
	wantEqual(t, d.String(), long)
	wantEqual(t, d.Neg().Scientific(), "-"+long)
	wantEqual(t, d.Float64(), 1.2222222222222223)
	wantEqual(t, d.Sign(), 1)
	wantEqual(t, d.IsZero(), false)
	wantEqual(t, d.IsNaN(), false)
	if isInf, _ := d.IsInf(); isInf {
		t.Fatal("an over-bound literal is not infinite")
	}
	if _, err := d.Add(Zero); !errors.Is(err, ErrTooManyDigits) {
		t.Fatalf("math on an over-bound literal fails with its bound: %v", err)
	}
	for name, op := range map[string]func(a, b Decimal) (Decimal, error){
		"Sub": Decimal.Sub, "Mul": Decimal.Mul, "Rem": Decimal.Rem,
		"Quo": func(a, b Decimal) (Decimal, error) { return a.Quo(b, Context128) },
	} {
		if _, err := op(MustParse("1"), d); !errors.Is(err, ErrTooManyDigits) {
			t.Errorf("%s: got %v", name, err)
		}
		if _, err := op(NaN(), d); !errors.Is(err, ErrTooManyDigits) {
			t.Errorf("%s with NaN: got %v", name, err)
		}
	}
	wantEqual(t, d.Cmp(MustParse("1.2222")), 1)
	wantEqual(t, d.Cmp(MustParse("1.3")), -1)
	wantEqual(t, MustParse("1.3").Cmp(d), 1)
	again, _ := Parse(long + "0")
	wantEqual(t, d.Cmp(again), 0)
	wantEqual(t, d.Neg().Cmp(d), -1)
	wantEqual(t, d.Cmp(Inf(1)), -1)
	wantEqual(t, d.Cmp(NaN()), 1)

	huge, err := Parse("-12.50e1000000000")
	if !errors.Is(err, ErrExponentRange) {
		t.Fatalf("got %v", err)
	}
	wantEqual(t, huge.Scientific(), "-1.250E+1000000001")
	wantEqual(t, huge.String(), "-1.25e+1000000001")
	wantEqual(t, huge.FloatString(), "-1.25e+1000000001")
	wantEqual(t, huge.Float64(), math.Inf(-1))
	wantEqual(t, huge.IsNegative(), true)
	wantEqual(t, huge.Cmp(MustParse("-1e999999999")), -1)

	tiny, err := Parse("1e-1000000000")
	if !errors.Is(err, ErrExponentRange) {
		t.Fatalf("got %v", err)
	}
	wantEqual(t, tiny.Scientific(), "1E-1000000000") // jq 1.8.2 prints the same
	wantEqual(t, tiny.Float64(), 0.0)
	wantEqual(t, tiny.Cmp(Zero), 1)
	wantEqual(t, tiny.Cmp(MustParse("1e-999999999")), -1)

	vast, err := Parse("1e123456789012345678901234567890")
	if !errors.Is(err, ErrExponentRange) {
		t.Fatalf("got %v", err)
	}
	wantEqual(t, vast.Scientific(), "1E+123456789012345678901234567890")
	wantEqual(t, vast.String(), "1e+123456789012345678901234567890")
	wantEqual(t, vast.Cmp(huge.Neg()), 1)
	if _, err := MustParse("1").Mul(vast); !errors.Is(err, ErrExponentRange) {
		t.Fatalf("got %v", err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("MustParse must panic on an over-bound literal")
		}
	}()
	MustParse("1e1000000000")
}

func TestWideExponentsCarryAndBorrow(t *testing.T) {
	for _, c := range []struct{ in, str, sci string }{
		{"1.5e10000000000000000000", "1.5e+10000000000000000000", "1.5E+10000000000000000000"},
		{"15e9999999999999999999", "1.5e+10000000000000000000", "1.5E+10000000000000000000"},
		{"1.50e10000000000000000000", "1.5e+10000000000000000000", "1.50E+10000000000000000000"},
		{"1.5e-1000000000000000000", "1.5e-1000000000000000000", "1.5E-1000000000000000000"},
		{"-12.5e-0000000000000000000099999", "-1.25e-99998", "-1.25E-99998"},
		{"0.000e-99999999999999999999", "0", "0E-999999999"},
	} {
		d, _ := Parse(c.in)
		if d.String() != c.str || d.Scientific() != c.sci {
			t.Errorf("%s printed %s and %s, want %s and %s", c.in, d.String(), d.Scientific(), c.str, c.sci)
		}
	}
	a, _ := Parse("1.5e10000000000000000000")
	b, _ := Parse("15e9999999999999999999")
	c, _ := Parse("1.6e9999999999999999999")
	wantEqual(t, a.Cmp(b), 0)
	wantEqual(t, a.Cmp(c), 1)
	wantEqual(t, c.Neg().Cmp(a.Neg()), 1)
	n, _ := Parse("1e-10000000000000000000")
	wantEqual(t, n.Cmp(c), -1)
	wantEqual(t, n.Cmp(Zero), 1)

	for _, c := range []struct {
		w    wideInt
		n    int64
		want string
	}{
		{wideInt{digits: "999"}, 1, "1000"},
		{wideInt{digits: "1000"}, -1, "999"},
		{wideInt{neg: true, digits: "1000"}, 1, "-999"},
		{wideInt{neg: true, digits: "5"}, 5, "0"},
		{wideInt{neg: true, digits: "5"}, 12, "7"},
		{wideInt{digits: "5"}, -12, "-7"},
		{wideInt{digits: "0"}, math.MinInt64, "-9223372036854775808"},
		{wideInt{digits: "0"}, 0, "0"},
	} {
		wantEqual(t, c.w.add(c.n).String(), c.want)
	}
}

func TestPrinters(t *testing.T) {
	for _, c := range []struct{ in, str, sci string }{
		{"0", "0", "0"},
		{"0.00", "0", "0.00"},
		{"-0.0", "-0", "-0.0"},
		{"0e5", "0", "0E+5"},
		{"123.4500", "123.45", "123.4500"},
		{"1.5e-6", "0.0000015", "0.0000015"},
		{"1.5e-7", "1.5e-07", "1.5E-7"},
		{"12e-8", "1.2e-07", "1.2E-7"},
		{"123456789012345678901", "123456789012345678901", "123456789012345678901"},
		{"1234567890123456789012", "1.234567890123456789012e+21", "1234567890123456789012"},
		{"5e2", "500", "5E+2"},
		{"-1.5e+300", "-1.5e+300", "-1.5E+300"},
	} {
		d := MustParse(c.in)
		wantEqual(t, d.String(), c.str)
		wantEqual(t, d.Scientific(), c.sci)
	}
	for _, s := range []func(Decimal) string{Decimal.String, Decimal.Scientific, Decimal.FloatString} {
		wantEqual(t, s(NaN()), "NaN")
		wantEqual(t, s(Inf(1)), "Infinity")
		wantEqual(t, s(Inf(-1)), "-Infinity")
	}
	// Each expectation is jq 1.8.2's output for the same double.
	for _, c := range []struct {
		f    float64
		want string
	}{
		{0, "0"}, {math.Copysign(0, -1), "-0"}, {1.5e17, "1.5e+17"}, {0.0001, "0.0001"}, {1.23e-5, "1.23e-05"},
		{1e301, "1e+301"}, {-1e16, "-1e+16"}, {123456789012345678, "123456789012345680"}, {12345678901234567, "12345678901234568"},
		{math.MaxFloat64, "1.7976931348623157e+308"}, {5e-324, "5e-324"}, {100, "100"}, {1.25, "1.25"},
	} {
		wantEqual(t, FromFloat64(c.f).FloatString(), c.want)
	}
}

func TestSpecialValueArithmetic(t *testing.T) {
	one, two := MustParse("1"), MustParse("2")
	check := func(name string, d Decimal, err error, want string) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		wantEqual(t, d.String(), want)
	}
	r, err := Inf(1).Add(Inf(-1))
	check("inf + -inf", r, err, "NaN")
	r, err = Inf(-1).Sub(one)
	check("-inf - 1", r, err, "-Infinity")
	r, err = Inf(1).Mul(Zero)
	check("inf * 0", r, err, "NaN")
	r, err = Inf(1).Mul(MustParse("-2"))
	check("inf * -2", r, err, "-Infinity")
	r, err = NaN().Mul(one)
	check("nan * 1", r, err, "NaN")
	r, err = one.Quo(Inf(-1), Context128)
	check("1 / -inf", r, err, "-0")
	r, err = Inf(1).Quo(MustParse("-2"), Context128)
	check("inf / -2", r, err, "-Infinity")
	r, err = Inf(1).Quo(Inf(1), Context128)
	check("inf / inf", r, err, "NaN")
	r, err = NaN().Quo(one, Context128)
	check("nan / 1", r, err, "NaN")
	if _, err := NaN().Quo(Zero, Context128); !errors.Is(err, ErrDivisionByZero) {
		t.Fatalf("a zero divisor fails before NaN propagates, as in jq: %v", err)
	}
	if _, err := Zero.Quo(NegZero, Context128); !errors.Is(err, ErrDivisionByZero) {
		t.Fatalf("0 / -0: %v", err)
	}
	r, err = MustParse("-0").Quo(two, Context128)
	check("-0 / 2", r, err, "-0")
	r, err = NaN().Rem(Zero)
	check("nan % 0", r, err, "NaN")
	if _, err := two.Rem(Zero); !errors.Is(err, ErrDivisionByZero) {
		t.Fatalf("2 %% 0: %v", err)
	}
	r, err = Inf(1).Rem(two)
	check("inf % 2", r, err, "NaN")
	r, err = MustParse("-2.5").Rem(Inf(1))
	check("-2.5 % inf", r, err, "-2.5")
	r, err = MustParse("-6").Rem(MustParse("3"))
	check("-6 % 3", r, err, "0") // jq prints 0, so a zero remainder is +0
	r, err = MustParse("-0").Rem(MustParse("3"))
	check("-0 % 3", r, err, "0")
	r, err = MustParse("-0.0").Rem(Inf(1))
	check("-0.0 % inf", r, err, "0")
	r, err = MustParse("-7.5").Rem(MustParse("2.5"))
	check("-7.5 % 2.5", r, err, "0")
	r, err = MustParse("-1e30").Rem(MustParse("5"))
	check("-1e30 % 5", r, err, "0")
	r, err = MustParse("5.5").Rem(two)
	check("5.5 % 2", r, err, "1.5")
	r, err = MustParse("1e30").Rem(MustParse("7"))
	check("1e30 % 7", r, err, "1")
	r, err = MustParse("-0").Add(NegZero)
	check("-0 + -0", r, err, "-0")
	r, err = MustParse("0").Sub(Zero)
	check("0 - 0", r, err, "0")
	r, err = MustParse("-0").Sub(Zero)
	check("-0 - 0", r, err, "-0")
	r, err = MustParse("1.5").Sub(MustParse("1.5"))
	check("1.5 - 1.5", r, err, "0")
	wantEqual(t, NegZero.String(), "-0")
	wantEqual(t, Inf(0).String(), "Infinity")
	wantEqual(t, NaN().Neg().IsNaN(), true)
	wantEqual(t, Inf(-1).Abs().String(), "Infinity")
}

func TestIntegralRounding(t *testing.T) {
	for _, c := range []struct{ in, floor, ceil, trunc, round string }{
		{"2.5", "2", "3", "2", "3"},
		{"-2.5", "-3", "-2", "-2", "-3"},
		{"-0.5", "-1", "-0", "-0", "-1"},
		{"-0.4", "-1", "-0", "-0", "-0"},
		{"0.4", "0", "1", "0", "0"},
		{"7", "7", "7", "7", "7"},
		{"1e1000", "1E+1000", "1E+1000", "1E+1000", "1E+1000"},
		{"1e-999999999", "0", "1", "0", "0"},
		{"-1e-999999999", "-1", "-0", "-0", "-0"},
		{"0.000", "0", "0", "0", "0"},
		{"123456789012345678901234567890.5", "123456789012345678901234567890", "123456789012345678901234567891", "123456789012345678901234567890", "123456789012345678901234567891"},
		{"-99999999999999999999.9", "-100000000000000000000", "-99999999999999999999", "-99999999999999999999", "-100000000000000000000"},
		{"18446744073709551615.5", "18446744073709551615", "18446744073709551616", "18446744073709551615", "18446744073709551616"},
	} {
		d := MustParse(c.in)
		wantEqual(t, d.Floor().Scientific(), c.floor)
		wantEqual(t, d.Ceil().Scientific(), c.ceil)
		wantEqual(t, d.Trunc().Scientific(), c.trunc)
		wantEqual(t, d.Round().Scientific(), c.round)
	}
	wantEqual(t, NaN().Floor().IsNaN(), true)
	wantEqual(t, Inf(-1).Ceil().String(), "-Infinity")
}

func TestPredicatesAndConversions(t *testing.T) {
	wantEqual(t, MustParse("1.000").IsInt(), true)
	wantEqual(t, MustParse("1.001").IsInt(), false)
	wantEqual(t, MustParse("1e-999999999").IsInt(), false)
	wantEqual(t, MustParse("1e999999999").IsInt(), true)
	wantEqual(t, MustParse("1"+strings.Repeat("0", 40)+"e-40").IsInt(), true)
	wantEqual(t, MustParse("1"+strings.Repeat("0", 39)+"1e-40").IsInt(), false)
	wantEqual(t, Inf(1).IsInt(), false)
	wantEqual(t, MustParse("-0").IsNegative(), true)
	wantEqual(t, MustParse("-0").Sign(), 0)
	wantEqual(t, MustParse("-0").IsZero(), true)
	wantEqual(t, NaN().Sign(), 0)
	wantEqual(t, NaN().IsNegative(), false)
	wantEqual(t, Inf(-1).Sign(), -1)
	_, s := Inf(-1).IsInf()
	wantEqual(t, s, -1)
	_, s = MustParse("1").IsInf()
	wantEqual(t, s, 0)
	for in, want := range map[string]int64{"9223372036854775807": math.MaxInt64, "1.0e3": 1000, "-12000e-3": -12, "0e999999999": 0, "1e18": 1e18} {
		got, ok := MustParse(in).Int64()
		wantEqual(t, ok, true)
		wantEqual(t, got, want)
	}
	for _, in := range []string{"1.5", "1e19", "-9223372036854775809", "1e40", "123456789012345678901234567890e-5"} {
		if _, ok := MustParse(in).Int64(); ok {
			t.Errorf("%s.Int64() reported ok", in)
		}
	}
	for _, i := range []int64{0, 1, -1, math.MaxInt64, math.MinInt64, 1234567890} {
		got, ok := FromInt64(i).Int64()
		wantEqual(t, ok, true)
		wantEqual(t, got, i)
	}
	wantEqual(t, FromInt64(math.MinInt64).String(), "-9223372036854775808")
	wantEqual(t, math.Signbit(MustParse("-0").Float64()), true)
	wantEqual(t, MustParse("1e-400").Float64(), 0.0)
	wantEqual(t, MustParse("0.1").Float64(), 0.1)
	wantEqual(t, MustParse("123456789012345678901234567890").Float64(), 1.2345678901234568e29)
	wantEqual(t, math.IsNaN(NaN().Float64()), true)
	wantEqual(t, math.IsNaN(FromFloat64(math.NaN()).Float64()), true)
	wantEqual(t, FromFloat64(math.Inf(-1)).String(), "-Infinity")
	wantEqual(t, FromFloat64(math.Copysign(0, -1)).String(), "-0")
	wantEqual(t, MustParse("1"+strings.Repeat("0", 30)).Reduce().Scientific(), "1E+30")
	wantEqual(t, MustParse("-0.000").Reduce().Scientific(), "-0")
	wantEqual(t, MustParse("12345678901234567890123").Reduce().Scientific(), "12345678901234567890123")
	wantEqual(t, Zero.Equal(NegZero), true)
	wantEqual(t, MustParse("1.0").Equal(MustParse("1")), true)
	wantEqual(t, MustParse("1").Equal(NaN()), false)
}

func TestCompareOrdersEveryForm(t *testing.T) {
	ordered := []string{"-1e1000", "-12345678901234567890123", "-1.5", "-1.4999999999999999999999999", "-1e-1000", "0", "1e-1000", "0.1", "1", "1.000000000000000000000000000001", "18446744073709551615", "18446744073709551616", "1e1000"}
	all := []Decimal{NaN(), Inf(-1)}
	for _, s := range ordered {
		all = append(all, MustParse(s))
	}
	all = append(all, Inf(1))
	for i := 1; i < len(all); i++ {
		for j := 1; j < len(all); j++ {
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if got := all[i].Cmp(all[j]); got != want {
				t.Errorf("%v.Cmp(%v) = %d, want %d", all[i], all[j], got, want)
			}
		}
		wantEqual(t, all[i].Cmp(NaN()), 1)
	}
	wantEqual(t, MustParse("1.10").Cmp(MustParse("1.1")), 0)
	wantEqual(t, MustParse("1e1000").Cmp(MustParse("10e999")), 0)
	wantEqual(t, MustParse("99999999999999999999").Cmp(MustParse("1.00000000000000000000e20")), -1)
}

// TestArithmeticAgreesWithExactRationals checks every operation against
// big.Rat on random operands, which is an independent exact model.
func TestArithmeticAgreesWithExactRationals(t *testing.T) {
	rng := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // G404: a fixed seed keeps the operands reproducible
	random := func() Decimal {
		n := 1 + rng.IntN(45)
		var b strings.Builder
		if rng.IntN(2) == 0 {
			b.WriteByte('-')
		}
		b.WriteByte("0123456789"[1+rng.IntN(9)])
		for range n - 1 {
			b.WriteByte("0123456789"[rng.IntN(10)])
		}
		b.WriteString("e")
		b.WriteString(big.NewInt(int64(rng.IntN(61) - 30)).String())
		if rng.IntN(20) == 0 {
			return MustParse("0e" + big.NewInt(int64(rng.IntN(11)-5)).String())
		}
		return MustParse(b.String())
	}
	for range 3000 {
		a, b := random(), random()
		ra, rb := rat(t, a), rat(t, b)
		s, err := a.Add(b)
		if err != nil || rat(t, s).Cmp(new(big.Rat).Add(ra, rb)) != 0 {
			t.Fatalf("%v + %v = %v (%v)", a.Scientific(), b.Scientific(), s.Scientific(), err)
		}
		d, err := a.Sub(b)
		if err != nil || rat(t, d).Cmp(new(big.Rat).Sub(ra, rb)) != 0 {
			t.Fatalf("%v - %v = %v (%v)", a.Scientific(), b.Scientific(), d.Scientific(), err)
		}
		p, err := a.Mul(b)
		if err != nil || rat(t, p).Cmp(new(big.Rat).Mul(ra, rb)) != 0 {
			t.Fatalf("%v * %v = %v (%v)", a.Scientific(), b.Scientific(), p.Scientific(), err)
		}
		wantEqual(t, a.Cmp(b), ra.Cmp(rb))
		wantEqual(t, a.Float64(), ratFloat(ra))
		back, err := Parse(a.String())
		if err != nil || back.Cmp(a) != 0 {
			t.Fatalf("String round trip of %v gave %v (%v)", a.Scientific(), back.Scientific(), err)
		}
		back, err = Parse(a.Scientific())
		if err != nil || back.Scientific() != a.Scientific() {
			t.Fatalf("Scientific round trip of %v gave %v (%v)", a.Scientific(), back.Scientific(), err)
		}
		checkIntegral(t, a, ra)
		if b.IsZero() {
			continue
		}
		checkQuotient(t, a, b, new(big.Rat).Quo(ra, rb))
		r, err := a.Rem(b)
		if err != nil {
			t.Fatalf("%v %% %v: %v", a.Scientific(), b.Scientific(), err)
		}
		q := new(big.Rat).Quo(ra, rb)
		tq := new(big.Int).Quo(q.Num(), q.Denom())
		want := new(big.Rat).Sub(ra, new(big.Rat).Mul(rb, new(big.Rat).SetInt(tq)))
		if rat(t, r).Cmp(want) != 0 || (!r.IsZero() && r.IsNegative() != a.IsNegative()) {
			t.Fatalf("%v %% %v = %v, want %v", a.Scientific(), b.Scientific(), r.Scientific(), want.FloatString(40))
		}
	}
}

func ratFloat(r *big.Rat) float64 {
	f, _ := r.Float64()
	return f
}

// checkQuotient verifies that q is the exact quotient rounded half-even to
// 34 significant digits: within half a unit in the last place, with an
// exact tie landing on an even digit.
func checkQuotient(t *testing.T, a, b Decimal, exact *big.Rat) {
	t.Helper()
	q, err := a.Quo(b, Context128)
	if err != nil {
		t.Fatalf("%v / %v: %v", a.Scientific(), b.Scientific(), err)
	}
	if q.IsZero() {
		if exact.Sign() != 0 {
			t.Fatalf("%v / %v = 0", a.Scientific(), b.Scientific())
		}
		return
	}
	digits := strings.TrimLeft(strings.ReplaceAll(strings.Split(strings.TrimPrefix(q.Scientific(), "-"), "E")[0], ".", ""), "0")
	if len(digits) > 34 {
		t.Fatalf("%v / %v = %v has %d digits", a.Scientific(), b.Scientific(), q.Scientific(), len(digits))
	}
	r := q.Reduce()
	ulpExp := r.adjusted() - 33
	ulp := new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(abs64(ulpExp)), nil))
	if ulpExp < 0 {
		ulp.Inv(ulp)
	}
	diff := new(big.Rat).Abs(new(big.Rat).Sub(rat(t, q), exact))
	half := new(big.Rat).Quo(ulp, big.NewRat(2, 1))
	switch diff.Cmp(half) {
	case 1:
		t.Fatalf("%v / %v = %v is more than half an ulp from %v", a.Scientific(), b.Scientific(), q.Scientific(), exact.FloatString(50))
	case 0:
		last := digits[len(digits)-1]
		if len(digits) == 34 && (last-'0')%2 != 0 {
			t.Fatalf("%v / %v = %v breaks a tie toward an odd digit", a.Scientific(), b.Scientific(), q.Scientific())
		}
	}
}

func checkIntegral(t *testing.T, d Decimal, r *big.Rat) {
	t.Helper()
	trunc := new(big.Int).Quo(r.Num(), r.Denom())
	frac := new(big.Rat).Sub(r, new(big.Rat).SetInt(trunc))
	floor, ceil := new(big.Int).Set(trunc), new(big.Int).Set(trunc)
	if frac.Sign() < 0 {
		floor.Sub(floor, big.NewInt(1))
	} else if frac.Sign() > 0 {
		ceil.Add(ceil, big.NewInt(1))
	}
	round := new(big.Int).Set(trunc)
	if new(big.Rat).Abs(frac).Cmp(big.NewRat(1, 2)) >= 0 {
		round.Add(round, big.NewInt(int64(frac.Sign())))
	}
	for name, c := range map[string]struct {
		got  Decimal
		want *big.Int
	}{"Floor": {d.Floor(), floor}, "Ceil": {d.Ceil(), ceil}, "Trunc": {d.Trunc(), trunc}, "Round": {d.Round(), round}} {
		if !c.got.IsInt() || rat(t, c.got).Cmp(new(big.Rat).SetInt(c.want)) != 0 {
			t.Fatalf("%v.%s() = %v, want %v", d.Scientific(), name, c.got.Scientific(), c.want)
		}
	}
}

func abs64(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}

func TestLessTravelledPaths(t *testing.T) {
	i, ok := MustParse("123456789012345678900000000000e-11").Int64()
	wantEqual(t, ok, true)
	wantEqual(t, i, int64(1234567890123456789))
	if _, ok := MustParse("123456789012345678901234567890").Int64(); ok {
		t.Fatal("a 30-digit integer does not fit an int64")
	}
	r, err := MustParse("1").Add(NaN())
	if err != nil || !r.IsNaN() {
		t.Fatalf("1 + nan: %v %v", r, err)
	}
	r, err = MustParse("1").Add(Inf(-1))
	if err != nil || r.String() != "-Infinity" {
		t.Fatalf("1 + -inf: %v %v", r, err)
	}
	twoTo70 := "1180591620717411303424"
	wantEqual(t, MustParse(twoTo70).Reduce().Scientific(), twoTo70)
	d, err := Parse("-5e-123456789012345678901234567890")
	if !errors.Is(err, ErrExponentRange) {
		t.Fatalf("got %v", err)
	}
	wantEqual(t, d.Scientific(), "-5E-123456789012345678901234567890")
	wantEqual(t, math.Signbit(d.Float64()), true)
	wantEqual(t, d.Float64(), 0.0)
	_, err = Parse(strings.Repeat("x", 100))
	if err == nil || len(err.Error()) > 100 || !strings.Contains(err.Error(), "...") {
		t.Fatalf("a long malformed literal is clipped in the error: %v", err)
	}
}
