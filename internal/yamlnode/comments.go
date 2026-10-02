//
// Copyright (c) 2011-2019 Canonical Ltd
// Copyright (c) 2006-2010 Kirill Simonov
//
// Permission is hereby granted, free of charge, to any person obtaining a copy of
// this software and associated documentation files (the "Software"), to deal in
// the Software without restriction, including without limitation the rights to
// use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies
// of the Software, and to permit persons to whom the Software is furnished to do
// so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.
//
// Derived from libyaml's comment scanning as ported in go.yaml.in/yaml/v3
// v3.0.4 (scannerc.go yaml_parser_scan_comments, parserc.go, and decode.go
// parser.mapping) and rewritten for graft to work on goccy positions.

package yamlnode

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/token"
)

// The comment placer reproduces where yaml.v3 attaches comments, for the
// slots neat prints and one more: a mapping key's head comment (#KH) and
// foot comment (#KF), a scalar value's line comment (#VL) and foot comment
// (#VF), and a scalar sequence item's head comment (#VH). It works from
// lexer comment tokens and node positions, because goccy's own comment
// placement differs from yaml.v3's and its ParseComments mode rejects
// valid input. Slots are keyed by a path of mapping pair and sequence
// indexes such as "d0{3}[1]{0}#KH", so duplicate keys never share a slot,
// and applySlots writes them onto the built tree. The segmentation follows
// yaml_parser_scan_comments (scannerc.go:2887-3040), the BLOCK-END
// placement in yaml_parser_unroll_indent (scannerc.go:1029-1080), the
// comment unfolding in parserc.go:80-108 and the document head split at
// parserc.go:288-301, and the foot moves in decode.go's parser.mapping.

// anc is one open level. It records the column of its key or dash and
// the slot where a foot comment at that level lands. key marks a mapping
// key's level, and block marks a level that libyaml closes with a
// BLOCK-END token, which every level but an indentless sequence is.
type anc struct {
	col   int
	foot  string
	key   bool
	block bool
}

// item is one key, scalar value, or sequence dash in document order.
type item struct {
	kind      byte // 'K' key, 'S' scalar value, 'E' sequence dash
	line, col int
	end       int
	head      string // slot for a head comment
	lineSlot  string
	valueNext bool // a key whose value starts on a later line
	null      bool // an empty value: an implicit null, or "~" on a rewritten bare-dash line
	plain     bool // a plain or block scalar, whose scanner reads past the line break after it
	ancs      []anc
}

// flowEntry is one entry of a flow collection. line and col locate the
// token a line comment after the entry follows.
type flowEntry struct {
	line, col int
	head      string
	lineSlot  string
	foot      string
}

// flowColl is a flow collection and its direct entries, with the
// positions of its opening and closing brackets.
type flowColl struct {
	startLine, startCol int
	endLine, endCol     int
	entries             []flowEntry
}

type cmt struct {
	line, col int
	text      string
	prev      *token.Token // the last token before the comment that is not a comment
	flow      bool         // the comment sits inside a flow collection
}

func endLine(tk *token.Token) int {
	o := strings.Trim(tk.Origin, " \t\r\n")
	return tk.Position.Line + strings.Count(o, "\n")
}

func unwrap(n ast.Node) ast.Node {
	for {
		switch x := n.(type) {
		case *ast.AnchorNode:
			n = x.Value
		case *ast.TagNode:
			n = x.Value
		default:
			return n
		}
	}
}

func isFlow(n ast.Node) bool {
	switch x := n.(type) {
	case *ast.MappingNode:
		return x.IsFlowStyle
	case *ast.SequenceNode:
		return x.IsFlowStyle
	default:
		return false
	}
}

func isCollection(n ast.Node) bool {
	switch n.(type) {
	case *ast.MappingNode, *ast.MappingValueNode, *ast.SequenceNode:
		return true
	default:
		return false
	}
}

// blockScalarEnd returns the last line of a literal or folded scalar that
// holds content. goccy's body token runs on through the blank lines and
// the next line's indentation, and its line number is unreliable, but its
// text always starts on the line after the header.
func blockScalarEnd(x *ast.LiteralNode) int {
	lines := strings.Split(x.Value.GetToken().Origin, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(lines[i]) != "" {
			return x.Start.Position.Line + 1 + i
		}
	}
	return x.Start.Position.Line
}

