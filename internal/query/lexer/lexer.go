package lexer

import (
	"sort"
	"strings"
	"unicode/utf8"
)

// frame is one level of the string/interpolation stack. The bottom frame
// is the top-level expression. A string frame is pushed at each opening
// quote, and an expression frame at each "\(".
type frame struct {
	inString bool
	// depth counts unmatched "(" inside an interpolation expression, so the
	// ")" that closes the interpolation can be told from a nested one.
	depth int
}

// Lexer produces tokens from one query source.
type Lexer struct {
	src     string
	set     TokenSet
	pos     int
	stack   []frame
	pending []Token
	ops     []string
	kw      map[string]bool
	done    bool
}

// New returns a Lexer over src using the dialect's token set.
func New(src string, set TokenSet) *Lexer {
	ops := make([]string, 0, len(set.Operators))
	for _, o := range set.Operators {
		if _, ok := operatorKinds[o]; ok {
			ops = append(ops, o)
		}
	}
	sort.SliceStable(ops, func(i, j int) bool { return len(ops[i]) > len(ops[j]) })
	kw := make(map[string]bool, len(set.Keywords))
	for _, k := range set.Keywords {
		kw[k] = true
	}
	return &Lexer{src: src, set: set, stack: []frame{{}}, ops: ops, kw: kw}
}

// Tokenize lexes all of src, ending the slice with an EOF token.
func Tokenize(src string, set TokenSet) ([]Token, error) {
	l := New(src, set)
	var toks []Token
	for {
		t, err := l.Next()
		if err != nil {
			return nil, err
		}
		toks = append(toks, t)
		if t.Kind == EOF {
			return toks, nil
		}
	}
}

// Next returns the next token. After the first EOF it keeps returning EOF.
func (l *Lexer) Next() (Token, error) {
	if len(l.pending) > 0 {
		t := l.pending[0]
		l.pending = l.pending[1:]
		return t, nil
	}
	if l.done {
		return Token{Kind: EOF, Pos: len(l.src), End: len(l.src)}, nil
	}
	if l.stack[len(l.stack)-1].inString {
		return l.lexStringPart()
	}
	return l.lexExpr()
}

func (l *Lexer) errorf(pos int, msg string) error {
	return &Error{Pos: pos, Msg: msg}
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isIdentPart(c byte) bool { return isIdentStart(c) || isDigit(c) }

// scanIdent reads a possibly namespaced identifier ("a::b") starting at i
// and returns the end offset. The caller has checked isIdentStart.
func (l *Lexer) scanIdent(i int) int {
	for {
		for i < len(l.src) && isIdentPart(l.src[i]) {
			i++
		}
		if i+2 < len(l.src) && l.src[i] == ':' && l.src[i+1] == ':' && isIdentStart(l.src[i+2]) {
			i += 2
			continue
		}
		return i
	}
}

func (l *Lexer) skipSpaceAndComments() {
	for l.pos < len(l.src) {
		switch l.src[l.pos] {
		case ' ', '\t', '\n', '\r':
			l.pos++
		case '#':
			l.skipComment()
		default:
			return
		}
	}
}

// skipComment consumes a "#" comment up to, but not including, the line
// break that ends it.
func (l *Lexer) skipComment() {
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == '\n' {
			return
		}
		if c == '\r' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '\n' {
			return
		}
		if c == '\\' && l.set.CommentContinuation {
			// A backslash takes the next character with it, so an even run of
			// backslashes before the newline does not continue the comment.
			l.pos++
			if l.pos < len(l.src) {
				if l.src[l.pos] == '\r' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '\n' {
					l.pos++
				}
				l.pos++
			}
			continue
		}
		l.pos++
	}
}

func (l *Lexer) tok(kind TokenKind, start, end int) Token {
	return Token{Kind: kind, Text: l.src[start:end], Pos: start, End: end}
}

func (l *Lexer) lexExpr() (Token, error) {
	l.skipSpaceAndComments()
	if l.pos >= len(l.src) {
		l.done = true
		if len(l.stack) > 1 {
			return Token{}, l.errorf(l.pos, "unterminated string interpolation")
		}
		return Token{Kind: EOF, Pos: len(l.src), End: len(l.src)}, nil
	}
	start := l.pos
	c := l.src[start]

	switch {
	case c == '"':
		l.pos++
		l.stack = append(l.stack, frame{inString: true})
		return l.tok(StrStart, start, l.pos), nil
	case c == '.':
		return l.lexDot(), nil
	case isDigit(c):
		return l.lexNumber(), nil
	case isIdentStart(c):
		end := l.scanIdent(start)
		l.pos = end
		kind := Ident
		if l.kw[l.src[start:end]] {
			kind = Keyword
		}
		return l.tok(kind, start, end), nil
	case c == '$':
		return l.lexVariable()
	case c == '@':
		return l.lexFormat()
	}

	return l.lexOperator(start)
}

