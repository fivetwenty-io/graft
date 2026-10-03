package graft

import (
	"bytes"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/token"
)

// MarshalYAML serializes a value to YAML with 2-space indentation,
// matching the output format expected by BOSH and CF ecosystem tools.
// Map keys are emitted in spruce's two-tier order (see spruceKeyLess).
// Every string, key or value, is written in the style spruce's emitter
// picks for it (plain, single-quoted, double-quoted, or a literal
// block), folded at the same column, and keys spruce cannot write as
// simple keys use the explicit "? key" form; see scalarEncoder.
func MarshalYAML(v interface{}) ([]byte, error) {
	return MarshalYAMLWithComments(v, nil)
}

// YAMLHeadComment attaches a block of comment lines (rendered "# "-
// prefixed, one per entry in Lines) directly above one node in
// MarshalYAMLWithComments' output. Path uses graft/spruce's dotted
// document-path syntax (e.g. "meta.password", or "jobs.0.name" for a
// list index - no leading "$."); MarshalYAMLWithComments converts it to
// the bracket-indexed form goccy/go-yaml's own path syntax requires
// ("$.jobs[0].name") internally, so callers never need to know that
// detail (or depend on goccy/go-yaml's types directly).
type YAMLHeadComment struct {
	Path  string
	Lines []string
}

// MarshalYAMLWithComments serializes v exactly like MarshalYAML (same
// spruce key ordering and quoting rules - both share this
// implementation, MarshalYAML is comments == nil), then additionally
// attaches each entry in comments as a head comment placed directly
// above the node at its Path.
//
// A comments entry whose Path cannot be converted to a valid
// goccy/go-yaml path (see graftPathToYAMLPath) is skipped rather than
// failing the whole encode: comment placement is best-effort, never
// load-bearing for the document's own data. cmd/graft's
// "graft merge --report-deferred=inline" is the only current caller,
// attaching one comment per deferred key directly above that key.
func MarshalYAMLWithComments(v interface{}, comments []YAMLHeadComment) ([]byte, error) {
	enc, prepared := newScalarEncoder(v, comments)

	opts := []yaml.EncodeOption{yaml.Indent(2)}
	if len(comments) > 0 {
		cm := yaml.CommentMap{}
		for _, c := range comments {
			if len(c.Lines) == 0 {
				continue
			}
			yamlPath, ok := graftPathToYAMLPath(enc.encodedPath(prepared, c.Path))
			if !ok {
				continue
			}
			cm[yamlPath] = append(cm[yamlPath], yaml.HeadComment(c.Lines...))
		}
		if len(cm) > 0 {
			opts = append(opts, yaml.WithComment(cm))
		}
	}

	var buf bytes.Buffer
	ye := yaml.NewEncoder(&buf, opts...)
	if err := ye.Encode(prepared); err != nil {
		return nil, err
	}
	if err := ye.Close(); err != nil {
		return nil, err
	}
	return enc.substitute(buf.Bytes()), nil
}

// graftPathToYAMLPath converts a graft/spruce dotted document path (no
// leading "$.", a purely-numeric segment meaning a list index - e.g.
// "jobs.0.name") to the "$."-rooted, bracket-indexed path string
// goccy/go-yaml's yaml.PathString (and so yaml.WithComment's CommentMap
// keys) requires (e.g. "$.jobs[0].name"). Reports ok=false for an empty
// path, or one containing a "$", "[", "]", or "'" character graft's own
// dotted-path syntax has no escaping convention for and that would
// otherwise be misparsed or rejected by yaml.PathString.
//
// A map key that is itself all-digits (e.g. a key literally named "0")
// is indistinguishable from a list index in this syntax and is treated
// as a list index - a pre-existing ambiguity in graft's own dotted-path
// representation (tree.Cursor.String()), not one this conversion
// introduces; see docs/user-guide/adaptive-merge.md's inline-placement
// caveat.
func graftPathToYAMLPath(path string) (string, bool) {
	if path == "" {
		return "", false
	}
	var b strings.Builder
	b.WriteString("$")
	for _, seg := range strings.Split(path, ".") {
		if seg == "" {
			continue
		}
		if isAllDigitsSegment(seg) {
			b.WriteString("[")
			b.WriteString(seg)
			b.WriteString("]")
			continue
		}
		if strings.ContainsAny(seg, "$[]'") {
			return "", false
		}
		b.WriteString(".")
		b.WriteString(seg)
	}
	return b.String(), true
}

