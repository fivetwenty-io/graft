package yamlout

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/fivetwenty-io/graft/pkg/query/value"

	"github.com/fivetwenty-io/graft/internal/query/jqout"
	"github.com/fivetwenty-io/graft/internal/query/load"
	"github.com/fivetwenty-io/graft/internal/yamlnode"
)

// Style is how a scalar is written.
type Style uint8

// The scalar styles. The emitter picks Plain, DoubleQuoted, SingleQuoted,
// and Literal on its own, and Folded only for a scalar that arrives
// carrying that style.
const (
	StylePlain Style = iota
	StyleDoubleQuoted
	StyleSingleQuoted
	StyleLiteral
	StyleFolded
)

// scalarContext says where a string is about to be written.
type scalarContext struct {
	Key  bool // a simple mapping key
	Flow bool // inside a flow collection
}

// scalarAnalysis records which styles can represent a string.
type scalarAnalysis struct {
	plainOK       bool
	singleOK      bool
	blockOK       bool
	plainFlowOK   bool
	onlyLFBreaks  bool
	hasLineBreaks bool
}

// scalarStyle picks the style for a string. It quotes what would read back
// as another type, in double quotes except where single quotes can say it
// without escapes, and writes a multi-line string as a literal block.
func scalarStyle(text string, sc scalarContext) Style {
	a := analyze(text)
	requested := StylePlain
	switch {
	case readsAsOtherType(text):
		requested = StyleDoubleQuoted
	case a.hasLineBreaks:
		requested = StyleLiteral
	}
	style := requested
	if sc.Key && a.hasLineBreaks {
		style = StyleDoubleQuoted
	}
	if style == StylePlain {
		style = plainOrQuoted(text, a, sc)
	}
	if style == StyleSingleQuoted && !a.singleOK {
		style = StyleDoubleQuoted
	}
	if style == StyleLiteral && !literalFits(a, sc) {
		style = StyleDoubleQuoted
	}
	return style
}

// plainOrQuoted settles a string that asked for plain style. Flow context
// never takes single quotes, since a string that needs quotes there is
// written with double quotes.
func plainOrQuoted(text string, a scalarAnalysis, sc scalarContext) Style {
	switch {
	case sc.Flow && !a.plainFlowOK:
		return StyleDoubleQuoted
	case !a.plainOK, text == "" && sc.Key:
		return StyleSingleQuoted
	}
	return StylePlain
}

func literalFits(a scalarAnalysis, sc scalarContext) bool {
	return a.blockOK && a.onlyLFBreaks && !sc.Key && !sc.Flow
}

func isBreakRune(r rune) bool {
	return r == '\n' || r == '\r' || r == 0x85 || r == 0x2028 || r == 0x2029
}

func isPrintableRune(r rune) bool {
	switch {
	case r == '\n', r >= 0x20 && r <= 0x7E, r == 0x85:
		return true
	case r >= 0xA0 && r <= 0xD7FF, r >= 0xE000 && r <= 0xFFFD && r != 0xFEFF, r >= 0x10000 && r <= 0x10FFFF:
		return true
	}
	return false
}

func isBlankRune(r rune) bool { return r == ' ' || r == '\t' }

