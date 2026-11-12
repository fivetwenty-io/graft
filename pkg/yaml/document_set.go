package yaml

import (
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"

	"github.com/fivetwenty-io/graft/internal/yamlcodec"
	"github.com/fivetwenty-io/graft/internal/yamlnode"
)

// reasonUnwritable is the reason Set refuses a value the encoder can't
// write as one plain scalar on one line.
const reasonUnwritable = "the value can't be written on one line"

// errNotWritten is what a Set check returns when the edited text holds
// a node other than the one Set meant to write.
var errNotWritten = errors.New("the edited text does not hold the value Set wrote")

// Set replaces the scalar at path with value, changing no other byte.
// value is a string, a bool, any Go integer or float, or nil.
//
// The Go types Set accepts are string, bool, int, int8, int16, int32,
// int64, uint, uint8, uint16, uint32, uint64, float32, and float64, and
// an untyped nil. Any other type, a named type over one of these
// included, refuses with ErrUnsupportedEdit.
//
// A string keeps the slot's double quotes, and keeps its single quotes
// when it holds no line break and no character that needs an escape. In
// a plain slot it takes the style MarshalYAML gives it, so a string
// such as yes or 1.0 comes out quoted, since written plain it would
// read back as another type. A string that MarshalYAML writes plain
// but the parser reads as something else, such as "<<", is
// double-quoted. A string with a line break is written in double
// quotes on one line, and inside a flow collection a string that holds
// a flow indicator is double-quoted. Any other value is written plain,
// as MarshalYAML writes it, so a quoted slot loses its quotes.
//
// Set only replaces a value that Editable accepts, and otherwise
// returns the error Editable returns. A path that names nothing is
// ErrPathNotFound, since Set never adds a key. Set parses the edited
// text again, and refuses with ErrUnsupportedEdit unless the new value
// decodes back to value and nothing else in the document has changed.
// A refused Set leaves the bytes as they were.
func (d *Document) Set(path string, value any) error {
	const op = "set"
	tag, ok := setTag(value)
	if !ok {
		reason := fmt.Sprintf("Set does not write a value of Go type %s", reflect.TypeOf(value))
		return &DocumentError{Op: op, Path: path, Reason: reason, err: ErrUnsupportedEdit}
	}
	loc, sp, err := d.setTarget(op, path)
	if err != nil {
		return err
	}
	w, ok := renderSet(loc, value, tag)
	if !ok {
		return loc.refusal(op, path, reasonUnwritable)
	}
	next := make([]byte, 0, len(d.src)-(sp.end-sp.start)+len(w.text))
	next = append(next, d.src[:sp.start]...)
	next = append(next, w.text...)
	next = append(next, d.src[sp.end:]...)
	wrote := span{sp.start, sp.start + len(w.text)}
	return d.commit(op, path, next, loc, sp, func(n *yamlnode.Node) error {
		return w.check(next, wrote, n, value)
	})
}

// setTag returns the tag a value of v's Go type is written with, and
// false for a type Set does not write.
func setTag(v any) (string, bool) {
	switch v.(type) {
	case nil:
		return yamlnode.TagNull, true
	case string:
		return yamlnode.TagStr, true
	case bool:
		return yamlnode.TagBool, true
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return yamlnode.TagInt, true
	case float32, float64:
		return yamlnode.TagFloat, true
	}
	return "", false
}

// setText is the text Set writes and the node it means that text to
// parse to.
type setText struct {
	text  string
	value string
	tag   string
	style yamlnode.Style
}

// renderSet returns the text Set writes for value, whose tag is tag, in
// the slot loc names. It returns false when value has no one-line text.
func renderSet(loc located, value any, tag string) (setText, bool) {
	s, isString := value.(string)
	if !isString {
		out, err := yamlcodec.MarshalYAML(value)
		text := strings.TrimSuffix(string(out), "\n")
		if err != nil || text == "" || strings.ContainsAny(text, "\r\n") {
			return setText{}, false
		}
		return setText{text: text, value: text, tag: tag}, true
	}
	prefer := yamlcodec.InlinePlain
	switch {
	case loc.node.Style&yamlnode.StyleDouble != 0:
		prefer = yamlcodec.InlineDouble
	case loc.node.Style&yamlnode.StyleSingle != 0:
		prefer = yamlcodec.InlineSingle
	}
	text := yamlcodec.InlineScalar(s, prefer)
	style := quotedStyle(text)
	if loc.parent.Style&yamlnode.StyleFlow != 0 && style == 0 && strings.ContainsAny(text, ",[]{}") {
		// A flow indicator ends a plain scalar in a flow collection.
		text = yamlcodec.InlineScalar(s, yamlcodec.InlineDouble)
		style = yamlnode.StyleDouble
	}
	return setText{text: text, value: s, tag: tag, style: style}, true
}

// quotedStyle returns the style of the one-line scalar text: double or
// single quotes by its first byte, and zero for plain text.
func quotedStyle(text string) yamlnode.Style {
	switch {
	case strings.HasPrefix(text, `"`):
		return yamlnode.StyleDouble
	case strings.HasPrefix(text, `'`):
		return yamlnode.StyleSingle
	}
	return 0
}

// check reports whether n, a node of the tree parsed from next, is the
// node Set meant to write for value at wrote. Its Value, Tag, and Style
// must be the ones w names, and its text must be exactly the bytes at
// wrote. The public Node for it must decode into value's Go type as a
// value equal to value, or as a NaN when value is a NaN.
func (w setText) check(next []byte, wrote span, n *yamlnode.Node, value any) error {
	if n.Kind != yamlnode.ScalarNode || n.Value != w.value || n.Tag != w.tag || n.Style != w.style || n.BareTag {
		return errNotWritten
	}
	edited := &Document{src: next, lines: newLineIndex(next)}
	if at, err := edited.scalarSpan(n); err != nil || at != wrote {
		return errNotWritten
	}
	if value == nil {
		return nil
	}
	got := reflect.New(reflect.TypeOf(value))
	if err := publicNode(next, n).Decode(got.Interface()); err != nil {
		return err
	}
	if isNaN(value) {
		if !isNaN(got.Elem().Interface()) {
			return errNotWritten
		}
		return nil
	}
	if !reflect.DeepEqual(got.Elem().Interface(), value) {
		return errNotWritten
	}
	return nil
}

// isNaN reports whether v is a float32 or float64 NaN.
func isNaN(v any) bool {
	switch f := v.(type) {
	case float32:
		return math.IsNaN(float64(f))
	case float64:
		return math.IsNaN(f)
	}
	return false
}