// isAllDigitsSegment reports whether seg consists entirely of ASCII
// digits (and is non-empty) - graftPathToYAMLPath's list-index test.
func isAllDigitsSegment(seg string) bool {
	if seg == "" {
		return false
	}
	for _, r := range seg {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// scalarPlaceholderPrefix starts the stand-in text scalarEncoder puts
// in place of a string before goccy encodes the tree. It is a plain
// scalar to goccy, so goccy writes it verbatim, and an "X" is appended
// to it for as long as the document's own text contains it.
const scalarPlaceholderPrefix = "graftscalar"

// scalarSlotKind says where a placeholder stands in the document.
type scalarSlotKind uint8

const (
	// valueSlot is a mapping value, a sequence item, or the document
	// root.
	valueSlot scalarSlotKind = iota
	// simpleKeySlot is a mapping key written as "key: value".
	simpleKeySlot
	// complexKeySlot is a mapping key written in the explicit
	// "? key" form, which spruce uses for a multi-line key and for one
	// longer than 128 bytes.
	complexKeySlot
)

// scalarSlot records one string that substitute writes in spruce's
// style in place of its placeholder.
type scalarSlot struct {
	s    string
	kind scalarSlotKind
	// indent is the block indent of a value's continuation lines, or
	// the column of a complex key's mapping.
	indent int
	// indention is libyaml's flag for a value that only indentation and
	// indicators precede on its line, as after "- ".
	indention bool
	// mapValue and seqValue mark a complex key whose value is a
	// non-empty block mapping or sequence. libyaml starts that value on
	// the ":" line, where goccy starts it on the next line.
	mapValue, seqValue bool
}

// nodePosition is where libyaml writes a node: the column a scalar
// starts at and the indent its continuation lines use, and the columns
// a block mapping's keys or a block sequence's dashes sit at.
type nodePosition struct {
	scalarColumn int
	scalarIndent int
	indention    bool
	mapColumn    int
	seqColumn    int
}

// rootPosition is the document root. A root scalar's continuation lines
// sit at libyaml's best indent.
var rootPosition = nodePosition{scalarIndent: scalarBestIndent, indention: true}

// mapValuePosition is the value of a simple key of width keyWidth in a
// mapping whose keys sit at column m. A sequence there is indentless.
func mapValuePosition(m, keyWidth int) nodePosition {
	return nodePosition{
		scalarColumn: m + keyWidth + 2,
		scalarIndent: m + scalarBestIndent,
		mapColumn:    m + scalarBestIndent,
		seqColumn:    m,
	}
}

// complexValuePosition is the value of a "? key" in a mapping whose keys
// sit at column m. It follows ": " and, unlike a simple key's value, a
// sequence there is indented.
func complexValuePosition(m int) nodePosition {
	return nodePosition{
		scalarColumn: m + 2,
		scalarIndent: m + scalarBestIndent,
		indention:    true,
		mapColumn:    m + scalarBestIndent,
		seqColumn:    m + scalarBestIndent,
	}
}

// seqItemPosition is an item of a sequence whose dashes sit at column d.
func seqItemPosition(d int) nodePosition {
	return nodePosition{
		scalarColumn: d + 2,
		scalarIndent: d + scalarBestIndent,
		indention:    true,
		mapColumn:    d + scalarBestIndent,
		seqColumn:    d + scalarBestIndent,
	}
}

// scalarEncoder writes strings the way spruce does on top of goccy's
// encoder. goccy lays out the mappings and sequences, which already
// match spruce's layout, but picks scalar styles by its own rules and
// cannot write an explicit "? key". So prepare swaps every string that
// goccy would write differently from spruce for a numbered placeholder,
// goccy encodes the tree, and substitute replaces each placeholder with
// the text spruce's emitter writes at that column (see yaml_scalar.go).
type scalarEncoder struct {
	prefix string
	slots  []scalarSlot
}

// newScalarEncoder prepares v for goccy and returns the encoder that
// fills its placeholders back in.
func newScalarEncoder(v interface{}, comments []YAMLHeadComment) (*scalarEncoder, interface{}) {
	prefix := scalarPlaceholderPrefix
	for {
		e := &scalarEncoder{prefix: prefix}
		prepared, ok := e.prepare(v, rootPosition)
		if ok && !commentsContain(comments, prefix) {
			return e, prepared
		}
		prefix += "X"
	}
}

func commentsContain(comments []YAMLHeadComment, s string) bool {
	for _, c := range comments {
		for _, line := range c.Lines {
			if strings.Contains(line, s) {
				return true
			}
		}
	}
	return false
}

// prepare rebuilds v for goccy: every map becomes a yaml.MapSlice with
// its keys in spruceKeyLess order (goccy's own encoder would sort them
// purely lexicographically), and strings become placeholders where
// needed. It reports ok=false when a string contains e.prefix. The
// input is not mutated.
func (e *scalarEncoder) prepare(v interface{}, pos nodePosition) (interface{}, bool) {
	switch val := v.(type) {
	case string:
		return e.prepareValue(val, pos)
	case map[string]interface{}:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			return spruceKeyLess(keys[i], keys[j])
		})
		out := make(yaml.MapSlice, 0, len(keys))
		for _, k := range keys {
			key, valuePos, ok := e.prepareKey(k, pos.mapColumn, val[k])
			if !ok {
				return nil, false
			}
			item, ok := e.prepare(val[k], valuePos)
			if !ok {
				return nil, false
			}
			out = append(out, yaml.MapItem{Key: key, Value: item})
		}
		return out, true
	case map[interface{}]interface{}:
		// Stringify keys before building MapItems: goccy's MapSlice
		// encoder type-asserts each key .(string) unchecked and would
		// panic on anything else.
		converted := make(map[string]interface{}, len(val))
		for k, item := range val {
			converted[fmt.Sprintf("%v", k)] = item
		}
		return e.prepare(converted, pos)
	case []interface{}:
		out := make([]interface{}, len(val))
		itemPos := seqItemPosition(pos.seqColumn)
		for i, item := range val {
			prepared, ok := e.prepare(item, itemPos)
			if !ok {
				return nil, false
			}
			out[i] = prepared
		}
		return out, true
	default:
		return v, true
	}
}