// walker collects the items and flow collections of one document.
// nulled holds stream lines, and offset turns a chunk line into one.
type walker struct {
	items  []item
	flows  []*flowColl
	nulled map[int]bool
	offset int
}

func (w *walker) scalarItem(n ast.Node, slot string, ancs []anc) item {
	tk := n.GetToken()
	it := item{kind: 'S', line: tk.Position.Line, col: tk.Position.Column, lineSlot: slot, ancs: ancs, end: endLine(tk)}
	if tk.Type == token.ImplicitNullType || (tk.Type == token.NullType && tk.Value == "~" && w.nulled[tk.Position.Line+w.offset]) {
		it.null = true
	}
	if tk.Type == token.ImplicitNullType {
		it.lineSlot = ""
	}
	it.plain = tk.Type != token.SingleQuoteType && tk.Type != token.DoubleQuoteType && !isFlow(n)
	switch x := n.(type) {
	case *ast.LiteralNode:
		it.end = blockScalarEnd(x)
	case *ast.MappingNode:
		if x.IsFlowStyle {
			it.end = x.End.Position.Line
		}
	case *ast.SequenceNode:
		if x.IsFlowStyle {
			it.end = x.End.Position.Line
		}
	}
	return it
}

// rootScalarLine is the line comment slot of a document whose root is a
// scalar.
const rootScalarLine = "d0#VL"

// root walks a document's root node. A scalar root is an item of its
// own, at libyaml's indentation of -1 outside every block collection, and
// yaml.v3 keeps its head, line, and foot comments on the scalar. An empty root
// holds no comments.
func (w *walker) root(n ast.Node) {
	u := unwrap(n)
	if isCollection(u) {
		w.node(n, "d0", nil)
		return
	}
	if tk := u.GetToken(); tk == nil || tk.Type == token.ImplicitNullType {
		return
	}
	it := w.scalarItem(u, rootScalarLine, []anc{{col: 0, foot: "d0#VF"}})
	it.head = "d0#VH"
	w.items = append(w.items, it)
}

func (w *walker) node(n ast.Node, p string, ancs []anc) {
	switch x := unwrap(n).(type) {
	case *ast.MappingNode:
		if !x.IsFlowStyle {
			w.mapping(x.Values, p, ancs)
		}
	case *ast.MappingValueNode:
		w.mapping([]*ast.MappingValueNode{x}, p, ancs)
	case *ast.SequenceNode:
		if !x.IsFlowStyle {
			w.sequence(x, p, ancs)
		}
	}
}

func (w *walker) mapping(values []*ast.MappingValueNode, p string, ancs []anc) {
	for i, mv := range values {
		kt := mv.Key.GetToken()
		kp := fmt.Sprintf("%s{%d}", p, i)
		kancs := append(append([]anc{}, ancs...), anc{kt.Position.Column, kp + "#KF", true, true})
		v := unwrap(mv.Value)
		valueNext := v.GetToken() != nil && v.GetToken().Position.Line > kt.Position.Line
		if _, ok := v.(*ast.LiteralNode); ok {
			valueNext = false
		}
		implicitNull := v.GetToken() != nil && v.GetToken().Type == token.ImplicitNullType
		if implicitNull {
			valueNext = true
		}
		w.items = append(w.items, item{kind: 'K', line: kt.Position.Line, col: kt.Position.Column, end: kt.Position.Line, head: kp + "#KH", valueNext: valueNext, ancs: kancs})
		switch {
		case isFlow(v):
			w.items = append(w.items, w.scalarItem(v, "", kancs))
			w.flow(v, kp)
		case isCollection(v):
			w.node(v, kp, kancs)
		case !implicitNull:
			w.items = append(w.items, w.scalarItem(v, kp+"#VL", kancs))
		}
	}
}

