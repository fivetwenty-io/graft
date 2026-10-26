package builtin

import (
	"math"
	"math/big"
)

// The functions in this file return the float64 nearest to the exact
// result, which Go's math does not promise for these four. Each computes
// in math/big at a rising precision until the result, widened by its
// error bound, rounds to the same float64 from both ends.

// guardBits is the working precision kept beyond the precision whose
// error bound a series claims. It covers the rounding of every operation
// the series and its argument reduction perform.
const guardBits = 32

// roundNearest returns the float64 nearest to the value that eval
// computes. eval(prec) must return a value whose relative error is below
// 2^-prec. A result that is exactly a float64, or exactly halfway between
// two, would never settle, so callers handle those inputs before this.
func roundNearest(eval func(prec uint) *big.Float) float64 {
	for prec := uint(96); ; prec *= 2 {
		y := eval(prec)
		if prec >= 4096 {
			f, _ := y.Float64()
			return f
		}
		eps := new(big.Float).Abs(y)
		eps.SetMantExp(eps, -int(prec))
		w := prec + guardBits
		lo, _ := new(big.Float).SetPrec(w).Sub(y, eps).Float64()
		hi, _ := new(big.Float).SetPrec(w).Add(y, eps).Float64()
		if lo == hi {
			return lo
		}
	}
}

// bigLn2 returns ln 2 at precision prec, as 2·atanh(1/3).
func bigLn2(prec uint) *big.Float {
	third := new(big.Float).SetPrec(prec).Quo(big.NewFloat(1), big.NewFloat(3))
	ln2 := atanhSeries(third, prec)
	return ln2.SetMantExp(ln2, 1)
}

// atanhSeries returns atanh(x) for |x| <= 1/2 at precision prec, from
// the sum of x^(2k+1)/(2k+1), whose terms all share x's sign.
func atanhSeries(x *big.Float, prec uint) *big.Float {
	sum := new(big.Float).SetPrec(prec).Set(x)
	if x.Sign() == 0 {
		return sum
	}
	x2 := new(big.Float).SetPrec(prec).Mul(x, x)
	pow := new(big.Float).SetPrec(prec).Set(x)
	term := new(big.Float).SetPrec(prec)
	for k := int64(1); ; k++ {
		pow.Mul(pow, x2)
		term.Quo(pow, new(big.Float).SetInt64(2*k+1))
		if negligible(term, sum, prec) {
			return sum
		}
		sum.Add(sum, term)
	}
}

// expm1Small returns e^r - 1 for |r| <= 1 at precision prec. It halves r
// until the series converges quickly, sums it, and undoes each halving
// with e^2y - 1 = (e^y - 1)(e^y - 1 + 2), which keeps the relative error
// small even when the result is tiny.
func expm1Small(r *big.Float, prec uint) *big.Float {
	y := new(big.Float).SetPrec(prec).Set(r)
	if y.Sign() == 0 {
		return y
	}
	halvings := 0
	for y.MantExp(nil) > -10 {
		y.SetMantExp(y, -1)
		halvings++
	}
	sum := new(big.Float).SetPrec(prec).Set(y)
	term := new(big.Float).SetPrec(prec).Set(y)
	for n := int64(2); ; n++ {
		term.Mul(term, y)
		term.Quo(term, new(big.Float).SetInt64(n))
		if negligible(term, sum, prec) {
			break
		}
		sum.Add(sum, term)
	}
	two := big.NewFloat(2)
	t := new(big.Float).SetPrec(prec)
	for ; halvings > 0; halvings-- {
		t.Add(sum, two)
		sum.Mul(sum, t)
	}
	return sum
}

// negligible reports whether term no longer changes sum at precision prec.
func negligible(term, sum *big.Float, prec uint) bool {
	return term.Sign() == 0 || term.MantExp(nil) < sum.MantExp(nil)-int(prec)-2
}

// exp2 returns 2^x correctly rounded.
func exp2(x float64) float64 {
	switch {
	case math.IsNaN(x) || math.IsInf(x, 1) || x >= 1024:
		return math.Inf(1) + x
	case x < -1075:
		// Below 2^-1075, half the smallest subnormal, which rounds to zero.
		return 0
	}
	n := math.Round(x)
	whole := new(big.Float).SetMantExp(big.NewFloat(1), int(n))
	if n == x {
		// A power of two is exact, though it may round as a subnormal.
		f, _ := whole.Float64()
		return f
	}
	// 2^x = 2^n · e^(f·ln 2), where f = x - n lies in [-1/2, 1/2]. f is
	// exact, because it is x itself when n is zero, and otherwise a
	// multiple of x's ulp no larger than x.
	frac := big.NewFloat(x - n)
	return roundNearest(func(prec uint) *big.Float {
		w := prec + guardBits
		r := new(big.Float).SetPrec(w).Mul(frac, bigLn2(w))
		y := expm1Small(r, w)
		y.Add(y, big.NewFloat(1))
		return y.Mul(y, whole)
	})
}

