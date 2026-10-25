package builtin

import (
	"encoding/base32"
	"encoding/base64"
	"strings"
	"unicode/utf8"

	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// registerFormats adds the @name formats, each a native named with its @
// and taking no arguments, and format/1, which applies one by name. A
// format that works on text turns any other value into its JSON text
// first.
func registerFormats(r *runtime.Registry) {
	text := func(name string, f func(s string) (string, error)) {
		unary(r, "@"+name, func(ctx *runtime.Ctx, in *value.Value) (*value.Value, error) {
			s, err := f(textOf(in, ctx.Numbers))
			if err != nil {
				return nil, err
			}
			return value.Str(s), nil
		})
	}
	text("text", func(s string) (string, error) { return s, nil })
	unary(r, "@json", func(ctx *runtime.Ctx, in *value.Value) (*value.Value, error) {
		return value.Str(runtime.JSONIn(in, ctx.Numbers)), nil
	})
	text("html", func(s string) (string, error) { return htmlEscaper.Replace(s), nil })
	text("uri", func(s string) (string, error) { return uriEncode(s), nil })
	text("urid", func(s string) (string, error) {
		out, ok := uriDecode(s)
		if !ok {
			return "", fail("%s is not a valid uri encoding", describe(value.Str(s)))
		}
		return out, nil
	})
	text("base64", func(s string) (string, error) { return base64.StdEncoding.EncodeToString([]byte(s)), nil })
	text("base64d", base64Decode)
	text("base32", func(s string) (string, error) { return base32.StdEncoding.EncodeToString([]byte(s)), nil })
	text("base32d", base32Decode)
	unary(r, "@csv", func(ctx *runtime.Ctx, in *value.Value) (*value.Value, error) {
		return row(ctx, in, "csv", ",", func(s string) string {
			return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
		})
	})
	unary(r, "@tsv", func(ctx *runtime.Ctx, in *value.Value) (*value.Value, error) {
		return row(ctx, in, "tsv", "\t", tsvEscaper.Replace)
	})
	unary(r, "@sh", shell)
	r.RegisterSingle("format", 1, func(ctx *runtime.Ctx, in *value.Value, args []runtime.Thunk, emit runtime.Emit) error {
		return args[0].Each(in, func(name *value.Value, _ value.Path) error {
			if name.Kind() != value.KindString {
				return fail("%s is not a valid format", describe(name))
			}
			reg := &runtime.Registry{}
			if ctx.Dialect != nil {
				reg = ctx.Dialect.Builtins()
			}
			f, err := reg.Format("@" + name.Str())
			if err != nil {
				return err
			}
			return f(ctx, in, nil, emit)
		})
	})
}

// textOf is a string's own text and any other value's JSON text.
func textOf(v *value.Value, numbers runtime.NumberMode) string {
	if v.Kind() == value.KindString {
		return v.Str()
	}
	return runtime.JSONIn(v, numbers)
}

var (
	htmlEscaper = strings.NewReplacer("<", "&lt;", ">", "&gt;", "&", "&amp;", "'", "&apos;", `"`, "&quot;")
	tsvEscaper  = strings.NewReplacer(`\`, `\\`, "\t", `\t`, "\n", `\n`, "\r", `\r`)
)

// row formats an array as one csv or tsv row. Strings go through quote,
// numbers and booleans print as JSON, and null is an empty field.
func row(ctx *runtime.Ctx, in *value.Value, kind, sep string, quote func(string) string) (*value.Value, error) {
	if in.Kind() != value.KindArray {
		return nil, fail("%s cannot be %s-formatted, only array", describe(in), kind)
	}
	fields := make([]string, in.Len())
	for i, e := range in.Elems() {
		switch e.Kind() {
		case value.KindNull:
		case value.KindString:
			fields[i] = quote(e.Str())
		case value.KindArray, value.KindObject:
			return nil, fail("%s is not valid in a csv row", describe(e))
		default:
			fields[i] = runtime.JSONIn(e, ctx.Numbers)
		}
	}
	return value.Str(strings.Join(fields, sep)), nil
}

// shell quotes a string, or each element of an array separated by spaces,
// for a POSIX shell. Other scalars print as JSON.
func shell(ctx *runtime.Ctx, in *value.Value) (*value.Value, error) {
	elems := []*value.Value{in}
	if in.Kind() == value.KindArray {
		elems = in.Elems()
	}
	words := make([]string, len(elems))
	for i, e := range elems {
		switch e.Kind() {
		case value.KindString:
			words[i] = "'" + strings.ReplaceAll(e.Str(), "'", `'\''`) + "'"
		case value.KindArray, value.KindObject:
			return nil, fail("%s can not be escaped for shell", describe(e))
		default:
			words[i] = runtime.JSONIn(e, ctx.Numbers)
		}
	}
	return value.Str(strings.Join(words, " ")), nil
}

// uriEncode percent-encodes every byte outside the unreserved set.
func uriEncode(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isDigit(c) || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || strings.IndexByte("-_.~", c) >= 0 {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hex[c>>4])
		b.WriteByte(hex[c&15])
	}
	return b.String()
}

// uriDecode reverses percent-encoding. A malformed escape, or bytes that
// are not UTF-8 once decoded, make the whole string invalid.
func uriDecode(s string) (string, bool) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			b.WriteByte(s[i])
			continue
		}
		if i+2 >= len(s) {
			return "", false
		}
		hi, ok1 := unhex(s[i+1])
		lo, ok2 := unhex(s[i+2])
		if !ok1 || !ok2 {
			return "", false
		}
		b.WriteByte(hi<<4 | lo)
		i += 2
	}
	out := b.String()
	return out, utf8.ValidString(out)
}

func unhex(c byte) (byte, bool) {
	switch {
	case isDigit(c):
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// base64Decode reads standard base64 up to the first padding character,
// with the padding optional. Decoded bytes that are not UTF-8 become
// U+FFFD, one for each byte.
func base64Decode(s string) (string, error) {
	data, _, _ := strings.Cut(s, "=")
	if len(data)%4 == 1 {
		return "", fail("%s trailing base64 byte found", describe(value.Str(s)))
	}
	raw, err := base64.RawStdEncoding.DecodeString(data)
	if err != nil {
		return "", fail("%s is not valid base64 data", describe(value.Str(s)))
	}
	return validText(raw), nil
}

// base32Decode reads standard base32 with the padding optional, under the
// same rules as base64Decode.
func base32Decode(s string) (string, error) {
	raw, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(strings.TrimRight(s, "="))
	if err != nil {
		return "", fail("%s is not valid base32 data", describe(value.Str(s)))
	}
	return validText(raw), nil
}

// validText turns bytes into a string, replacing each byte that does not
// start a valid UTF-8 sequence with U+FFFD.
func validText(raw []byte) string {
	if utf8.Valid(raw) {
		return string(raw)
	}
	var b strings.Builder
	for len(raw) > 0 {
		c, size := utf8.DecodeRune(raw)
		b.WriteRune(c)
		raw = raw[size:]
	}
	return b.String()
}