// sequence walks a block sequence. A foot comment at the level of a dash
// whose item is a block collection lands on the sequence in yaml.v3, and
// decode.go moves a sequence's foot to the key that holds it, so that
// level's foot slot is the parent key's. A scalar or flow item keeps the
// foot itself, and neat prints it only on a scalar.
func (w *walker) sequence(x *ast.SequenceNode, p string, ancs []anc) {
	seqFoot, indentless := "", false
	if n := len(ancs); n > 0 && ancs[n-1].key {
		seqFoot = ancs[n-1].foot
		indentless = len(x.Entries) > 0 && x.Entries[0].Start.Position.Column == ancs[n-1].col
	}
	for i, v := range x.Values {
		ip := fmt.Sprintf("%s[%d]", p, i)
		dash := x.Entries[i].Start
		uv := unwrap(v)
		scalar := !isCollection(uv)
		foot, head := seqFoot, ""
		if scalar || isFlow(uv) {
			foot = ip + "#VF"
		}
		if scalar {
			head = ip + "#VH"
		}
		eancs := append(append([]anc{}, ancs...), anc{dash.Position.Column, foot, false, !indentless})
		w.items = append(w.items, item{kind: 'E', line: dash.Position.Line, col: dash.Position.Column, end: dash.Position.Line, head: head, ancs: eancs})
		e := len(w.items) - 1
		switch {
		case isFlow(uv):
			w.items = append(w.items, w.scalarItem(uv, "", eancs))
			w.flow(uv, ip)
		case !scalar:
			w.node(uv, ip, eancs)
			w.stem(e, v != uv)
		default:
			it := w.scalarItem(uv, ip+"#VL", eancs)
			it.head = head
			w.items = append(w.items, it)
		}
	}
}

// stem gives the dash item at e the head slot of its mapping's first key
// when an anchor or tag sits between the dash and a mapping that starts
// on a later line. yaml.v3 otherwise keeps a comment above the dash as
// the mapping's own head comment, which neat never prints, because
// yaml_parser_split_stem_comment only sees a mapping that follows the
// dash directly.
func (w *walker) stem(e int, decorated bool) {
	if !decorated || e+1 >= len(w.items) {
		return
	}
	if first := w.items[e+1]; first.kind == 'K' && first.line > w.items[e].line {
		w.items[e].head = first.head
	}
}

// flow records a flow collection's direct entries so comments inside it
// can find them. A nested collection is recorded on its own.
func (w *walker) flow(n ast.Node, p string) {
	switch x := n.(type) {
	case *ast.SequenceNode:
		f := &flowColl{x.Start.Position.Line, x.Start.Position.Column, x.End.Position.Line, x.End.Position.Column, nil}
		w.flows = append(w.flows, f)
		for i, v := range x.Values {
			ip := fmt.Sprintf("%s[%d]", p, i)
			uv := unwrap(v)
			tk := uv.GetToken()
			if isFlow(uv) {
				f.entries = append(f.entries, flowEntry{line: tk.Position.Line, col: tk.Position.Column})
				w.flow(uv, ip)
				continue
			}
			f.entries = append(f.entries, flowEntry{tk.Position.Line, tk.Position.Column, ip + "#VH", ip + "#VL", ip + "#VF"})
		}
	case *ast.MappingNode:
		f := &flowColl{x.Start.Position.Line, x.Start.Position.Column, x.End.Position.Line, x.End.Position.Column, nil}
		w.flows = append(w.flows, f)
		for i, mv := range x.Values {
			kp := fmt.Sprintf("%s{%d}", p, i)
			kt, uv := mv.Key.GetToken(), unwrap(mv.Value)
			f.entries = append(f.entries, flowEntry{line: kt.Position.Line, col: kt.Position.Column, head: kp + "#KH"})
			vt := uv.GetToken()
			if isFlow(uv) {
				w.flow(uv, kp)
				continue
			}
			f.entries = append(f.entries, flowEntry{line: vt.Position.Line, col: vt.Position.Column, lineSlot: kp + "#VL", foot: kp + "#KF"})
		}
	}
}

// commentSlots computes the printed comment slots of the one document a
// chunk holds. Everything here works in the chunk's own lines, which
// start at 1. nulled holds the stream lines whose bare "-" became "- ~".
func commentSlots(file *ast.File, c chunk, nulled map[int]bool) map[string]string {
	out := map[string]string{}
	for _, doc := range file.Docs {
		if doc.Body == nil {
			continue
		}
		if _, ok := doc.Body.(*ast.DirectiveNode); ok {
			continue
		}
		w := &walker{nulled: nulled, offset: c.lineOffset()}
		w.root(doc.Body)
		if len(w.items) > 0 {
			placeComments(w, c, doc.Start != nil, out)
		}
		break
	}
	return out
}

// lexed is what commentSlots needs from the lexer: the comments, the
// least column of content on each line, and the "---" lines.
type lexed struct {
	cmts       []cmt
	contentCol map[int]int
	headerLine map[int]bool
}

