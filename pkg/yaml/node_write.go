package yaml

import (
	"errors"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/fivetwenty-io/graft/internal/yamlnode"
)

// The limits writeNode keeps. maxNodeDepth is the parser's own nesting
// limit, so any tree Unmarshal returns is within it. maxBlockDepth is
// how deep the writer nests block collections before it writes the rest
// in flow style, which keeps the indentation of a deep tree from growing
// the text quadratically. maxImplicitKey is a little under the 1024
// characters YAML allows an implicit key, past which a key is written
// with "? ".
const (
	maxNodeDepth   = 10000
	maxBlockDepth  = 32
	maxImplicitKey = 1000
	maxAliasChain  = 64
)

// The messages for a tree writeNode cannot write. None of them quotes a
// value, a tag, or an anchor name.
const (
	nodeDepthMessage        = "exceeded max depth of 10000"
	nodeKindMessage         = "node has no valid kind"
	nodeNilMessage          = "node holds a nil child"
	nodeOddMappingMessage   = "mapping node holds an odd number of children"
	nodeEmptyAliasMessage   = "alias node points at no node"
	nodeAliasChainMessage   = "alias node never reaches a value"
	nodeDocumentMessage     = "document node appears inside another node"
	nodeDocumentRootMessage = "document node holds more than one root"
	nodeCollectionKeyMsg    = "mapping key is a sequence or mapping"
	nodeUTF8Message         = "scalar is not valid UTF-8"
	nodeTagMessage          = "node has a tag YAML cannot write"
)

// place is a 1-based line and column.
type place struct {
	line, col int
}

// mark ties a place in the written text to the node written there.
type mark struct {
	col int
	src place
}

// placeTable maps the lines of the written text to the places in the
// input that the nodes written on them came from.
type placeTable map[int][]mark

// relocate moves err's position from the written text back to the input.
// A position at the start of a node takes that node's line and column. A
// position inside a node's text takes that node's line and no column, and
// one on a line where no node starts has no position at all, so Line is
// always the input's line or 0.
func (t placeTable) relocate(err error) error {
	var de *DecodeError
	if !errors.As(err, &de) || de.Line == 0 {
		return err
	}
	out := *de
	out.Line, out.Column = 0, 0
	marks := t[de.Line]
	i := sort.Search(len(marks), func(i int) bool { return marks[i].col > de.Column }) - 1
	switch {
	case i >= 0 && marks[i].col == de.Column:
		out.Line, out.Column = marks[i].src.line, marks[i].src.col
	case i >= 0:
		out.Line = marks[i].src.line
	case de.Column == 0 && len(marks) > 0:
		out.Line = marks[0].src.line
	}
	if out.Line == 0 {
		out.Column = 0
	}
	return &out
}

// treeError reports a tree that cannot be written, at the line of the
// node at fault, which is the caller's own line for a hand-built node.
func treeError(n *Node, msg string) *DecodeError {
	e := &DecodeError{Kind: ErrorKindSyntax, Msg: msg}
	if n != nil && n.Line > 0 {
		e.Line, e.Column = n.Line, max(n.Column, 0)
	}
	return e
}

// writeNode writes n as the YAML text of one document, with a table that
// maps the text's places back to the input. It returns nil text for a
// DocumentNode with no root. A node reached more than once, directly or
// through aliases, is written once with an anchor and then as an alias,
// so the text grows with the tree and not with its expansion.
func writeNode(n *Node) ([]byte, placeTable, error) {
	root := n
	if n.Kind == DocumentNode {
		switch len(n.Content) {
		case 0:
			return nil, nil, nil
		case 1:
			if root = n.Content[0]; root == nil {
				return nil, nil, treeError(n, nodeNilMessage)
			}
		default:
			return nil, nil, treeError(n, nodeDocumentRootMessage)
		}
	}
	w := &writer{
		refs:    make(map[*Node]int),
		open:    make(map[*Node]bool),
		anchors: make(map[*Node]string),
		places:  make(placeTable),
		line:    1,
		col:     1,
	}
	if err := w.scan(root, 0); err != nil {
		return nil, nil, err
	}
	w.value(root, 0, atRoot, 0)
	return w.buf, w.places, nil
}

// writer writes a Node tree as YAML text. refs counts how often scan
// reached each node, open holds the collections scan is inside, and
// anchors names each node written with an anchor.
type writer struct {
	buf       []byte
	line, col int
	lastAlias bool

	refs    map[*Node]int
	open    map[*Node]bool
	anchors map[*Node]string
	places  placeTable
}