// analyze decides which styles can carry text without losing it.
//
//nolint:gocyclo // one pass over the characters that tracks every property the styles depend on
func analyze(text string) scalarAnalysis {
	a := scalarAnalysis{plainOK: true, singleOK: true, blockOK: true, plainFlowOK: true, onlyLFBreaks: true}
	if text == "" {
		return a
	}
	var (
		indicators, special                          bool
		leadSpace, leadBreak, trailSpace, trailBreak bool
		breakSpace, spaceBreak, prevSpace, prevBreak bool
		prevBlankz                                   = true
	)
	if strings.HasPrefix(text, "---") || strings.HasPrefix(text, "...") {
		indicators = true
	}
	rs := []rune(text)
	if !utf8.ValidString(text) {
		special = true
	}
	for i, r := range rs {
		followedBlank := i+1 >= len(rs) || isBlankRune(rs[i+1])
		if i == 0 {
			switch r {
			case '#', ',', '[', ']', '{', '}', '&', '*', '!', '|', '>', '\'', '"', '%', '@', '`':
				indicators = true
			case '?', ':', '-':
				if followedBlank {
					indicators = true
				}
			}
		} else {
			switch r {
			case ':':
				if followedBlank {
					indicators = true
				}
			case '#':
				if prevBlankz {
					indicators = true
				}
			}
		}
		switch r {
		case ',', '[', ']', '{', '}':
			a.plainFlowOK = false
		}
		if !isPrintableRune(r) {
			special = true
		}
		last := i == len(rs)-1
		switch {
		case r == ' ':
			leadSpace = leadSpace || i == 0
			trailSpace = trailSpace || last
			breakSpace = breakSpace || prevBreak
			prevSpace, prevBreak = true, false
		case isBreakRune(r):
			a.hasLineBreaks = true
			if r != '\n' {
				a.onlyLFBreaks = false
			}
			leadBreak = leadBreak || i == 0
			trailBreak = trailBreak || last
			spaceBreak = spaceBreak || prevSpace
			prevSpace, prevBreak = false, true
		default:
			prevSpace, prevBreak = false, false
		}
		prevBlankz = isBlankRune(r) || isBreakRune(r)
	}
	if leadSpace || leadBreak || trailSpace || trailBreak {
		a.plainOK = false
	}
	if trailSpace {
		a.blockOK = false
	}
	if breakSpace {
		a.plainOK, a.singleOK = false, false
	}
	if spaceBreak || special {
		a.plainOK, a.singleOK, a.blockOK = false, false, false
	}
	if a.hasLineBreaks || indicators {
		a.plainOK = false
	}
	return a
}

var otherTypeWords = map[string]bool{
	"yes": true, "Yes": true, "YES": true,
	"no": true, "No": true, "NO": true,
	"true": true, "True": true, "TRUE": true,
	"false": true, "False": true, "FALSE": true,
	"on": true, "On": true, "ON": true,
	"off": true, "Off": true, "OFF": true,
	"": true, "~": true, "null": true, "Null": true, "NULL": true,
	".nan": true, ".NaN": true, ".NAN": true,
	".inf": true, ".Inf": true, ".INF": true,
	"+.inf": true, "+.Inf": true, "+.INF": true,
	"-.inf": true, "-.Inf": true, "-.INF": true,
	"<<": true,
}

// readsAsOtherType reports whether a plain scalar with this text would be
// read back as a boolean, null, number, or merge key.
func readsAsOtherType(s string) bool {
	if otherTypeWords[s] {
		return true
	}
	c := s[0]
	if (c < '0' || c > '9') && c != '+' && c != '-' && c != '.' {
		return false
	}
	if looksLikeBase60(s) {
		return true
	}
	plain := s
	if c != '.' {
		// A leading dot starts a YAML float, and its digits take no
		// underscores in the core schema.
		plain = strings.ReplaceAll(s, "_", "")
	}
	if _, err := strconv.ParseInt(plain, 0, 64); err == nil {
		return true
	}
	if _, err := strconv.ParseUint(plain, 0, 64); err == nil {
		return true
	}
	if _, err := strconv.ParseFloat(plain, 64); err == nil {
		return true
	}
	if rest, ok := cutBinary(plain); ok {
		if _, err := strconv.ParseUint(rest, 2, 64); err == nil {
			return true
		}
	}
	return false
}

// cutBinary splits a 0b or -0b prefix off s.
func cutBinary(s string) (rest string, ok bool) {
	switch {
	case strings.HasPrefix(s, "0b"):
		return s[2:], true
	case strings.HasPrefix(s, "-0b"):
		return s[3:], true
	}
	return "", false
}

// looksLikeBase60 matches YAML 1.1 sexagesimal numbers such as 1:30.
func looksLikeBase60(s string) bool {
	if !strings.Contains(s, ":") {
		return false
	}
	if s[0] == '+' || s[0] == '-' {
		s = s[1:]
	}
	head, rest, _ := strings.Cut(s, ":")
	if head == "" || !allDigits(head, "_") {
		return false
	}
	groups, frac, _ := strings.Cut(rest, ".")
	for _, g := range strings.Split(groups, ":") {
		if g == "" || len(g) > 2 || !allDigits(g, "") {
			return false
		}
	}
	return allDigits(frac, "_")
}

