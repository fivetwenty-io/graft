package load

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// defaultDepthLimit matches jq's parser limit.
const defaultDepthLimit = 10000

// maxChunk is the most bytes the reader takes at once, which is what jq's
// input buffer holds, so a very long line is taken in pieces as in jq.
const maxChunk = 4095

// recordSeparator is the byte --seq puts before each JSON text.
const recordSeparator = 0x1e

// JSONReader reads concatenated JSON values one at a time. It keeps every
// number's literal text, keeps object key order with the last value winning
// for a repeated key, and stops right after each value so the bytes behind
// it stay untouched until the next call.
type JSONReader struct {
	br     *bufio.Reader
	source string
	limit  int
	seq    bool
	line   int // newlines consumed
	col    int // bytes consumed on the current line
	err    error

	// The reader takes bytes from br a line at a time, as jq does, and
	// never waits for more than the first byte of a line to arrive.
	buf []byte
	pos int
	fed int // newlines in the lines taken from br so far

	stream streamState
}

// NewJSONReader reads JSON values from br. The source name goes into
// syntax errors, and opts supplies DepthLimit and Seq.
func NewJSONReader(br *bufio.Reader, source string, opts LoadOptions) *JSONReader {
	limit := opts.DepthLimit
	if limit <= 0 {
		limit = defaultDepthLimit
	}
	r := &JSONReader{br: br, source: source, limit: limit, seq: opts.Seq}
	r.stream.explicitLimit = opts.DepthLimit > 0
	r.stream.recover = opts.StreamErrors
	return r
}

// Line returns the number of newlines in the input taken so far, which is
// what input_line_number reports. jq takes its input a line at a time, so
// a value that ends in the middle of a line counts that whole line, and a
// last line with no newline never counts.
func (r *JSONReader) Line() int { return r.fed }

// fill takes the next piece of input into buf, which is the rest of the
// line br has ready, ending at its newline. It returns an error only when
// br has no more bytes to give.
func (r *JSONReader) fill(keep bool) error {
	if _, err := r.br.Peek(1); err != nil {
		return err
	}
	ready, _ := r.br.Peek(r.br.Buffered())
	n := min(len(ready), maxChunk)
	if i := bytes.IndexByte(ready[:n], '\n'); i >= 0 {
		n = i + 1
		r.fed++
	}
	if keep {
		r.buf = append(r.buf, ready[:n]...)
	} else {
		r.buf = append(r.buf[:0], ready[:n]...)
		r.pos = 0
	}
	_, _ = r.br.Discard(n)
	return nil
}

// Next returns the next value and the line it started on, and io.EOF after
// the last one. A failure sticks, so later calls return the same error.
func (r *JSONReader) Next() (*value.Value, int, error) {
	if r.err != nil {
		return nil, 0, r.err
	}
	if err := r.skipSpace(); err != nil {
		if !errors.Is(err, io.EOF) {
			r.err = err
		}
		return nil, 0, err
	}
	start := r.line + 1
	v, err := r.value(0)
	if err != nil {
		r.err = err
		return nil, 0, err
	}
	return v, start, nil
}

func (r *JSONReader) syntaxError(line, col int, format string, args ...any) error {
	return &SyntaxError{Source: r.source, Line: line, Col: col, Msg: fmt.Sprintf(format, args...)}
}

// errHere reports a problem at the next unread byte.
func (r *JSONReader) errHere(format string, args ...any) error {
	return r.syntaxError(r.line+1, r.col+1, format, args...)
}

// peek returns the next byte without consuming it, and io.EOF at the end.
func (r *JSONReader) peek() (byte, error) {
	if r.pos >= len(r.buf) {
		if err := r.fill(false); err != nil {
			return 0, err
		}
	}
	return r.buf[r.pos], nil
}

// peekIn is peek inside a value, where the end of input is an error.
func (r *JSONReader) peekIn() (byte, error) {
	c, err := r.peek()
	if errors.Is(err, io.EOF) {
		return 0, r.errHere("unexpected end of input")
	}
	return c, err
}

// advance consumes the byte peek returned.
func (r *JSONReader) advance(c byte) {
	r.pos++
	if c == '\n' {
		r.line++
		r.col = 0
		return
	}
	r.col++
}