// target follows x through any aliases to the node they name.
func target(x *Node) (*Node, *DecodeError) {
	t := x
	for range maxAliasChain {
		if t.Kind != AliasNode {
			return t, nil
		}
		if t.Alias == nil {
			return nil, treeError(t, nodeEmptyAliasMessage)
		}
		t = t.Alias
	}
	return nil, treeError(x, nodeAliasChainMessage)
}

// scan checks that x, reached at depth collections down, can be written,
// and counts how often each node is reached. It descends into a node only
// the first time, so it ends on any tree, and a node reached again while
// scan is inside it is a cycle.
func (w *writer) scan(x *Node, depth int) *DecodeError {
	t, err := target(x)
	if err != nil {
		return err
	}
	if w.open[t] {
		return treeError(x, selfContainingAnchorMessage)
	}
	w.refs[t]++
	if w.refs[t] > 1 {
		return nil
	}
	if err := checkNode(t, depth); err != nil || t.Kind == ScalarNode {
		return err
	}
	w.open[t] = true
	defer delete(w.open, t)
	for i, child := range t.Content {
		if child == nil {
			return treeError(t, nodeNilMessage)
		}
		if err := w.scan(child, depth+1); err != nil {
			return err
		}
		if t.Kind == MappingNode && i%2 == 0 {
			if k, _ := target(child); k.Kind != ScalarNode {
				return treeError(child, nodeCollectionKeyMsg)
			}
		}
	}
	return nil
}

// checkNode checks the node t itself, reached at depth collections down,
// leaving its children to scan.
func checkNode(t *Node, depth int) *DecodeError {
	switch t.Kind {
	case ScalarNode:
		if !utf8.ValidString(t.Value) {
			return treeError(t, nodeUTF8Message)
		}
	case MappingNode, SequenceNode:
		if depth >= maxNodeDepth {
			return &DecodeError{Kind: ErrorKindLimit, Msg: nodeDepthMessage}
		}
		if t.Kind == MappingNode && len(t.Content)%2 != 0 {
			return treeError(t, nodeOddMappingMessage)
		}
	case DocumentNode:
		return treeError(t, nodeDocumentMessage)
	default:
		return treeError(t, nodeKindMessage)
	}
	return checkTag(t)
}

// checkTag refuses a tag that holds a space or a character that is not
// printable, which no form of tag can carry.
func checkTag(t *Node) *DecodeError {
	for _, r := range t.Tag {
		if r == ' ' || r == '<' || r == '>' || !unicode.IsPrint(r) {
			return treeError(t, nodeTagMessage)
		}
	}
	if !utf8.ValidString(t.Tag) {
		return treeError(t, nodeTagMessage)
	}
	return nil
}

// position says where a value sits, which decides what goes before it.
type position uint8

const (
	atRoot position = iota
	atMapValue
	atSeqItem
)

// write appends s and moves the current place past it, counting columns
// in characters.
func (w *writer) write(s string) {
	w.buf = append(w.buf, s...)
	for _, r := range s {
		if r == '\n' {
			w.line, w.col = w.line+1, 1
		} else {
			w.col++
		}
	}
	w.lastAlias = false
}

// indent starts a line indented by n spaces.
func (w *writer) indent(n int) {
	w.write(strings.Repeat(" ", n))
}

// at records that the node src is written from the current place. A
// scalar or an alias replaces a collection recorded at the same place,
// since an error there is about the scalar.
func (w *writer) at(src *Node, scalar bool) {
	m := mark{col: w.col, src: place{src.Line, src.Column}}
	if src.Line <= 0 {
		m.src = place{}
	}
	marks := w.places[w.line]
	if n := len(marks); n > 0 && marks[n-1].col == w.col {
		if scalar {
			marks[n-1] = m
		}
		return
	}
	w.places[w.line] = append(marks, m)
}

// ref reports how x is written. It returns the node to write and its
// anchor, or alias set to the anchor name when that node was written
// before and x is written as an alias of it.
func (w *writer) ref(x *Node) (t *Node, anchor, alias string) {
	t, _ = target(x)
	if w.refs[t] < 2 {
		return t, "", ""
	}
	if name, ok := w.anchors[t]; ok {
		return t, "", name
	}
	name := "a" + strconv.Itoa(len(w.anchors)+1)
	w.anchors[t] = name
	return t, name, ""
}

