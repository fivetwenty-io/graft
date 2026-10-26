package decimal_test

import (
	"errors"
	"fmt"
	"strings"

	"github.com/fivetwenty-io/graft/pkg/decimal"
)

func ExampleParse() {
	d, err := decimal.Parse("100000000000000000001")
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(d)
	fmt.Println(decimal.MustParse("3.0e2").Scientific())
	// Output:
	// 100000000000000000001
	// 3.0E+2
}

func ExampleDecimal_Add() {
	sum, err := decimal.MustParse("0.1").Add(decimal.MustParse("0.2"))
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(sum)
	// Output: 0.3
}

func ExampleDecimal_Quo() {
	for _, divisor := range []string{"4", "3"} {
		q, err := decimal.MustParse("1").Quo(decimal.MustParse(divisor), decimal.Context128)
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println(q)
	}
	// Output:
	// 0.25
	// 0.3333333333333333333333333333333333
}

func ExampleDecimal_String() {
	for _, s := range []string{"1e18", "1.50", "1E-7", "1e1000"} {
		fmt.Println(decimal.MustParse(s))
	}
	// Output:
	// 1000000000000000000
	// 1.5
	// 1e-07
	// 1e+1000
}

func ExampleDecimal_Err() {
	d, err := decimal.Parse("1" + strings.Repeat("0", 100000) + ".5")
	fmt.Println(errors.Is(err, decimal.ErrTooManyDigits), len(d.Scientific()))
	_, err = d.Add(decimal.MustParse("1"))
	fmt.Println(err)
	fmt.Println(errors.Is(d.Err(), decimal.ErrTooManyDigits))
	// Output:
	// true 100003
	// number exceeds 100000 significant digits
	// true
}

func ExampleContext() {
	third := decimal.Context{Precision: 5, Rounding: decimal.RoundHalfEven}
	q, err := decimal.MustParse("1").Quo(decimal.MustParse("3"), third)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(q)
	// Output: 0.33333
}

func ExampleRounding() {
	c := decimal.Context{Precision: 1, Rounding: decimal.RoundHalfEven}
	for _, s := range []string{"5", "15"} {
		q, err := decimal.MustParse(s).Quo(decimal.MustParse("10"), c)
		if err != nil {
			fmt.Println(err)
			return
		}
		fmt.Println(q)
	}
	// Output:
	// 0.5
	// 2
}

func ExampleDecimal() {
	var zero decimal.Decimal
	fmt.Println(zero, decimal.MustParse("1.50"), decimal.NegZero)
	// Output: 0 1.5 -0
}

func ExampleFromInt64() {
	fmt.Println(decimal.FromInt64(-9007199254740993))
	// Output: -9007199254740993
}

func ExampleFromFloat64() {
	fmt.Println(decimal.FromFloat64(0.1), decimal.FromFloat64(1e300).Scientific())
	// Output: 0.1 1E+300
}

func ExampleMustParse() {
	fmt.Println(decimal.MustParse("2.50").Scientific())
	// Output: 2.50
}

func ExampleInf() {
	fmt.Println(decimal.Inf(1), decimal.Inf(-1))
	// Output: Infinity -Infinity
}

func ExampleNaN() {
	n := decimal.NaN()
	fmt.Println(n, n.IsNaN(), n.Equal(decimal.NaN()))
	// Output: NaN true false
}

func ExampleDecimal_Abs() {
	fmt.Println(decimal.MustParse("-2.5").Abs())
	// Output: 2.5
}

func ExampleDecimal_Neg() {
	fmt.Println(decimal.MustParse("2.5").Neg(), decimal.MustParse("0").Neg())
	// Output: -2.5 -0
}

func ExampleDecimal_Sub() {
	d, err := decimal.MustParse("0.3").Sub(decimal.MustParse("0.1"))
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(d)
	// Output: 0.2
}

func ExampleDecimal_Mul() {
	d, err := decimal.MustParse("1.5").Mul(decimal.MustParse("-4"))
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(d)
	// Output: -6
}