// allDigits reports whether s holds only ASCII digits and the extra bytes,
// and starts with a digit when it is not empty.
func allDigits(s, extra string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if (c < '0' || c > '9') && strings.IndexByte(extra, c) < 0 {
			return false
		}
	}
	return s == "" || s[0] >= '0' && s[0] <= '9'
}

// yamlNumber writes a number the way YAML spells it. YAML can say NaN, the
// infinities, and negative zero where JSON cannot, and a computed infinity
// must not print as a finite number. With goFloats, a number a yq operator
// tagged !!float prints as Go prints a float64.
func yamlNumber(v *value.Value, floatMode, goFloats bool) string {
	if v.Literal() == "" {
		d := v.Dec()
		switch {
		case d.IsNaN():
			return ".nan"
		case d.IsZero() && d.IsNegative():
			return "-0"
		}
		if inf, sign := d.IsInf(); inf {
			if sign < 0 {
				return "-.inf"
			}
			return ".inf"
		}
	}
	if goFloats && v.Literal() == "" && v.Meta() != nil && v.Meta().Tag == yamlnode.TagFloat {
		// A float64 a yq operator computed prints as Go prints one,
		// with an exponent from 1e+06 up.
		return strconv.FormatFloat(v.Float(), 'g', -1, 64)
	}
	if floatMode {
		return jqout.FormatNumberFloat(v)
	}
	return jqout.FormatNumber(v)
}

const hexDigits = "0123456789ABCDEF"

var simpleEscapes = map[rune]string{
	'"': `\"`, '\\': `\\`, 0: `\0`, 7: `\a`, 8: `\b`, '\t': `\t`, '\n': `\n`,
	0x0B: `\v`, 0x0C: `\f`, '\r': `\r`, 0x1B: `\e`, 0x85: `\N`, 0xA0: `\_`,
	0x2028: `\L`, 0x2029: `\P`,
}