// prepareValue returns s, or a placeholder when goccy would write s
// differently from spruce.
func (e *scalarEncoder) prepareValue(s string, pos nodePosition) (interface{}, bool) {
	if strings.Contains(s, e.prefix) {
		return nil, false
	}
	if !utf8.ValidString(s) {
		// goccy keeps these; spruce cannot read such a string anyway.
		return s, true
	}
	if renderValueScalar(s, pos.scalarColumn, pos.scalarIndent, pos.indention) == s && goccyWritesVerbatim(s) {
		return s, true
	}
	return e.placeholder(scalarSlot{s: s, kind: valueSlot, indent: pos.scalarIndent, indention: pos.indention}), true
}

// prepareKey returns key k, or a placeholder for it, and the position
// of its value in a mapping whose keys sit at column m.
func (e *scalarEncoder) prepareKey(k string, m int, value interface{}) (interface{}, nodePosition, bool) {
	if strings.Contains(k, e.prefix) {
		return nil, nodePosition{}, false
	}
	if !utf8.ValidString(k) {
		return k, mapValuePosition(m, len(k)), true
	}
	text, simple := renderSimpleKey(k, m)
	if !simple {
		slot := scalarSlot{s: k, kind: complexKeySlot, indent: m}
		slot.mapValue, slot.seqValue = blockCollectionKind(value)
		return e.placeholder(slot), complexValuePosition(m), true
	}
	pos := mapValuePosition(m, utf8.RuneCountInString(text))
	if text == k && goccyWritesVerbatim(k) {
		return k, pos, true
	}
	return e.placeholder(scalarSlot{s: k, kind: simpleKeySlot}), pos, true
}

