// Package jqout encodes query values as JSON text the way jq 1.8.2 prints
// them, including its raw, join, NUL, ASCII, sequence, and color modes.
package jqout

import (
	"bufio"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/fivetwenty-io/graft/internal/yamlnode"
	"github.com/fivetwenty-io/graft/pkg/query/value"
	"github.com/fivetwenty-io/graft/pkg/sortkeys"
)

// Encoder writes values to an output stream.
type Encoder interface {
	// Write encodes one value, followed by its terminator.
	Write(v *value.Value) error
	// Flush sends any buffered output to the underlying writer.
	Flush() error
}

// Options selects the output form.
type Options struct {
	Indent       int  // spaces per level; zero still breaks lines but adds no indentation, as jq --indent 0 does
	Tab          bool // indent with one tab per level instead of Indent spaces
	Compact      bool // no whitespace at all; only Compact selects this, so the caller maps --compact-output and nothing else onto it
	Raw          bool // top-level strings print without quotes, unless ASCII is set, which prints them quoted and escaped
	Join         bool // implies Raw, and no newline after a value
	RawNUL       bool // implies Raw, and a NUL byte after a value instead of a newline
	ASCII        bool // escape every non-ASCII character as \uXXXX
	Seq          bool // write an RS byte before each value
	FloatNumbers bool // print computed numbers by jq's double rule
	GoFloats     bool // print a computed !!float as Go prints a float64, as yq does
	SortKeys     sortkeys.Mode
	Color        bool
	Palette      Palette
	Unbuffered   bool // flush after every value
}

const recordSeparator = '\x1e'

type encoder struct {
	w   *bufio.Writer
	o   Options
	buf []byte
}

// New returns an Encoder that writes to w.
func New(w io.Writer, o Options) Encoder {
	return &encoder{w: bufio.NewWriter(w), o: o}
}

func (e *encoder) Flush() error { return e.w.Flush() }

func (e *encoder) Write(v *value.Value) error {
	if e.o.SortKeys != "" {
		v = sortkeys.Sort(v, e.o.SortKeys)
	}
	raw := e.printsRaw(v)
	if raw && e.o.RawNUL && strings.IndexByte(v.Str(), 0) >= 0 {
		return errors.New("Cannot dump a string containing NUL with --raw-output0 option") //nolint:staticcheck // jq's wording
	}
	e.buf = e.buf[:0]
	if e.o.Seq && !raw {
		e.buf = append(e.buf, recordSeparator)
	}
	if raw {
		e.rawString(v.Str())
	} else {
		e.encode(v, 0)
	}
	switch {
	case e.o.RawNUL:
		e.buf = append(e.buf, 0)
	case !e.o.Join:
		e.buf = append(e.buf, '\n')
	}
	if _, err := e.w.Write(e.buf); err != nil {
		return err
	}
	if e.o.Unbuffered {
		return e.w.Flush()
	}
	return nil
}

// printsRaw reports whether v is written as bare text. ASCII output keeps
// strings quoted and escaped.
func (e *encoder) printsRaw(v *value.Value) bool {
	return (e.o.Raw || e.o.Join || e.o.RawNUL) && !e.o.ASCII && v.Kind() == value.KindString
}

func (e *encoder) pretty() bool { return !e.o.Compact }

func (e *encoder) newline(depth int) {
	e.buf = append(e.buf, '\n')
	if e.o.Tab {
		for range depth {
			e.buf = append(e.buf, '\t')
		}
		return
	}
	for range depth * e.o.Indent {
		e.buf = append(e.buf, ' ')
	}
}

// open starts a colored token and close ends it, and both do nothing
// without color.
func (e *encoder) open(sgr string) {
	if e.o.Color {
		e.buf = append(e.buf, colorStart(sgr)...)
	}
}

func (e *encoder) close() {
	if e.o.Color {
		e.buf = append(e.buf, colorReset...)
	}
}

func (e *encoder) token(sgr, text string) {
	e.open(sgr)
	e.buf = append(e.buf, text...)
	e.close()
}

