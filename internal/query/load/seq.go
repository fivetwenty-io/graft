package load

import (
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// seqByte handles a byte of a --seq text sequence ahead of the ordinary
// parser and reports whether it dealt with the byte. Text before the first
// RS is abandoned. An RS ends whatever text came before it, and when that
// text was still going the RS reports it truncated, as jq does.
func (r *JSONReader) seqByte(c byte) bool {
	s := &r.stream
	if s.waiting {
		s.waiting = c != recordSeparator
		return true
	}
	if c != recordSeparator {
		s.lastWS = c == ' ' || c == '\t' || c == '\n' || c == '\r'
		return false
	}
	s.atRS = true
	if r.seqTruncated(len(s.tok)) {
		r.streamFail(r.failAt(r.truncation(string(s.tok))))
		return true
	}
	if r.attempt(func() error { return r.flushLiteral(false) }) {
		r.seqReset()
	}
	s.atRS = false
	return true
}

// truncation is jq's message for an RS that cuts a text short, where text
// is the number, keyword, or string contents being read.
func (r *JSONReader) truncation(text string) string {
	if len(r.stream.path) == 0 {
		if _, ok := jqNumber(text); ok {
			return "Potentially truncated top-level numeric value"
		}
	}
	return "Truncated value"
}

// seqTruncated reports whether an RS now would cut a text short, with n
// bytes of a number, keyword, or string being read. A text read as events
// is cut short anywhere inside a container. A whole value is cut short
// when no whitespace ended its last byte, and when it is a container or
// has a token under way.
func (r *JSONReader) seqTruncated(n int) bool {
	s := &r.stream
	if s.events {
		return len(s.path) > 0
	}
	return !s.lastWS && (len(s.path) > 0 || n > 0)
}

// seqCut handles an RS inside a string, whose contents so far are raw.
func (r *JSONReader) seqCut(raw []byte) error {
	s := &r.stream
	s.atRS = true
	text := string(raw)
	switch {
	case r.seqTruncated(len(raw)):
		return r.failAt(r.truncation(text))
	case s.events && len(raw) > 0:
		// jq reads the contents of a string it finds cut short as a
		// number or keyword, and drops it if it is one.
		if !validLiteral(text) {
			return r.failAt(literalMessage(text, false))
		}
	}
	r.seqReset()
	s.atRS = false
	return nil
}

func validLiteral(text string) bool {
	switch text {
	case "true", "false", "null":
		return true
	}
	_, ok := jqNumber(text)
	return ok
}

// literalMessage is jq's complaint about a number or keyword it cannot read.
func literalMessage(text string, eof bool) string {
	msg := "Invalid numeric literal"
	if c := text[0]; c == 't' || c == 'f' || c == 'n' && len(text) > 1 && text[1] == 'u' {
		msg = "Invalid literal"
	}
	if eof {
		msg += " at EOF"
	}
	return msg
}

// seqFail reports a damaged text and starts over, reading on from the next
// byte, where a plain stream throws away the rest of its line.
func (r *JSONReader) seqFail(se *streamError) {
	s := &r.stream
	text := se.text()
	if !s.atRS && !s.done {
		text += " (need RS to resync)"
	}
	if s.recover {
		s.out = append(s.out, value.Arr(value.Str(text), se.path))
	} else {
		s.note(seqNote{text: text})
	}
	r.seqReset()
	s.atRS = false
}

// seqReset makes the parser start a new text.
func (r *JSONReader) seqReset() {
	s := &r.stream
	s.path, s.next, s.filled, s.comma, s.tok = s.path[:0], nil, false, false, s.tok[:0]
	s.note(seqNote{reset: true})
}

// note leaves n in the queue behind the events already there.
func (s *streamState) note(n seqNote) {
	if s.notes == nil {
		s.notes = map[*value.Value]seqNote{}
	}
	m := value.Arr(value.Num(0))
	s.notes[m] = n
	s.out = append(s.out, m)
}

// nextSeqValue reads the next whole value of a text sequence. It puts a
// value together from the parser's events, which lets a damaged text be
// dropped and the next one read.
func (r *JSONReader) nextSeqValue() (*value.Value, int, error) {
	var a assembler
	for {
		ev, note, err := r.nextItem()
		if err != nil {
			return nil, 0, err
		}
		if note != nil {
			if note.reset {
				a = assembler{}
			}
			if note.text != "" && r.stream.warn != nil {
				r.stream.warn(note.text)
			}
			continue
		}
		if v := a.add(ev); v != nil {
			return v, r.line + 1, nil
		}
	}
}

// assembler puts a value together from streaming events. The path of an
// event says where it belongs, with a number for an array index and a
// string for an object key.
type assembler struct {
	root *anode
}

type anode struct {
	obj    bool
	closed bool
	keys   []string
	kids   []*anode
	leaf   *value.Value
}

func isKeyElem(e *value.Value) bool { return e.Kind() == value.KindString }

// add takes one event and returns the value it completes, or nil.
func (a *assembler) add(ev *value.Value) *value.Value {
	parts := ev.Elems()
	path := parts[0].Elems()
	n := len(path)
	if len(parts) == 2 {
		if n == 0 {
			return parts[1]
		}
		a.container(path, n-1).put(path[n-1], &anode{leaf: parts[1], closed: true})
		return nil
	}
	if n == 1 {
		v := a.root.build()
		a.root = nil
		return v
	}
	a.container(path, n-1).closed = true
	return nil
}

// container returns the container at the given depth of path, making the
// ones on the way as needed. A repeated key starts a new member.
func (a *assembler) container(path []*value.Value, depth int) *anode {
	if a.root == nil {
		a.root = &anode{obj: isKeyElem(path[0])}
	}
	cur := a.root
	for d := 0; d < depth; d++ {
		cur = cur.member(path[d], isKeyElem(path[d+1]))
	}
	return cur
}

// member returns the open container under elem, or starts one.
func (n *anode) member(elem *value.Value, obj bool) *anode {
	if n.obj {
		key := elem.Str()
		for i := len(n.keys) - 1; i >= 0; i-- {
			if n.keys[i] == key {
				if k := n.kids[i]; k.leaf == nil && !k.closed {
					return k
				}
				break
			}
		}
		kid := &anode{obj: obj}
		n.keys, n.kids = append(n.keys, key), append(n.kids, kid)
		return kid
	}
	idx := int(elem.Float())
	for len(n.kids) <= idx {
		n.kids = append(n.kids, &anode{obj: obj})
	}
	return n.kids[idx]
}

// put sets the member at elem.
func (n *anode) put(elem *value.Value, kid *anode) {
	if n.obj {
		n.keys, n.kids = append(n.keys, elem.Str()), append(n.kids, kid)
		return
	}
	idx := int(elem.Float())
	for len(n.kids) <= idx {
		n.kids = append(n.kids, nil)
	}
	n.kids[idx] = kid
}

func (n *anode) build() *value.Value {
	switch {
	case n.leaf != nil:
		return n.leaf
	case n.obj:
		b := value.NewObjBuilder(len(n.kids))
		for i, k := range n.kids {
			b.Set(n.keys[i], k.build())
		}
		return b.Seal()
	}
	elems := make([]*value.Value, len(n.kids))
	for i, k := range n.kids {
		elems[i] = k.build()
	}
	return value.Arr(elems...)
}
