package yaml

import (
	"bytes"

	"github.com/fivetwenty-io/graft/internal/yamlnode"
)

// reasonOtherPart is the reason the verifier refuses every edit with.
const reasonOtherPart = "the edit would change another part of the document"

// commit puts next in place of d.src when next is d.src with the bytes
// of old replaced, parses to exactly one document whose tree matches
// d's everywhere except at the node loc names, and check accepts the
// node next holds there. On any failure it leaves d unchanged and
// returns a DocumentError wrapping ErrUnsupportedEdit.
//
// old is the span of d.src the edit replaces. Every byte before its
// start must open next unchanged, and every byte after its end must
// close next unchanged. The node next holds where loc pointed must keep
// the old node's comments and anchor. The lines before the node's first
// line must stay as they are, and the lines after its last line must
// follow the node's new last line unchanged, so they move only by the
// number of lines the node gained or lost.
//
// commit does not look at the bytes the edit wrote in place of old, and
// does not compare the new node's Value, Style, or Tag with the old
// one's, since an edit means to change them. check must compare them
// with what the edit meant to write, and make sure the new node's text
// is the text the edit wrote.
func (d *Document) commit(op, path string, next []byte, loc located, old span, check func(n *yamlnode.Node) error) error {
	if !keepsAround(d.src, next, old) {
		return refuseEdit(op, path, loc.written().Line)
	}
	was := loc.node
	return d.swapIn(op, path, loc.written().Line, next, func(root *yamlnode.Node, lines []int) bool {
		var found *yamlnode.Node
		if !sameTree(d.docs[0], root, was, &found) || found == nil {
			return false
		}
		if !sameComments(was, found) || was.Anchor != found.Anchor {
			return false
		}
		first := max(was.Line, found.Line)
		if !sameOutside(d.src, d.lines, next, lines, first,
			lastLine(d.src, d.lines, d.docs[0], was), lastLine(next, lines, root, found)) {
			return false
		}
		return check(found) == nil
	})
}

// keepsAround reports whether next is src with only the bytes of old
// replaced: old lies within src, next opens with every byte of src
// before old and closes with every byte after it, and the two runs
// don't overlap in next.
func keepsAround(src, next []byte, old span) bool {
	if old.start < 0 || old.start > old.end || old.end > len(src) {
		return false
	}
	before, after := src[:old.start], src[old.end:]
	return len(next) >= len(before)+len(after) &&
		bytes.HasPrefix(next, before) && bytes.HasSuffix(next, after)
}

// swapIn parses next and, when it holds exactly one document that
// accept takes, makes next d's text. accept gets that document and the
// line index of next. Otherwise swapIn leaves d as it is and returns the
// verifier's refusal at line line. Every edit goes through swapIn, so
// that no edit can leave d's text, tree, and line index out of step.
func (d *Document) swapIn(op, path string, line int, next []byte, accept func(root *yamlnode.Node, lines []int) bool) error {
	docs, err := yamlnode.Parse(next)
	if err != nil || len(docs) != 1 || len(d.docs) != 1 {
		return refuseEdit(op, path, line)
	}
	lines := newLineIndex(next)
	if !accept(docs[0], lines) {
		return refuseEdit(op, path, line)
	}
	d.src, d.docs, d.lines = next, docs, lines
	return nil
}

// refuseEdit is the verifier's refusal of an edit.
func refuseEdit(op, path string, line int) error {
	return &DocumentError{Op: op, Path: path, Line: line, Reason: reasonOtherPart, err: ErrUnsupportedEdit}
}

// sameTree compares a and b on Kind, Tag, Value, Anchor, Style,
// BareTag, comments, alias targets (by anchor name), and Content
// length, recursively, ignoring positions. When a is skip it stops
// there and reports b through *found.
func sameTree(a, b, skip *yamlnode.Node, found **yamlnode.Node) bool {
	if a == skip {
		*found = b
		return true
	}
	if a.Kind != b.Kind || a.Tag != b.Tag || a.Value != b.Value || a.Anchor != b.Anchor ||
		a.Style != b.Style || a.BareTag != b.BareTag || !sameComments(a, b) || len(a.Content) != len(b.Content) {
		return false
	}
	if a.Kind == yamlnode.AliasNode {
		return a.Alias != nil && b.Alias != nil && a.Alias.Anchor == b.Alias.Anchor
	}
	for i := range a.Content {
		if !sameTree(a.Content[i], b.Content[i], skip, found) {
			return false
		}
	}
	return true
}

// sameComments reports whether a and b carry the same comments.
func sameComments(a, b *yamlnode.Node) bool {
	return a.HeadComment == b.HeadComment && a.LineComment == b.LineComment && a.FootComment == b.FootComment
}

// sameOutside reports whether a and b, whose line indexes are al and bl,
// differ only on the lines of one node: from line first to line aLast
// in a, and from line first to line bLast in b. It takes the lines the
// two have in common at the start and at the end, and requires that
// those cover every line before first and every line after the node's
// last line on each side, so the lines after the node move by
// bLast-aLast and nothing else.
func sameOutside(a []byte, al []int, b []byte, bl []int, first, aLast, bLast int) bool {
	na, nb := len(al), len(bl)
	head := 0
	for head < na && head < nb && bytes.Equal(lineOf(a, al, head+1), lineOf(b, bl, head+1)) {
		head++
	}
	tail := 0
	for tail < na-head && tail < nb-head && bytes.Equal(lineOf(a, al, na-tail), lineOf(b, bl, nb-tail)) {
		tail++
	}
	return head >= first-1 && na-tail <= aLast && nb-tail <= bLast
}

// lastLine returns the last line that node n of root, whose text is src
// with line index lines, can reach. That is the line before the first
// node written after n, or the last line of src when none is. A node
// written later on n's own line keeps n to that line. For a scalar that
// is not a block scalar the blank and comment lines at the end are left
// out, since such a scalar never ends with one. A block scalar keeps
// them, since its text can.
func lastLine(src []byte, lines []int, root, n *yamlnode.Node) int {
	last := len(lines)
	if after := lineAfter(root, n); after > 0 {
		last = max(after-1, n.Line)
	}
	if n.Kind == yamlnode.ScalarNode && n.Style&(yamlnode.StyleLiteral|yamlnode.StyleFolded) == 0 {
		for last > n.Line && blankOrComment(lineOf(src, lines, last)) {
			last--
		}
	}
	return last
}

// lineAfter returns the line of the first node of root, in the order
// they are written, that comes after n and everything under it, or 0
// when there is none. Aliases are not followed, and a node with no
// source position is passed over.
func lineAfter(root, n *yamlnode.Node) int {
	passed := false
	var walk func(x *yamlnode.Node) int
	walk = func(x *yamlnode.Node) int {
		if passed && x.Line > 0 {
			return x.Line
		}
		if x == n {
			passed = true
			return 0
		}
		for _, c := range x.Content {
			if line := walk(c); line > 0 {
				return line
			}
		}
		return 0
	}
	return walk(root)
}

// blankOrComment reports whether line holds only white space, or only a
// comment after it.
func blankOrComment(line []byte) bool {
	t := bytes.TrimLeft(line, " \t")
	return len(bytes.TrimRight(t, " \t\r\n")) == 0 || t[0] == '#'
}
