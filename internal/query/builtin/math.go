package builtin

import (
	"math"
	"strings"

	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/decimal"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// maxRepeat is the longest string that multiplying a string may build.
const maxRepeat = 1 << 29

func registerMath(r *runtime.Registry) {
	arith(r, "_plus", plus)
	arith(r, "_minus", minus)
	arith(r, "_multiply", multiply)
	arith(r, "_divide", divide)
	arith(r, "_mod", modulo)
	unary(r, "_negate", func(ctx *runtime.Ctx, in *value.Value) (*value.Value, error) {
		if in.Kind() != value.KindNumber {
			return nil, fail("%s cannot be negated", describe(in))
		}
		if ctx.Numbers == runtime.NumbersFloat {
			// Negating a zero gives a positive zero, as jq does.
			if f := in.Float(); f != 0 {
				return floatResult(-f), nil
			}
			return floatResult(0), nil
		}
		return negate(in), nil
	})

	constant(r, "infinite", value.NumDec(decimal.Inf(1)))
	constant(r, "nan", value.NumDec(decimal.NaN()))
	unary(r, "isinfinite", func(_ *runtime.Ctx, in *value.Value) (*value.Value, error) {
		inf, _ := in.Dec().IsInf()
		return value.Bool(in.Kind() == value.KindNumber && inf), nil
	})
	unary(r, "isfinite", func(_ *runtime.Ctx, in *value.Value) (*value.Value, error) {
		inf, _ := in.Dec().IsInf()
		return value.Bool(in.Kind() == value.KindNumber && !inf), nil
	})
	unary(r, "isnan", func(_ *runtime.Ctx, in *value.Value) (*value.Value, error) {
		return value.Bool(in.Kind() == value.KindNumber && in.Dec().IsNaN()), nil
	})
	unary(r, "isnormal", func(_ *runtime.Ctx, in *value.Value) (*value.Value, error) {
		if in.Kind() != value.KindNumber {
			return value.Bool(false), nil
		}
		f := math.Abs(in.Float())
		return value.Bool(f >= 0x1p-1022 && !math.IsInf(f, 0)), nil
	})

	exact := map[string]func(decimal.Decimal) decimal.Decimal{
		"floor": decimal.Decimal.Floor,
		"ceil":  decimal.Decimal.Ceil,
		"round": decimal.Decimal.Round,
		"trunc": decimal.Decimal.Trunc,
		"fabs":  decimal.Decimal.Abs,
	}
	for name, f := range exact {
		unary(r, name, func(ctx *runtime.Ctx, in *value.Value) (*value.Value, error) {
			d, err := numberArg(ctx, in)
			if err != nil {
				return nil, err
			}
			if ctx.Numbers == runtime.NumbersFloat {
				// A double result, even from a literal that fits a decimal.
				d = decimal.FromFloat64(in.Float())
			}
			return value.NumDec(f(d)), nil
		})
	}

	registerLibm(r)
}

// libmOneArg maps each one-argument libm name to its float64 function.
var libmOneArg = map[string]func(float64) float64{
	"acos":      math.Acos,
	"acosh":     math.Acosh,
	"asin":      math.Asin,
	"asinh":     math.Asinh,
	"atan":      math.Atan,
	"atanh":     atanh,
	"cbrt":      cbrt,
	"cos":       math.Cos,
	"cosh":      math.Cosh,
	"erf":       math.Erf,
	"erfc":      math.Erfc,
	"exp":       math.Exp,
	"exp2":      exp2,
	"exp10":     func(x float64) float64 { return math.Pow(10, x) },
	"expm1":     expm1,
	"gamma":     lgamma,
	"j0":        math.J0,
	"j1":        math.J1,
	"lgamma":    lgamma,
	"log":       math.Log,
	"log10":     math.Log10,
	"log1p":     math.Log1p,
	"log2":      math.Log2,
	"logb":      math.Logb,
	"nearbyint": math.RoundToEven,
	"rint":      math.RoundToEven,
	"significand": func(x float64) float64 {
		if x == 0 || math.IsInf(x, 0) || math.IsNaN(x) {
			return x
		}
		frac, _ := math.Frexp(x)
		return frac * 2
	},
	"sin":    math.Sin,
	"sinh":   math.Sinh,
	"sqrt":   math.Sqrt,
	"tan":    math.Tan,
	"tanh":   math.Tanh,
	"tgamma": math.Gamma,
	"y0":     math.Y0,
	"y1":     math.Y1,
}

// libmTwoArg maps each two-argument libm name to its float64 function.
var libmTwoArg = map[string]func(a, b float64) float64{
	"atan2":      math.Atan2,
	"copysign":   math.Copysign,
	"drem":       math.Remainder,
	"fdim":       fdim,
	"fmax":       fmax,
	"fmin":       fmin,
	"fmod":       math.Mod,
	"hypot":      math.Hypot,
	"jn":         func(n, x float64) float64 { return math.Jn(clampInt(n), x) },
	"ldexp":      ldexp,
	"nextafter":  math.Nextafter,
	"nexttoward": math.Nextafter,
	"pow":        math.Pow,
	"remainder":  math.Remainder,
	"scalb":      ldexp,
	"scalbln":    ldexp,
	"yn":         func(n, x float64) float64 { return math.Yn(clampInt(n), x) },
}

// registerLibm registers jq's libm names. Each one converts its operands to
// float64, computes there, and converts the result exactly.
func registerLibm(r *runtime.Registry) {
	for name, f := range libmOneArg {
		unary(r, name, func(ctx *runtime.Ctx, in *value.Value) (*value.Value, error) {
			if _, err := numberArg(ctx, in); err != nil {
				return nil, err
			}
			return floatResult(f(in.Float())), nil
		})
	}
	for name, f := range libmTwoArg {
		registerLibmArgs(r, name, 2, func(x []float64) *value.Value { return floatResult(f(x[0], x[1])) })
	}
	registerLibmArgs(r, "fma", 3, func(x []float64) *value.Value { return floatResult(math.FMA(x[0], x[1], x[2])) })

	arrayResult := map[string]func(float64) (float64, float64){
		"frexp": func(x float64) (float64, float64) {
			frac, exp := math.Frexp(x)
			return frac, float64(exp)
		},
		"modf": func(x float64) (float64, float64) {
			if math.IsInf(x, 0) {
				return math.Copysign(0, x), x
			}
			whole, frac := math.Modf(x)
			return frac, whole
		},
		"lgamma_r": func(x float64) (float64, float64) {
			lg, sign := math.Lgamma(x)
			return lg, float64(sign)
		},
	}
	for name, f := range arrayResult {
		unary(r, name, func(ctx *runtime.Ctx, in *value.Value) (*value.Value, error) {
			if _, err := numberArg(ctx, in); err != nil {
				return nil, err
			}
			a, b := f(in.Float())
			out := value.NewArrBuilder(2)
			out.Append(floatResult(a))
			out.Append(floatResult(b))
			return out.Seal(), nil
		})
	}
}

// registerLibmArgs registers a libm native over n number arguments, run
// on every combination of their outputs.
func registerLibmArgs(r *runtime.Registry, name string, n int, f func(x []float64) *value.Value) {
	r.RegisterSingle(name, n, func(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		return cartesian(in, args, func(vals []*value.Value) error {
			x := make([]float64, len(vals))
			for i, v := range vals {
				if _, err := numberArg(ctx, v); err != nil {
					return err
				}
				x[i] = v.Float()
			}
			return emit(f(x), nil)
		})
	})
}

func lgamma(x float64) float64 {
	lg, _ := math.Lgamma(x)
	return lg
}

// clampInt truncates f to an int, saturating at the 64-bit ends and
// treating NaN as zero.
func clampInt(f float64) int {
	if math.IsNaN(f) {
		return 0
	}
	return int(toInt64(f))
}

func ldexp(x, e float64) float64 { return math.Ldexp(x, clampInt(e)) }

func fdim(x, y float64) float64 {
	switch {
	case math.IsNaN(x) || math.IsNaN(y):
		return math.NaN()
	case x > y:
		return x - y
	default:
		return 0
	}
}

// fmin and fmax return the other operand when one is NaN, as C does.
func fmin(x, y float64) float64 {
	switch {
	case math.IsNaN(x):
		return y
	case math.IsNaN(y):
		return x
	default:
		return math.Min(x, y)
	}
}

func fmax(x, y float64) float64 {
	switch {
	case math.IsNaN(x):
		return y
	case math.IsNaN(y):
		return x
	default:
		return math.Max(x, y)
	}
}

// constant registers a native with no arguments that ignores its input.
func constant(r *runtime.Registry, name string, v *value.Value) {
	r.RegisterSingle(name, 0, func(_ *runtime.Ctx, _ *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		return emit(v, nil)
	})
}

// numberArg returns a math builtin's operand, which must be a number in
// bounds.
func numberArg(ctx *runtime.Ctx, v *value.Value) (decimal.Decimal, error) {
	if v.Kind() != value.KindNumber {
		return decimal.Decimal{}, fail("%s number required", describe(v))
	}
	return ctx.Number(v)
}

// floatResult wraps a double that libm or --float-numbers computed.
func floatResult(f float64) *value.Value {
	return value.NumDec(decimal.FromFloat64(f))
}

// arith registers a binary operator's native. The evaluator passes the
// left operand first and runs the right one outermost.
func arith(r *runtime.Registry, name string, op func(ctx *runtime.Ctx, l, r *value.Value) (*value.Value, error)) {
	r.RegisterSingle(name, 2, func(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		// The two loops are written out rather than run through cartesian,
		// since arithmetic runs once for every element an update touches.
		return args[1].Each(in, func(rv *value.Value, _ value.Path) error {
			return args[0].Each(in, func(lv *value.Value, _ value.Path) error {
				v, err := op(ctx, lv, rv)
				if err != nil {
					return err
				}
				return emit(v, nil)
			})
		})
	})
}

// operands returns two numbers' decimals, or the bound error of the first
// one past the bounds.
func operands(ctx *runtime.Ctx, l, r *value.Value) (decimal.Decimal, decimal.Decimal, error) {
	a, err := ctx.Number(l)
	if err != nil {
		return a, a, err
	}
	b, err := ctx.Number(r)
	return a, b, err
}

// decimalResult wraps an exact operation's result, turning a bound error
// from pkg/decimal into a runtime error with the package's text.
func decimalResult(d decimal.Decimal, err error) (*value.Value, error) {
	if err != nil {
		return nil, &query.RuntimeError{Msg: err.Error()}
	}
	return value.NumDec(d), nil
}

func bothNumbers(l, r *value.Value) bool {
	return l.Kind() == value.KindNumber && r.Kind() == value.KindNumber
}

func plus(ctx *runtime.Ctx, l, r *value.Value) (*value.Value, error) {
	switch {
	case l.Kind() == value.KindNull:
		return r, nil
	case r.Kind() == value.KindNull:
		return l, nil
	case bothNumbers(l, r):
		if ctx.Numbers == runtime.NumbersFloat {
			return floatResult(l.Float() + r.Float()), nil
		}
		a, b, err := operands(ctx, l, r)
		if err != nil {
			return nil, err
		}
		return decimalResult(a.Add(b))
	case l.Kind() != r.Kind():
	case l.Kind() == value.KindString:
		return value.Str(l.Str() + r.Str()), nil
	case l.Kind() == value.KindArray:
		// The sum carries no Meta. Append grows into l's spare room when l
		// ends its storage, so a loop that appends a step stays linear.
		if l.Meta() != nil {
			l = l.WithMeta(nil)
		}
		return l.Append(r.Elems()...), nil
	case l.Kind() == value.KindObject && r.Len() == 1:
		e := r.Entries()[0]
		return l.SetEntry(e.Key, e.Value), nil
	case l.Kind() == value.KindObject:
		b := value.NewObjBuilderFrom(l)
		for _, e := range r.Entries() {
			b.SetEntry(e.Key, e.Value)
		}
		return b.Seal(), nil
	}
	return nil, fail("%s and %s cannot be added", describe(l), describe(r))
}

func minus(ctx *runtime.Ctx, l, r *value.Value) (*value.Value, error) {
	switch {
	case bothNumbers(l, r):
		if ctx.Numbers == runtime.NumbersFloat {
			return floatResult(l.Float() - r.Float()), nil
		}
		a, b, err := operands(ctx, l, r)
		if err != nil {
			return nil, err
		}
		return decimalResult(a.Sub(b))
	case l.Kind() == value.KindArray && r.Kind() == value.KindArray:
		b := value.NewArrBuilder(l.Len())
		for _, e := range l.Elems() {
			keep := true
			for _, x := range r.Elems() {
				if ctx.Equal(e, x) {
					keep = false
					break
				}
			}
			if keep {
				b.Append(e)
			}
		}
		return b.Seal(), nil
	}
	return nil, fail("%s and %s cannot be subtracted", describe(l), describe(r))
}

func multiply(ctx *runtime.Ctx, l, r *value.Value) (*value.Value, error) {
	switch {
	case bothNumbers(l, r):
		if ctx.Numbers == runtime.NumbersFloat {
			return floatResult(l.Float() * r.Float()), nil
		}
		a, b, err := operands(ctx, l, r)
		if err != nil {
			return nil, err
		}
		return decimalResult(a.Mul(b))
	case l.Kind() == value.KindString && r.Kind() == value.KindNumber:
		return repeat(l, r)
	case l.Kind() == value.KindNumber && r.Kind() == value.KindString:
		return repeat(r, l)
	case l.Kind() == value.KindObject && r.Kind() == value.KindObject:
		return deepMerge(l, r), nil
	}
	return nil, fail("%s and %s cannot be multiplied", describe(l), describe(r))
}

// repeat is string times number. A count below one gives null when it is
// negative or NaN and an empty string otherwise, and a fractional count
// rounds down.
func repeat(s, n *value.Value) (*value.Value, error) {
	f := n.Float()
	if math.IsNaN(f) || f < 0 {
		return value.Null(), nil
	}
	count := math.Trunc(f)
	if count == 0 || s.Str() == "" {
		return value.Str(""), nil
	}
	if count*float64(len(s.Str())) > maxRepeat {
		return nil, fail("Repeat string result too long")
	}
	return value.Str(strings.Repeat(s.Str(), int(count))), nil
}

// deepMerge merges r into l, recursing where both hold an object under the
// same key.
func deepMerge(l, r *value.Value) *value.Value {
	b := value.NewObjBuilderFrom(l)
	for _, e := range r.Entries() {
		key := e.Key.Str()
		if cur, ok := l.Get(key); ok && cur.Kind() == value.KindObject && e.Value.Kind() == value.KindObject {
			b.Set(key, deepMerge(cur, e.Value))
			continue
		}
		b.SetEntry(e.Key, e.Value)
	}
	return b.Seal()
}

func divide(ctx *runtime.Ctx, l, r *value.Value) (*value.Value, error) {
	switch {
	case bothNumbers(l, r):
		a, b, err := operands(ctx, l, r)
		if err != nil {
			return nil, err
		}
		if b.IsZero() {
			return nil, fail("%s and %s cannot be divided because the divisor is zero", describe(l), describe(r))
		}
		if ctx.Numbers == runtime.NumbersFloat {
			return floatResult(l.Float() / r.Float()), nil
		}
		return decimalResult(a.Quo(b, decimal.Context128))
	case l.Kind() == value.KindString && r.Kind() == value.KindString:
		return splitString(l.Str(), r.Str()), nil
	}
	return nil, fail("%s and %s cannot be divided", describe(l), describe(r))
}

// splitString splits s at every sep, into codepoints for an empty sep, and
// gives an empty array for an empty s.
func splitString(s, sep string) *value.Value {
	if s == "" {
		return value.Arr()
	}
	var parts []string
	if sep == "" {
		parts = strings.Split(s, "")
	} else {
		parts = strings.Split(s, sep)
	}
	b := value.NewArrBuilder(len(parts))
	for _, p := range parts {
		b.Append(value.Str(p))
	}
	return b.Seal()
}

// modulo truncates both operands toward zero and takes the remainder,
// which has the dividend's sign, except that a zero result is +0. The
// infinities stand for the largest 64-bit integers, as jq treats them, and
// NaN on either side gives NaN. Under float numbers, both operands become
// 64-bit integers first, with saturation, so 1e30 % 7 is 0 as in jq.
func modulo(ctx *runtime.Ctx, l, r *value.Value) (*value.Value, error) {
	if !bothNumbers(l, r) {
		return nil, fail("%s and %s cannot be divided (remainder)", describe(l), describe(r))
	}
	a, b, err := operands(ctx, l, r)
	if err != nil {
		return nil, err
	}
	if a.IsNaN() || b.IsNaN() {
		return value.NumDec(decimal.NaN()), nil
	}
	if ctx.Numbers == runtime.NumbersFloat {
		x, y := toInt64(a.Float64()), toInt64(b.Float64())
		switch y {
		case 0:
			return nil, fail("%s and %s cannot be divided (remainder) because the divisor is zero", describe(l), describe(r))
		case -1:
			// The smallest integer divided by -1 overflows, and every
			// remainder by -1 is 0 anyway.
			return value.Num(0), nil
		default:
		}
		return floatResult(float64(x % y)), nil
	}
	a, b = saturate(a).Trunc(), saturate(b).Trunc()
	if b.IsZero() {
		return nil, fail("%s and %s cannot be divided (remainder) because the divisor is zero", describe(l), describe(r))
	}
	m, err := a.Rem(b)
	if err != nil {
		return nil, &query.RuntimeError{Msg: err.Error()}
	}
	if m.IsZero() {
		return value.Num(0), nil
	}
	return value.NumDec(m), nil
}

// toInt64 truncates f toward zero, and a value past either end of the
// 64-bit integers, an infinity among them, becomes the integer at that end.
func toInt64(f float64) int64 {
	switch {
	case f < math.MinInt64:
		return math.MinInt64
	case f >= math.MaxInt64:
		return math.MaxInt64
	default:
		return int64(f)
	}
}

// saturate turns an infinity into the 64-bit integer at its end.
func saturate(d decimal.Decimal) decimal.Decimal {
	if inf, sign := d.IsInf(); inf {
		if sign < 0 {
			return decimal.FromInt64(math.MinInt64)
		}
		return decimal.FromInt64(math.MaxInt64)
	}
	return d
}

// negate flips a number's sign. A literal stays a literal, so -1.0 prints as
// written and a number past the bounds keeps its text, while negating a
// zero gives a positive zero, as decNumber's minus does.
func negate(in *value.Value) *value.Value {
	lit := in.Literal()
	if lit == "" {
		return value.NumDec(in.Dec().Neg())
	}
	positive := strings.TrimPrefix(lit, "-")
	if positive != lit || in.Dec().IsZero() {
		return value.NumText(positive)
	}
	return value.NumText("-" + lit)
}