// props writes a node's anchor and tag, followed by a space, and records
// the node at their start. It writes nothing when the node has neither.
func (w *writer) props(src *Node, anchor, tag string) {
	if anchor == "" && tag == "" {
		return
	}
	w.at(src, false)
	if anchor != "" {
		w.write("&" + anchor + " ")
	}
	if tag != "" {
		w.write(tagText(tag) + " ")
	}
}

// value writes x in block context, after its key's colon, after its
// sequence entry's dash, or at the start of the text, and ends the line.
// indent is the indentation of the collection x sits in, and depth how
// many collections enclose it.
func (w *writer) value(x *Node, indent int, pos position, depth int) {
	t, anchor, alias := w.ref(x)
	if pos != atRoot {
		w.write(" ")
	}
	if alias != "" {
		w.at(x, true)
		w.write("*" + alias + "\n")
		return
	}
	if t.Kind == ScalarNode {
		w.blockScalar(t, anchor, indent)
		return
	}
	if len(t.Content) == 0 || t.style&yamlnode.StyleFlow != 0 || depth >= maxBlockDepth {
		w.flowBody(t, anchor)
		w.write("\n")
		return
	}
	child := indent + 2
	switch tag := collectionTag(t); {
	case anchor != "" || tag != "":
		w.props(t, anchor, tag)
		w.endLine()
		if pos == atRoot {
			child = 0
		}
		w.block(t, child, false, depth)
	case pos == atSeqItem:
		w.block(t, child, true, depth)
	case pos == atMapValue:
		w.endLine()
		w.block(t, child, false, depth)
	default:
		w.block(t, 0, false, depth)
	}
}

// endLine ends the current line, dropping the space before the newline.
func (w *writer) endLine() {
	if n := len(w.buf); n > 0 && w.buf[n-1] == ' ' {
		w.buf = w.buf[:n-1]
		w.col--
	}
	w.write("\n")
}

// blockScalar writes the scalar t in block context and ends the line. An
// empty null with no anchor is written as nothing at all, as it most
// often was in the input.
func (w *writer) blockScalar(t *Node, anchor string, indent int) {
	tag, f := scalarForm(t, false)
	if f == formEmpty && anchor == "" && len(w.buf) > 0 {
		w.at(t, true)
		w.endLine()
		return
	}
	w.props(t, anchor, tag)
	w.at(t, true)
	if f == formLiteral {
		w.literal(t.Value, indent+2)
		return
	}
	w.write(scalarWritten(t.Value, f) + "\n")
}

// literal writes v as a literal block scalar whose lines are indented by
// indent, ending its last line. The chomping indicator keeps v's trailing
// newlines exactly.
func (w *writer) literal(v string, indent int) {
	body := strings.TrimRight(v, "\n")
	trail := len(v) - len(body)
	switch trail {
	case 0:
		w.write("|-\n")
	case 1:
		w.write("|\n")
	default:
		w.write("|+\n")
	}
	if v == "" {
		return
	}
	pad := strings.Repeat(" ", indent)
	for line := range strings.SplitSeq(body, "\n") {
		if line != "" {
			w.write(pad + line)
		}
		w.write("\n")
	}
	if trail > 1 {
		w.write(strings.Repeat("\n", trail-1))
	}
}

// block writes the non-empty collection t in block style at indent. When
// first is set, the line of its first entry has already begun after a
// sequence entry's dash.
func (w *writer) block(t *Node, indent int, first bool, depth int) {
	if t.Kind == SequenceNode {
		for i, item := range t.Content {
			if i > 0 || !first {
				w.indent(indent)
			}
			if i == 0 {
				w.at(t, false)
			}
			w.write("-")
			w.value(item, indent, atSeqItem, depth+1)
		}
		return
	}
	for i := 0; i < len(t.Content); i += 2 {
		if i > 0 || !first {
			w.indent(indent)
		}
		if i == 0 {
			w.at(t, false)
		}
		w.key(t.Content[i], t.Content[i+1], indent)
		w.value(t.Content[i+1], indent, atMapValue, depth+1)
	}
}