func (e *encoder) encode(v *value.Value, depth int) {
	p := e.o.Palette
	switch v.Kind() {
	case value.KindNull:
		e.token(p.Null, "null")
	case value.KindFalse:
		e.token(p.False, "false")
	case value.KindTrue:
		e.token(p.True, "true")
	case value.KindNumber:
		switch {
		case e.o.GoFloats && goFloat(v):
			e.token(p.Number, strconv.FormatFloat(v.Float(), 'g', -1, 64))
		case e.o.FloatNumbers:
			e.token(p.Number, FormatNumberFloat(v))
		default:
			e.token(p.Number, FormatNumber(v))
		}
	case value.KindString:
		e.open(p.String)
		e.quote(v.Str())
		e.close()
	case value.KindArray:
		e.array(v.Elems(), depth)
	case value.KindObject:
		e.object(v, depth)
	}
}

func (e *encoder) array(elems []*value.Value, depth int) {
	sgr := e.o.Palette.Array
	if len(elems) == 0 {
		e.token(sgr, "[]")
		return
	}
	e.token(sgr, "[")
	for i, el := range elems {
		if i > 0 {
			e.token(sgr, ",")
		}
		if e.pretty() {
			e.newline(depth + 1)
		}
		e.encode(el, depth+1)
	}
	if e.pretty() {
		e.newline(depth)
	}
	e.token(sgr, "]")
}

func (e *encoder) object(v *value.Value, depth int) {
	sgr := e.o.Palette.Object
	entries := v.Entries()
	if len(entries) == 0 {
		e.token(sgr, "{}")
		return
	}
	keys := v.Keys()
	e.token(sgr, "{")
	for i, en := range entries {
		if i > 0 {
			e.token(sgr, ",")
		}
		if e.pretty() {
			e.newline(depth + 1)
		}
		e.open(e.o.Palette.ObjectKey)
		e.quote(keys[i])
		e.close()
		e.token(sgr, ":")
		if e.pretty() {
			e.buf = append(e.buf, ' ')
		}
		e.encode(en.Value, depth+1)
	}
	if e.pretty() {
		e.newline(depth)
	}
	e.token(sgr, "}")
}

// rawString writes s as is, with one U+FFFD for each invalid byte, as jq does.
func (e *encoder) rawString(s string) {
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && n == 1 {
			e.buf = utf8.AppendRune(e.buf, utf8.RuneError)
		} else {
			e.buf = append(e.buf, s[i:i+n]...)
		}
		i += n
	}
}

const hexDigits = "0123456789abcdef"

func (e *encoder) unicode(r rune) {
	e.buf = append(e.buf, '\\', 'u',
		hexDigits[r>>12&0xf], hexDigits[r>>8&0xf], hexDigits[r>>4&0xf], hexDigits[r&0xf])
}

// quote writes s as a JSON string. Invalid UTF-8 becomes U+FFFD, as in jq.
func (e *encoder) quote(s string) {
	e.buf = append(e.buf, '"')
	for _, r := range s {
		switch {
		case r == '"':
			e.buf = append(e.buf, '\\', '"')
		case r == '\\':
			e.buf = append(e.buf, '\\', '\\')
		case r == '\b':
			e.buf = append(e.buf, '\\', 'b')
		case r == '\f':
			e.buf = append(e.buf, '\\', 'f')
		case r == '\n':
			e.buf = append(e.buf, '\\', 'n')
		case r == '\r':
			e.buf = append(e.buf, '\\', 'r')
		case r == '\t':
			e.buf = append(e.buf, '\\', 't')
		case r < 0x20 || r == 0x7f:
			e.unicode(r)
		case r < utf8.RuneSelf:
			e.buf = append(e.buf, byte(r))
		case !e.o.ASCII:
			e.buf = utf8.AppendRune(e.buf, r)
		case r >= 0x10000:
			r -= 0x10000
			e.unicode(0xd800 + r>>10)
			e.unicode(0xdc00 + r&0x3ff)
		default:
			e.unicode(r)
		}
	}
	e.buf = append(e.buf, '"')
}

// goFloat reports whether v is a finite float a yq operator computed, which
// carries no literal and the !!float tag.
func goFloat(v *value.Value) bool {
	if v.Literal() != "" || v.Meta() == nil || v.Meta().Tag != yamlnode.TagFloat {
		return false
	}
	f := v.Float()
	return !math.IsInf(f, 0) && !math.IsNaN(f)
}
