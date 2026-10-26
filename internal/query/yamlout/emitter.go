// Package yamlout writes query values as YAML text, keeping object keys in
// input order. A value without Meta comes out in block style by the rules
// graft's own YAML output follows. A value the yq loader read keeps its
// comments, anchors, aliases, tags, and styles, laid out the way yq lays
// them out.
package yamlout

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"

	"github.com/fivetwenty-io/graft/pkg/query/value"
	"github.com/fivetwenty-io/graft/pkg/sortkeys"

	"github.com/fivetwenty-io/graft/internal/query/jqout"
	"github.com/fivetwenty-io/graft/internal/query/load"
)

const (
	minIndent = 2
	maxIndent = 9
)

// docSeparator stands for a "---" line in leading content. It is the
// loader's marker, which the load package keeps unexported.
const docSeparator = "$yqDocSeparator$"

//nolint:misspell // the message is yq's, spelling and all
var errNULInValue = errors.New("can't serialise value because it contains NUL char and you are using NUL separated output")

// Options selects the output form.
type Options struct {
	Indent           int  // spaces per level, clamped to 2 through 9
	NoDoc            bool // omit the --- line between documents
	UnwrapScalar     bool // a top-level scalar prints as its bare text
	FloatNumbers     bool // print computed numbers by jq's double rule
	GoFloats         bool // print a computed !!float as Go prints a float64
	SortKeys         sortkeys.Mode
	CompactSeqIndent bool // a sequence under a mapping key is not indented
	NUL              bool // end each document with a NUL in place of its last line break
}

// Encoder writes values as YAML documents. WriteDocument also writes the
// leading content of the document a value came from, and decides on the
// "---" between documents the way yq does.
type Encoder interface {
	jqout.Encoder
	WriteDocument(d load.Document, v *value.Value) error
}

// nodeCtx is where a node sits, which decides its indentation and the
// styles open to a scalar.
type nodeCtx uint8

const (
	ctxRoot nodeCtx = iota
	ctxBlockSeq
	ctxBlockKey
	ctxBlockValue
	ctxFlowSeq
	ctxFlowKey
	ctxFlowValue
)

type emitter struct {
	w      *bufio.Writer
	o      Options
	indent int
	docs   int
	buf    strings.Builder

	// What WriteDocument compares the next result with. yq sets the file
	// index once, from the first result, and never again.
	started           bool
	prevDoc, prevFile int

	layout
}

// layout is the state of the document being written. It follows the
// libyaml emitter that yq writes through, so lines break, indent, and
// carry comments where yq puts them.
type layout struct {
	best       int
	col        int
	ws         bool // the last character written was whitespace
	indention  bool // only indentation is on the current line so far
	level      int  // the current indentation, -1 before the root
	levels     []int
	flow       int
	footIndent int
	mapping    bool // the node being started is a mapping key or value
	simpleKey  bool // the node being started is a simple mapping key
	leading    string

	head, line, foot, tail, keyLine string
}