func (r *JSONReader) skipSpace() error {
	for {
		c, err := r.peek()
		if err != nil {
			return err
		}
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
		case c == recordSeparator && r.seq:
		default:
			return nil
		}
		r.advance(c)
	}
}

// skipIn is skipSpace inside a value, where the end of input is left for
// the next read to report.
func (r *JSONReader) skipIn() error {
	if err := r.skipSpace(); !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

func (r *JSONReader) value(depth int) (*value.Value, error) {
	c, err := r.peekIn()
	if err != nil {
		return nil, err
	}
	switch {
	case c == '{':
		return r.object(depth + 1)
	case c == '[':
		return r.array(depth + 1)
	case c == '"':
		s, err := r.str()
		if err != nil {
			return nil, err
		}
		return value.Str(s), nil
	case c == '-' || c >= '0' && c <= '9' || c == 't' || c == 'f' || c == 'n':
		return r.token()
	}
	return nil, r.errHere("unexpected character %q", c)
}

func (r *JSONReader) tooDeep(depth int) error {
	if depth > r.limit {
		return r.errHere("nesting exceeds maximum depth of %d", r.limit)
	}
	return nil
}

func (r *JSONReader) array(depth int) (*value.Value, error) {
	if err := r.tooDeep(depth); err != nil {
		return nil, err
	}
	r.advance('[')
	b := value.NewArrBuilder(0)
	if err := r.skipIn(); err != nil {
		return nil, err
	}
	if c, err := r.peekIn(); err != nil {
		return nil, err
	} else if c == ']' {
		r.advance(c)
		return b.Seal(), nil
	}
	for {
		if err := r.skipIn(); err != nil {
			return nil, err
		}
		v, err := r.value(depth)
		if err != nil {
			return nil, err
		}
		b.Append(v)
		if err := r.skipIn(); err != nil {
			return nil, err
		}
		c, err := r.peekIn()
		if err != nil {
			return nil, err
		}
		switch c {
		case ',':
			r.advance(c)
		case ']':
			r.advance(c)
			return b.Seal(), nil
		default:
			return nil, r.errHere("expected ',' or ']' but found %q", c)
		}
	}
}

func (r *JSONReader) object(depth int) (*value.Value, error) {
	if err := r.tooDeep(depth); err != nil {
		return nil, err
	}
	r.advance('{')
	b := value.NewObjBuilder(0)
	if err := r.skipIn(); err != nil {
		return nil, err
	}
	if c, err := r.peekIn(); err != nil {
		return nil, err
	} else if c == '}' {
		r.advance(c)
		return b.Seal(), nil
	}
	for {
		if err := r.member(b, depth); err != nil {
			return nil, err
		}
		if err := r.skipIn(); err != nil {
			return nil, err
		}
		c, err := r.peekIn()
		if err != nil {
			return nil, err
		}
		switch c {
		case ',':
			r.advance(c)
		case '}':
			r.advance(c)
			return b.Seal(), nil
		default:
			return nil, r.errHere("expected ',' or '}' but found %q", c)
		}
	}
}

// member reads one "key": value pair into b.
func (r *JSONReader) member(b *value.ObjBuilder, depth int) error {
	if err := r.skipIn(); err != nil {
		return err
	}
	c, err := r.peekIn()
	if err != nil {
		return err
	}
	if c != '"' {
		return r.errHere("object key must be a string, found %q", c)
	}
	key, err := r.str()
	if err != nil {
		return err
	}
	if err := r.skipIn(); err != nil {
		return err
	}
	if c, err = r.peekIn(); err != nil {
		return err
	}
	if c != ':' {
		return r.errHere("expected ':' but found %q", c)
	}
	r.advance(c)
	if err := r.skipIn(); err != nil {
		return err
	}
	v, err := r.value(depth)
	if err != nil {
		return err
	}
	b.Set(key, v)
	return nil
}

// token reads a number, true, false, or null.
func (r *JSONReader) token() (*value.Value, error) {
	line, col := r.line+1, r.col+1
	var tok []byte
	for {
		c, err := r.peek()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if !isTokenByte(c) {
			break
		}
		tok = append(tok, c)
		r.advance(c)
	}
	switch s := string(tok); s {
	case "true":
		return value.Bool(true), nil
	case "false":
		return value.Bool(false), nil
	case "null":
		return value.Null(), nil
	default:
		if !isJSONNumber(s) {
			return nil, r.syntaxError(line, col, "invalid literal %q", s)
		}
		return numberLiteral(s), nil
	}
}

func (r *JSONReader) str() (string, error) {
	r.advance('"')
	var buf []byte
	for {
		c, err := r.peekIn()
		if err != nil {
			return "", err
		}
		switch {
		case c == '"':
			r.advance(c)
			return toValidUTF8(buf), nil
		case c == '\\':
			r.advance(c)
			if buf, err = r.escape(buf); err != nil {
				return "", err
			}
		case c < 0x20:
			return "", r.errHere("invalid string: control characters from U+0000 through U+001F must be escaped")
		default:
			buf = append(buf, c)
			r.advance(c)
		}
	}
}

// escape reads the text after a backslash and appends what it stands for.
func (r *JSONReader) escape(buf []byte) ([]byte, error) {
	c, err := r.peekIn()
	if err != nil {
		return nil, err
	}
	var out byte
	switch c {
	case '"', '\\', '/':
		out = c
	case 'b':
		out = '\b'
	case 'f':
		out = '\f'
	case 'n':
		out = '\n'
	case 'r':
		out = '\r'
	case 't':
		out = '\t'
	case 'u':
		r.advance(c)
		return r.unicodeEscape(buf)
	default:
		return nil, r.errHere("invalid escape \\%c", c)
	}
	r.advance(c)
	return append(buf, out), nil
}

// unicodeEscape reads the four hex digits after \u, joining a surrogate
// pair and turning a lone surrogate into U+FFFD.
func (r *JSONReader) unicodeEscape(buf []byte) ([]byte, error) {
	hi, err := r.hex4()
	if err != nil {
		return nil, err
	}
	rn := hi
	switch {
	case utf16.IsSurrogate(rn) && rn < 0xDC00:
		if lo, ok := r.pairedLow(); ok {
			rn = utf16.DecodeRune(rn, lo)
		} else {
			rn = utf8.RuneError
		}
	case utf16.IsSurrogate(rn):
		rn = utf8.RuneError
	}
	return utf8.AppendRune(buf, rn), nil
}

// pairedLow consumes a following \uXXXX escape when it is a low surrogate.
func (r *JSONReader) pairedLow() (rune, bool) {
	for len(r.buf)-r.pos < 6 {
		// Only a following escape is worth reading ahead for, and a
		// newline in the way rules it out without taking the next line.
		if !mayStartEscape(r.buf[r.pos:]) || r.fill(true) != nil {
			return 0, false
		}
	}
	b := r.buf[r.pos : r.pos+6]
	if b[0] != '\\' || b[1] != 'u' {
		return 0, false
	}
	n, ok := parseHex4(b[2:6])
	if !ok || n < 0xDC00 || n > 0xDFFF {
		return 0, false
	}
	for _, c := range b {
		r.advance(c)
	}
	return n, true
}

// mayStartEscape reports whether rest could be the start of a \uXXXX
// escape that has not all arrived.
func mayStartEscape(rest []byte) bool {
	for i, c := range rest {
		switch {
		case i == 0 && c != '\\', i == 1 && c != 'u', c == '\n':
			return false
		}
	}
	return true
}

func (r *JSONReader) hex4() (rune, error) {
	var digits [4]byte
	for i := range digits {
		c, err := r.peekIn()
		if err != nil {
			return 0, err
		}
		digits[i] = c
		if _, ok := parseHex4([]byte{c, '0', '0', '0'}); !ok {
			return 0, r.errHere("invalid \\u escape")
		}
		r.advance(c)
	}
	n, _ := parseHex4(digits[:])
	return n, nil
}

func parseHex4(b []byte) (rune, bool) {
	var n rune
	for _, c := range b[:4] {
		switch {
		case c >= '0' && c <= '9':
			n = n<<4 | rune(c-'0')
		case c >= 'a' && c <= 'f':
			n = n<<4 | rune(c-'a'+10)
		case c >= 'A' && c <= 'F':
			n = n<<4 | rune(c-'A'+10)
		default:
			return 0, false
		}
	}
	return n, true
}
