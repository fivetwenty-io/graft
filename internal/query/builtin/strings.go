package builtin

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/fivetwenty-io/graft/internal/query/load"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// asciiSpace is the whitespace that trim removes.
const asciiSpace = " \t\n\v\f\r"

func registerStrings(r *runtime.Registry) {
	affix := func(name, check string, f func(s, a string) *value.Value) {
		binary(r, name, func(_ *runtime.Ctx, in, a *value.Value) (*value.Value, error) {
			if in.Kind() != value.KindString || a.Kind() != value.KindString {
				return nil, fail("%s() requires string inputs", check)
			}
			return f(in.Str(), a.Str()), nil
		})
	}
	affix("startswith", "startswith", func(s, a string) *value.Value { return value.Bool(strings.HasPrefix(s, a)) })
	affix("endswith", "endswith", func(s, a string) *value.Value { return value.Bool(strings.HasSuffix(s, a)) })
	affix("ltrimstr", "startswith", func(s, a string) *value.Value { return value.Str(strings.TrimPrefix(s, a)) })
	affix("rtrimstr", "endswith", func(s, a string) *value.Value { return value.Str(strings.TrimSuffix(s, a)) })
	affix("trimstr", "startswith", func(s, a string) *value.Value {
		return value.Str(strings.TrimSuffix(strings.TrimPrefix(s, a), a))
	})

	trim := func(name string, f func(string) string) {
		unary(r, name, func(_ *runtime.Ctx, in *value.Value) (*value.Value, error) {
			if in.Kind() != value.KindString {
				return nil, fail("trim input must be a string")
			}
			return value.Str(f(in.Str())), nil
		})
	}
	trim("trim", func(s string) string { return strings.Trim(s, asciiSpace) })
	trim("ltrim", func(s string) string { return strings.TrimLeft(s, asciiSpace) })
	trim("rtrim", func(s string) string { return strings.TrimRight(s, asciiSpace) })

	unary(r, "explode", func(_ *runtime.Ctx, in *value.Value) (*value.Value, error) {
		if in.Kind() != value.KindString {
			return nil, fail("explode input must be a string")
		}
		b := value.NewArrBuilder(utf8.RuneCountInString(in.Str()))
		for _, c := range in.Str() {
			b.Append(value.Num(float64(c)))
		}
		return b.Seal(), nil
	})
	unary(r, "implode", implode)
	binary(r, "split", func(_ *runtime.Ctx, in, sep *value.Value) (*value.Value, error) {
		if in.Kind() != value.KindString || sep.Kind() != value.KindString {
			return nil, fail("split input and separator must be strings")
		}
		return splitString(in.Str(), sep.Str()), nil
	})
	asciiCase := func(name string, from, to byte) {
		unary(r, name, func(_ *runtime.Ctx, in *value.Value) (*value.Value, error) {
			if in.Kind() != value.KindString {
				return nil, fail("explode input must be a string")
			}
			b := []byte(in.Str())
			for i, c := range b {
				if c >= from && c < from+26 {
					b[i] = c - from + to
				}
			}
			return value.Str(string(b)), nil
		})
	}
	asciiCase("ascii_downcase", 'A', 'a')
	asciiCase("ascii_upcase", 'a', 'A')
	unary(r, "fromjson", fromjson)
}

// implode turns an array of codepoints into a string. A number that is
// not a valid codepoint becomes U+FFFD.
func implode(_ *runtime.Ctx, in *value.Value) (*value.Value, error) {
	if in.Kind() != value.KindArray {
		return nil, fail("implode input must be an array")
	}
	var b strings.Builder
	for _, e := range in.Elems() {
		if e.Kind() != value.KindNumber {
			return nil, fail("%s can't be imploded, unicode codepoint needs to be numeric", describe(e))
		}
		f := e.Float()
		c := rune(utf8.RuneError)
		if f >= 0 && f <= utf8.MaxRune {
			c = rune(f)
		}
		if !utf8.ValidRune(c) {
			c = utf8.RuneError
		}
		b.WriteRune(c)
	}
	return value.Str(b.String()), nil
}

// fromjson parses a string holding exactly one JSON value. jq also reads
// nan wherever a value may stand, as null, so that bare word becomes null
// before the reader sees it.
func fromjson(_ *runtime.Ctx, in *value.Value) (*value.Value, error) {
	if in.Kind() != value.KindString {
		return nil, fail("%s only strings can be parsed", describe(in))
	}
	s := in.Str()
	parseErr := func(msg string) error {
		return &query.RuntimeError{Msg: msg + " (while parsing '" + s + "')"}
	}
	unfinished := func() error {
		line := 1 + strings.Count(s, "\n")
		col := len(s) - strings.LastIndexByte(s, '\n') - 1
		return parseErr(fmt.Sprintf("Unfinished JSON term at EOF at line %d, column %d", line, col))
	}
	rd := load.NewJSONReader(bufio.NewReader(strings.NewReader(nanToNull(s))), "", load.LoadOptions{})
	v, _, err := rd.Next()
	var se *load.SyntaxError
	switch {
	case errors.As(err, &se) && strings.HasPrefix(se.Msg, "unexpected end of input"):
		return nil, unfinished()
	case errors.As(err, &se):
		return nil, parseErr(fmt.Sprintf("%s at line %d, column %d", se.Msg, se.Line, se.Col))
	case errors.Is(err, io.EOF) && strings.Trim(s, asciiSpace) == "":
		return nil, parseErr("Expected JSON value")
	case err != nil:
		return nil, unfinished()
	}
	if extra, _, err := rd.Next(); err == nil && extra != nil {
		return nil, parseErr("Unexpected extra JSON values")
	}
	return v, nil
}

// nanToNull replaces each bare nan outside a string with null.
func nanToNull(s string) string {
	if !strings.Contains(s, "nan") {
		return s
	}
	var b strings.Builder
	inString, escaped := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inString:
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
		case c == '"':
			inString = true
		case strings.HasPrefix(s[i:], "nan") && !wordByte(s, i-1) && !wordByte(s, i+3):
			b.WriteString("null")
			i += 2
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// wordByte reports whether s has a letter or digit at i.
func wordByte(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return false
	}
	c := s[i]
	return isDigit(c) || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}
