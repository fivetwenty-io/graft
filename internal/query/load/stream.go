package load

import (
	"errors"
	"io"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// streamError is a parse failure in streaming mode. It keeps the path the
// parser was at, which --stream-errors reports next to the message.
type streamError struct {
	msg       string
	line, col int
	path      *value.Value
}

func (e *streamError) Error() string { return e.msg }

// text is the message jq prints for the failure, with its position.
func (e *streamError) text() string {
	return e.msg + " at line " + strconv.Itoa(e.line) + ", column " + strconv.Itoa(e.col)
}

// pathElem is one step of the path the streaming parser is at: an array
// index, or an object key that is null until its string is read.
type pathElem struct {
	obj    bool
	idx    int
	key    string
	keySet bool
}

// streamState is where the streaming parser stands.
type streamState struct {
	explicitLimit bool
	recover       bool // report a failure as an event and read on

	path   []pathElem
	next   *value.Value // the leaf read but not yet confirmed by a delimiter
	filled bool         // the current slot holds a leaf or a closed container
	comma  bool         // the last token was a comma
	tok    []byte       // a number or keyword being read
	out    []*value.Value
	err    error
	done   bool

	// A --seq text sequence.
	seq     bool
	waiting bool // text before the first RS is abandoned
	lastWS  bool // the last byte read outside a string was whitespace
	atRS    bool // the byte being handled is an RS
	events  bool // the caller wants stream events, not whole values
	warn    func(string)
	notes   map[*value.Value]seqNote
}

// seqNote is something a --seq reader leaves in the event queue between
// events: a warning to pass on, or the parser starting over, which drops
// a value being put together.
type seqNote struct {
	text  string
	reset bool
}

// NextEvent returns the next streaming event, which is [path, leaf] for a
// scalar or an empty container, and [path] after the last member of a
// container, as jq's --stream prints them. A leaf is held back until the
// delimiter after it arrives, so a value cut short never shows its last
// element. It reads no further than the event needs, and returns io.EOF
// after the last one. A failure is a *streamError after every event that
// came before it, and then it sticks.
func (r *JSONReader) NextEvent() (*value.Value, error) {
	for {
		ev, note, err := r.nextItem()
		if err != nil || note == nil {
			return ev, err
		}
		if note.text != "" && r.stream.warn != nil {
			r.stream.warn(note.text)
		}
	}
}

// nextItem is NextEvent but also returns the notes a --seq reader leaves
// between events, one at a time.
func (r *JSONReader) nextItem() (*value.Value, *seqNote, error) {
	s := &r.stream
	for {
		if len(s.out) > 0 {
			ev := s.out[0]
			s.out[0] = nil
			s.out = s.out[1:]
			if n, ok := s.notes[ev]; ok {
				delete(s.notes, ev)
				return nil, &n, nil
			}
			return ev, nil, nil
		}
		if s.err != nil {
			return nil, nil, s.err
		}
		if s.done {
			return nil, nil, io.EOF
		}
		r.streamStep()
	}
}

func (r *JSONReader) streamStep() {
	s := &r.stream
	c, err := r.peek()
	if err != nil {
		s.done = true
		if errors.Is(err, io.EOF) {
			r.streamEOF()
		} else {
			s.err = err
		}
		return
	}
	r.advance(c)
	if s.seq && r.seqByte(c) {
		return
	}
	switch {
	case c == ' ' || c == '\t' || c == '\n' || c == '\r':
		r.attempt(func() error { return r.flushLiteral(false) })
	case c == '"':
		// The quote ends a number or keyword on a step of its own, so
		// what that produced stands even when the string then fails.
		if r.attempt(func() error { return r.flushLiteral(false) }) {
			r.attempt(r.streamString)
		}
	case strings.IndexByte("[]{},:", c) >= 0:
		r.attempt(func() error {
			if err := r.flushLiteral(false); err != nil {
				return err
			}
			return r.streamToken(c)
		})
	default:
		s.tok = append(s.tok, c)
	}
}

// attempt runs one step of the parser and reports whether it went through.
// What a failing step produced is dropped with it, as jq drops a leaf it
// finished on the very delimiter that broke the text.
func (r *JSONReader) attempt(step func() error) bool {
	s := &r.stream
	before := len(s.out)
	err := step()
	if err == nil {
		return true
	}
	for i := before; i < len(s.out); i++ {
		s.out[i] = nil
	}
	s.out = s.out[:before]
	r.streamFail(err)
	return false
}

// streamFail ends the text on a failure. With --stream-errors the failure
// becomes an event, and unless the input has ended the parser starts over
// on the next line, because jq throws away the rest of the line it was
// reading when a text does not parse.
func (r *JSONReader) streamFail(err error) {
	s := &r.stream
	var se *streamError
	if s.seq && errors.As(err, &se) {
		r.seqFail(se)
		return
	}
	if !s.recover || !errors.As(err, &se) {
		s.err = err
		return
	}
	s.out = append(s.out, value.Arr(value.Str(se.text()), se.path))
	if s.done {
		return
	}
	s.path, s.next, s.filled, s.comma, s.tok = s.path[:0], nil, false, false, s.tok[:0]
	r.pos = len(r.buf)
}

func (r *JSONReader) streamEOF() {
	s := &r.stream
	if s.waiting {
		r.streamFail(r.failAt("Unfinished abandoned text at EOF"))
		return
	}
	if s.seq && !s.events && !s.lastWS && len(s.path) == 0 && len(s.tok) > 0 {
		if _, ok := jqNumber(string(s.tok)); ok {
			s.tok = s.tok[:0]
			r.streamFail(r.failAt("Potentially truncated top-level numeric value at EOF"))
			return
		}
	}
	if err := r.flushLiteral(true); err != nil {
		r.streamFail(err)
		return
	}
	if len(s.path) > 0 {
		r.streamFail(r.failAt("Unfinished JSON term at EOF"))
	}
}

// failAt builds an error for the position just after the last byte read.
func (r *JSONReader) failAt(msg string) *streamError {
	return &streamError{msg: msg, line: r.line + 1, col: r.col, path: r.stream.pathValue()}
}

func (s *streamState) pathValue() *value.Value {
	return pathOf(s.path)
}

func pathOf(path []pathElem) *value.Value {
	elems := make([]*value.Value, len(path))
	for i, p := range path {
		switch {
		case !p.obj:
			elems[i] = value.Num(float64(p.idx))
		case p.keySet:
			elems[i] = value.Str(p.key)
		default:
			elems[i] = value.Null()
		}
	}
	return value.Arr(elems...)
}

// emit queues [path, leaf], or [path] when leaf is nil.
func (s *streamState) emit(path *value.Value, leaf *value.Value) {
	if leaf == nil {
		s.out = append(s.out, value.Arr(path))
		return
	}
	s.out = append(s.out, value.Arr(path, leaf))
}

// flushLiteral ends the number or keyword being read.
func (r *JSONReader) flushLiteral(eof bool) error {
	s := &r.stream
	if len(s.tok) == 0 {
		return nil
	}
	text := string(s.tok)
	s.tok = s.tok[:0]
	var v *value.Value
	switch text {
	case "true":
		v = value.Bool(true)
	case "false":
		v = value.Bool(false)
	case "null":
		v = value.Null()
	default:
		if n, ok := jqNumber(text); ok {
			v = n
			break
		}
		return r.failAt(literalMessage(text, eof))
	}
	err := r.streamValue(v)
	var se *streamError
	if errors.As(err, &se) && eof {
		se.msg += " at EOF"
	}
	return err
}

// streamValue takes a finished scalar.
func (r *JSONReader) streamValue(v *value.Value) error {
	s := &r.stream
	if len(s.path) == 0 {
		s.emit(value.Arr(), v)
		return nil
	}
	if s.filled {
		return r.failAt("Expected separator between values")
	}
	s.next, s.filled, s.comma = v, true, false
	return nil
}

// streamString reads the rest of a string, whose opening quote has been
// read. jq reads up to the closing quote before it looks at what is
// inside, so a bad escape is reported at the quote.
func (r *JSONReader) streamString() error {
	var raw []byte
	for {
		c, err := r.peek()
		if err != nil {
			r.stream.done = true
			if errors.Is(err, io.EOF) {
				return r.failAt("Unfinished string at EOF")
			}
			return err
		}
		r.advance(c)
		if c == recordSeparator && r.stream.seq {
			return r.seqCut(raw)
		}
		if c == '"' {
			break
		}
		raw = append(raw, c)
		if c != '\\' {
			continue
		}
		e, err := r.peek()
		if err != nil {
			r.stream.done = true
			if errors.Is(err, io.EOF) {
				return r.failAt("Unfinished string at EOF")
			}
			return err
		}
		r.advance(e)
		if e == recordSeparator && r.stream.seq {
			return r.seqCut(raw)
		}
		raw = append(raw, e)
	}
	s, msg := decodeStreamString(raw)
	if msg != "" {
		return r.failAt(msg)
	}
	return r.streamValue(value.Str(s))
}

// decodeStreamString resolves the escapes in the text between a string's
// quotes, joining a surrogate pair and turning a lone surrogate or a bad
// UTF-8 byte into U+FFFD. It returns jq's message when the text is bad.
func decodeStreamString(raw []byte) (string, string) {
	out := make([]byte, 0, len(raw))
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		switch {
		case c < 0x20:
			return "", "Invalid string: control characters from U+0000 through U+001F must be escaped"
		case c != '\\':
			out = append(out, c)
			continue
		}
		i++
		var esc byte
		switch raw[i] {
		case '"', '\\', '/':
			esc = raw[i]
		case 'b':
			esc = '\b'
		case 'f':
			esc = '\f'
		case 'n':
			esc = '\n'
		case 'r':
			esc = '\r'
		case 't':
			esc = '\t'
		case 'u':
			rn, n, ok := unicodeAt(raw[i+1:])
			if !ok {
				return "", "Invalid \\uXXXX escape"
			}
			i += n
			out = utf8.AppendRune(out, rn)
			continue
		default:
			return "", "Invalid escape"
		}
		out = append(out, esc)
	}
	return toValidUTF8(out), ""
}

