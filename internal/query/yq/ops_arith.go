package yq

import (
	"math"
	"math/big"
	"strconv"
	"strings"

	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/decimal"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// asFloat marks a computed number as a float whatever its text.
func asFloat(v *value.Value) *value.Value { return v.WithMeta(&value.Meta{Tag: floatTag}) }

// registerArith wraps the arithmetic natives r already holds so that a
// result of two numbers takes the tag yq gives it. "+", "-", "*", and "%"
// give !!int for two !!int operands and !!float otherwise, and "/" always
// gives !!float. Exact decimals do the arithmetic, so an integer widens
// past int64, except under float numbers, where yq's int64 and float64
// model holds. Anything but two numbers goes to the wrapped native.
func registerArith(r *runtime.Registry) {
	for _, op := range []struct {
		name string
		sym  byte
	}{{"_plus", '+'}, {"_minus", '-'}, {"_multiply", '*'}, {"_divide", '/'}, {"_mod", '%'}} {
		base, ok := r.Lookup(op.name, 2)
		if !ok {
			panic("yq: " + op.name + " is not registered")
		}
		r.RegisterSingle(op.name, 2, func(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
			return args[0].Each(in, func(lv *value.Value, lp value.Path) error {
				return args[1].Each(in, func(rv *value.Value, rp value.Path) error {
					switch {
					case lv.Kind() == value.KindNumber && rv.Kind() == value.KindNumber:
						return numeric(ctx, base, op.sym, lv, rv, emit)
					case op.sym == '+' || op.sym == '-':
						if v, ok, err := dateArith(ctx, lv, rv, op.sym == '-'); ok || err != nil {
							if err != nil {
								return err
							}
							return emit(v, nil)
						}
						if op.sym == '+' {
							return add(ctx, base, lv, lp, rv, rp, emit)
						}
					default:
					}
					return base(ctx, in, []runtime.Thunk{constant{lv}, constant{rv}}, emit)
				})
			})
		})
	}
}

// add is yq's + for anything but two numbers. null takes the other
// side's value, a sequence takes on a sequence's elements or any other
// node, a map merges a map, and a scalar joins its text to a string's.
// A sequence or a map keeps the left side's style.
// Anything else fails, naming the place each collection came from.
func add(ctx *runtime.Ctx, base runtime.Native, l *value.Value, lp value.Path, r *value.Value, rp value.Path, emit runtime.Emit) error {
	collection := func(v *value.Value) bool { return v.Kind() == value.KindArray || v.Kind() == value.KindObject }
	switch {
	case l.Kind() == value.KindNull:
		return emit(r, nil)
	case l.Kind() == value.KindArray && r.Kind() != value.KindArray:
		r = value.Arr(r)
	case l.Kind() == value.KindArray || (l.Kind() == value.KindObject && r.Kind() == value.KindObject):
	case collection(l) || collection(r):
		return &query.RuntimeError{Msg: tagOf(r) + " (" + pathText(rp) + ") cannot be added to a " + tagOf(l) + " (" + pathText(lp) + ")"}
	case tagOf(l) == strTag || tagOf(r) == strTag:
		return emit(keepAnchor(l, value.Str(addText(l)+addText(r))), nil)
	default:
		return &query.RuntimeError{Msg: tagOf(l) + " cannot be added to " + tagOf(r)}
	}
	return base(ctx, nil, []runtime.Thunk{constant{l}, constant{r}}, func(v *value.Value, p value.Path) error {
		return emit(keepAnchor(l, like(l, v)), p)
	})
}

// addText is a scalar's text as + joins it, where null has none.
func addText(v *value.Value) string {
	if v.Kind() == value.KindNull {
		return ""
	}
	return nodeText(v)
}

// numeric emits l op r tagged as yq tags it.
func numeric(ctx *runtime.Ctx, base runtime.Native, op byte, l, r *value.Value, emit runtime.Emit) error {
	isInt := func(v *value.Value) bool { return tagOf(v) == "!!int" }
	ints := op != '/' && isInt(l) && isInt(r)
	if ints && op == '%' && r.Dec().IsZero() {
		return &query.RuntimeError{Msg: "cannot modulo by 0"}
	}
	var v *value.Value
	var err error
	switch {
	case ctx.Numbers == runtime.NumbersFloat:
		v, err = floatNumeric(op, ints, l, r)
	case op == '%' && !ints:
		v = decRem(l, r)
	default:
	}
	if err != nil {
		return err
	}
	if v != nil {
		return emit(numericResult(ints, l, v), nil)
	}
	args := []runtime.Thunk{constant{l}, constant{r}}
	return base(ctx, nil, args, func(v *value.Value, p value.Path) error {
		return emit(numericResult(ints, l, v), p)
	})
}

// numericResult tags v, the result of an arithmetic operator whose left
// operand is l, as yq does: an integer keeps l's base and any other
// number is a float.
func numericResult(ints bool, l, v *value.Value) *value.Value {
	return keepAnchor(l, numberResult(ints, l, v))
}

// keepAnchor gives v the anchor l defines, as yq's arithmetic does for
// the node it builds from its left operand.
func keepAnchor(l, v *value.Value) *value.Value {
	lm := l.Meta()
	if lm == nil || lm.Anchor == "" {
		return v
	}
	m := metaOf(v)
	m.Anchor = lm.Anchor
	return withMeta(v, m)
}

// numberResult is numericResult without the anchor.
func numberResult(ints bool, l, v *value.Value) *value.Value {
	switch {
	case ints && v.Kind() == value.KindNumber && strings.HasPrefix(numberText(v), "-") && v.Dec().IsZero():
		// An integer has no negative zero, which a float does.
		return keepBase(l, value.NumDec(decimal.FromInt64(0)))

	case v.Kind() != value.KindNumber:
		return v
	case ints:
		return keepBase(l, v)
	case v.Meta() != nil && v.Meta().Tag == floatTag:
		return v
	default:
		return asFloat(value.NumDec(v.Dec().Reduce()))
	}
}

// keepBase writes an integer result in the base the left operand's text
// is written in, as yq does for 0x and 0o, with hexadecimal digits in
// upper case. A negative result stays in decimal, since yq's 0x-A does
// not read back as a number.
func keepBase(l, v *value.Value) *value.Value {
	m := l.Meta()
	if m == nil || len(m.Text) < 2 || m.Text[0] != '0' {
		return v
	}
	var base int
	switch m.Text[1] {
	case 'x', 'X':
		base = 16
	case 'o':
		base = 8
	default:
		return v
	}
	n, ok := v.Dec().Int64()
	if !ok || n < 0 {
		return v
	}
	return v.WithMeta(&value.Meta{Text: "0" + strings.ToLower(m.Text[1:2]) + strings.ToUpper(strconv.FormatInt(n, base))})
}

// decRem is the !!float remainder of l and r in exact decimals, or nil
// for a zero divisor, which the wrapped native reports.
func decRem(l, r *value.Value) *value.Value {
	if r.Dec().IsZero() {
		return nil
	}
	d, err := l.Dec().Rem(r.Dec())
	if err != nil {
		return nil
	}
	return asFloat(value.NumDec(d.Reduce()))
}

// floatNumeric is l op r in yq's own model of int64s and float64s, or
// nil for a zero divisor, which the wrapped native reports.
func floatNumeric(op byte, ints bool, l, r *value.Value) (*value.Value, error) {
	if ints {
		return int64Op(op, l, r)
	}
	if v, ok := float64Op(op, l.Float(), r.Float()); ok {
		return asFloat(v), nil
	}
	return nil, nil
}

// int64Op is l op r on yq's int64s. It fails when the result overflows,
// and gives nil for a zero divisor, which the wrapped native reports.
func int64Op(op byte, l, r *value.Value) (*value.Value, error) {
	a, aok := l.Dec().Int64()
	b, bok := r.Dec().Int64()
	if !aok || !bok {
		return nil, &query.RuntimeError{Msg: numberText(l) + " " + string(op) + " " + numberText(r) + " overflows int64"}
	}
	x, y := big.NewInt(a), big.NewInt(b)
	switch op {
	case '+':
		x.Add(x, y)
	case '-':
		x.Sub(x, y)
	case '*':
		x.Mul(x, y)
	default:
		if b == 0 {
			return nil, nil
		}
		x.Rem(x, y)
	}
	if !x.IsInt64() {
		return nil, &query.RuntimeError{Msg: numberText(l) + " " + string(op) + " " + numberText(r) + " overflows int64"}
	}
	return value.NumDec(decimal.FromInt64(x.Int64())), nil
}

// float64Op is l op r on float64s, and reports false for a zero divisor,
// which the wrapped native reports.
func float64Op(op byte, l, r float64) (*value.Value, bool) {
	var f float64
	switch op {
	case '+':
		f = l + r
	case '-':
		f = l - r
	case '*':
		f = l * r
	case '/':
		if r == 0 {
			return nil, false
		}
		f = l / r
	default:
		if r == 0 {
			return nil, false
		}
		f = math.Mod(l, r)
	}
	return value.Num(f), true
}