func lex(text string) lexed {
	l := lexed{contentCol: map[int]int{}, headerLine: map[int]bool{}}
	var prev *token.Token
	depth := 0
	for _, t := range lexer.Tokenize(text) {
		switch t.Type {
		case token.CommentType:
			l.cmts = append(l.cmts, cmt{t.Position.Line, t.Position.Column, "#" + t.Value, prev, depth > 0})
			continue
		case token.DocumentHeaderType:
			l.headerLine[t.Position.Line] = true
		case token.SequenceStartType, token.MappingStartType:
			depth++
		case token.SequenceEndType, token.MappingEndType:
			depth--
		default:
			// Every other token is content and counts toward contentCol below.
		}
		// A block scalar's body holds no comments, and goccy numbers its
		// lines unreliably, so it does not count as content.
		blockBody := prev != nil && (prev.Type == token.LiteralType || prev.Type == token.FoldedType)
		if t.Type != token.DocumentHeaderType && !blockBody {
			for ln := t.Position.Line; ln <= endLine(t); ln++ {
				if col, ok := l.contentCol[ln]; !ok || t.Position.Column < col {
					l.contentCol[ln] = t.Position.Column
				}
			}
		}
		prev = t
	}
	return l
}

func placeComments(w *walker, c chunk, explicit bool, out map[string]string) {
	lines := strings.Split(strings.TrimSuffix(c.text, "\n"), "\n")
	l := lex(c.text)
	own, dashLines, inline := lineComments(w, l, out)
	headFoot, ownFeet := "", map[string]bool{}
	if c.index == 0 {
		headFoot = w.items[0].ancs[len(w.items[0].ancs)-1].foot
	}
	for _, g := range buildGaps(w.items, len(lines), dashLines, c.marker) {
		if !gapHasComment(g, own) {
			continue
		}
		s := newGapScanner(g, own, inline, lines, out, c.lineOffset())
		s.headFoot, s.ownFeet = headFoot, ownFeet
		if c.unterminated && g.next == nil {
			s.eofCol = utf8.RuneCountInString(lines[len(lines)-1])
		}
		s.scan()
		if g.prev != nil {
			s.resolve()
		}
		s.finishHead(explicit)
	}
}

func appendSlot(out map[string]string, slot, text string) {
	if slot == "" || text == "" {
		return
	}
	if out[slot] != "" {
		out[slot] += "\n" + text
	} else {
		out[slot] = text
	}
}

// lineComments places every comment that shares a line with earlier
// content, and every comment inside a flow collection. A comment that
// follows a sequence dash with no value of its own is a head comment for
// whatever follows, as in yaml.v3, so it is returned with the comments
// that sit on lines of their own, and its line is marked in dashLines.
// inline marks every line that ends in a line comment.
func lineComments(w *walker, l lexed, out map[string]string) (own map[int]cmt, dashLines, inline map[int]bool) {
	own, dashLines, inline = map[int]cmt{}, map[int]bool{}, map[int]bool{}
	for _, c := range l.cmts {
		isInline := l.inline(w.items, c)
		if c.flow {
			flowComment(w.flows, c, isInline, out)
			continue
		}
		if !isInline {
			own[c.line] = c
			continue
		}
		inline[c.line] = true
		best := lastItemOnLine(w.items, c.line)
		switch {
		case best < 0:
		case afterBareDash(w.items, best):
			own[c.line] = c
			dashLines[c.line] = true
		case w.items[best].kind == 'S':
			appendSlot(out, w.items[best].lineSlot, c.text)
		}
	}
	return own, dashLines, inline
}

// inline reports whether c follows content on its line. On a "---"
// line, a comment after a root scalar is its line comment, and a comment
// after anything else is a head comment for what follows.
func (l lexed) inline(items []item, c cmt) bool {
	cc, hasContent := l.contentCol[c.line]
	if !hasContent || cc >= c.col {
		return false
	}
	if !l.headerLine[c.line] {
		return true
	}
	i := lastItemOnLine(items, c.line)
	return i >= 0 && items[i].lineSlot == rootScalarLine
}

// afterBareDash reports whether items[i], the last item on its line, is
// a sequence dash with no value, or the empty value of one.
func afterBareDash(items []item, i int) bool {
	it := items[i]
	if it.kind == 'E' {
		return true
	}
	return it.kind == 'S' && it.null && i > 0 && items[i-1].kind == 'E' && items[i-1].line == it.line
}

