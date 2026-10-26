package yamlout

import "github.com/fivetwenty-io/graft/pkg/query/value"

// Comments wait in the layout until the point where yq would write them.
// A node's opening sets them, and later calls write and clear them, so a
// comment the layout cannot place right away rides along to the next
// place that takes its kind. That is how yq's emitter behaves, down to a
// collection's foot comment landing after the next key's value.

// analyzeStart picks up the comments of the event that opens v. A scalar
// or alias brings all of its comments, and a collection only its head,
// since its line and foot comments come at its end. tail is the foot
// comment of the key before, which a key brings along. dropFoot leaves
// out a foot comment written elsewhere, as a key's or the root's is.
func (e *emitter) analyzeStart(v *value.Value, tail string, dropFoot bool) {
	if tail != "" {
		e.tail = tail
	}
	m := v.Meta()
	if m == nil {
		return
	}
	if m.HeadComment != "" {
		e.head = m.HeadComment
	}
	switch kindOf(v) {
	case scalarNode, aliasNode:
		e.pickLineAndFoot(m, dropFoot)
	case seqNode, mapNode:
	}
}

// analyzeEnd picks up the comments of the event that closes collection v.
func (e *emitter) analyzeEnd(v *value.Value, tail string, dropFoot bool) {
	if tail != "" {
		e.tail = tail
	}
	if m := v.Meta(); m != nil {
		e.pickLineAndFoot(m, dropFoot)
	}
}

func (e *emitter) pickLineAndFoot(m *value.Meta, dropFoot bool) {
	if m.LineComment != "" {
		e.line = m.LineComment
	}
	if m.FootComment != "" && !dropFoot {
		e.foot = m.FootComment
	}
}

// commentsPending reports whether a comment is waiting to follow the node
// about to be written, which in flow style moves the comma before it.
func (e *emitter) commentsPending() bool {
	return e.line != "" || e.foot != "" || e.tail != ""
}

// processHead writes a waiting tail and then a waiting head comment, each
// on lines of its own. A tail sets the next line at this indentation
// apart, as a foot comment does.
func (e *emitter) processHead() {
	if e.tail != "" {
		e.writeIndent()
		e.writeComment(e.tail)
		e.tail = ""
		e.footIndent = max(e.level, 0)
	}
	if e.head != "" {
		e.writeIndent()
		e.writeComment(e.head)
		e.head = ""
	}
}

// processLine writes a waiting line comment at the end of the current
// line. With none waiting it ends the line only when linebreak is set,
// as after a block scalar's header.
func (e *emitter) processLine(linebreak bool) {
	if e.line == "" {
		if linebreak {
			e.newline()
		}
		return
	}
	if !e.ws {
		e.put(" ")
	}
	e.writeComment(e.line)
	e.line = ""
}

// processFoot writes a waiting foot comment on lines of its own.
func (e *emitter) processFoot() {
	if e.foot == "" {
		return
	}
	e.writeIndent()
	e.writeComment(e.foot)
	e.foot = ""
	e.footIndent = max(e.level, 0)
}

// writeComment writes comment lines at the current indentation, adding
// "# " to a line that does not start with "#", and ends the last line.
func (e *emitter) writeComment(comment string) {
	breaks, pound := false, false
	for _, r := range comment {
		if isBreakRune(r) {
			e.lineBreak(r)
			breaks, pound = true, false
			continue
		}
		if breaks {
			e.writeIndent()
		}
		if !pound {
			if r != '#' {
				e.put("# ")
			}
			pound = true
		}
		e.put(string(r))
		e.indention = false
		breaks = false
	}
	if !breaks {
		e.newline()
	}
	e.ws = true
}
