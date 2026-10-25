// Package yamlout writes plain query values as YAML text in block style,
// keeping object keys in input order.
package yamlout

import (
	"bufio"
	"io"
	"strings"

	"github.com/fivetwenty-io/graft/pkg/query/value"
	"github.com/fivetwenty-io/graft/pkg/sortkeys"

	"github.com/fivetwenty-io/graft/internal/query/jqout"
)

const (
	minIndent = 2
	maxIndent = 7
)

// Options selects the output form.
type Options struct {
	Indent       int  // spaces per level, clamped to 2 through 7
	NoDoc        bool // omit the --- line between documents
	UnwrapScalar bool // a top-level string prints as its bare text
	FloatNumbers bool // print computed numbers by jq's double rule
	SortKeys     sortkeys.Mode
}

type emitter struct {
	w      *bufio.Writer
	o      Options
	indent int
	docs   int
	buf    strings.Builder
}

// New returns an Encoder that writes each value to w as a YAML document.
func New(w io.Writer, o Options) jqout.Encoder {
	indent := o.Indent
	if indent < minIndent {
		indent = minIndent
	}
	if indent > maxIndent {
		indent = maxIndent
	}
	return &emitter{w: bufio.NewWriter(w), o: o, indent: indent}
}

func (e *emitter) Flush() error { return e.w.Flush() }

func (e *emitter) Write(v *value.Value) error {
	if e.o.SortKeys != "" && e.o.SortKeys != sortkeys.None {
		v = sortkeys.Sort(v, e.o.SortKeys)
	}
	e.buf.Reset()
	if e.docs > 0 && !e.o.NoDoc {
		e.buf.WriteString("---\n")
	}
	e.docs++
	switch {
	case v.Kind() == value.KindString && e.o.UnwrapScalar:
		e.buf.WriteString(v.Str())
		e.buf.WriteByte('\n')
	case isCollection(v) && v.Len() > 0:
		e.collection(v, 0)
	default:
		e.buf.WriteString(e.topScalar(v))
		e.buf.WriteByte('\n')
	}
	_, err := e.w.WriteString(e.buf.String())
	return err
}

func isCollection(v *value.Value) bool {
	return v.Kind() == value.KindArray || v.Kind() == value.KindObject
}

// topScalar writes a document that is a single scalar. A string always
// carries double quotes unless the caller asked for it bare.
func (e *emitter) topScalar(v *value.Value) string {
	if v.Kind() == value.KindString {
		return doubleQuoted(v.Str())
	}
	return e.scalar(v, 0, 0, false)
}

// scalar returns the text of a scalar or an empty collection. A literal
// block is laid out with its body at bodyIndent, hinted relative to
// parentIndent.
func (e *emitter) scalar(v *value.Value, bodyIndent, parentIndent int, key bool) string {
	switch v.Kind() {
	case value.KindNull:
		return "null"
	case value.KindTrue:
		return "true"
	case value.KindFalse:
		return "false"
	case value.KindNumber:
		return yamlNumber(v, e.o.FloatNumbers)
	case value.KindArray:
		return "[]"
	case value.KindObject:
		return "{}"
	case value.KindString:
		return e.str(v.Str(), bodyIndent, parentIndent, key)
	}
	return ""
}

func (e *emitter) str(s string, bodyIndent, parentIndent int, key bool) string {
	switch scalarStyle(s, scalarContext{Key: key}) {
	case StyleDoubleQuoted:
		return doubleQuoted(s)
	case StyleSingleQuoted:
		return singleQuoted(s)
	case StyleLiteral:
		return e.literal(s, bodyIndent, bodyIndent-parentIndent)
	case StylePlain, StyleFolded:
	}
	return s
}

func (e *emitter) literal(s string, bodyIndent, hint int) string {
	var b strings.Builder
	b.WriteString(literalHeader(s, hint))
	pad := strings.Repeat(" ", bodyIndent)
	body := strings.TrimSuffix(s, "\n")
	for _, line := range strings.Split(body, "\n") {
		b.WriteByte('\n')
		if line != "" {
			b.WriteString(pad)
			b.WriteString(line)
		}
	}
	return b.String()
}

// collection writes a non-empty mapping or sequence whose first line starts
// at the current position and whose lines are indented col spaces. The first
// line is not padded, so a caller can place it after a "- ".
func (e *emitter) collection(v *value.Value, col int) {
	e.block(v, col, false)
}

func (e *emitter) pad(col int) {
	for i := 0; i < col; i++ {
		e.buf.WriteByte(' ')
	}
}

// block writes v's lines at col. When inline is true the first line follows
// text already written, so it is not padded.
func (e *emitter) block(v *value.Value, col int, inline bool) {
	if v.Kind() == value.KindObject {
		for i, en := range v.Entries() {
			if i > 0 || !inline {
				e.pad(col)
			}
			e.entry(en, col)
		}
		return
	}
	for i, el := range v.Elems() {
		if i > 0 || !inline {
			e.pad(col)
		}
		e.buf.WriteByte('-')
		e.child(el, col+2, col, true)
	}
}

func (e *emitter) entry(en value.Entry, col int) {
	e.buf.WriteString(e.key(en.Key))
	e.buf.WriteByte(':')
	e.child(en.Value, col+e.indent, col, false)
}

// child writes what follows a "key:" or "-" at column parent. A scalar or
// empty collection stays on the line, and a non-empty collection starts a
// new line in the mapping case or follows the dash in the sequence case.
func (e *emitter) child(v *value.Value, inner, parent int, dash bool) {
	if isCollection(v) && v.Len() > 0 {
		if dash {
			e.buf.WriteByte(' ')
			e.block(v, inner, true)
			return
		}
		e.buf.WriteByte('\n')
		e.block(v, inner, false)
		return
	}
	e.buf.WriteByte(' ')
	e.buf.WriteString(e.scalar(v, inner, parent, false))
	e.buf.WriteByte('\n')
}

func (e *emitter) key(k *value.Value) string {
	return e.scalar(k, 0, 0, true)
}