// lastItemOnLine returns the index of the last item that ends on line or
// starts there. A scalar that starts on the line and ends later is a
// block scalar, and a comment after its header is its line comment.
func lastItemOnLine(items []item, line int) int {
	best := -1
	for i := range items {
		if items[i].end == line || items[i].line == line {
			best = i
		}
	}
	return best
}

// flowComment places a comment inside a flow collection. A comment after
// an entry, or after the "," that follows it, is that entry's line
// comment. A comment on a line of its own is the head comment of the
// next entry, or the foot comment of the last one before the closing
// bracket.
func flowComment(flows []*flowColl, c cmt, inline bool, out map[string]string) {
	if inline {
		prev := c.prev
		if prev != nil && prev.Type == token.CollectEntryType {
			prev = prev.Prev
		}
		for prev != nil && prev.Type == token.CommentType {
			prev = prev.Prev
		}
		if prev == nil {
			return
		}
		for _, f := range flows {
			for _, e := range f.entries {
				if e.line == prev.Position.Line && e.col == prev.Position.Column {
					appendSlot(out, e.lineSlot, c.text)
					return
				}
			}
		}
		return
	}
	f := innermostFlow(flows, c)
	if f == nil {
		return
	}
	for i, e := range f.entries {
		if before(c.line, c.col, e.line, e.col) {
			appendSlot(out, e.head, c.text)
			return
		}
		if i == len(f.entries)-1 {
			appendSlot(out, e.foot, c.text)
		}
	}
}

func before(l1, c1, l2, c2 int) bool {
	return l1 < l2 || (l1 == l2 && c1 < c2)
}

// innermostFlow returns the last recorded flow collection that encloses
// the comment. Collections are recorded outer first.
func innermostFlow(flows []*flowColl, c cmt) *flowColl {
	var best *flowColl
	for _, f := range flows {
		if before(f.startLine, f.startCol, c.line, c.col) && before(c.line, c.col, f.endLine, f.endCol) {
			best = f
		}
	}
	return best
}

// gap is the run of lines between the end of one item and the start of
// the next. dash marks a gap that starts on the line of a bare sequence
// dash, with the comment after it. marker marks the last gap of a chunk
// that a "---" or "..." ends, rather than the end of the stream.
type gap struct {
	prev, next *item
	after      *item // the item after next
	from, to   int
	dash       bool
	marker     bool
}

func buildGaps(items []item, lastLine int, dashLines map[int]bool, marker bool) []gap {
	gaps := []gap{{prev: nil, next: &items[0], from: 1, to: items[0].line - 1}}
	for i := 0; i+1 < len(items); i++ {
		if items[i+1].line > items[i].end {
			g := gap{prev: &items[i], next: &items[i+1], from: items[i].end + 1, to: items[i+1].line - 1}
			if i+2 < len(items) {
				g.after = &items[i+2]
			}
			if dashLines[items[i].end] {
				g.from, g.dash = items[i].end, true
			}
			gaps = append(gaps, g)
		}
	}
	lastEnd := 0
	var lastItem *item
	for i := range items {
		if items[i].end >= lastEnd {
			lastEnd, lastItem = items[i].end, &items[i]
		}
	}
	g := gap{prev: lastItem, from: lastEnd + 1, to: lastLine, marker: marker}
	if dashLines[lastEnd] {
		g.from, g.dash = lastEnd, true
	}
	return append(gaps, g)
}

func gapHasComment(g gap, own map[int]cmt) bool {
	for l := g.from; l <= g.to; l++ {
		if _, ok := own[l]; ok {
			return true
		}
	}
	return false
}

// pos is a position in the chunk. A line break sits at column eol, past
// anything else on its line.
type pos struct{ line, col int }

const eol = 1 << 20

func (p pos) before(q pos) bool { return before(p.line, p.col, q.line, q.col) }

// foot is a foot comment waiting for the token it attaches to. libyaml
// attaches a comment to the first token at or after its token mark, so
// mark is where the comment starts when it is dedented, where the scan
// stood after the previous foot, or nothing, which means the token before
// the gap.
type foot struct {
	text  string
	start pos
	mark  pos
	prior bool
}