// doubleQuoted writes text in double quotes, escaping what a reader could
// not take literally.
func doubleQuoted(text string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(text); {
		r, w := utf8.DecodeRuneInString(text[i:])
		if r == utf8.RuneError && w == 1 {
			writeHexEscape(&b, rune(text[i]))
			i++
			continue
		}
		i += w
		if esc, ok := simpleEscapes[r]; ok {
			b.WriteString(esc)
		} else if !isPrintableRune(r) {
			writeHexEscape(&b, r)
		} else {
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// writeHexEscape writes r as \xXX, \uXXXX, or \UXXXXXXXX, whichever is
// the shortest that holds it.
func writeHexEscape(b *strings.Builder, r rune) {
	digits, prefix := 8, byte('U')
	switch {
	case r <= 0xFF:
		digits, prefix = 2, 'x'
	case r <= 0xFFFF:
		digits, prefix = 4, 'u'
	}
	b.WriteByte('\\')
	b.WriteByte(prefix)
	for k := (digits - 1) * 4; k >= 0; k -= 4 {
		b.WriteByte(hexDigits[(r>>uint(k))&15])
	}
}

// quoteStyles are the style flags that ask for a scalar style.
const quoteStyles = value.SingleQuoted | value.DoubleQuoted | value.Literal | value.Folded

// scalar writes a scalar whole: anchor, tag, and text in its style. A
// block scalar's lines indent one level past the node that holds it.
func (e *emitter) scalar(v *value.Value, c nodeCtx) {
	m := v.Meta()
	text, style, fromMeta := e.scalarStyle(v, c)
	e.anchor(m)
	if m != nil {
		e.tag(scalarTag(v, m))
	}
	e.increase(true, c, false)
	switch style {
	case StylePlain:
		e.plain(text)
	case StyleSingleQuoted:
		e.single(text)
	case StyleDoubleQuoted:
		if !e.ws {
			e.put(" ")
		}
		e.word(doubleQuoted(text))
	case StyleLiteral:
		e.block("|", text, fromMeta)
	case StyleFolded:
		e.block(">", text, fromMeta)
	}
	e.decrease()
}

// scalarStyle returns a scalar's text and style, and whether they follow
// the scalar's Meta. A string read with Meta keeps its style where the
// spot allows it. One built new takes the rules scalars without Meta
// have always had, and at the root always takes double quotes.
func (e *emitter) scalarStyle(v *value.Value, c nodeCtx) (string, Style, bool) {
	m := v.Meta()
	flow := e.flow > 0
	if v.Kind() != value.KindString {
		text := e.scalarText(v)
		if text == "" && (flow || e.simpleKey) {
			// An empty plain scalar cannot stand there, so the null
			// of "a:" is written as libyaml writes it, as ''.
			return text, StyleSingleQuoted, false
		}
		if m == nil || m.Style&quoteStyles == 0 {
			return text, StylePlain, false
		}
		return text, selectStyle(text, requestedStyle(m, text, v), flow, e.simpleKey), true
	}
	s := v.Str()
	if m == nil || (m.Text != s && m.Style&quoteStyles == 0) {
		if c == ctxRoot {
			return s, StyleDoubleQuoted, false
		}
		return s, scalarStyle(s, scalarContext{Key: e.simpleKey, Flow: flow}), false
	}
	return s, selectStyle(s, requestedStyle(m, s, v), flow, e.simpleKey), true
}

// requestedStyle is the style scalar v with Meta asks for. Without a
// style flag, text with a line break asks for a literal block, and an
// untagged scalar that plain style would read back as something else
// asks for double quotes. A scalar given a tag other than !!str, which
// only an assignment of the tag does, keeps plain style, as yq's does.
func requestedStyle(m *value.Meta, text string, v *value.Value) Style {
	switch {
	case m.Style&value.DoubleQuoted != 0:
		return StyleDoubleQuoted
	case m.Style&value.SingleQuoted != 0:
		return StyleSingleQuoted
	case m.Style&value.Literal != 0:
		return StyleLiteral
	case m.Style&value.Folded != 0:
		return StyleFolded
	case strings.Contains(text, "\n"):
		return StyleLiteral
	}
	if m.Style&value.Tagged == 0 && (m.Tag == "" || shortTag(m.Tag) == yamlnode.TagStr) && !readsAs(text, v) {
		return StyleDoubleQuoted
	}
	return StylePlain
}

// selectStyle settles a requested style the way libyaml does. A plain
// scalar the spot cannot hold takes single quotes, single quotes that
// cannot hold it take double ones, and so does a block scalar outside
// block context or with breaks a block cannot keep.
func selectStyle(text string, style Style, flow, simpleKey bool) Style {
	a := libyamlAnalyze(text)
	if simpleKey && a.multiline {
		style = StyleDoubleQuoted
	}
	if style == StylePlain && !a.plainFits(text, flow, simpleKey) {
		style = StyleSingleQuoted
	}
	if style == StyleSingleQuoted && !a.single {
		style = StyleDoubleQuoted
	}
	if (style == StyleLiteral || style == StyleFolded) && (!a.block || !a.onlyLF || flow || simpleKey) {
		style = StyleDoubleQuoted
	}
	return style
}

// plainFits reports whether text can stay plain where it is written.
func (a libyamlAllowed) plainFits(text string, flow, simpleKey bool) bool {
	if text == "" && (flow || simpleKey) {
		return false
	}
	if flow {
		return a.flowPlain
	}
	return a.blockPlain
}

// scalarText is a non-string scalar's text: the text it was read with,
// while that still reads back as the value, or else the text a scalar
// without Meta is written with.
func (e *emitter) scalarText(v *value.Value) string {
	if m := v.Meta(); m != nil && v.Kind() != value.KindString && readsAs(m.Text, v) {
		return m.Text
	}
	switch v.Kind() {
	case value.KindNull:
		return "null"
	case value.KindTrue:
		return "true"
	case value.KindFalse:
		return "false"
	case value.KindNumber:
		return yamlNumber(v, e.o.FloatNumbers, e.o.GoFloats)
	case value.KindString:
		return v.Str()
	case value.KindArray, value.KindObject:
	}
	return ""
}

// readsAs reports whether text, read as a plain scalar, is v.
func readsAs(text string, v *value.Value) bool {
	got, err := load.FromNode(&yamlnode.Node{Kind: yamlnode.ScalarNode, Value: text}, load.FromNodeOptions{})
	if err != nil || got.Kind() != v.Kind() {
		return false
	}
	if v.Kind() == value.KindNumber && got.Dec().IsNaN() && v.Dec().IsNaN() {
		return true
	}
	return value.Equal(got, v)
}

// scalarTag returns the tag a scalar is written with: one marked as
// written, or one that is not the tag its kind takes anyway. A string
// given a number's tag, which only an assignment of the tag does, prints
// without it, as yq's does, and so does a string that reads as a
// timestamp and is tagged one.
func scalarTag(v *value.Value, m *value.Meta) string {
	if m.Tag == "" || m.Style&value.Tagged != 0 {
		return m.Tag
	}
	own := map[value.Kind][]string{
		value.KindString: {yamlnode.TagStr, yamlnode.TagInt, yamlnode.TagFloat},
		value.KindNumber: {yamlnode.TagInt, yamlnode.TagFloat},
		value.KindTrue:   {yamlnode.TagBool},
		value.KindFalse:  {yamlnode.TagBool},
		value.KindNull:   {yamlnode.TagNull},
	}[v.Kind()]
	for _, t := range own {
		if shortTag(m.Tag) == t {
			return ""
		}
	}
	if v.Kind() == value.KindString && shortTag(m.Tag) == yamlnode.TagTimestamp && yamlnode.ResolvePlainTag(v.Str()) == yamlnode.TagTimestamp {
		return ""
	}
	return m.Tag
}

// libyamlAllowed records the styles libyaml's emitter lets a scalar take.
type libyamlAllowed struct {
	flowPlain, blockPlain, single, block bool
	multiline, onlyLF                    bool
}

// libyamlAnalyze decides the styles open to text by libyaml's rules,
// which differ from analyze in letting tabs into block scalars and in
// the indicators that rule out plain style in flow context.
//
//nolint:gocyclo // one pass over the characters that tracks every property the styles depend on
func libyamlAnalyze(text string) libyamlAllowed {
	if text == "" {
		return libyamlAllowed{blockPlain: true, single: true, onlyLF: true}
	}
	var (
		flowInd, blockInd, breaks, tabs, special     bool
		leadSpace, leadBreak, trailSpace, trailBreak bool
		breakSpace, spaceBreak, prevSpace, prevBreak bool
		onlyLF                                       = true
		precededByWS                                 = true
	)
	if strings.HasPrefix(text, "---") || strings.HasPrefix(text, "...") {
		flowInd, blockInd = true, true
	}
	if !utf8.ValidString(text) {
		special = true
	}
	rs := []rune(text)
	for i, r := range rs {
		followedByWS := i+1 >= len(rs) || isBlankRune(rs[i+1])
		if i == 0 {
			switch r {
			case '#', ',', '[', ']', '{', '}', '&', '*', '!', '|', '>', '\'', '"', '%', '@', '`':
				flowInd, blockInd = true, true
			case '?', ':':
				flowInd = true
				blockInd = blockInd || followedByWS
			case '-':
				if followedByWS {
					flowInd, blockInd = true, true
				}
			}
		} else {
			switch r {
			case ',', '?', '[', ']', '{', '}':
				flowInd = true
			case ':':
				flowInd = true
				blockInd = blockInd || followedByWS
			case '#':
				if precededByWS {
					flowInd, blockInd = true, true
				}
			}
		}
		switch {
		case r == '\t':
			tabs = true
		case !isPrintableRune(r):
			special = true
		}
		last := i == len(rs)-1
		switch {
		case r == ' ':
			leadSpace = leadSpace || i == 0
			trailSpace = trailSpace || last
			breakSpace = breakSpace || prevBreak
			prevSpace, prevBreak = true, false
		case isBreakRune(r):
			breaks = true
			onlyLF = onlyLF && r == '\n'
			leadBreak = leadBreak || i == 0
			trailBreak = trailBreak || last
			spaceBreak = spaceBreak || prevSpace
			prevSpace, prevBreak = false, true
		default:
			prevSpace, prevBreak = false, false
		}
		precededByWS = isBlankRune(r) || isBreakRune(r) || r == 0
	}
	a := libyamlAllowed{flowPlain: true, blockPlain: true, single: true, block: true, multiline: breaks, onlyLF: onlyLF}
	if leadSpace || leadBreak || trailSpace || trailBreak || breaks {
		a.flowPlain, a.blockPlain = false, false
	}
	if trailSpace {
		a.block = false
	}
	if breakSpace || spaceBreak || tabs || special {
		a.flowPlain, a.blockPlain, a.single = false, false, false
	}
	if spaceBreak || special {
		a.block = false
	}
	if flowInd {
		a.flowPlain = false
	}
	if blockInd {
		a.blockPlain = false
	}
	return a
}

// plain writes text as it is, after a space unless one comes before it.
func (e *emitter) plain(text string) {
	if text != "" && !e.ws {
		e.put(" ")
	}
	breaks := false
	for _, r := range text {
		if isBreakRune(r) {
			if !breaks && r == '\n' {
				e.newline()
			}
			e.lineBreak(r)
			breaks = true
			continue
		}
		if breaks {
			e.writeIndent()
		}
		e.put(string(r))
		e.indention = false
		breaks = false
	}
	if text != "" {
		e.ws = false
	}
	e.indention = false
}

// single writes text in single quotes. A line break inside them takes an
// empty line, which is how a reader gets the break back.
func (e *emitter) single(text string) {
	e.indicator("'", true, false, false)
	breaks := false
	for _, r := range text {
		if isBreakRune(r) {
			if !breaks && r == '\n' {
				e.newline()
			}
			e.lineBreak(r)
			breaks = true
			continue
		}
		if breaks {
			e.writeIndent()
		}
		if r == '\'' {
			e.put("'")
		}
		e.put(string(r))
		e.indention = false
		breaks = false
	}
	e.indicator("'", false, false, false)
	e.ws = false
	e.indention = false
}

// block writes a literal or folded block scalar, its header and then its
// lines one level in.
func (e *emitter) block(indicator, text string, fromMeta bool) {
	e.indicator(indicator, true, false, false)
	e.blockHints(text, fromMeta)
	e.processLine(true)
	e.ws = true
	if indicator == ">" {
		e.foldedBody(text)
		return
	}
	breaks := true
	for _, r := range text {
		if isBreakRune(r) {
			e.lineBreak(r)
			breaks = true
			continue
		}
		if breaks {
			e.writeIndent()
		}
		e.put(string(r))
		e.indention = false
		breaks = false
	}
}

// blockHints writes the indentation hint a block scalar needs when its
// first line starts with a space, and its chomping indicator. A scalar
// built new also takes the hint when it starts with a line break, as
// it always has; yq leaves the hint off there.
func (e *emitter) blockHints(text string, fromMeta bool) {
	if strings.HasPrefix(text, " ") || !fromMeta && strings.HasPrefix(text, "\n") {
		e.indicator(strconv.Itoa(e.level-e.parentLevel()), false, false, false)
	}
	rs := []rune(text)
	n := len(rs)
	switch {
	case n == 0 || !isBreakRune(rs[n-1]):
		e.indicator("-", false, false, false)
	case n == 1 || isBreakRune(rs[n-2]):
		e.indicator("+", false, false, false)
	}
}

// foldedBody writes a folded scalar's lines. A break after a line of text
// takes an extra empty line so that it reads back as a break and not as a
// space. Like yq, the test for that looks at the start of the whole text
// and not at the line that follows.
func (e *emitter) foldedBody(text string) {
	rs := []rune(text)
	k := 0
	for k < len(rs) && isBreakRune(rs[k]) {
		k++
	}
	extraBreak := k < len(rs) && !isBlankRune(rs[k]) && rs[k] != 0
	breaks, leadingSpaces := true, true
	for i, r := range rs {
		if isBreakRune(r) {
			if !breaks && !leadingSpaces && r == '\n' && extraBreak {
				e.newline()
			}
			e.lineBreak(r)
			breaks = true
			continue
		}
		if breaks {
			e.writeIndent()
			leadingSpaces = isBlankRune(rs[i])
		}
		e.put(string(r))
		e.indention = false
		breaks = false
	}
}
