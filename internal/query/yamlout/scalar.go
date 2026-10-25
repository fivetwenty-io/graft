package yamlout

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/fivetwenty-io/graft/pkg/query/value"

	"github.com/fivetwenty-io/graft/internal/query/jqout"
)

// Style is how a scalar is written.
type Style uint8

// The scalar styles. The emitter picks Plain, DoubleQuoted, SingleQuoted,
// and Literal on its own, and Folded is reserved for scalars that arrive
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
// must not print as a finite number.
func yamlNumber(v *value.Value, floatMode bool) string {
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

// singleQuoted writes text in single quotes, doubling any it holds.
func singleQuoted(text string) string {
	return "'" + strings.ReplaceAll(text, "'", "''") + "'"
}

// literalHeader returns the block scalar header for text, "|" followed by an
// indentation hint when the text starts with a space or a break, then the
// chomping indicator. The hint is the body's indent past its parent.
func literalHeader(text string, hint int) string {
	h := "|"
	if text != "" && (text[0] == ' ' || text[0] == '\n') {
		h += strconv.Itoa(hint)
	}
	switch {
	case !strings.HasSuffix(text, "\n"):
		h += "-"
	case strings.HasSuffix(text, "\n\n") || text == "\n":
		h += "+"
	}
	return h
}