// gapScanner runs yaml_parser_scan_comments' segmentation over one gap,
// collecting foot comments as it goes and leaving the final block as the
// next item's head comment. resolve then places the feet the way the
// BLOCK-END tokens of yaml_parser_unroll_indent and decode.go's moves of
// foot comments do.
type gapScanner struct {
	g           gap
	own         map[int]cmt
	lines       []string
	out         map[string]string
	nextIndent  int
	isValue     bool
	footLine    int
	firstEmpty  bool
	recentEmpty bool
	text        []string
	startLine   int
	startCol    int

	feet   []foot
	starts []pos // the start of every comment block, feet and head alike
	mark   pos   // where the scan stood after the last foot
	marked bool  // whether a foot has moved mark off the token before the gap

	// headFoot is where a foot comment right after a document's "---"
	// lands, preHead holds the head comments seen before that "---", and
	// afterHeader records that the scan has passed it.
	headFoot    string
	preHead     []string
	afterHeader bool

	// ownFeet holds the key foot slots filled from the key's own event.
	// decode.go's parser.mapping then leaves the value's foot on the value.
	ownFeet map[string]bool

	// eofCol is the 0-based column libyaml stands at when the stream ends
	// after the gap. It is 0 after a final line break, and the length of
	// the last line when the stream has none.
	eofCol int

	// streamStart records that the chunk's first line is the stream's
	// first line, where libyaml has no foot line.
	streamStart bool
}

// newGapScanner prepares the scan of one gap. inline holds the lines that
// end in a line comment. libyaml has no foot line when its scan starts on
// the stream's first line, which happens there unless the token before
// the gap is a plain or block scalar with no line comment, whose scanner
// reads on past the line break.
//
// lineOffset is the chunk's line offset. When the chunk does not start
// the stream, the scan of its first gap starts as it would after the
// blank lines that stand in for the lines before the chunk.
func newGapScanner(g gap, own map[int]cmt, inline map[int]bool, lines []string, out map[string]string, lineOffset int) *gapScanner {
	s := &gapScanner{g: g, own: own, lines: lines, out: out, footLine: -1, firstEmpty: true, streamStart: lineOffset == 0}
	if g.prev == nil && !s.streamStart {
		s.firstEmpty, s.recentEmpty = false, true
	}
	if g.prev != nil {
		s.nextIndent = g.prev.ancs[len(g.prev.ancs)-1].col - 1
		s.isValue = g.prev.kind == 'K' && g.prev.valueNext
		firstLine := s.streamStart && g.prev.end == 1 && (!g.prev.plain || inline[1])
		if !g.dash && !firstLine {
			s.footLine = g.from
		}
	}
	return s
}

func (s *gapScanner) scan() {
	for l := s.g.from; l <= s.g.to+1; l++ {
		if l > s.g.to {
			s.end(l)
			return
		}
		c, isCmt := s.own[l]
		switch {
		case isCmt:
			s.comment(l, c)
		case strings.TrimSpace(s.lines[l-1]) == "":
			s.blankLine(l)
		default:
			s.marker(l) // a "---", a "...", or a directive
		}
	}
}

// end handles the line after the gap: the next item, the end of the
// stream, which closes comments like a blank line, or a "---" or "..."
// that ends the chunk.
func (s *gapScanner) end(l int) {
	switch {
	case s.g.next != nil:
		if len(s.text) > 0 && s.g.next.col-1 < s.nextIndent && s.g.next.col != s.startCol {
			s.emitFoot(s.tokenMark(), pos{l, s.g.next.col})
		}
	case s.g.marker:
		s.marker(l)
	default:
		s.blankLine(l)
	}
}

func (s *gapScanner) comment(l int, c cmt) {
	if len(s.text) > 0 && c.col-1 < s.nextIndent && c.col != s.startCol {
		s.emitFoot(s.tokenMark(), pos{l, c.col})
	}
	if len(s.text) == 0 {
		s.startLine, s.startCol = l, c.col
		s.starts = append(s.starts, pos{l, c.col})
	}
	s.recentEmpty = false
	s.text = append(s.text, c.text)
}