// expm1 returns e^x - 1 correctly rounded.
func expm1(x float64) float64 {
	switch {
	case x == 0 || math.IsNaN(x) || math.IsInf(x, 1):
		return x
	case x > 710:
		return math.Inf(1)
	case x < -40:
		// e^x is below half an ulp of -1 here, so -1 is nearest.
		return -1
	}
	bx := big.NewFloat(x)
	if math.Abs(x) <= 0.5 {
		return roundNearest(func(prec uint) *big.Float {
			return expm1Small(bx, prec+guardBits)
		})
	}
	// e^x - 1 = 2^n · e^r - 1, where r = x - n·ln 2 lies within ln 2 of
	// zero. Neither term cancels the other, because n is nonzero and x
	// is at least 1/2 away from zero.
	n := math.Round(x / math.Ln2)
	return roundNearest(func(prec uint) *big.Float {
		w := prec + guardBits
		// r gets 12 more bits, since n·ln 2 is up to 2^10 times r's size.
		r := new(big.Float).SetPrec(w+12).Mul(big.NewFloat(n), bigLn2(w+12))
		r.Sub(bx, r)
		y := expm1Small(r, w)
		y.Add(y, big.NewFloat(1))
		y.SetMantExp(y, int(n))
		return y.Sub(y, big.NewFloat(1))
	})
}

// atanh returns atanh(x) correctly rounded.
func atanh(x float64) float64 {
	ax := math.Abs(x)
	switch {
	case x == 0 || math.IsNaN(x):
		return x
	case ax > 1:
		return math.NaN()
	case ax == 1:
		return math.Copysign(math.Inf(1), x)
	}
	if ax <= 0.5 {
		bx := big.NewFloat(x)
		return roundNearest(func(prec uint) *big.Float {
			return atanhSeries(bx, prec+guardBits)
		})
	}
	// atanh(|x|) = ln((1 + |x|)/(1 - |x|))/2, where the quotient is at
	// least 3. Both sums are exact at 64 bits, since |x| lies in (1/2, 1).
	num := new(big.Float).SetPrec(64).Add(big.NewFloat(1), big.NewFloat(ax))
	den := new(big.Float).SetPrec(64).Sub(big.NewFloat(1), big.NewFloat(ax))
	r := roundNearest(func(prec uint) *big.Float {
		w := prec + guardBits
		y := bigLog(new(big.Float).SetPrec(w).Quo(num, den), w)
		return y.SetMantExp(y, -1)
	})
	return math.Copysign(r, x)
}

// bigLog returns ln(t) for t >= 3 at precision prec. It writes t as
// m·2^e with m in [1/√2, √2), so ln(t) = e·ln 2 + 2·atanh((m-1)/(m+1)),
// where the atanh argument is at most 0.18 and e·ln 2 dominates.
func bigLog(t *big.Float, prec uint) *big.Float {
	m := new(big.Float).SetPrec(prec)
	e := t.MantExp(m)
	if m.Cmp(big.NewFloat(math.Sqrt2/2)) < 0 {
		m.SetMantExp(m, 1)
		e--
	}
	one := big.NewFloat(1)
	s := new(big.Float).SetPrec(prec).Sub(m, one)
	s.Quo(s, new(big.Float).SetPrec(prec).Add(m, one))
	y := atanhSeries(s, prec)
	y.SetMantExp(y, 1)
	ln2e := bigLn2(prec)
	ln2e.Mul(ln2e, new(big.Float).SetInt64(int64(e)))
	return y.Add(y, ln2e)
}

// cbrt returns the cube root of x correctly rounded. A cube root is never
// halfway between two float64 values, so the nearest one is the c whose
// rounding interval holds the root, which the cubes of the interval's
// ends decide exactly.
func cbrt(x float64) float64 {
	if x == 0 || math.IsNaN(x) || math.IsInf(x, 0) {
		return x
	}
	ax := math.Abs(x)
	bx := big.NewFloat(ax)
	c := math.Cbrt(ax)
	for range 4 {
		if cube(midpoint(c, math.Inf(1))).Cmp(bx) < 0 {
			c = math.Nextafter(c, math.Inf(1))
			continue
		}
		if cube(midpoint(c, 0)).Cmp(bx) > 0 {
			c = math.Nextafter(c, 0)
			continue
		}
		break
	}
	return math.Copysign(c, x)
}

// midpoint returns the exact value halfway between c and its neighbor
// toward dir.
func midpoint(c, dir float64) *big.Float {
	m := new(big.Float).SetPrec(64).Add(big.NewFloat(c), big.NewFloat(math.Nextafter(c, dir)))
	return m.SetMantExp(m, -1)
}

// cube returns m³ exactly, since m carries at most 54 bits.
func cube(m *big.Float) *big.Float {
	y := new(big.Float).SetPrec(192).Mul(m, m)
	return y.Mul(y, m)
}
