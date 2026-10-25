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
func BenchmarkMul(b *testing.B) { benchOp(b, func(x, y Decimal) { _, _ = x.Mul(y) }) }
func BenchmarkQuo(b *testing.B) {
	benchOp(b, func(x, y Decimal) { _, _ = x.Quo(y, Context128) })
}
func BenchmarkCmp(b *testing.B) { benchOp(b, func(x, y Decimal) { _ = x.Cmp(y) }) }