// marker handles a line at column 1 that is not a comment or an item. In
// the document's first gap, the comments so far become head comments of
// the first node, and a "---" starts a fresh scan whose foot line is the
// line below it, except on the first line of the stream, where libyaml
// has no foot line. At the end of a document, comments that are not a
// foot by then become the document's foot, which neat never prints.
func (s *gapScanner) marker(l int) {
	if len(s.text) > 0 && 0 < s.nextIndent && s.startCol != 1 {
		s.emitFoot(s.tokenMark(), pos{l, 1})
	}
	if s.g.prev != nil {
		s.text = nil
		return
	}
	if len(s.text) > 0 {
		s.preHead = append(s.preHead, strings.Join(s.text, "\n"))
		s.text = nil
	}
	if strings.HasPrefix(s.lines[l-1], "---") {
		s.afterHeader, s.firstEmpty, s.recentEmpty, s.footLine = true, true, false, -1
		if l > 1 || !s.streamStart {
			s.footLine = l + 1
		}
	}
}

func (s *gapScanner) blankLine(l int) {
	if !s.recentEmpty {
		dedented := s.startCol <= s.nextIndent
		switch {
		case s.firstEmpty && dedented:
			s.emitFoot(foot{start: pos{s.startLine, s.startCol}, mark: pos{s.startLine, s.startCol}}, pos{l, eol})
		case s.firstEmpty && s.startLine == s.footLine && !s.isValue:
			s.emitFoot(s.tokenMark(), pos{l, eol})
		case len(s.text) > 0 && l <= s.g.to:
			s.text = append(s.text, "")
		}
	}
	s.firstEmpty = false
	s.recentEmpty = true
}

// tokenMark returns a foot that carries libyaml's token_mark. That mark
// is the token before the gap until a foot is emitted, and the scan
// position after that.
func (s *gapScanner) tokenMark() foot {
	return foot{start: pos{s.startLine, s.startCol}, mark: s.mark, prior: !s.marked}
}

// emitFoot closes the current comment block as a foot comment and moves
// the scan position to at.
func (s *gapScanner) emitFoot(f foot, at pos) {
	if len(s.text) == 0 {
		return
	}
	f.text = strings.Join(s.text, "\n")
	s.text = nil
	s.mark, s.marked = at, true
	if s.g.prev == nil {
		if s.afterHeader {
			s.ownFoot(s.headFoot, f.text)
		}
		return
	}
	s.feet = append(s.feet, f)
}

// blockEnd is a BLOCK-END token. It records where libyaml put the token
// and the slot a foot comment lands in when the event it ends takes
// comments. A mapping's end takes them, and decode.go moves them to its
// last key, while a block sequence's end leaves them for the next event
// that takes comments.
type blockEnd struct {
	at    pos
	foot  string
	takes bool
}

// blockEnds returns the BLOCK-END tokens between the gap's two items,
// innermost first. yaml_parser_unroll_indent moves each one back to the
// earliest comment in the gap that starts at its level's column. At the
// end of the stream or before "---" or "...", libyaml first closes the
// levels indented past the column it stands at, and closes the rest
// there, after every comment.
func (s *gapScanner) blockEnds() []blockEnd {
	a := s.g.prev.ancs
	var out []blockEnd
	for i := len(a) - 1; i >= 0; i-- {
		if !a[i].block {
			continue
		}
		if s.g.next != nil && a[i].col <= s.g.next.col {
			break
		}
		at := pos{s.g.from, 0}
		if s.g.next == nil && a[i].col-1 <= s.eofCol {
			at = pos{eol, eol}
		} else {
			for _, st := range s.starts {
				if st.col == a[i].col {
					at = st
					break
				}
			}
		}
		out = append(out, blockEnd{at, a[i].foot, a[i].key})
	}
	return out
}

// resolve places the gap's foot comments. A foot marked with the token
// before the gap belongs to that token, unless the token is a bare
// sequence dash, which holds no comments. Any other foot is unfolded at
// the first BLOCK-END at or after its mark and lands on the first
// mapping that ends from there on, or failing that on the next item.
//
// decode.go assigns a mapping's foot, and a key's foot handed to the key
// before it, over whatever that key held, so those feet replace the slot.
// A value's foot moves to its key only when the key has none of its own.
func (s *gapScanner) resolve() {
	if len(s.feet) == 0 {
		return
	}
	ends := s.blockEnds()
	replace := map[string][]string{}
	q := 0
	for _, f := range s.feet {
		if f.prior && !s.g.dash {
			s.priorFoot(f.text)
			continue
		}
		for !f.prior && q < len(ends) && ends[q].at.before(f.mark) {
			q++
		}
		k := q
		for k < len(ends) && !ends[k].takes {
			k++
		}
		if k < len(ends) {
			replace[ends[k].foot] = append(replace[ends[k].foot], f.text)
			continue
		}
		if slot, handed := s.nextFoot(); handed {
			replace[slot] = append(replace[slot], f.text)
		} else {
			s.ownFoot(slot, f.text)
		}
	}
	for slot, texts := range replace {
		if slot != "" {
			s.out[slot] = strings.Join(texts, "\n")
		}
	}
}