// New returns an Encoder that writes each value to w as a YAML document.
func New(w io.Writer, o Options) Encoder {
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

// Write writes v as the next document, after a "---" line from the second
// document on.
func (e *emitter) Write(v *value.Value) error {
	sep := e.docs > 0 && !e.o.NoDoc
	e.docs++
	e.buf.Reset()
	e.encode(e.sorted(v), "")
	return e.finish(sep)
}

// WriteDocument writes v, a result from document d, after d's leading
// content. A caller passes the leading content only with the result that
// stands for the whole document, as yq keeps it on the document's root.
// The "---" goes before a result whose document or file differs from the
// last one's, unless the leading content opens with one of its own. The
// first result printed keeps a "---" of its leading content only when it
// comes from a file's first document, since a later document's "---"
// parts it from the documents before it.
func (e *emitter) WriteDocument(d load.Document, v *value.Value) error {
	separators := !e.o.NoDoc
	if !e.started {
		e.prevDoc, e.prevFile = d.DocIndex, d.FileIndex
		e.started = true
		separators = separators && d.DocIndex == 0
	}
	changed := e.prevDoc != d.DocIndex || e.prevFile != d.FileIndex
	sep := changed && !strings.HasPrefix(d.LeadingContent, docSeparator) && !e.o.NoDoc
	e.prevDoc = d.DocIndex
	e.docs++
	e.buf.Reset()
	writeLeadingContent(&e.buf, d.LeadingContent, separators)
	e.encode(e.sorted(v), d.LeadingContent)
	return e.finish(sep)
}

func (e *emitter) sorted(v *value.Value) *value.Value {
	if e.o.SortKeys != "" && e.o.SortKeys != sortkeys.None {
		return sortkeys.Sort(v, e.o.SortKeys)
	}
	return v
}

// finish writes the document in e.buf, after a separator when sep is set.
// With NUL output the document's last line break turns into a NUL, and a
// NUL inside the document is an error since it would read as two.
func (e *emitter) finish(sep bool) error {
	if sep {
		if _, err := e.w.WriteString("---\n"); err != nil {
			return err
		}
	}
	doc := e.buf.String()
	if e.o.NUL {
		doc = strings.TrimSuffix(doc, "\n")
		doc = strings.TrimSuffix(doc, "\r")
		if strings.IndexByte(doc, 0) >= 0 {
			return errNULInValue
		}
		doc += "\x00"
	}
	_, err := e.w.WriteString(doc)
	return err
}

// encode writes one document's value. The root's foot comment comes out
// after the value, written the way leading content is.
func (e *emitter) encode(v *value.Value, leading string) {
	if e.o.UnwrapScalar && kindOf(v) == scalarNode {
		e.unwrapped(v, leading)
		return
	}
	e.layout = layout{best: e.indent, ws: true, indention: true, level: -1, footIndent: -1, leading: leading}
	e.analyzeStart(v, "", true)
	e.processHead()
	flow := e.start(v, ctxRoot, false)
	e.processLine(false)
	e.processFoot()
	e.body(v, ctxRoot, flow, true)
	e.footIndent = 0
	e.processFoot()
	e.footIndent = -1
	e.writeIndent()
	if m := v.Meta(); m != nil {
		writeLeadingContent(&e.buf, m.FootComment, !e.o.NoDoc)
	}
}

// unwrapped writes a scalar's bare text. Its line break matches the
// leading content's, and yq leaves it off an empty scalar that had some.
func (e *emitter) unwrapped(v *value.Value, leading string) {
	text := e.scalarText(v)
	if v.Kind() == value.KindString {
		text = v.Str()
	}
	e.buf.WriteString(text)
	if leading == "" || text != "" {
		if strings.Contains(leading, "\r\n") {
			e.buf.WriteString("\r\n")
		} else {
			e.buf.WriteByte('\n')
		}
	}
}

type nodeKind uint8

const (
	scalarNode nodeKind = iota
	aliasNode
	seqNode
	mapNode
)

func kindOf(v *value.Value) nodeKind {
	if m := v.Meta(); m != nil && m.AliasOf != "" {
		return aliasNode
	}
	switch v.Kind() {
	case value.KindArray:
		return seqNode
	case value.KindObject:
		return mapNode
	case value.KindNull, value.KindFalse, value.KindTrue, value.KindNumber, value.KindString:
	}
	return scalarNode
}

func hasFlowStyle(v *value.Value) bool {
	m := v.Meta()
	return m != nil && m.Style&value.Flow != 0
}

// isSimpleKey reports whether k can be written as "k:". A scalar always
// can, in double quotes when it spans lines, and a collection only when it
// is empty.
func isSimpleKey(k *value.Value) bool {
	switch kindOf(k) {
	case seqNode, mapNode:
		return k.Len() == 0
	case scalarNode, aliasNode:
	}
	return true
}

func footOf(v *value.Value) string {
	if m := v.Meta(); m != nil {
		return m.FootComment
	}
	return ""
}

// start writes the opening of v in context c. A scalar or an alias is
// written whole. A collection gets its anchor and tag, and start reports
// whether it is written in flow style.
func (e *emitter) start(v *value.Value, c nodeCtx, simpleKey bool) bool {
	e.mapping = c == ctxBlockKey || c == ctxBlockValue || c == ctxFlowKey || c == ctxFlowValue
	e.simpleKey = simpleKey
	m := v.Meta()
	switch kindOf(v) {
	case aliasNode:
		e.indicator("*", true, false, false)
		e.word(m.AliasOf)
		return false
	case scalarNode:
		e.scalar(v, c)
		return false
	case seqNode, mapNode:
	}
	e.anchor(m)
	if m != nil {
		e.tag(collectionTag(v, m))
	}
	return e.flow > 0 || hasFlowStyle(v) || v.Len() == 0
}

// body writes what follows a collection's opening, through its end.
func (e *emitter) body(v *value.Value, c nodeCtx, flow, dropFoot bool) {
	switch kind := kindOf(v); {
	case kind == seqNode && flow:
		e.flowSeq(v, c, dropFoot)
	case kind == seqNode:
		e.blockSeq(v, c, dropFoot)
	case kind == mapNode && flow:
		e.flowMap(v, c, dropFoot)
	case kind == mapNode:
		e.blockMap(v, c, dropFoot)
	}
}

func (e *emitter) blockSeq(v *value.Value, c nodeCtx, dropFoot bool) {
	compact := e.o.CompactSeqIndent && e.mapping && (e.col == 0 || !e.indention)
	for i, it := range v.Elems() {
		e.analyzeStart(it, "", false)
		if i == 0 {
			e.increase(false, c, compact)
		}
		e.processHead()
		e.writeIndent()
		e.indicator("-", true, false, true)
		flow := e.start(it, ctxBlockSeq, false)
		e.processLine(false)
		e.processFoot()
		e.body(it, ctxBlockSeq, flow, false)
	}
	e.analyzeEnd(v, "", dropFoot)
	e.decrease()
}

// blockMap writes a block mapping. A key's foot comment waits until the
// key's value is written, and comes out as a tail before the next key or
// at the mapping's end.
func (e *emitter) blockMap(v *value.Value, c nodeCtx, dropFoot bool) {
	tail := ""
	for i, en := range v.Entries() {
		e.analyzeStart(en.Key, tail, true)
		if i == 0 {
			e.increase(false, c, false)
		}
		e.processHead()
		e.writeIndent()
		if e.line != "" {
			e.keyLine, e.line = e.line, ""
		}
		simple := isSimpleKey(en.Key)
		if !simple {
			e.indicator("?", true, false, true)
		}
		flow := e.start(en.Key, ctxBlockKey, simple)
		if simple && kindOf(en.Key) == aliasNode {
			e.put(" ")
		}
		e.body(en.Key, ctxBlockKey, flow, true)
		tail = footOf(en.Key)

		e.analyzeStart(en.Value, "", false)
		if simple {
			e.indicator(":", false, false, false)
		} else {
			e.writeIndent()
			e.indicator(":", true, false, true)
		}
		e.keyLineBefore(en.Value)
		flow = e.start(en.Value, ctxBlockValue, false)
		e.processLine(false)
		e.processFoot()
		e.body(en.Value, ctxBlockValue, flow, false)
	}
	e.analyzeEnd(v, tail, dropFoot)
	e.processHead()
	e.decrease()
}

// keyLineBefore places a line comment that came on a key. A scalar value
// takes it as its own unless it has one, and a block collection has it
// written after the colon.
func (e *emitter) keyLineBefore(v *value.Value) {
	if e.keyLine == "" {
		return
	}
	switch kindOf(v) {
	case scalarNode:
		if e.line == "" {
			e.line, e.keyLine = e.keyLine, ""
		}
	case seqNode, mapNode:
		if !hasFlowStyle(v) {
			e.line, e.keyLine = e.keyLine, e.line
			e.processLine(false)
			e.line, e.keyLine = e.keyLine, e.line
		}
	case aliasNode:
	}
}

// flowSeq writes a flow sequence. An item with a comment after it takes
// its comma before the comment, and the next item none.
func (e *emitter) flowSeq(v *value.Value, c nodeCtx, dropFoot bool) {
	e.indicator("[", true, true, false)
	e.increase(true, c, false)
	e.flow++
	trail := false
	for i, it := range v.Elems() {
		e.analyzeStart(it, "", false)
		if i > 0 && !trail {
			e.indicator(",", false, false, false)
		}
		e.processHead()
		if e.col == 0 {
			e.writeIndent()
		}
		trail = e.commentsPending()
		flow := e.start(it, ctxFlowSeq, false)
		if e.commentsPending() {
			e.indicator(",", false, false, false)
		}
		e.processLine(false)
		e.processFoot()
		e.body(it, ctxFlowSeq, flow, false)
	}
	e.analyzeEnd(v, "", dropFoot)
	e.flow--
	e.decrease()
	if e.col == 0 {
		e.writeIndent()
	}
	e.indicator("]", false, false, false)
	e.processLine(false)
	e.processFoot()
}

func (e *emitter) flowMap(v *value.Value, c nodeCtx, dropFoot bool) {
	e.indicator("{", true, true, false)
	e.increase(true, c, false)
	e.flow++
	trail, tail := false, ""
	entries := v.Entries()
	for i, en := range entries {
		e.analyzeStart(en.Key, tail, true)
		if i > 0 && !trail {
			e.indicator(",", false, false, false)
		}
		e.processHead()
		if e.col == 0 {
			e.writeIndent()
		}
		simple := isSimpleKey(en.Key)
		if !simple {
			e.indicator("?", true, false, false)
		}
		flow := e.start(en.Key, ctxFlowKey, simple)
		e.body(en.Key, ctxFlowKey, flow, true)
		tail = footOf(en.Key)

		e.analyzeStart(en.Value, "", false)
		e.indicator(":", !simple, false, false)
		trail = e.commentsPending()
		flow = e.start(en.Value, ctxFlowValue, false)
		if e.commentsPending() {
			e.indicator(",", false, false, false)
		}
		e.processLine(false)
		e.processFoot()
		e.body(en.Value, ctxFlowValue, flow, false)
	}
	e.analyzeEnd(v, tail, dropFoot)
	if e.head+e.foot+e.tail != "" && len(entries) > 0 && !trail {
		e.indicator(",", false, false, false)
	}
	e.processHead()
	e.flow--
	e.decrease()
	e.indicator("}", false, false, false)
	e.processLine(false)
	e.processFoot()
}

// increase opens a level. The first level inside a block sequence item
// steps past the "- ", and every other one aligns to a multiple of the
// indent. A compact sequence under a mapping key moves back over its "- ".
func (e *emitter) increase(flow bool, c nodeCtx, compact bool) {
	e.levels = append(e.levels, e.level)
	switch {
	case e.level < 0 && flow:
		e.level = e.best
	case e.level < 0:
		e.level = 0
	case c == ctxBlockSeq:
		e.level += 2
	default:
		e.level = e.best * ((e.level + e.best) / e.best)
		if compact {
			e.level -= 2
		}
	}
}

func (e *emitter) decrease() {
	e.level = e.levels[len(e.levels)-1]
	e.levels = e.levels[:len(e.levels)-1]
}

// parentLevel is the indentation a block scalar's hint counts from.
func (e *emitter) parentLevel() int {
	return max(e.levels[len(e.levels)-1], 0)
}

func (e *emitter) put(s string) {
	e.buf.WriteString(s)
	e.col += utf8Len(s)
}

func (e *emitter) newline() {
	e.buf.WriteByte('\n')
	e.col = 0
	e.indention = true
}

// lineBreak writes a break character from a scalar or comment, keeping
// one other than a line feed as it is.
func (e *emitter) lineBreak(r rune) {
	if r == '\n' {
		e.newline()
		return
	}
	e.buf.WriteRune(r)
	e.col = 0
	e.indention = true
}

// word writes an anchor, alias, or tag name.
func (e *emitter) word(s string) {
	e.put(s)
	e.ws = false
	e.indention = false
}

func (e *emitter) indicator(s string, needWS, isWS, isIndention bool) {
	if needWS && !e.ws {
		e.put(" ")
	}
	e.put(s)
	e.ws = isWS
	e.indention = e.indention && isIndention
}

// writeIndent moves to the current indentation, on a new line unless only
// indentation is on this one. A foot comment just written at this
// indentation is set apart by a blank line.
func (e *emitter) writeIndent() {
	indent := max(e.level, 0)
	if !e.indention || e.col > indent || (e.col == indent && !e.ws) {
		e.newline()
	}
	if e.footIndent == indent {
		e.newline()
	}
	for e.col < indent {
		e.put(" ")
	}
	e.ws = true
	e.footIndent = -1
}

func (e *emitter) anchor(m *value.Meta) {
	if m == nil || m.Anchor == "" {
		return
	}
	e.indicator("&", true, false, false)
	e.word(m.Anchor)
}

// tag writes a tag in the shortest form that names it: "!!" for the YAML
// types, a local tag as it is, and anything else in "!<...>".
func (e *emitter) tag(t string) {
	if t == "" {
		return
	}
	const yamlPrefix = "tag:yaml.org,2002:"
	handle, suffix := "", t
	switch {
	case strings.HasPrefix(t, yamlPrefix):
		handle, suffix = "!!", t[len(yamlPrefix):]
	case strings.HasPrefix(t, "!!"):
		handle, suffix = "!!", t[2:]
	case strings.HasPrefix(t, "!"):
		handle, suffix = "!", t[1:]
	}
	if handle == "" {
		e.indicator("!<", true, false, false)
		e.word(tagURI(suffix))
		e.indicator(">", false, false, false)
		return
	}
	if !e.ws {
		e.put(" ")
	}
	e.word(handle + tagURI(suffix))
}

// tagURI escapes the bytes a tag cannot hold as they are.
func tagURI(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if isTagByte(c) {
			b.WriteByte(c)
			continue
		}
		b.WriteByte('%')
		b.WriteByte(hexDigits[c>>4])
		b.WriteByte(hexDigits[c&15])
	}
	return b.String()
}

