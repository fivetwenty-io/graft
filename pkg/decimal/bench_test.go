package decimal

import (
	"fmt"
	"strings"
	"testing"
)

// benchDigits are the operand sizes: one digit, decimal128's precision,
// and the digit bound.
var benchDigits = []int{1, 34, 100_000}

// benchOperand is a number of n digits with no trailing zeros.
func benchOperand(n int) Decimal {
	s := strings.Repeat("7", n)
	if n > 1 {
		s = "1." + s[1:]
	}
	return MustParse(s)
}

func benchOp(b *testing.B, op func(x, y Decimal)) {
	for _, n := range benchDigits {
		x, y := benchOperand(n), benchOperand(n)
		b.Run(fmt.Sprintf("digits=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				op(x, y)
			}
		})
	}
}

func BenchmarkAdd(b *testing.B) { benchOp(b, func(x, y Decimal) { _, _ = x.Add(y) }) }

// BenchmarkMul has three sizes that fit and one that does not. The
// digits=49998 case is the bound-sized figure, since its product has 99,996
// digits and so fits the bound. The over-bound case multiplies two
// 100,000-digit operands, whose product exceeds the bound, and times only
// the rejection path.
func BenchmarkMul(b *testing.B) {
	for _, n := range []int{1, 34, 49_998} {
		x, y := benchOperand(n), benchOperand(n)
		b.Run(fmt.Sprintf("digits=%d", n), func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				_, _ = x.Mul(y)
			}
		})
	}
	x := benchOperand(100_000)
	b.Run("over-bound", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			if _, err := x.Mul(x); err == nil {
				b.Fatal("the product fits the bound")
			}
		}
	})
}

func BenchmarkQuo(b *testing.B) {
	benchOp(b, func(x, y Decimal) { _, _ = x.Quo(y, Context128) })
}
func BenchmarkCmp(b *testing.B) { benchOp(b, func(x, y Decimal) { _ = x.Cmp(y) }) }