// unicodeAt reads the four hex digits after \u in rest, and a following
// \uXXXX when it completes a surrogate pair. It returns the rune and the
// bytes of rest it used.
func unicodeAt(rest []byte) (rune, int, bool) {
	if len(rest) < 4 {
		return 0, 0, false
	}
	hi, ok := parseHex4(rest)
	if !ok {
		return 0, 0, false
	}
	switch {
	case utf16.IsSurrogate(hi) && hi < 0xDC00:
		if len(rest) >= 10 && rest[4] == '\\' && rest[5] == 'u' {
			if lo, ok := parseHex4(rest[6:]); ok && lo >= 0xDC00 && lo <= 0xDFFF {
				return utf16.DecodeRune(hi, lo), 10, true
			}
		}
		return utf8.RuneError, 4, true
	case utf16.IsSurrogate(hi):
		return utf8.RuneError, 4, true
	}
	return hi, 4, true
}

// streamToken handles one of the structural characters.
func (r *JSONReader) streamToken(c byte) error {
	s := &r.stream
	switch c {
	case '[', '{':
		if err := r.checkOpen(c); err != nil {
			return err
		}
		if s.explicitLimit && len(s.path) >= r.limit {
			return r.failAt("Exceeds depth limit for parsing")
		}
		s.path = append(s.path, pathElem{obj: c == '{'})
		s.next, s.filled, s.comma = nil, false, false
	case ':':
		return r.streamColon()
	case ',':
		return r.streamComma()
	case ']':
		return r.streamCloseArray()
	case '}':
		return r.streamCloseObject()
	}
	return nil
}

