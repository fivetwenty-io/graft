// Package builtin holds the natives that the query dialects share. It
// imports runtime and never eval, so the import graph stays builtin ->
// runtime <- eval, and a dialect registers these natives next to its own.
//
// Every native follows the contract on runtime.Native. It returns the
// error its emit or an argument returned unchanged, and it raises only
// *query.RuntimeError values of its own. A native that emits at most once
// for each combination of its arguments' outputs is registered with
// RegisterSingle, so a tail call can loop behind it.
package builtin

import (
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/decimal"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// RegisterCore adds the shared natives to r.
func RegisterCore(r *runtime.Registry) {
	registerCore(r)
	registerMath(r)
	registerPaths(r)
	registerStrings(r)
	registerCollections(r)
	registerStreams(r)
	registerFormats(r)
	registerDates(r)
}

func registerCore(r *runtime.Registry) {
	r.RegisterSingle("empty", 0, func(*runtime.Ctx, *value.Value, []runtime.Thunk, runtime.Emit) error { return nil })
	unary(r, "not", func(ctx *runtime.Ctx, in *value.Value) (*value.Value, error) {
		return value.Bool(!ctx.Dialect.Truthy(in)), nil
	})
	unary(r, "length", length)
	unary(r, "utf8bytelength", func(_ *runtime.Ctx, in *value.Value) (*value.Value, error) {
		if in.Kind() != value.KindString {
			return nil, fail("%s only strings have UTF-8 byte length", describe(in))
		}
		return value.Num(float64(len(in.Str()))), nil
	})
	unary(r, "type", func(_ *runtime.Ctx, in *value.Value) (*value.Value, error) {
		return value.Str(runtime.TypeName(in)), nil
	})
	r.RegisterSingle("error", 0, func(_ *runtime.Ctx, in *value.Value, _ []runtime.Thunk, _ runtime.Emit) error {
		return Raise(in)
	})
	r.RegisterSingle("error", 1, func(_ *runtime.Ctx, in *value.Value, args []runtime.Thunk, _ runtime.Emit) error {
		return args[0].Each(in, func(v *value.Value, _ value.Path) error { return Raise(v) })
	})
	unary(r, "tostring", func(_ *runtime.Ctx, in *value.Value) (*value.Value, error) {
		if in.Kind() == value.KindString {
			return in, nil
		}
		return value.Str(runtime.JSON(in)), nil
	})
	unary(r, "tojson", func(_ *runtime.Ctx, in *value.Value) (*value.Value, error) {
		return value.Str(runtime.JSON(in)), nil
	})
	unary(r, "tonumber", tonumber)
	unary(r, "toboolean", func(_ *runtime.Ctx, in *value.Value) (*value.Value, error) {
		switch {
		case in.Kind() == value.KindTrue || in.Kind() == value.KindFalse:
			return in, nil
		case in.Kind() == value.KindString && (in.Str() == "true" || in.Str() == "false"):
			return value.Bool(in.Str() == "true"), nil
		}
		return nil, fail("%s cannot be parsed as a boolean", describe(in))
	})
	unary(r, "keys", func(_ *runtime.Ctx, in *value.Value) (*value.Value, error) { return keys(in, true) })
	unary(r, "keys_unsorted", func(_ *runtime.Ctx, in *value.Value) (*value.Value, error) { return keys(in, false) })
	binary(r, "has", func(_ *runtime.Ctx, in, key *value.Value) (*value.Value, error) {
		switch {
		case in.Kind() == value.KindObject && key.Kind() == value.KindString:
			_, ok := in.Get(key.Str())
			return value.Bool(ok), nil
		case in.Kind() == value.KindArray && key.Kind() == value.KindNumber:
			f := key.Float()
			return value.Bool(f >= 0 && f < float64(in.Len())), nil
		}
		return nil, fail("Cannot check whether %s has a %s key", runtime.TypeName(in), runtime.TypeName(key))
	})
	binary(r, "contains", func(ctx *runtime.Ctx, in, b *value.Value) (*value.Value, error) {
		ok, err := contains(ctx, in, b)
		if err != nil {
			return nil, err
		}
		return value.Bool(ok), nil
	})
	unary(r, "env", func(ctx *runtime.Ctx, _ *value.Value) (*value.Value, error) {
		if err := ctx.Caps.RequireEnv(); err != nil {
			return nil, err
		}
		return envObject(ctx.Env()), nil
	})
}

// unary registers a native with no arguments that maps its input to one
// output.
func unary(r *runtime.Registry, name string, f func(ctx *runtime.Ctx, in *value.Value) (*value.Value, error)) {
	r.RegisterSingle(name, 0, func(ctx *runtime.Ctx, in *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		v, err := f(ctx, in)
		if err != nil {
			return err
		}
		return emit(v, nil)
	})
}

// binary registers a native with one value argument that maps its input
// and each output of the argument to one output.
func binary(r *runtime.Registry, name string, f func(ctx *runtime.Ctx, in, arg *value.Value) (*value.Value, error)) {
	r.RegisterSingle(name, 1, func(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		return args[0].Each(in, func(a *value.Value, _ value.Path) error {
			v, err := f(ctx, in, a)
			if err != nil {
				return err
			}
			return emit(v, nil)
		})
	})
}

// cartesian runs k on every combination of the arguments' outputs, each
// argument run against in. The last argument varies slowest, which is the
// order jq runs a native's arguments in.
func cartesian(in *value.Value, args []runtime.Thunk, k func(vals []*value.Value) error) error {
	vals := make([]*value.Value, len(args))
	var step func(i int) error
	step = func(i int) error {
		if i < 0 {
			return k(vals)
		}
		return args[i].Each(in, func(v *value.Value, _ value.Path) error {
			vals[i] = v
			return step(i - 1)
		})
	}
	return step(len(args) - 1)
}

// fail returns a runtime error with a formatted message.
func fail(format string, args ...any) error {
	return &query.RuntimeError{Msg: fmt.Sprintf(format, args...)}
}

// describe names a value by its type and its dump, as in `number (1)`.
func describe(v *value.Value) string {
	return runtime.TypeName(v) + " (" + runtime.Dump(v) + ")"
}

// Raise is error's error. A string is the message itself, and any other
// value is its dump marked as not a string. The value rides along so that
// catch sees it unchanged.
func Raise(v *value.Value) error {
	if v.Kind() == value.KindString {
		return &query.RuntimeError{Msg: v.Str(), Value: v}
	}
	return &query.RuntimeError{Msg: runtime.Dump(v) + " (not a string)", Value: v}
}

// length is the number of codepoints in a string, elements in an array,
// and entries in an object, the absolute value of a number, and zero for
// null.
func length(ctx *runtime.Ctx, in *value.Value) (*value.Value, error) {
	switch in.Kind() {
	case value.KindNull:
		return value.Num(0), nil
	case value.KindNumber:
		d, err := ctx.Number(in)
		if err != nil {
			return nil, err
		}
		if !d.IsNegative() {
			return in, nil
		}
		return value.NumDec(d.Abs()), nil
	case value.KindString:
		return value.Num(float64(utf8.RuneCountInString(in.Str()))), nil
	case value.KindArray, value.KindObject:
		return value.Num(float64(in.Len())), nil
	default:
		return nil, fail("%s has no length", describe(in))
	}
}

// keys returns an object's keys, sorted by codepoint when sorted is set,
// or an array's indices.
func keys(in *value.Value, sorted bool) (*value.Value, error) {
	switch in.Kind() {
	case value.KindObject:
		names := in.Keys()
		if sorted {
			names = slices.Clone(names)
			slices.Sort(names)
		}
		b := value.NewArrBuilder(len(names))
		for _, k := range names {
			b.Append(value.Str(k))
		}
		return b.Seal(), nil
	case value.KindArray:
		b := value.NewArrBuilder(in.Len())
		for i := range in.Len() {
			b.Append(value.Num(float64(i)))
		}
		return b.Seal(), nil
	default:
		return nil, fail("%s has no keys", describe(in))
	}
}

// contains is jq's containment. The two kinds must match, and true and
// false are different kinds here. Below the top level, a value of another
// kind is simply not contained.
func contains(ctx *runtime.Ctx, a, b *value.Value) (bool, error) {
	if a.Kind() != b.Kind() {
		return false, fail("%s and %s cannot have their containment checked", describe(a), describe(b))
	}
	return contained(ctx, a, b), nil
}

// contained reports whether a contains b. A string contains its
// substrings, an array contains b when every element of b is contained in
// one of its elements, an object contains b when each of b's keys is
// present and contains b's value, and any other value contains only what
// equals it.
func contained(ctx *runtime.Ctx, a, b *value.Value) bool {
	if a.Kind() != b.Kind() {
		return false
	}
	switch a.Kind() {
	case value.KindString:
		return strings.Contains(a.Str(), b.Str())
	case value.KindArray:
		for _, be := range b.Elems() {
			if !slices.ContainsFunc(a.Elems(), func(ae *value.Value) bool { return contained(ctx, ae, be) }) {
				return false
			}
		}
		return true
	case value.KindObject:
		for _, e := range b.Entries() {
			av, ok := a.Get(e.Key.Str())
			if !ok || !contained(ctx, av, e.Value) {
				return false
			}
		}
		return true
	default:
		return ctx.Equal(a, b)
	}
}

// tonumber parses a string the way jq's tonumber does. It accepts a sign,
// leading zeros, a bare fraction such as .5, a trailing point, and nan and
// infinity in any case. A number is already one.
func tonumber(_ *runtime.Ctx, in *value.Value) (*value.Value, error) {
	switch in.Kind() {
	case value.KindNumber:
		return in, nil
	case value.KindString:
	default:
		return nil, fail("%s cannot be parsed as a number", describe(in))
	}
	s := in.Str()
	if v, ok := special(s); ok {
		return v, nil
	}
	norm, ok := normalizeNumber(s)
	if !ok {
		return nil, fail("%s cannot be parsed as a number", describe(in))
	}
	d, err := decimal.Parse(norm)
	if err != nil {
		// A number past the bounds is still a value. It keeps its text and
		// raises its bound error only when arithmetic reads it.
		return value.NumText(norm), nil //nolint:nilerr // the bound error travels with the number
	}
	return value.NumLit(d, norm), nil
}

// special reads nan and infinity, signed or not, in any case.
func special(s string) (*value.Value, bool) {
	neg := false
	body := s
	if body != "" && (body[0] == '+' || body[0] == '-') {
		neg = body[0] == '-'
		body = body[1:]
	}
	switch strings.ToLower(body) {
	case "nan":
		return value.NumDec(decimal.NaN()), true
	case "inf", "infinity":
		if neg {
			return value.NumDec(decimal.Inf(-1)), true
		}
		return value.NumDec(decimal.Inf(1)), true
	}
	return nil, false
}

// normalizeNumber rewrites jq's looser number syntax as a JSON number. It
// drops a plus sign and leading zeros, puts a zero before a bare fraction,
// and drops a trailing point, and it reports false for anything else.
func normalizeNumber(s string) (string, bool) {
	sign, rest := "", s
	if rest != "" && (rest[0] == '+' || rest[0] == '-') {
		if rest[0] == '-' {
			sign = "-"
		}
		rest = rest[1:]
	}
	exp := ""
	if i := strings.IndexAny(rest, "eE"); i >= 0 {
		digits := strings.TrimPrefix(strings.TrimPrefix(rest[i+1:], "+"), "-")
		if digits == "" || !allDigits(digits) || len(rest[i+1:])-len(digits) > 1 {
			return "", false
		}
		rest, exp = rest[:i], "e"+rest[i+1:]
	}
	whole, frac, _ := strings.Cut(rest, ".")
	if whole+frac == "" || !allDigits(whole) || !allDigits(frac) {
		return "", false
	}
	whole = strings.TrimLeft(whole, "0")
	if whole == "" {
		whole = "0"
	}
	if frac != "" {
		whole += "." + frac
	}
	return sign + whole + exp, true
}

func allDigits(s string) bool {
	for i := range len(s) {
		if !isDigit(s[i]) {
			return false
		}
	}
	return true
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// envObject is the environment as an object with its keys sorted.
func envObject(env map[string]string) *value.Value {
	names := make([]string, 0, len(env))
	for k := range env {
		names = append(names, k)
	}
	slices.Sort(names)
	b := value.NewObjBuilder(len(names))
	for _, k := range names {
		b.Set(k, value.Str(env[k]))
	}
	return b.Seal()
}
