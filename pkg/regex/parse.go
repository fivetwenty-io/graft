package regex

import (
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxRepeat is the largest count a {} quantifier accepts, as in PCRE2.
const maxRepeat = 65535

// maxNameLen is the longest group name PCRE2 accepts.
const maxNameLen = 32

// parser reads a pattern by recursive descent. Positions are byte offsets
// into src.
type parser struct {
	src   string
	pos   int
	cfg   *config
	names []string // one per group, group 0 first
}

// parse turns a pattern into a tree and returns the group names, group 0
// first.
func parse(src string, cfg *config) (*node, []string, error) {
	p := &parser{src: src, cfg: cfg, names: []string{""}}
	n, err := p.alternation()
	if err != nil {
		return nil, nil, err
	}
	if p.pos < len(p.src) {
		// alternation stops only at the end or at a ')' it did not open.
		return nil, nil, p.errorAt(p.pos, "unmatched closing parenthesis")
	}
	return n, p.names, nil
}

func (p *parser) errorAt(pos int, msg string) error {
	return &SyntaxError{Pos: pos, Msg: msg}
}

func unsupported(construct string) error {
	return &UnsupportedError{Construct: construct}
}

func (p *parser) eof() bool { return p.pos >= len(p.src) }

// peek returns the rune at the current position without consuming it.
func (p *parser) peek() rune {
	if p.eof() {
		return -1
	}
	r, _ := utf8.DecodeRuneInString(p.src[p.pos:])
	return r
}

// next consumes the rune at the current position.
func (p *parser) next() (rune, error) {
	r, w := utf8.DecodeRuneInString(p.src[p.pos:])
	if r == utf8.RuneError && w <= 1 {
		return 0, p.errorAt(p.pos, "invalid UTF-8")
	}
	p.pos += w
	return r, nil
}

func (p *parser) lookingAt(s string) bool { return strings.HasPrefix(p.src[p.pos:], s) }

// skipExtended skips whitespace and comments under the Extended flag.
func (p *parser) skipExtended() {
	if !p.cfg.has(Extended) {
		return
	}
	for !p.eof() {
		r, w := utf8.DecodeRuneInString(p.src[p.pos:])
		switch {
		case isPatternSpace(r):
			p.pos += w
		case r == '#':
			if i := strings.IndexByte(p.src[p.pos:], '\n'); i >= 0 {
				p.pos += i + 1
			} else {
				p.pos = len(p.src)
			}
		default:
			return
		}
	}
}

// isPatternSpace reports whether the Extended flag skips r, which follows
// PCRE2's list for UTF mode.
func isPatternSpace(r rune) bool {
	switch r {
	case ' ', '\t', '\n', '\v', '\f', '\r', 0x85, 0x200e, 0x200f, 0x2028, 0x2029:
		return true
	}
	return false
}

// alternation parses branches separated by | up to the end of the pattern
// or a closing parenthesis, which it leaves unconsumed.
func (p *parser) alternation() (*node, error) {
	var branches []*node
	for {
		b, err := p.concat()
		if err != nil {
			return nil, err
		}
		branches = append(branches, b)
		if p.eof() || p.peek() != '|' {
			break
		}
		p.pos++
	}
	if len(branches) == 1 {
		return branches[0], nil
	}
	return &node{kind: kindAlternate, subs: branches}, nil
}

// concat parses a sequence of quantified atoms and merges adjacent literals
// into runs.
func (p *parser) concat() (*node, error) {
	seq := &node{kind: kindConcat}
	for {
		p.skipExtended()
		if p.eof() || p.peek() == '|' || p.peek() == ')' {
			break
		}
		atom, err := p.atom()
		if err != nil {
			return nil, err
		}
		if atom == nil {
			if err := p.refuseQuantifier(); err != nil {
				return nil, err
			}
			continue
		}
		if atom, err = p.quantified(atom); err != nil {
			return nil, err
		}
		seq.append(atom)
	}
	if len(seq.subs) == 1 {
		return seq.subs[0], nil
	}
	return seq, nil
}

// append adds n to a concat, merging a literal into a literal before it and
// flattening a nested concat.
func (seq *node) append(n *node) {
	if n.kind == kindConcat {
		for _, s := range n.subs {
			seq.append(s)
		}
		return
	}
	if k := len(seq.subs); k > 0 && n.kind == kindLiteral {
		last := seq.subs[k-1]
		if last.kind == kindLiteral && last.fold == n.fold {
			last.runes = append(last.runes, n.runes...)
			return
		}
	}
	if n.kind == kindLiteral {
		n = &node{kind: kindLiteral, runes: slices.Clone(n.runes), fold: n.fold}
	}
	seq.subs = append(seq.subs, n)
}

// refuseQuantifier reports a quantifier that follows nothing repeatable.
func (p *parser) refuseQuantifier() error {
	p.skipExtended()
	if _, _, _, ok := p.scanQuantifier(); ok {
		return p.errorAt(p.pos, "quantifier does not follow a repeatable item")
	}
	return nil
}

// scanQuantifier reads the quantifier at the current position without
// consuming it, returning its bounds and length.
func (p *parser) scanQuantifier() (lo, hi, width int, ok bool) {
	if p.eof() {
		return 0, 0, 0, false
	}
	switch p.src[p.pos] {
	case '*':
		return 0, unbounded, 1, true
	case '+':
		return 1, unbounded, 1, true
	case '?':
		return 0, 1, 1, true
	case '{':
		return p.scanBraces()
	}
	return 0, 0, 0, false
}

// scanBraces reads {n}, {n,}, {n,m}, or {,m}. Anything else starting with {
// is a literal brace, as in PCRE2. A count too large to hold is returned as
// maxRepeat+1 so the caller can report it.
func (p *parser) scanBraces() (lo, hi, width int, ok bool) {
	s := p.src[p.pos+1:]
	end := strings.IndexByte(s, '}')
	if end < 0 {
		return 0, 0, 0, false
	}
	body := s[:end]
	loText, hiText, comma := strings.Cut(body, ",")
	if !allDigits(loText) || !allDigits(hiText) || (loText == "" && hiText == "") || (loText == "" && !comma) {
		return 0, 0, 0, false
	}
	lo = parseCount(loText)
	switch {
	case !comma:
		hi = lo
	case hiText == "":
		hi = unbounded
	default:
		hi = parseCount(hiText)
	}
	return lo, hi, end + 2, true
}

func allDigits(s string) bool {
	for i := range len(s) {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

func parseCount(s string) int {
	if s == "" {
		return 0
	}
	n, err := strconv.Atoi(s)
	if err != nil || n > maxRepeat {
		return maxRepeat + 1
	}
	return n
}

// quantified applies the quantifier that follows atom, if there is one.
func (p *parser) quantified(atom *node) (*node, error) {
	p.skipExtended()
	qpos := p.pos
	lo, hi, width, ok := p.scanQuantifier()
	if !ok {
		return atom, nil
	}
	if atom.kind == kindAssert {
		return nil, p.errorAt(qpos, "quantifier does not follow a repeatable item")
	}
	if lo > maxRepeat || hi > maxRepeat {
		return nil, p.errorAt(qpos, "number too big in {} quantifier")
	}
	if hi != unbounded && hi < lo {
		return nil, p.errorAt(qpos, "numbers out of order in {} quantifier")
	}
	text := p.src[qpos : qpos+width]
	p.pos += width
	greedy := true
	if !p.eof() {
		switch p.src[p.pos] {
		case '?':
			greedy = false
			p.pos++
		case '+':
			return nil, unsupported("possessive quantifier " + text + "+")
		}
	}
	p.skipExtended()
	if _, _, _, again := p.scanQuantifier(); again {
		return nil, p.errorAt(p.pos, "quantifier does not follow a repeatable item")
	}
	return &node{kind: kindRepeat, subs: []*node{atom}, min: lo, max: hi, greedy: greedy}, nil
}

// atom parses one item that a quantifier can follow. It returns nil for an
// item that matches nothing at all, such as a comment.
func (p *parser) atom() (*node, error) {
	start := p.pos
	r, err := p.next()
	if err != nil {
		return nil, err
	}
	switch r {
	case '(':
		return p.group(start)
	case '[':
		return p.class(start)
	case '.':
		return &node{kind: kindAny, dotAll: p.cfg.has(DotAll)}, nil
	case '^':
		return &node{kind: kindAssert, assert: assertBeginText}, nil
	case '$':
		return &node{kind: kindAssert, assert: assertEndText}, nil
	case '\\':
		return p.escape(start)
	case '*', '+', '?':
		return nil, p.errorAt(start, "quantifier does not follow a repeatable item")
	case '{':
		p.pos = start
		if _, _, _, ok := p.scanBraces(); ok {
			return nil, p.errorAt(start, "quantifier does not follow a repeatable item")
		}
		p.pos++
	}
	return p.literal(r), nil
}

func (p *parser) literal(r rune) *node {
	return &node{kind: kindLiteral, runes: []rune{r}, fold: p.cfg.has(IgnoreCase)}
}

// group parses everything after an opening parenthesis at start.
func (p *parser) group(start int) (*node, error) {
	capture, name := true, ""
	switch {
	case p.lookingAt("?"):
		var err error
		var done bool
		capture, name, done, err = p.groupPrefix(start)
		if err != nil || done {
			return nil, err
		}
	case p.lookingAt("*") && p.pos+1 < len(p.src) && (isUpperASCII(p.src[p.pos+1]) || p.src[p.pos+1] == ':'):
		return nil, unsupported("backtracking control verb (" + upToParen(p.src[p.pos:]) + ")")
	}

	index := 0
	if capture {
		index = len(p.names)
		p.names = append(p.names, name)
	}
	body, err := p.alternation()
	if err != nil {
		return nil, err
	}
	if p.eof() {
		return nil, p.errorAt(start, "missing closing parenthesis")
	}
	p.pos++ // ')'
	if !capture {
		return body, nil
	}
	return &node{kind: kindGroup, subs: []*node{body}, index: index, name: name}, nil
}

func isUpperASCII(b byte) bool { return b >= 'A' && b <= 'Z' }

// groupPrefix reads what follows "(?" and says whether the group captures
// and under which name. done is true when the group was a comment, which
// leaves nothing to parse.
//
//nolint:gocyclo // one case per group form, each a line or two
func (p *parser) groupPrefix(start int) (capture bool, name string, done bool, err error) {
	p.pos++ // '?'
	at := p.pos
	rest := p.src[p.pos:]
	switch {
	case strings.HasPrefix(rest, ":"):
		p.pos++
		return false, "", false, nil
	case strings.HasPrefix(rest, "#"):
		end := strings.IndexByte(rest, ')')
		if end < 0 {
			return false, "", true, p.errorAt(start, "missing ) after (?# comment")
		}
		p.pos += end + 1
		return false, "", true, nil
	case strings.HasPrefix(rest, "<="):
		return false, "", true, unsupported("lookbehind (?<=...)")
	case strings.HasPrefix(rest, "<!"):
		return false, "", true, unsupported("negative lookbehind (?<!...)")
	case strings.HasPrefix(rest, "="):
		return false, "", true, unsupported("lookahead (?=...)")
	case strings.HasPrefix(rest, "!"):
		return false, "", true, unsupported("negative lookahead (?!...)")
	case strings.HasPrefix(rest, ">"):
		return false, "", true, unsupported("atomic group (?>...)")
	case strings.HasPrefix(rest, "|"):
		return false, "", true, unsupported("branch reset group (?|...)")
	case strings.HasPrefix(rest, "("):
		return false, "", true, unsupported("conditional group (?(...)")
	case strings.HasPrefix(rest, "C"):
		return false, "", true, unsupported("callout (?C...)")
	case strings.HasPrefix(rest, "P="):
		return false, "", true, unsupported("backreference (?P=" + upToParen(rest[2:]) + ")")
	case strings.HasPrefix(rest, "P>"):
		return false, "", true, unsupported("recursion (?P>" + upToParen(rest[2:]) + ")")
	case strings.HasPrefix(rest, "&"):
		return false, "", true, unsupported("recursion (?&" + upToParen(rest[1:]) + ")")
	case strings.HasPrefix(rest, "R"), rest != "" && (rest[0] >= '0' && rest[0] <= '9'),
		len(rest) > 1 && (rest[0] == '+' || rest[0] == '-') && rest[1] >= '0' && rest[1] <= '9':
		return false, "", true, unsupported("recursion (?" + upToParen(rest) + ")")
	case strings.HasPrefix(rest, "P<"):
		p.pos += 2
		name, err = p.groupName('>')
		return true, name, false, err
	case strings.HasPrefix(rest, "<"):
		p.pos++
		name, err = p.groupName('>')
		return true, name, false, err
	case strings.HasPrefix(rest, "'"):
		p.pos++
		name, err = p.groupName('\'')
		return true, name, false, err
	}
	if flags, isFlags := inlineFlags(rest); isFlags {
		return false, "", true, unsupported("inline flag group (?" + flags + ")")
	}
	return false, "", true, p.errorAt(at, "unrecognized character after (? or (?-")
}

// inlineFlags recognizes (?flags) and (?flags:...), returning how the
// construct is named in an UnsupportedError.
func inlineFlags(rest string) (string, bool) {
	i := 0
	for i < len(rest) && (strings.IndexByte("imnsxJU^-", rest[i]) >= 0) {
		i++
	}
	if i == 0 || i >= len(rest) {
		return "", false
	}
	switch rest[i] {
	case ')':
		return rest[:i], true
	case ':':
		return rest[:i] + ":...", true
	}
	return "", false
}

// upToParen returns s up to its first ), or all of s.
func upToParen(s string) string {
	if i := strings.IndexByte(s, ')'); i >= 0 {
		return s[:i]
	}
	return s
}

// groupName reads a group name up to its terminator and records it.
func (p *parser) groupName(term byte) (string, error) {
	start := p.pos
	end := strings.IndexByte(p.src[p.pos:], term)
	if end < 0 {
		return "", p.errorAt(start, "syntax error in subpattern name (missing terminator?)")
	}
	name := p.src[start : start+end]
	switch {
	case name == "":
		return "", p.errorAt(start, "subpattern name expected")
	case name[0] >= '0' && name[0] <= '9':
		return "", p.errorAt(start, "subpattern name must start with a non-digit")
	case len(name) > maxNameLen:
		return "", p.errorAt(start, "subpattern name is too long (maximum 32 code units)")
	}
	for _, r := range name {
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return "", p.errorAt(start, "syntax error in subpattern name (missing terminator?)")
		}
	}
	if slices.Contains(p.names, name) {
		return "", p.errorAt(start, "two named subpatterns have the same name")
	}
	p.pos = start + end + 1
	return name, nil
}

// escape parses what follows a backslash outside a class.
//
//nolint:gocyclo // one case per escape letter, each a line or two
func (p *parser) escape(start int) (*node, error) {
	if p.eof() {
		return nil, p.errorAt(start, `\ at end of pattern`)
	}
	r, err := p.next()
	if err != nil {
		return nil, err
	}
	switch r {
	case 'd', 'D', 'w', 'W', 's', 'S', 'h', 'H', 'v', 'V':
		set := escapeClass(r, p.cfg.flavor)
		return &node{kind: kindClass, set: set}, nil
	case 'p', 'P':
		set, err := p.property(start, r == 'P')
		if err != nil {
			return nil, err
		}
		return &node{kind: kindClass, set: set}, nil
	case 'N':
		if p.lookingAt("{") {
			return nil, unsupported(`named character \N{...}`)
		}
		return &node{kind: kindAny}, nil
	case 'b', 'B', 'A', 'z', 'Z', 'G':
		return nil, unsupported(`assertion \` + string(r))
	case 'K':
		return nil, unsupported(`match reset \K`)
	case 'X':
		return nil, unsupported(`extended grapheme cluster \X`)
	case 'R':
		return nil, unsupported(`newline sequence \R`)
	case 'Q':
		return nil, unsupported(`quoted sequence \Q...\E`)
	case 'E':
		return nil, nil // a stray \E is ignored
	case 'k':
		return nil, unsupported(`backreference \k` + p.bracketed())
	case 'g':
		if p.lookingAt("<") || p.lookingAt("'") {
			return nil, unsupported(`recursion \g` + p.bracketed())
		}
		ref := p.bracketed()
		if ref == "" {
			ref = p.digits()
		}
		return nil, unsupported(`backreference \g` + ref)
	case '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return nil, unsupported(`backreference \` + string(r) + p.digits())
	}
	c, err := p.characterEscape(start, r)
	if err != nil {
		return nil, err
	}
	return p.literal(c), nil
}

// bracketed consumes a <...>, '...', or {...} reference and returns it with
// its brackets.
func (p *parser) bracketed() string {
	if p.eof() {
		return ""
	}
	closer := map[byte]byte{'<': '>', '\'': '\'', '{': '}'}[p.src[p.pos]]
	if closer == 0 {
		return ""
	}
	end := strings.IndexByte(p.src[p.pos+1:], closer)
	if end < 0 {
		return ""
	}
	s := p.src[p.pos : p.pos+end+2]
	p.pos += end + 2
	return s
}

func (p *parser) digits() string {
	start := p.pos
	for !p.eof() && p.src[p.pos] >= '0' && p.src[p.pos] <= '9' {
		p.pos++
	}
	return p.src[start:p.pos]
}

// characterEscape decodes an escape that stands for one character, with r
// the rune after the backslash at start. It is shared by classes and the
// rest of the pattern.
//
//nolint:gocyclo // one case per escape letter, each a line or two
func (p *parser) characterEscape(start int, r rune) (rune, error) {
	switch r {
	case 'a':
		return '\a', nil
	case 'e':
		return 0x1b, nil
	case 'f':
		return '\f', nil
	case 'n':
		return '\n', nil
	case 'r':
		return '\r', nil
	case 't':
		return '\t', nil
	case '0':
		return p.number(start, 8, 2, 0)
	case 'o':
		if !p.lookingAt("{") {
			return 0, p.errorAt(start, `missing opening brace after \o`)
		}
		return p.braced(start, 8)
	case 'x':
		if p.lookingAt("{") {
			return p.braced(start, 16)
		}
		return p.number(start, 16, 2, 0)
	case 'u':
		c, err := p.number(start, 16, 4, 4)
		if err != nil {
			return 0, p.errorAt(start, `\u must be followed by four hexadecimal digits`)
		}
		return c, nil
	case 'c':
		if p.eof() {
			return 0, p.errorAt(start, `\c at end of pattern`)
		}
		c := p.src[p.pos]
		if c < 0x20 || c > 0x7e {
			return 0, p.errorAt(start, `\c must be followed by a printable ASCII character`)
		}
		p.pos++
		return unicode.ToUpper(rune(c)) ^ 0x40, nil
	}
	if r < utf8.RuneSelf && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
		return 0, p.errorAt(start, `unrecognized character follows \`)
	}
	return r, nil
}

// number reads up to maxDigits digits in base, requiring at least
// minDigits, and returns the character they name.
func (p *parser) number(start, base, maxDigits, minDigits int) (rune, error) {
	v, n := 0, 0
	for n < maxDigits && !p.eof() {
		d := digitValue(p.src[p.pos])
		if d >= base {
			break
		}
		v = v*base + d
		p.pos++
		n++
	}
	if n < minDigits {
		return 0, p.errorAt(start, "too few digits")
	}
	return rune(v), nil
}

// braced reads {digits} in base and checks the code point it names.
func (p *parser) braced(start, base int) (rune, error) {
	end := strings.IndexByte(p.src[p.pos:], '}')
	if end < 0 {
		return 0, p.errorAt(start, `missing } after \x{ or \o{`)
	}
	text := p.src[p.pos+1 : p.pos+end]
	if text == "" {
		return 0, p.errorAt(start, `missing } after \x{ or \o{`)
	}
	v := 0
	for i := range len(text) {
		d := digitValue(text[i])
		if d >= base {
			return 0, p.errorAt(start, `non-hex character in \x{} (closing brace missing?)`)
		}
		v = v*base + d
		if v > unicode.MaxRune {
			return 0, p.errorAt(start, `character code point value in \x{} or \o{} is too large`)
		}
	}
	if v >= 0xd800 && v <= 0xdfff {
		return 0, p.errorAt(start, "disallowed Unicode code point (>= 0xd800 && <= 0xdfff)")
	}
	p.pos += end + 1
	return rune(v), nil
}

func digitValue(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return 99
}

// property reads the name after \p or \P, where start is the backslash.
func (p *parser) property(start int, negated bool) (runeSet, error) {
	if p.eof() {
		return nil, p.errorAt(start, `malformed \P or \p sequence`)
	}
	var name string
	if p.src[p.pos] == '{' {
		end := strings.IndexByte(p.src[p.pos:], '}')
		if end < 0 {
			return nil, p.errorAt(start, `malformed \P or \p sequence`)
		}
		name = p.src[p.pos+1 : p.pos+end]
		p.pos += end + 1
		if strings.HasPrefix(name, "^") {
			negated = !negated
			name = name[1:]
		}
	} else {
		r, err := p.next()
		if err != nil {
			return nil, err
		}
		name = string(r)
	}
	// Oniguruma leaves a bare property unfolded and folds one in a class
	// along with the rest of the class.
	set, ok := propertyClass(strings.TrimSpace(name), p.cfg.has(IgnoreCase) && p.cfg.flavor == PCRE)
	if !ok {
		return nil, p.errorAt(start, `unknown property after \P or \p`)
	}
	if negated {
		set = set.negate()
	}
	return set, nil
}

// class parses a bracket class whose [ is at start.
func (p *parser) class(start int) (*node, error) {
	if isPosixItem(p.src[start:]) {
		return nil, p.errorAt(start, "POSIX named classes are supported only within a class")
	}
	negated := false
	if p.lookingAt("^") {
		negated = true
		p.pos++
	}
	var folded, kept []runeRange
	for first := true; ; first = false {
		if p.eof() {
			return nil, p.errorAt(start, "missing terminating ] for character class")
		}
		if p.src[p.pos] == ']' && !first {
			p.pos++
			return p.classNode(folded, kept, negated), nil
		}
		var err error
		if folded, kept, err = p.classItem(folded, kept); err != nil {
			return nil, err
		}
	}
}

// classItem reads one item of a bracket class. A literal or a range goes on
// folded, the items PCRE folds under IgnoreCase, and an escape, a property,
// or a POSIX name goes on kept.
func (p *parser) classItem(folded, kept []runeRange) ([]runeRange, []runeRange, error) {
	itemPos := p.pos
	if p.lookingAt("[:") {
		set, ok, err := p.posix()
		if err != nil {
			return nil, nil, err
		}
		if ok {
			return folded, append(kept, set...), nil
		}
	}
	lo, set, err := p.classAtom()
	if err != nil {
		return nil, nil, err
	}
	if set != nil {
		if p.lookingAt("-") && !p.lookingAt("-]") {
			return nil, nil, p.errorAt(itemPos, "invalid range in character class")
		}
		return folded, append(kept, set...), nil
	}
	hi := lo
	if p.lookingAt("-") && !p.lookingAt("-]") && p.pos+1 < len(p.src) {
		p.pos++
		var hiSet runeSet
		hi, hiSet, err = p.classAtom()
		if err != nil {
			return nil, nil, err
		}
		if hiSet != nil {
			return nil, nil, p.errorAt(itemPos, "invalid range in character class")
		}
		if hi < lo {
			return nil, nil, p.errorAt(itemPos, "range out of order in character class")
		}
	}
	return append(folded, runeRange{lo, hi}), kept, nil
}

// classNode builds a class from its items. PCRE folds only literals and
// ranges, and Oniguruma folds every item, escapes, properties, and POSIX
// names included.
func (p *parser) classNode(folded, kept []runeRange, negated bool) *node {
	var set runeSet
	switch {
	case p.cfg.has(IgnoreCase) && p.cfg.flavor == Oniguruma:
		set = union(normalize(folded), normalize(kept)).foldClosure()
	case p.cfg.has(IgnoreCase):
		set = union(normalize(folded).foldClosure(), normalize(kept))
	default:
		set = union(normalize(folded), normalize(kept))
	}
	if negated {
		set = set.negate()
	}
	return &node{kind: kindClass, set: set}
}

// posix reads a [:name:] or [:^name:] item. It returns false, leaving the
// position alone, when the text is not shaped like a POSIX item, so [: is
// read as two literals.
func (p *parser) posix() (runeSet, bool, error) {
	at := p.pos
	end := strings.Index(p.src[p.pos+2:], ":]")
	if end < 0 {
		return nil, false, nil
	}
	name := p.src[p.pos+2 : p.pos+2+end]
	negated := strings.HasPrefix(name, "^")
	name = strings.TrimPrefix(name, "^")
	if name == "" || !allLowerASCII(name) {
		return nil, false, nil
	}
	set, ok := posixClass(name, p.cfg.flavor, p.cfg.has(IgnoreCase))
	if !ok {
		return nil, false, p.errorAt(at, "unknown POSIX class name")
	}
	p.pos += end + 4
	if negated {
		set = set.negate()
	}
	return set, true, nil
}

// isPosixItem reports whether s starts with a whole [:name:] item.
func isPosixItem(s string) bool {
	if !strings.HasPrefix(s, "[:") {
		return false
	}
	end := strings.Index(s[2:], ":]")
	name := ""
	if end >= 0 {
		name = strings.TrimPrefix(s[2:2+end], "^")
	}
	return name != "" && allLowerASCII(name)
}

func allLowerASCII(s string) bool {
	for i := range len(s) {
		if s[i] < 'a' || s[i] > 'z' {
			return false
		}
	}
	return true
}

// classAtom reads one item inside a class. It returns a set for an escape
// such as \d and a single rune otherwise.
func (p *parser) classAtom() (rune, runeSet, error) {
	start := p.pos
	r, err := p.next()
	if err != nil {
		return 0, nil, err
	}
	if r != '\\' {
		return r, nil, nil
	}
	if p.eof() {
		return 0, nil, p.errorAt(start, `\ at end of pattern`)
	}
	r, err = p.next()
	if err != nil {
		return 0, nil, err
	}
	switch r {
	case 'd', 'D', 'w', 'W', 's', 'S', 'h', 'H', 'v', 'V':
		set := escapeClass(r, p.cfg.flavor)
		return 0, set, nil
	case 'p', 'P':
		set, err := p.property(start, r == 'P')
		return 0, set, err
	case 'b':
		return '\b', nil, nil
	case 'N':
		return 0, nil, p.errorAt(start, `\N is not supported in a class`)
	case 'Q':
		return 0, nil, unsupported(`quoted sequence \Q...\E`)
	case '1', '2', '3', '4', '5', '6', '7':
		p.pos--
		c, err := p.number(start, 8, 3, 1)
		return c, nil, err
	}
	c, err := p.characterEscape(start, r)
	return c, nil, err
}