// checkOpen refuses a container that opens where no value can go.
func (r *JSONReader) checkOpen(c byte) error {
	s := &r.stream
	if len(s.path) == 0 {
		return nil
	}
	// jq lets a '[' follow a container that has closed, and refuses a '{'.
	if s.next != nil || c == '{' && s.filled {
		if !s.events {
			return r.failAt("Expected separator between values")
		}
		return r.failAt("Expected a separator between values")
	}
	if !s.events {
		return nil // the plain parser lets a container stand as a key
	}
	if top := s.path[len(s.path)-1]; top.obj && !top.keySet {
		after := "'{'"
		if s.comma {
			after = "',' in object"
		}
		return r.failAt("Expected string key after " + after + ", not '" + string(c) + "'")
	}
	return nil
}

// topLevel is what jq adds to a message about a bracket at the top level,
// which its streaming parser says and its plain one does not.
func (r *JSONReader) topLevel() string {
	if r.stream.events {
		return " at the top-level"
	}
	return ""
}

func (r *JSONReader) streamColon() error {
	s := &r.stream
	if !s.events && s.next == nil && (len(s.path) == 0 || !s.path[len(s.path)-1].obj) {
		return r.failAt("Expected string key before ':'")
	}
	if len(s.path) == 0 || !s.path[len(s.path)-1].obj {
		return r.failAt("':' not as part of an object")
	}
	if s.next == nil && s.filled && !s.events {
		return r.failAt("Object keys must be strings")
	}
	if s.next == nil || s.comma {
		return r.failAt("Expected string key before ':'")
	}
	if s.next.Kind() != value.KindString {
		return r.failAt("Object keys must be strings")
	}
	top := &s.path[len(s.path)-1]
	top.key, top.keySet = s.next.Str(), true
	s.next, s.filled = nil, false
	return nil
}