// key writes the mapping key x and its colon. A key too long for an
// implicit key is written after "? ", with its colon on the next line,
// where the colon stands for value, the key's value, since the parser
// reports a mapping at its first colon.
func (w *writer) key(x, value *Node, indent int) {
	t, anchor, alias := w.ref(x)
	if alias != "" {
		w.at(x, true)
		w.write("*" + alias + " :")
		return
	}
	tag, f := scalarForm(t, true)
	text := scalarWritten(t.Value, f)
	long := len(text)+len(tag) > maxImplicitKey
	if long {
		w.write("? ")
	}
	w.props(t, anchor, tag)
	w.at(t, true)
	w.write(text)
	if long {
		w.write("\n")
		w.indent(indent)
		v, _ := target(value)
		w.at(v, false)
	}
	w.write(":")
}

// flow writes x in flow style on the current line, after sep.
func (w *writer) flow(x *Node, sep string) {
	t, anchor, alias := w.ref(x)
	w.write(sep)
	if alias != "" {
		w.at(x, true)
		w.write("*" + alias)
		w.lastAlias = true
		return
	}
	w.flowBody(t, anchor)
}

// flowBody writes t in flow style with anchor, which may be empty.
func (w *writer) flowBody(t *Node, anchor string) {
	if t.Kind == ScalarNode {
		tag, f := scalarForm(t, true)
		w.props(t, anchor, tag)
		w.at(t, true)
		w.write(scalarWritten(t.Value, f))
		return
	}
	w.props(t, anchor, collectionTag(t))
	w.at(t, false)
	open, end := "[", "]"
	if t.Kind == MappingNode {
		open, end = "{", "}"
	}
	w.write(open)
	for i, child := range t.Content {
		sep := ""
		switch {
		case t.Kind == MappingNode && i%2 == 1:
			sep = w.space() + ": "
		case i > 0:
			sep = w.space() + ", "
		}
		if t.Kind == MappingNode && i%2 == 0 && w.longKey(child) {
			sep += "? "
		}
		w.flow(child, sep)
	}
	w.write(w.space() + end)
}

// longKey reports whether the key x, written in full, is too long for
// an implicit key.
func (w *writer) longKey(x *Node) bool {
	t, _ := target(x)
	if _, written := w.anchors[t]; written {
		return false
	}
	tag, f := scalarForm(t, true)
	return len(scalarWritten(t.Value, f))+len(tag) > maxImplicitKey
}

// space returns the space an alias needs before the indicator after it,
// since an indicator right after an alias could read as part of its name.
func (w *writer) space() string {
	if w.lastAlias {
		return " "
	}
	return ""
}

// form is how a scalar is written.
type form uint8

const (
	formPlain form = iota
	formDouble
	formLiteral
	formEmpty
)

// scalarWritten is the text of v written in form, other than a literal.
func scalarWritten(v string, f form) string {
	switch f {
	case formPlain:
		return v
	case formEmpty:
		return "~"
	default:
		return quote(v)
	}
}

// scalarForm decides how the scalar t is written, and the tag written
// before it, if any. A scalar keeps the form it was read in, so a quoted
// yes stays a string and a plain one stays a YAML 1.1 boolean. A scalar
// whose tag is not the one its text resolves to plainly is quoted when
// its tag is !!str, and written with its tag otherwise. A scalar a caller
// built with the tag !!str is quoted, and one with no tag is written as
// plain text when it can be.
func scalarForm(t *Node, flow bool) (string, form) {
	v := t.Value
	quoted := t.style&(yamlnode.StyleSingle|yamlnode.StyleDouble) != 0
	block := t.style&(yamlnode.StyleLiteral|yamlnode.StyleFolded) != 0
	switch {
	case t.style&yamlnode.StyleTagged != 0 && t.Tag != "":
		return t.Tag, keptForm(v, quoted, block, flow)
	case t.Tag == "":
		return "", untaggedForm(v, flow)
	case t.Tag == yamlnode.TagStr && (quoted || block || !t.read):
		return "", keptForm(v, quoted || !t.read, block, flow)
	case t.Tag == yamlnode.ResolvePlainTag(v):
		if f := untaggedForm(v, flow); f != formDouble {
			return "", f
		}
	}
	if t.Tag == yamlnode.TagStr {
		return "", formDouble
	}
	return t.Tag, keptForm(v, false, false, flow)
}

// keptForm is the form of a scalar that keeps how it was written. A
// block scalar stays a literal block where one can hold it, a quoted
// scalar stays quoted, and a plain one stays plain where it can.
func keptForm(v string, quoted, block, flow bool) form {
	switch {
	case block && !flow && (v == "" || literalSafe(v)):
		return formLiteral
	case quoted || block || v == "" || !plainSafe(v, flow):
		return formDouble
	}
	return formPlain
}