// priorFoot places a foot that belongs to the scalar before the gap. A
// mapping value hands it to its key unless the key has a foot of its own.
func (s *gapScanner) priorFoot(text string) {
	a := s.g.prev.ancs
	slot := a[len(a)-1].foot
	if s.g.prev.kind == 'S' && a[len(a)-1].key && s.ownFeet[slot] {
		slot = strings.TrimSuffix(slot, "#KF") + "#VF"
	}
	appendSlot(s.out, slot, text)
}

// ownFoot places a foot that a key's or a scalar item's own event takes.
func (s *gapScanner) ownFoot(slot, text string) {
	appendSlot(s.out, slot, text)
	if slot != "" {
		s.ownFeet[slot] = true
	}
}

// nextFoot returns the slot of a foot comment that reaches the next item,
// and whether decode.go hands it on to an earlier key. A key hands it to
// the key before it in the same mapping, as parser.mapping does, and keeps
// it when it has no such key. A sequence dash passes it to the first node
// of its item that takes comments: a scalar item, or the first key of a
// mapping item.
func (s *gapScanner) nextFoot() (string, bool) {
	next, a := s.g.next, s.g.prev.ancs
	switch {
	case next == nil:
		return "", false
	case next.kind == 'E':
		if s.g.after == nil || s.g.after.line < next.line {
			return "", false
		}
		return s.g.after.ancs[len(s.g.after.ancs)-1].foot, false
	}
	for i := len(a) - 1; i >= 0; i-- {
		if a[i].key && a[i].col == next.col {
			return a[i].foot, true
		}
	}
	return next.ancs[len(next.ancs)-1].foot, false
}

func (s *gapScanner) finishHead(explicit bool) {
	if s.g.next == nil {
		return
	}
	t := strings.Join(s.text, "\n")
	if s.g.prev == nil && !explicit {
		t = documentHeadRemainder(t)
	}
	if t != "" {
		s.preHead = append(s.preHead, t)
	}
	appendSlot(s.out, s.g.next.head, strings.Join(s.preHead, "\n"))
}

// documentHeadRemainder splits the comments before an implicit document's
// first key at the last blank line. yaml.v3 gives the part above to the
// document, which neat never prints, and the rest to the first key.
func documentHeadRemainder(t string) string {
	if i := strings.LastIndex(t, "\n\n"); i >= 0 {
		if i+2 >= len(t) {
			return ""
		}
		t = t[i+2:]
	}
	if strings.HasSuffix(t, "\n") {
		return ""
	}
	return t
}

// applySlots writes the computed slots onto a built document, walking it
// with the same path keys the walker used.
func applySlots(n *Node, path string, slots map[string]string) {
	switch n.Kind {
	case DocumentNode:
		for _, c := range n.Content {
			if c.Kind == ScalarNode {
				c.HeadComment = slots[path+"#VH"]
				c.LineComment = slots[path+"#VL"]
				c.FootComment = slots[path+"#VF"]
			}
			applySlots(c, path, slots)
		}
	case MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			kp := fmt.Sprintf("%s{%d}", path, i/2)
			k.HeadComment = slots[kp+"#KH"]
			k.FootComment = slots[kp+"#KF"]
			if v.Kind == ScalarNode {
				v.LineComment = slots[kp+"#VL"]
				v.FootComment = slots[kp+"#VF"]
			}
			applySlots(v, kp, slots)
		}
	case SequenceNode:
		for i, c := range n.Content {
			ip := fmt.Sprintf("%s[%d]", path, i)
			if c.Kind == ScalarNode {
				c.HeadComment = slots[ip+"#VH"]
				c.LineComment = slots[ip+"#VL"]
				c.FootComment = slots[ip+"#VF"]
			}
			applySlots(c, ip, slots)
		}
	default:
		// Scalars and aliases hold no further slots.
	}
}