func (r *JSONReader) streamComma() error {
	s := &r.stream
	if len(s.path) == 0 {
		return r.failAt("Expected value before ','")
	}
	top := &s.path[len(s.path)-1]
	if !s.filled {
		return r.failAt("Expected value before ','")
	}
	if top.obj && !top.keySet {
		return r.failAt("Objects must consist of key:value pairs")
	}
	if s.next != nil {
		s.emit(s.pathValue(), s.next)
	}
	if top.obj {
		top.key, top.keySet = "", false
	} else {
		top.idx++
	}
	s.next, s.filled, s.comma = nil, false, true
	return nil
}

func (r *JSONReader) streamCloseArray() error {
	s := &r.stream
	if len(s.path) == 0 {
		return r.failAt("Unmatched ']'" + r.topLevel())
	}
	if s.comma {
		return r.failAt("Expected another array element")
	}
	if s.path[len(s.path)-1].obj {
		if !s.events {
			return r.failAt("Unmatched ']'")
		}
		return r.failAt("Unmatched ']' in the middle of an object")
	}
	r.closeContainer(value.Arr())
	return nil
}

func (r *JSONReader) streamCloseObject() error {
	s := &r.stream
	if len(s.path) == 0 {
		return r.failAt("Unmatched '}'" + r.topLevel())
	}
	if s.comma {
		return r.failAt("Expected another key:value pair")
	}
	top := s.path[len(s.path)-1]
	if !top.obj && !s.events {
		if s.filled {
			return r.failAt("Objects must consist of key:value pairs")
		}
		return r.failAt("Unmatched '}'")
	}
	if !top.obj {
		return r.failAt("Unmatched '}' in the middle of an array")
	}
	switch {
	case top.keySet && !s.filled:
		return r.failAt("Missing value in key:value pair")
	case !top.keySet && s.filled:
		return r.failAt("Objects must consist of key:value pairs")
	}
	r.closeContainer(value.Obj())
	return nil
}

// closeContainer ends the innermost container. A container with members
// ends with a closing event for its last member, and an empty one is a
// leaf of its parent, which empty stands for.
func (r *JSONReader) closeContainer(empty *value.Value) {
	s := &r.stream
	if s.filled {
		here := s.pathValue()
		if s.next != nil {
			s.emit(here, s.next)
		}
		s.emit(here, nil)
	} else {
		s.emit(pathOf(s.path[:len(s.path)-1]), empty)
	}
	s.path = s.path[:len(s.path)-1]
	s.next, s.comma = nil, false
	s.filled = len(s.path) > 0
}

// StreamEvents returns the streaming events of v, which is what jq's
// tostream makes of it. Input read in a syntax that has no incremental
// reader, such as YAML, streams this way once its document is loaded.
func StreamEvents(v *value.Value) []*value.Value {
	var out []*value.Value
	return appendEvents(out, v, nil)
}

func appendEvents(out []*value.Value, v *value.Value, path []*value.Value) []*value.Value {
	here := func() *value.Value { return value.Arr(append([]*value.Value(nil), path...)...) }
	switch {
	case v.Kind() == value.KindArray && v.Len() > 0:
		elems := v.Elems()
		for i, e := range elems {
			out = appendEvents(out, e, append(path, value.Num(float64(i))))
		}
		return append(out, value.Arr(value.Arr(append(append([]*value.Value(nil), path...), value.Num(float64(len(elems)-1)))...)))
	case v.Kind() == value.KindObject && v.Len() > 0:
		entries := v.Entries()
		for _, e := range entries {
			out = appendEvents(out, e.Value, append(path, value.Str(e.Key.Str())))
		}
		last := entries[len(entries)-1]
		return append(out, value.Arr(value.Arr(append(append([]*value.Value(nil), path...), value.Str(last.Key.Str()))...)))
	}
	return append(out, value.Arr(here(), v))
}