func (l *Lexer) lexVariable() (Token, error) {
	start := l.pos
	if start+1 >= len(l.src) || !isIdentStart(l.src[start+1]) {
		return Token{}, l.errorf(start, "unexpected character '$'")
	}
	end := l.scanIdent(start + 1)
	l.pos = end
	kind := Variable
	if l.set.LocToken && l.src[start:end] == "$__loc__" {
		kind = Loc
	}
	return l.tok(kind, start, end), nil
}

func (l *Lexer) lexFormat() (Token, error) {
	start := l.pos
	end := start + 1
	for end < len(l.src) && isIdentPart(l.src[end]) {
		end++
	}
	if end == start+1 {
		return Token{}, l.errorf(start, "unexpected character '@'")
	}
	l.pos = end
	return l.tok(Format, start, end), nil
}

func (l *Lexer) lexOperator(start int) (Token, error) {
	c := l.src[start]
	// Parentheses are tracked so the ")" that ends an interpolation can be
	// told apart from one that closes a group inside it.
	top := &l.stack[len(l.stack)-1]
	if c == ')' && len(l.stack) > 1 && top.depth == 0 {
		l.pos++
		l.stack = l.stack[:len(l.stack)-1]
		return l.tok(InterpEnd, start, l.pos), nil
	}

	for _, o := range l.ops {
		if strings.HasPrefix(l.src[start:], o) {
			l.pos += len(o)
			switch o {
			case "(":
				top.depth++
			case ")":
				if top.depth > 0 {
					top.depth--
				}
			case "*", "*=":
				l.flags(l.set.MergeFlags)
			case "=", "|=":
				l.flags(l.set.AssignFlags)
			}
			return l.tok(operatorKinds[o], start, l.pos), nil
		}
	}
	r, _ := utf8.DecodeRuneInString(l.src[start:])
	return Token{}, l.errorf(start, "unexpected character '"+string(r)+"'")
}

// flags consumes the characters of set that follow an operator, which
// belong to its token.
func (l *Lexer) flags(set string) {
	for l.pos < len(l.src) && set != "" && strings.IndexByte(set, l.src[l.pos]) >= 0 {
		l.pos++
	}
}

func (l *Lexer) lexDot() Token {
	start := l.pos
	rest := l.src[start:]
	switch {
	case l.set.TripleDot && strings.HasPrefix(rest, "..."):
		l.pos += 3
		return l.tok(DotDotDot, start, l.pos)
	case strings.HasPrefix(rest, ".."):
		l.pos += 2
		return l.tok(DotDot, start, l.pos)
	case len(rest) > 1 && isDigit(rest[1]):
		return l.lexNumber()
	case len(rest) > 1 && isIdentStart(rest[1]):
		end := start + 1
		for end < len(l.src) && isIdentPart(l.src[end]) {
			end++
		}
		l.pos = end
		return l.tok(Field, start, end)
	}
	l.pos++
	return l.tok(Dot, start, l.pos)
}

// lexNumber reads digits, an optional fraction, and an optional exponent.
// A leading "." is allowed, so ".5" is a number.
func (l *Lexer) lexNumber() Token {
	start := l.pos
	if end := l.scanHex(start); end > start {
		l.pos = end
		return l.tok(Number, start, end)
	}
	i := l.scanDigits(start)
	if i < len(l.src) && l.src[i] == '.' {
		i = l.scanDigits(i + 1)
	}
	if i < len(l.src) && (l.src[i] == 'e' || l.src[i] == 'E') {
		j := i + 1
		if j < len(l.src) && (l.src[j] == '+' || l.src[j] == '-') {
			j++
		}
		if end := l.scanDigits(j); end > j {
			i = end
		}
	}
	l.pos = i
	return l.tok(Number, start, i)
}

// scanHex returns the offset after a hex number, as 0x10, that starts at i,
// or i when the set has no hex numbers or one does not start there.
func (l *Lexer) scanHex(i int) int {
	if !l.set.HexNumbers || i+2 >= len(l.src) || l.src[i] != '0' || (l.src[i+1] != 'x' && l.src[i+1] != 'X') {
		return i
	}
	j := i + 2
	for j < len(l.src) && isHexDigit(l.src[j]) {
		j++
	}
	if j == i+2 {
		return i
	}
	return j
}