func isTagByte(c byte) bool {
	switch {
	case c >= '0' && c <= '9', c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z':
		return true
	}
	return strings.IndexByte(";/?:@&=+$,_.~*'()[]-", c) >= 0
}

// collectionTag returns the tag a collection is written with: one marked
// as written, or one that is not the collection's own.
func collectionTag(v *value.Value, m *value.Meta) string {
	if m.Tag == "" || m.Style&value.Tagged != 0 {
		return m.Tag
	}
	own := "!!seq"
	if v.Kind() == value.KindObject {
		own = "!!map"
	}
	if shortTag(m.Tag) == own {
		return ""
	}
	return m.Tag
}

func shortTag(t string) string {
	if rest, ok := strings.CutPrefix(t, "tag:yaml.org,2002:"); ok {
		return "!!" + rest
	}
	return t
}

func utf8Len(s string) int {
	n := 0
	for range s {
		n++
	}
	return n
}

// LeadingContent is content printed the way yq prints leading content,
// with its separator markers as "---" or as nothing, as separators says.
func LeadingContent(content string, separators bool) string {
	var b strings.Builder
	writeLeadingContent(&b, content, separators)
	return b.String()
}

// writeLeadingContent writes content the way yq prints leading content: a
// separator marker as "---" when separators print and as nothing when
// they do not, and any other line not already a comment, a blank line, or
// a directive as a comment.
func writeLeadingContent(b *strings.Builder, content string, separators bool) {
	if content == "" {
		return
	}
	lines := bytes.SplitAfter([]byte(content), []byte("\n"))
	for i, l := range lines {
		line := string(l)
		last := i == len(lines)-1
		if last && line == "" {
			break
		}
		if strings.Contains(line, docSeparator) {
			if separators {
				if strings.HasSuffix(line, "\r\n") {
					b.WriteString("---\r\n")
				} else {
					b.WriteString("---\n")
				}
			}
		} else {
			if line != "\n" && line[0] != '%' && !isCommentLine(line) {
				b.WriteString("# ")
			}
			b.WriteString(line)
		}
		if last {
			b.WriteByte('\n')
		}
	}
}

// isCommentLine reports whether line starts with a "#" after the
// whitespace yq's \s matches.
func isCommentLine(line string) bool {
	return strings.HasPrefix(strings.TrimLeft(line, " \t\n\f\r"), "#")
}