// goccyWritesVerbatim reports whether goccy writes s, a string spruce
// writes as the plain scalar s, as that same text. goccy quotes what
// token.IsNeedQuoted flags and writes a string with a line break as a
// literal block.
//
// token.IsNeedQuoted tries five time layouts on every string, and each
// failed time.Parse allocates an error, which on a large document costs
// more than the rest of the encode. Only a string that starts with a
// digit can match those layouts or read as one of goccy's numbers
// (goccy strips a leading sign or dot first, and spruce already quotes
// every such number), so a string that starts with anything else gets
// the remaining checks inline. goccy's reserved words are all words
// spruce's resolver reads as bools or nulls, so a string spruce writes
// plain is never one of them.
func goccyWritesVerbatim(s string) bool {
	if s == "" || strings.ContainsAny(s, "\r\n") {
		return false
	}
	switch c := s[0]; {
	case c >= '0' && c <= '9', c == '+', c == '-', c == '.':
		return !token.IsNeedQuoted(s)
	case strings.IndexByte("*&[{}],!|>%'\"@ `", c) >= 0:
		return false
	}
	if last := s[len(s)-1]; last == ':' || last == ' ' {
		return false
	}
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '#', '\\':
			return false
		case ':', '-':
			if i+1 < len(s) && s[i+1] == ' ' {
				return false
			}
		}
	}
	return true
}

// blockCollectionKind reports whether v encodes as a non-empty block
// mapping or a non-empty block sequence.
func blockCollectionKind(v interface{}) (isMap, isSeq bool) {
	switch val := v.(type) {
	case nil, []byte:
		return false, false
	case yaml.MapSlice:
		return len(val) > 0, false
	}
	rv := reflect.ValueOf(v)
	if k := rv.Kind(); k == reflect.Map {
		return rv.Len() > 0, false
	} else if k == reflect.Slice || k == reflect.Array {
		return false, rv.Len() > 0
	}
	return false, false
}

func (e *scalarEncoder) placeholder(slot scalarSlot) string {
	e.slots = append(e.slots, slot)
	return e.prefix + strconv.Itoa(len(e.slots)-1) + "_"
}

// original returns the string a prepared key stands for.
func (e *scalarEncoder) original(key string) string {
	if idx, n := e.slotAt([]byte(key)); n == len(key) {
		return e.slots[idx].s
	}
	return key
}

// slotAt parses a placeholder at the start of b, returning its slot
// index and length, or a length of zero when b does not start with one.
func (e *scalarEncoder) slotAt(b []byte) (int, int) {
	if !bytes.HasPrefix(b, []byte(e.prefix)) {
		return 0, 0
	}
	i := len(e.prefix)
	start := i
	for i < len(b) && b[i] >= '0' && b[i] <= '9' {
		i++
	}
	if i == start || i >= len(b) || b[i] != '_' {
		return 0, 0
	}
	idx, err := strconv.Atoi(string(b[start:i]))
	if err != nil || idx >= len(e.slots) {
		return 0, 0
	}
	return idx, i + 1
}

// encodedPath rewrites the keys of a dotted document path to the keys
// they became in prepared, so a comment path still names its node once
// that node's key is a placeholder.
func (e *scalarEncoder) encodedPath(prepared interface{}, path string) string {
	segs := strings.Split(path, ".")
	node := prepared
	for i, seg := range segs {
		switch n := node.(type) {
		case yaml.MapSlice:
			node = nil
			for _, item := range n {
				key, _ := item.Key.(string)
				if e.original(key) == seg {
					segs[i] = key
					node = item.Value
					break
				}
			}
		case []interface{}:
			node = nil
			if idx, err := strconv.Atoi(seg); err == nil && idx >= 0 && idx < len(n) {
				node = n[idx]
			}
		default:
			node = nil
		}
	}
	return strings.Join(segs, ".")
}

// substitute copies goccy's output, writing spruce's text for each
// placeholder at the column it lands on. After a "? key" whose value is
// a block mapping or sequence it also moves that value up onto the ":"
// line, as libyaml writes it, and indents the rest of such a sequence
// by two more columns, since libyaml does not write it indentless.
func (e *scalarEncoder) substitute(in []byte) []byte {
	if len(e.slots) == 0 {
		return in
	}
	st := &substitution{in: in, out: make([]byte, 0, len(in)+len(in)/8)}
	lineStart := true
	for i := 0; i < len(in); {
		if lineStart {
			st.startLine(i)
		}
		if in[i] == e.prefix[0] {
			if idx, n := e.slotAt(in[i:]); n > 0 {
				i, lineStart = st.writeSlot(e.slots[idx], i+n)
				continue
			}
		}
		lineStart = st.copyByte(in[i])
		i++
	}
	return st.out
}

