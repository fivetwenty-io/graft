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
