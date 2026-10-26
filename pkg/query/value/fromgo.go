package value

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"unicode"

	"github.com/fivetwenty-io/graft/pkg/decimal"
)

// FromGo converts a Go value as the YAML and JSON decoders produce it into
// a Value. It takes nil, bool, string, every integer and float type,
// decimal.Decimal, a Value (which it returns as it is), []interface{},
// []string, and map[string]interface{}. A map becomes an object whose keys
// follow spruce's order, because a Go map has none of its own. Any other
// type is an error that names it and where it sits.
func FromGo(v interface{}) (*Value, error) {
	return fromGo(v, "")
}

func fromGo(v interface{}, at string) (*Value, error) {
	switch x := v.(type) {
	case []string:
		elems := make([]*Value, len(x))
		for i, s := range x {
			elems[i] = Str(s)
		}
		return Arr(elems...), nil
	case []interface{}:
		elems := make([]*Value, len(x))
		for i, e := range x {
			c, err := fromGo(e, at+"["+strconv.Itoa(i)+"]")
			if err != nil {
				return nil, err
			}
			elems[i] = c
		}
		return Arr(elems...), nil
	case map[string]interface{}:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.SliceStable(keys, func(i, j int) bool { return spruceKeyLess(keys[i], keys[j]) })
		pairs := make([]Pair, len(keys))
		for i, k := range keys {
			c, err := fromGo(x[k], at+"."+k)
			if err != nil {
				return nil, err
			}
			pairs[i] = Pair{Key: k, Value: c}
		}
		return Obj(pairs...), nil
	}
	if sv, ok := scalarFromGo(v); ok {
		return sv, nil
	}
	if at == "" {
		at = "the root"
	}
	return nil, fmt.Errorf("value: cannot convert %T at %s", v, at)
}

// scalarFromGo converts the Go types that hold one value, and reports
// false for any other.
func scalarFromGo(v interface{}) (*Value, bool) {
	switch x := v.(type) {
	case nil:
		return Null(), true
	case *Value:
		if x == nil {
			return Null(), true
		}
		return x, true
	case bool:
		return Bool(x), true
	case string:
		return Str(x), true
	case float32:
		return Num(float64(x)), true
	case float64:
		return Num(x), true
	case decimal.Decimal:
		return NumDec(x), true
	}
	return integerFromGo(v)
}

func integerFromGo(v interface{}) (*Value, bool) {
	switch x := v.(type) {
	case int:
		return NumDec(decimal.FromInt64(int64(x))), true
	case int8:
		return NumDec(decimal.FromInt64(int64(x))), true
	case int16:
		return NumDec(decimal.FromInt64(int64(x))), true
	case int32:
		return NumDec(decimal.FromInt64(int64(x))), true
	case int64:
		return NumDec(decimal.FromInt64(x)), true
	case uint:
		return uintValue(uint64(x)), true
	case uint8:
		return uintValue(uint64(x)), true
	case uint16:
		return uintValue(uint64(x)), true
	case uint32:
		return uintValue(uint64(x)), true
	case uint64:
		return uintValue(x), true
	}
	return nil, false
}

func uintValue(u uint64) *Value {
	d, err := decimal.Parse(strconv.FormatUint(u, 10))
	if err != nil {
		return Num(float64(u))
	}
	return NumDec(d)
}

// ToGoNumbers chooses the Go type ToGo gives a number.
type ToGoNumbers uint8

// The number types. ToGoDecimal, the zero value, keeps every digit.
const (
	// ToGoDecimal returns a decimal.Decimal.
	ToGoDecimal ToGoNumbers = iota
	// ToGoInt64 returns an int64 when the number is an integer that fits,
	// and a decimal.Decimal otherwise.
	ToGoInt64
	// ToGoFloat64 returns the nearest float64.
	ToGoFloat64
)

// ToGoOptions tunes ToGo.
type ToGoOptions struct {
	// Numbers is the Go type a number becomes.
	Numbers ToGoNumbers
}

// ToGo converts a Value to the Go value FromGo reads back: nil, bool,
// string, a number as ToGoOptions.Numbers chooses, []interface{}, and
// map[string]interface{}. A map has no order and no repeated keys, so the
// last entry for a key wins and the key order is lost.
func ToGo(v *Value, opts ...ToGoOptions) interface{} {
	var o ToGoOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	return toGo(v, o)
}

func toGo(v *Value, o ToGoOptions) interface{} {
	if v == nil {
		return nil
	}
	switch v.Kind() {
	case KindFalse:
		return false
	case KindTrue:
		return true
	case KindNumber:
		switch o.Numbers {
		case ToGoFloat64:
			return v.Float()
		case ToGoInt64:
			if n, ok := v.Dec().Int64(); ok {
				return n
			}
		case ToGoDecimal:
		}
		return v.Dec()
	case KindString:
		return v.Str()
	case KindArray:
		out := make([]interface{}, len(v.arr))
		for i, e := range v.arr {
			out[i] = toGo(e, o)
		}
		return out
	case KindObject:
		out := make(map[string]interface{}, len(v.obj.entries))
		for _, e := range v.obj.entries {
			out[keyString(e.Key)] = toGo(e.Value, o)
		}
		return out
	case KindNull:
	}
	return nil
}

