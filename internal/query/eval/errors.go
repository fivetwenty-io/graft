package eval

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// RuntimeError returns a runtime error carrying msg, which try catches.
func RuntimeError(msg string) *query.RuntimeError {
	return &query.RuntimeError{Msg: msg}
}

// BreakError is break $name on its way to the label that owns Label. try
// catches it, as jq 1.8.2 does, and so does the label it names.
type BreakError struct{ Label uintptr }

// Error describes the break.
func (e *BreakError) Error() string { return "break" }

// StopError ends a generator early. limit, first, and isempty raise it
// and swallow only their own, and try never catches it.
type StopError struct{}

// Error describes the stop.
func (e *StopError) Error() string { return "stop" }

// DownstreamError wraps an error that came back from the continuation a
// try handed its body, so the try lets it pass instead of catching an
// error raised outside it.
type DownstreamError struct{ Err error }

// Error returns the wrapped error's message.
func (e *DownstreamError) Error() string { return e.Err.Error() }

// Unwrap returns the wrapped error.
func (e *DownstreamError) Unwrap() error { return e.Err }

// catchable reports whether try and ?// handle err, which is true only for
// an error raised directly in their body and never for a wrapped one.
func catchable(err error) bool {
	switch err.(type) { //nolint:errorlint // only an error raised directly is caught, never a wrapped one
	case *query.RuntimeError, *BreakError:
		return true
	default:
		return false
	}
}

// invalidPathResult is the error for an output of a path expression that
// is not a path.
func invalidPathResult(v *value.Value) error {
	return RuntimeError("Invalid path expression with result " + dump(v))
}

// invalidPathAccess is the error for indexing a value that is not a path.
func invalidPathAccess(key, v *value.Value) error {
	return RuntimeError("Invalid path expression near attempt to access element " + dump(key) + " of " + dump(v))
}

// invalidPathIterate is the error for iterating a value that is not a
// path.
func invalidPathIterate(v *value.Value) error {
	return RuntimeError("Invalid path expression near attempt to iterate through " + dump(v))
}

// dumpLimit is the longest dump jq prints whole in an error message.
const dumpLimit = 29

// dump returns v as compact JSON the way jq quotes a value in an error
// message, cut to its first 25 bytes, an ellipsis, and its last byte when
// it is longer than 29 bytes.
func dump(v *value.Value) string {
	s := toJSON(v)
	if len(s) <= dumpLimit {
		return s
	}
	cut := 25
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..." + s[len(s)-1:]
}

// toJSON returns v as compact JSON, with numbers printed as jq prints them.
func toJSON(v *value.Value) string {
	var b strings.Builder
	writeJSON(&b, v)
	return b.String()
}

func writeJSON(b *strings.Builder, v *value.Value) {
	switch v.Kind() {
	case value.KindNull:
		b.WriteString("null")
	case value.KindFalse:
		b.WriteString("false")
	case value.KindTrue:
		b.WriteString("true")
	case value.KindNumber:
		b.WriteString(numberText(v))
	case value.KindString:
		writeString(b, v.Str())
	case value.KindArray:
		b.WriteByte('[')
		for i, e := range v.Elems() {
			if i > 0 {
				b.WriteByte(',')
			}
			writeJSON(b, e)
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
			writeJSON(b, e.Value)
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
	return toJSON(k)
}

// numberText prints a literal in its canonical form and a computed number
// reduced, with NaN as null and the infinities as the largest double.
func numberText(v *value.Value) string {
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
