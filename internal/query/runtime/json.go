package runtime

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// TypeName is the name jq gives a value's type, both in error messages and
// as the output of type.
func TypeName(v *value.Value) string {
	switch v.Kind() {
	case value.KindNull:
		return "null"
	case value.KindFalse, value.KindTrue:
		return "boolean"
	case value.KindNumber:
		return "number"
	case value.KindString:
		return "string"
	case value.KindArray:
		return "array"
	default:
		return "object"
	}
}

// dumpLimit is the longest dump jq prints whole in an error message.
const dumpLimit = 29

// Dump returns v as compact JSON the way jq quotes a value in an error
// message, cut to its first 25 bytes, an ellipsis, and its last byte when
// it is longer than 29 bytes.
func Dump(v *value.Value) string {
	s := JSON(v)
	if len(s) <= dumpLimit {
		return s
	}
	cut := 25
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..." + s[len(s)-1:]
}

// JSON returns v as compact JSON, with numbers printed as jq prints them
// with exact decimals.
func JSON(v *value.Value) string {
	return JSONIn(v, NumbersExact)
}

// JSONIn is JSON with computed numbers printed as the number mode says,
// which is what tojson, tostring, and @text do.
func JSONIn(v *value.Value, numbers NumberMode) string {
	var b strings.Builder
	writeJSON(&b, v, numbers)
	return b.String()
}

func writeJSON(b *strings.Builder, v *value.Value, numbers NumberMode) {
	switch v.Kind() {
	case value.KindNull:
		b.WriteString("null")
	case value.KindFalse:
		b.WriteString("false")
	case value.KindTrue:
		b.WriteString("true")
	case value.KindNumber:
		b.WriteString(numberText(v, numbers))
	case value.KindString:
		writeString(b, v.Str())
	case value.KindArray:
		b.WriteByte('[')
		for i, e := range v.Elems() {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSON(b, e, numbers)
		}
		b.WriteByte(']')
	case value.KindObject:
		b.WriteByte('{')
		for i, e := range v.Entries() {
			if i > 0 {
				b.WriteByte(',')
			}
			writeString(b, keyText(e.Key))
			b.WriteByte(':')
			writeJSON(b, e.Value, numbers)
		}
		b.WriteByte('}')
	}
}

// keyText is an object key as text. A key that is not a string, which only
// yq keeps, prints as its JSON.
func keyText(k *value.Value) string {
	if k.Kind() == value.KindString {
		return k.Str()
	}
	return JSON(k)
}

// numberText prints a literal in its canonical form and a computed number
// reduced, with NaN as null and the infinities as the largest double.
func numberText(v *value.Value, numbers NumberMode) string {
	if lit := v.Literal(); lit != "" {
		return value.CanonicalLiteral(lit)
	}
	d := v.Dec()
	if d.IsNaN() {
		return "null"
	}
	if inf, sign := d.IsInf(); inf {
		if sign < 0 {
			return "-1.7976931348623157e+308"
		}
		return "1.7976931348623157e+308"
	}
	if numbers == NumbersFloat {
		return d.FloatString()
	}
	return d.String()
}

// writeString quotes s with jq's escapes, which spell every control
// character and DEL as \u00XX and leave / alone.
func writeString(b *strings.Builder, s string) {
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if r < 0x20 || r == 0x7f {
				b.WriteString(`\u00`)
				if r < 0x10 {
					b.WriteByte('0')
				}
				b.WriteString(strconv.FormatInt(int64(r), 16))
				continue
			}
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
}