// untaggedForm is the form of a scalar whose text alone decides what it
// is, which is empty for an empty null and plain where it can be.
func untaggedForm(v string, flow bool) form {
	switch {
	case v == "":
		return formEmpty
	case plainSafe(v, flow):
		return formPlain
	}
	return formDouble
}

// collectionTag is the tag written before the collection t, which is its
// tag when it was written explicitly or is not the default for its kind.
func collectionTag(t *Node) string {
	if t.Tag == "" {
		return ""
	}
	if t.style&yamlnode.StyleTagged != 0 {
		return t.Tag
	}
	if (t.Kind == MappingNode && t.Tag == yamlnode.TagMap) || (t.Kind == SequenceNode && t.Tag == yamlnode.TagSeq) {
		return ""
	}
	return t.Tag
}

// tagText writes tag in its short form when it has one, and in the
// verbatim !<...> form otherwise.
func tagText(tag string) string {
	suffix, ok := strings.CutPrefix(tag, "!!")
	if !ok {
		suffix, ok = strings.CutPrefix(tag, "!")
	}
	if ok && suffix != "" && strings.IndexFunc(suffix, func(r rune) bool { return !tagRune(r) }) < 0 {
		return tag
	}
	return "!<" + tag + ">"
}

// tagRune reports whether r may appear in a short tag's suffix.
func tagRune(r rune) bool {
	return r < utf8.RuneSelf && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || strings.ContainsRune("-_.:/;=+~$@", r))
}

// plainSafe reports whether v reads back as the same text when written
// as a plain scalar, in flow context when flow is set. It errs toward
// false, since a quoted string reads back as the same string.
func plainSafe(v string, flow bool) bool {
	if !plainStart(v) || strings.Contains(v, ": ") || strings.Contains(v, " #") {
		return false
	}
	for i, r := range v {
		switch {
		case r == ' ':
		case !unicode.IsPrint(r):
			return false
		case !flow:
		case strings.ContainsRune(",[]{}", r):
			return false
		case r == ':' && i+1 < len(v) && strings.ContainsRune(",[]{}", rune(v[i+1])):
			return false
		}
	}
	return true
}

// plainStart reports whether v begins and ends the way a plain scalar
// may, with no indicator first, no space at either end, no colon last,
// and no document marker.
func plainStart(v string) bool {
	if v == "" || v[0] == ' ' || v[len(v)-1] == ' ' || v[len(v)-1] == ':' {
		return false
	}
	if strings.HasPrefix(v, "---") || strings.HasPrefix(v, "...") {
		return false
	}
	if v[0] == '-' {
		return len(v) > 1 && v[1] != ' '
	}
	return !strings.ContainsRune("?:,[]{}#&*!|>'\"%@`", rune(v[0]))
}

// literalSafe reports whether v can be written as a literal block scalar
// that reads back as v. Its first line must begin with text, so the
// indentation is detected, and no line may hold only spaces.
func literalSafe(v string) bool {
	if v == "" || v[0] == ' ' || v[0] == '\t' || v[0] == '\n' {
		return false
	}
	for line := range strings.SplitSeq(strings.TrimRight(v, "\n"), "\n") {
		if line != "" && strings.Trim(line, " \t") == "" {
			return false
		}
		for _, r := range line {
			if r != '\t' && !unicode.IsPrint(r) && r != ' ' {
				return false
			}
		}
	}
	return true
}

// quote writes v as a double-quoted scalar on one line, escaping quotes,
// backslashes, and every character that is not printable.
func quote(v string) string {
	var b strings.Builder
	b.Grow(len(v) + 2)
	b.WriteByte('"')
	for _, r := range v {
		switch {
		case r == '"':
			b.WriteString(`\"`)
		case r == '\\':
			b.WriteString(`\\`)
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\t':
			b.WriteString(`\t`)
		case r == ' ' || unicode.IsPrint(r):
			b.WriteRune(r)
		case r < 0x100:
			b.WriteString(`\x`)
			b.WriteString(hex(r, 2))
		case r < 0x10000:
			b.WriteString(`\u`)
			b.WriteString(hex(r, 4))
		default:
			b.WriteString(`\U`)
			b.WriteString(hex(r, 8))
		}
	}
	b.WriteByte('"')
	return b.String()
}

// hex formats r as width hexadecimal digits.
func hex(r rune, width int) string {
	s := strconv.FormatInt(int64(r), 16)
	return strings.Repeat("0", width-len(s)) + s
}
