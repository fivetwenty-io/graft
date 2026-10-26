package yq

import (
	"math"
	"math/big"

	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/decimal"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// asFloat marks a computed number as a float whatever its text.
func asFloat(v *value.Value) *value.Value { return v.WithMeta(&value.Meta{Tag: "!!float"}) }

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
			return args[1].Each(in, func(rv *value.Value, _ value.Path) error {
				return args[0].Each(in, func(lv *value.Value, _ value.Path) error {
					if lv.Kind() != value.KindNumber || rv.Kind() != value.KindNumber {
						return base(ctx, in, []runtime.Thunk{constant{lv}, constant{rv}}, emit)
					}
					return numeric(ctx, base, op.sym, lv, rv, emit)
				})
			})
		})
	}
}

// numeric emits l op r tagged as yq tags it.
func numeric(ctx *runtime.Ctx, base runtime.Native, op byte, l, r *value.Value, emit runtime.Emit) error {
	isInt := func(v *value.Value) bool { return tagOf(v) == "!!int" }
	ints := op != '/' && isInt(l) && isInt(r)
	if ints && op == '%' && r.Dec().IsZero() {
		return &query.RuntimeError{Msg: "cannot modulo by 0"}
	}
	if ctx.Numbers == runtime.NumbersFloat {
		if v, err := floatNumeric(op, ints, l, r); err != nil || v != nil {
			if err != nil {
				return err
			}
			return emit(v, nil)
		}
	}
	if op == '%' && !ints {
		if v := decRem(l, r); v != nil {
			return emit(v, nil)
		}
	}
	args := []runtime.Thunk{constant{l}, constant{r}}
	return base(ctx, nil, args, func(v *value.Value, p value.Path) error {
		if ints || v.Kind() != value.KindNumber {
			return emit(v, p)
		}
		return emit(asFloat(value.NumDec(v.Dec().Reduce())), p)
	})
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
