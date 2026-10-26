package load

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// defaultDepthLimit matches jq's parser limit.
const defaultDepthLimit = 10000

// maxChunk is the most bytes the reader takes at once, which is what jq
// reads from a line at a time, so a very long line is taken in pieces as
// in jq. jq 1.8.2 reads 4,091 bytes of a line at most.
const maxChunk = 4091

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
	off int // bytes taken from br since the last newline

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
	r.stream.explicitLimit = opts.DepthLimit > 0 || opts.Seq && !opts.Stream
	r.stream.recover = opts.StreamErrors
	r.stream.seq, r.stream.waiting = opts.Seq, opts.Seq
	r.stream.events, r.stream.warn = opts.Stream, opts.Warn
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
func (r *JSONReader) fill() error {
	if _, err := r.br.Peek(1); err != nil {
		return err
	}
	ready, _ := r.br.Peek(r.br.Buffered())
	// Pieces of a line end at multiples of maxChunk from its start, where
	// jq's pieces do, whatever br happened to have ready.
	n := min(len(ready), maxChunk-r.off%maxChunk)
	if i := bytes.IndexByte(ready[:n], '\n'); i >= 0 {
		n = i + 1
		r.fed++
		r.off = 0
	} else {
		r.off += n
	}
	r.buf = append(r.buf[:0], ready[:n]...)
	r.pos = 0
	_, _ = r.br.Discard(n)
	return nil
}

// Next returns the next value and the line it started on, and io.EOF after
// the last one. A failure sticks, so later calls return the same error.
func (r *JSONReader) Next() (*value.Value, int, error) {
	if r.err != nil {
		return nil, 0, r.err
	}
	if r.seq {
		v, line, err := r.nextSeqValue()
		if err != nil && !errors.Is(err, io.EOF) {
			r.err = err
		}
		return v, line, err
	}
	if err := r.skipSpace(); err != nil {
		if !errors.Is(err, io.EOF) {
			r.err = err
		}
		return nil, 0, err
	}
	start := r.line + 1
	first, _ := r.peek()
	v, err := r.value(0)
	if err == nil && isLiteralByte(first) {
		err = r.afterLiteral()
	}
	if err != nil {
		r.err = err
		return nil, 0, err
	}
	return v, start, nil
}

// isLiteralByte reports whether jq takes c to be part of a number or
// keyword, which is any byte that is not whitespace, a bracket, a comma, a
// colon, or a quote.
func isLiteralByte(c byte) bool {
	return !strings.ContainsRune(" \t\n\r[]{},:\"", rune(c))
}

// afterLiteral refuses a number or keyword that a closing bracket, comma,
// or colon follows at once. jq finishes such a literal on that very byte,
// finds the byte wrong, and loses the literal with it.
func (r *JSONReader) afterLiteral() error {
	c, err := r.peek()
	if err != nil {
		return nil //nolint:nilerr // the end of input ends the literal, and a read failure shows on the next call
	}
	if !strings.ContainsRune("]},:", rune(c)) {
		return nil
	}
	return r.errHere("unexpected character %q", c)
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
		if err := r.fill(); err != nil {
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
	case isLiteralByte(c):
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
		if !isLiteralByte(c) {
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
		v, ok := jqNumber(s)
		if !ok {
			return nil, r.syntaxError(line, col, "invalid literal %q", s)
		}
		return v, nil
	}
}

// str reads a string. It takes the text up to the closing quote, as jq
// does, and then decodes it, so a bad escape is reported after the quote.
func (r *JSONReader) str() (string, error) {
	r.advance('"')
	var raw []byte
	for {
		c, err := r.peekIn()
		if err != nil {
			return "", err
		}
		switch {
		case c == '"':
			r.advance(c)
			s, msg := decodeString(raw)
			if msg != "" {
				return "", r.errHere("%s", msg)
			}
			return s, nil
		case c < 0x20:
			return "", r.errHere("invalid string: control characters from U+0000 through U+001F must be escaped")
		}
		raw = append(raw, c)
		r.advance(c)
		if c != '\\' {
			continue
		}
		e, err := r.peekIn()
		if err != nil {
			return "", err
		}
		raw = append(raw, e)
		r.advance(e)
	}
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
