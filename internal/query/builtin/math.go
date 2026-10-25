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
			return floatResult(-in.Float()), nil
		}
		return value.NumDec(in.Dec().Neg()), nil
	})

	constant(r, "infinite", value.NumDec(decimal.Inf(1)))
	constant(r, "nan", value.NumDec(decimal.NaN()))
	unary(r, "isinfinite", func(_ *runtime.Ctx, in *value.Value) (*value.Value, error) {
		inf, _ := in.Dec().IsInf()
		return value.Bool(in.Kind() == value.KindNumber && inf), nil
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
			return value.NumDec(f(d)), nil
		})
	}

	libm := map[string]func(float64) float64{
		"sqrt":  math.Sqrt,
		"log":   math.Log,
		"log2":  math.Log2,
		"log10": math.Log10,
		"exp":   math.Exp,
		"exp2":  math.Exp2,
		"exp10": func(x float64) float64 { return math.Pow(10, x) },
	}
	for name, f := range libm {
		unary(r, name, func(ctx *runtime.Ctx, in *value.Value) (*value.Value, error) {
			if _, err := numberArg(ctx, in); err != nil {
				return nil, err
			}
			return floatResult(f(in.Float())), nil
		})
	}
	r.RegisterSingle("pow", 2, func(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		return cartesian(in, args, func(vals []*value.Value) error {
			for _, v := range vals {
				if _, err := numberArg(ctx, v); err != nil {
					return err
				}
			}
			return emit(floatResult(math.Pow(vals[0].Float(), vals[1].Float())), nil)
		})
	})
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
		b := value.NewArrBuilder(l.Len() + r.Len())
		for _, e := range l.Elems() {
			b.Append(e)
		}
		for _, e := range r.Elems() {
			b.Append(e)
		}
		return b.Seal(), nil
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
// NaN on either side gives NaN.
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
	a, b = saturate(a).Trunc(), saturate(b).Trunc()
	if b.IsZero() {
		return nil, fail("%s and %s cannot be divided (remainder) because the divisor is zero", describe(l), describe(r))
	}
	if ctx.Numbers == runtime.NumbersFloat {
		return floatResult(math.Mod(a.Float64(), b.Float64()) + 0), nil
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