// substitution is substitute's state: goccy's output, the text written
// so far, the character column the next byte lands on, and, for each
// sequence moved under a "? key", the goccy column of its dashes.
type substitution struct {
	in     []byte
	out    []byte
	column int
	shifts []int
}

// startLine indents a line of goccy's output that lies inside a
// sequence moved under a "? key" by two columns per such sequence,
// first dropping each sequence the line has left.
func (st *substitution) startLine(i int) {
	if len(st.shifts) == 0 || st.in[i] == '\n' {
		return
	}
	n := countSpaces(st.in[i:])
	rest := st.in[i+n:]
	for len(st.shifts) > 0 {
		t := st.shifts[len(st.shifts)-1]
		if n > t || (n == t && isDashIndicator(rest)) {
			break
		}
		st.shifts = st.shifts[:len(st.shifts)-1]
	}
	for j := 0; j < 2*len(st.shifts); j++ {
		st.out = append(st.out, ' ')
	}
	st.column += 2 * len(st.shifts)
}

// copyByte copies one byte of goccy's output and reports whether it
// ended a line.
func (st *substitution) copyByte(c byte) bool {
	st.out = append(st.out, c)
	switch {
	case c == '\n':
		st.column = 0
		return true
	case c&0xC0 != 0x80:
		st.column++
	}
	return false
}

// writeSlot writes spruce's text for slot, whose placeholder ended at
// i, and returns where copying resumes and whether a line starts there.
func (st *substitution) writeSlot(slot scalarSlot, i int) (int, bool) {
	var text string
	switch slot.kind {
	case valueSlot:
		text = renderValueScalar(slot.s, st.column, slot.indent, slot.indention)
	case simpleKeySlot:
		text, _ = renderSimpleKey(slot.s, st.column)
	case complexKeySlot:
		if i < len(st.in) && st.in[i] == ':' {
			i++
		}
		text = renderComplexKey(slot.s, slot.indent)
	}
	st.out = append(st.out, text...)
	st.column = advanceColumn(st.column, text)
	if strings.HasSuffix(text, "\n") {
		// A literal block ends its own last line.
		if i < len(st.in) && st.in[i] == '\n' {
			i++
		}
		return i, true
	}
	if slot.kind == complexKeySlot && (slot.mapValue || slot.seqValue) {
		i = st.joinComplexValue(i, slot)
	}
	return i, false
}

// joinComplexValue moves the block mapping or sequence goccy wrote on
// the line after a "? key"'s ":" up onto that line, as libyaml writes
// it, and returns where copying resumes. A head comment above the
// value's first node leaves the value where goccy put it.
func (st *substitution) joinComplexValue(i int, slot scalarSlot) int {
	if i >= len(st.in) || st.in[i] != '\n' {
		return i
	}
	n := countSpaces(st.in[i+1:])
	rest := st.in[i+1+n:]
	if slot.mapValue && (len(rest) == 0 || rest[0] == '#' || rest[0] == '\n') {
		return i
	}
	if slot.seqValue && !isDashIndicator(rest) {
		return i
	}
	st.out = append(st.out, ' ')
	st.column++
	if slot.seqValue {
		st.shifts = append(st.shifts, n)
	}
	return i + 1 + n
}

func countSpaces(b []byte) int {
	n := 0
	for n < len(b) && b[n] == ' ' {
		n++
	}
	return n
}

// isDashIndicator reports whether b starts with a block sequence's "-".
func isDashIndicator(b []byte) bool {
	return len(b) > 0 && b[0] == '-' && (len(b) == 1 || b[1] == ' ' || b[1] == '\n')
}

// advanceColumn returns the column after writing text from column,
// counting characters as libyaml does.
func advanceColumn(column int, text string) int {
	if i := strings.LastIndexByte(text, '\n'); i >= 0 {
		return utf8.RuneCountInString(text[i+1:])
	}
	return column + utf8.RuneCountInString(text)
}