func isHexDigit(c byte) bool {
	return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// scanDigits returns the offset after the run of digits starting at i.
func (l *Lexer) scanDigits(i int) int {
	for i < len(l.src) && isDigit(l.src[i]) {
		i++
	}
	return i
}

// lexStringPart reads string content up to the closing quote or the next
// "\(". It returns the StrLit first, when the run is non-empty, and queues
// the token that ended the run behind it.
func (l *Lexer) lexStringPart() (Token, error) {
	litStart := l.pos
	var sb strings.Builder
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch c {
		case '"':
			lit := l.takeLit(&sb, litStart)
			end := l.tok(StrEnd, l.pos, l.pos+1)
			l.pos++
			l.stack = l.stack[:len(l.stack)-1]
			return l.emit(lit, end), nil
		case '\\':
			if l.pos+1 < len(l.src) && l.src[l.pos+1] == '(' {
				lit := l.takeLit(&sb, litStart)
				interp := l.tok(InterpStart, l.pos, l.pos+2)
				l.pos += 2
				l.stack = append(l.stack, frame{})
				return l.emit(lit, interp), nil
			}
			if err := l.readEscape(&sb); err != nil {
				return Token{}, err
			}
		default:
			sb.WriteByte(c)
			l.pos++
		}
	}
	l.done = true
	return Token{}, l.errorf(litStart-1, "unterminated string")
}

// takeLit returns the literal token for the run that began at start, or nil
// when the run is empty.
func (l *Lexer) takeLit(sb *strings.Builder, start int) *Token {
	if l.pos == start {
		return nil
	}
	t := Token{Kind: StrLit, Text: sb.String(), Pos: start, End: l.pos}
	sb.Reset()
	return &t
}

// emit returns lit if present, queueing next behind it; otherwise next.
func (l *Lexer) emit(lit *Token, next Token) Token {
	if lit == nil {
		return next
	}
	l.pending = append(l.pending, next)
	return *lit
}

// readEscape decodes the escape at l.pos (a backslash) into sb.
func (l *Lexer) readEscape(sb *strings.Builder) error {
	at := l.pos
	if at+1 >= len(l.src) {
		l.pos = len(l.src)
		l.done = true
		return l.errorf(at, "unterminated string")
	}
	c := l.src[at+1]
	l.pos += 2
	lenient := l.set.LenientEscapes
	switch {
	case c == 'u' && lenient:
		return l.keepEscape(sb, at, "")
	case c == 'u':
		return l.readUnicode(sb, at)
	}
	if r, ok := simpleEscape(c, lenient); ok {
		sb.WriteByte(r)
		return nil
	}
	return l.keepEscape(sb, at, "invalid escape in string")
}

// simpleEscape is the byte a one-character escape stands for. A lenient
// set has \a and \v and lacks \/, which yq reads as it is written.
func simpleEscape(c byte, lenient bool) (byte, bool) {
	switch c {
	case '"', '\\':
		return c, true
	case '/':
		return c, !lenient
	case 'b':
		return '\b', true
	case 'f':
		return '\f', true
	case 'n':
		return '\n', true
	case 'r':
		return '\r', true
	case 't':
		return '\t', true
	case 'a':
		return '\a', lenient
	case 'v':
		return '\v', lenient
	default:
		return 0, false
	}
}

// readUnicode decodes the \u escape at at into sb.
func (l *Lexer) readUnicode(sb *strings.Builder, at int) error {
	r, ok := l.hex4()
	if !ok {
		return l.errorf(at, "invalid \\u escape in string")
	}
	sb.WriteRune(l.joinSurrogates(r))
	return nil
}

// keepEscape handles an escape at at that the string syntax does not
// have. A lenient set keeps its backslash and goes on from the character
// after it, and any other fails the lex with msg.
func (l *Lexer) keepEscape(sb *strings.Builder, at int, msg string) error {
	if !l.set.LenientEscapes {
		return l.errorf(at, msg)
	}
	sb.WriteByte('\\')
	l.pos = at + 1
	return nil
}

// joinSurrogates combines a high surrogate with a following \uXXXX low
// surrogate. A lone surrogate becomes the replacement character.
func (l *Lexer) joinSurrogates(r rune) rune {
	switch {
	case r >= 0xD800 && r < 0xDC00:
		if strings.HasPrefix(l.src[l.pos:], "\\u") {
			save := l.pos
			l.pos += 2
			if lo, ok := l.hex4(); ok && lo >= 0xDC00 && lo < 0xE000 {
				return 0x10000 + (r-0xD800)<<10 + (lo - 0xDC00)
			}
			l.pos = save
		}
		return utf8.RuneError
	case r >= 0xDC00 && r < 0xE000:
		return utf8.RuneError
	}
	return r
}

// hex4 reads four hex digits at l.pos.
func (l *Lexer) hex4() (rune, bool) {
	if l.pos+4 > len(l.src) {
		return 0, false
	}
	var r rune
	for _, c := range []byte(l.src[l.pos : l.pos+4]) {
		switch {
		case c >= '0' && c <= '9':
			r = r<<4 | rune(c-'0')
		case c >= 'a' && c <= 'f':
			r = r<<4 | rune(c-'a'+10)
		case c >= 'A' && c <= 'F':
			r = r<<4 | rune(c-'A'+10)
		default:
			return 0, false
		}
	}
	l.pos += 4
	return r, true
}