func ExampleDecimal_Rem() {
	d, err := decimal.MustParse("7.5").Rem(decimal.MustParse("2"))
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(d)
	_, err = d.Rem(decimal.MustParse("0"))
	fmt.Println(errors.Is(err, decimal.ErrDivisionByZero))
	// Output:
	// 1.5
	// true
}

func ExampleDecimal_Cmp() {
	a, b := decimal.MustParse("1.10"), decimal.MustParse("1.1")
	fmt.Println(a.Cmp(b), a.Cmp(decimal.MustParse("2")), decimal.NaN().Cmp(a))
	// Output: 0 -1 -1
}

func ExampleDecimal_Equal() {
	fmt.Println(decimal.MustParse("0").Equal(decimal.NegZero), decimal.MustParse("1.0").Equal(decimal.MustParse("1")))
	// Output: true true
}

func ExampleDecimal_Sign() {
	fmt.Println(decimal.MustParse("-3").Sign(), decimal.NegZero.Sign(), decimal.MustParse("3").Sign())
	// Output: -1 0 1
}

func ExampleDecimal_IsNegative() {
	fmt.Println(decimal.NegZero.IsNegative(), decimal.MustParse("0").IsNegative())
	// Output: true false
}

func ExampleDecimal_IsZero() {
	fmt.Println(decimal.NegZero.IsZero(), decimal.MustParse("0.000").IsZero(), decimal.MustParse("1e-9").IsZero())
	// Output: true true false
}

func ExampleDecimal_IsInt() {
	fmt.Println(decimal.MustParse("2.0").IsInt(), decimal.MustParse("2.5").IsInt(), decimal.Inf(1).IsInt())
	// Output: true false false
}

func ExampleDecimal_IsInf() {
	fmt.Println(decimal.Inf(-1).IsInf())
	fmt.Println(decimal.MustParse("1").IsInf())
	// Output:
	// true -1
	// false 0
}

func ExampleDecimal_IsNaN() {
	fmt.Println(decimal.NaN().IsNaN(), decimal.MustParse("1").IsNaN())
	// Output: true false
}

func ExampleDecimal_Int64() {
	fmt.Println(decimal.MustParse("42").Int64())
	fmt.Println(decimal.MustParse("4.2").Int64())
	fmt.Println(decimal.MustParse("1e20").Int64())
	// Output:
	// 42 true
	// 0 false
	// 0 false
}

func ExampleDecimal_Float64() {
	fmt.Println(decimal.MustParse("0.1").Float64(), decimal.MustParse("1e400").Float64())
	// Output: 0.1 +Inf
}

func ExampleDecimal_FloatString() {
	for _, s := range []string{"1e16", "1.5e16", "0.00001", "0.3"} {
		fmt.Println(decimal.MustParse(s).FloatString())
	}
	// Output:
	// 1e+16
	// 15000000000000000
	// 1e-05
	// 0.3
}

func ExampleDecimal_Scientific() {
	for _, s := range []string{"3.0e2", "0.000001", "0.0000001", "100"} {
		fmt.Println(decimal.MustParse(s).Scientific())
	}
	// Output:
	// 3.0E+2
	// 0.000001
	// 1E-7
	// 100
}

func ExampleDecimal_Reduce() {
	d := decimal.MustParse("1.500")
	fmt.Println(d.Scientific(), d.Reduce().Scientific())
	// Output: 1.500 1.5
}

func ExampleDecimal_Floor() {
	fmt.Println(decimal.MustParse("2.7").Floor(), decimal.MustParse("-0.5").Floor())
	// Output: 2 -1
}

func ExampleDecimal_Ceil() {
	fmt.Println(decimal.MustParse("2.1").Ceil(), decimal.MustParse("-0.5").Ceil())
	// Output: 3 -0
}

func ExampleDecimal_Round() {
	fmt.Println(decimal.MustParse("2.5").Round(), decimal.MustParse("-2.5").Round(), decimal.MustParse("2.4").Round())
	// Output: 3 -3 2
}

func ExampleDecimal_Trunc() {
	fmt.Println(decimal.MustParse("2.9").Trunc(), decimal.MustParse("-2.9").Trunc())
	// Output: 2 -2
}