// spruceKeyLess orders coerced map keys the way spruce's YAML encoder
// (geofffranks/yaml, a yaml.v2 fork) orders typed keys: numeric keys sort
// numerically among themselves and all of them precede string keys, and
// string keys sort with the fork's natural comparison, where digit runs
// compare numerically and non-letters sort before letters by raw rune.
//
// Keys arrive as strings, so numeric classification is recovered by
// re-parsing the key. A quoted "10" and a bare 10 are indistinguishable
// here, which is the documented residual divergence for maps mixing
// quoted-numeric keys with other keys (see docs/spruce/known-gaps.md,
// mixed-key-type-map-encoding-order).
func spruceKeyLess(a, b string) bool {
	af, aInt, aNum := keyNumericValue(a)
	bf, bInt, bNum := keyNumericValue(b)
	switch {
	case aNum && bNum:
		if aInt && bInt {
			// Exact compare so huge int64 keys don't collapse into
			// the same float64.
			an, _ := strconv.ParseInt(a, 10, 64)
			bn, _ := strconv.ParseInt(b, 10, 64)
			if an != bn {
				return an < bn
			}
		} else if af != bf {
			return af < bf
		}
		if aInt != bInt {
			// Equal values in different shapes: spruce sorts the int
			// key first (reflect.Int < reflect.Float64).
			return aInt
		}
		// Distinct spellings of the same number ("1" vs "01"):
		// fall through to the string comparison so the order never
		// depends on map iteration order.
		return naturalLess(a, b)
	case aNum != bNum:
		// Numeric tier before string tier, regardless of content
		// (spruce compares reflect.Kind values here).
		return aNum
	default:
		return naturalLess(a, b)
	}
}

// keyNumericValue reports whether a coerced key reads as a plain base-10
// number, its value, and whether it has integer shape. Exponent and
// signed forms count ("1e3" sorts at 1000, though its label keeps the
// source spelling); hex, octal, and underscore forms do not, and neither
// do "inf"/"nan" lookalikes that strconv.ParseFloat would accept — spruce
// types none of those as numbers from a bare base-10-looking scalar.
func keyNumericValue(s string) (f float64, isInt bool, ok bool) {
	// Typical keys are words; a base-10 number can only start with a
	// digit, sign, or dot, so skip both strconv calls (and their
	// allocated errors) for everything else. ParseFloat's other
	// spellings ("inf", "nan") are rejected by the IsInf/IsNaN check
	// below anyway, so skipping them here answers identically.
	if s == "" {
		return 0, false, false
	}
	if c := s[0]; (c < '0' || c > '9') && c != '-' && c != '+' && c != '.' {
		return 0, false, false
	}
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return float64(n), true, true
	}
	n, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsInf(n, 0) || math.IsNaN(n) {
		return 0, false, false
	}
	return n, false, true
}

// digitRun reads the run of digits starting at index start, returning its
// value and the index one past its last digit. A start position that is
// not a digit yields the empty run — value 0, end == start — which is the
// behavior naturalLess's third documented quirk depends on.
func digitRun(rs []rune, start int) (int64, int) {
	var n int64
	end := start
	for ; end < len(rs) && unicode.IsDigit(rs[end]); end++ {
		n = n*10 + int64(rs[end]-'0')
	}
	return n, end
}

// naturalLess is a verbatim port of the string branch of the sorter in
// spruce's vendored geofffranks/yaml (sorter.go, Copyright 2011-2016
// Canonical Ltd., Apache License 2.0). Its quirks are kept deliberately —
// diverging from spruce here would be a parity bug:
//
//   - a rune position where exactly one side is a letter sorts the
//     non-letter first, so punctuation and digits both precede letters;
//   - a non-digit rune contributes an empty digit run that counts as 0,
//     which is why "int_val" sorts before "int64_val";
//   - unicode.IsDigit matches non-ASCII digits whose r-'0' arithmetic is
//     garbage, and the int64 run accumulator can overflow on absurd runs.
func naturalLess(a, b string) bool {
	ar, br := []rune(a), []rune(b)
	for i := 0; i < len(ar) && i < len(br); i++ {
		if ar[i] == br[i] {
			if !unicode.IsDigit(ar[i]) || !unicode.IsDigit(br[i]) {
				continue
			}
		}
		al := unicode.IsLetter(ar[i])
		bl := unicode.IsLetter(br[i])
		if al && bl {
			return ar[i] < br[i]
		}
		if al || bl {
			return bl
		}
		an, ai := digitRun(ar, i)
		bn, bi := digitRun(br, i)
		if an != bn {
			return an < bn
		}
		if ai != bi {
			return ai < bi
		}
		if ar[i] == br[i] {
			continue
		}
		return ar[i] < br[i]
	}
	return len(ar) < len(br)
}
