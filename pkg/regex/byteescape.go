package regex

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// jq's Oniguruma reads \xhh and the octal escapes as bytes of the pattern's
// UTF-8 encoding rather than as code points, so \xc3\xa9 is é and \xe9
// alone is an incomplete character. The functions in this file read those
// escapes under the byteEscapes rule. \x{...} and \o{...} stay code points.

// classPoint is one end of a bracket-class range. multi says that the end
// came from a character of more than one UTF-8 byte, which matters because
// Oniguruma checks a single-byte rune against the single-byte ends of a
// class and a longer rune against the longer ends.
type classPoint struct {
	v     rune
	multi bool
}

// pointRange returns the runes a class range from lo to hi holds. Between
// two single-byte ends it holds only ASCII runes, because a single byte
// from \x80 up stands for no character. Between two multibyte ends it holds
// no ASCII rune, and between one of each it holds every rune from lo to hi.
// It reports false when the range holds nothing.
func pointRange(lo, hi classPoint) (runeRange, bool) {
	a, b := lo.v, hi.v
	if !lo.multi && !hi.multi {
		b = min(b, utf8.RuneSelf-1)
	}
	if lo.multi && hi.multi {
		a = max(a, utf8.RuneSelf)
	}
	b = min(b, unicode.MaxRune)
	return runeRange{a, b}, a <= b
}

// byteEscape reads the byte escape at p.pos, if one is there, and returns
// its value. It leaves the position alone and reports false for any other
// item. A number that names a group or starts with 8 or 9 is no byte
// escape, so neither is a reference. An octal escape past \377 keeps its
// low byte outside a class, as jq's does, and is refused inside one.
func (p *parser) byteEscape(inClass bool) (byte, bool, error) {
	start := p.pos
	rest := p.src[start:]
	if len(rest) < 2 || rest[0] != '\\' {
		return 0, false, nil
	}
	switch c := rest[1]; {
	case c == 'x' && !strings.HasPrefix(rest[2:], "{"):
		p.pos += 2
		v, err := p.number(start, 16, 2, 0)
		return byte(v & 0xff), true, err
	case c == '0':
		p.pos += 2
		v, err := p.number(start, 8, 2, 0)
		return byte(v & 0xff), true, err
	case c >= '1' && c <= '7' && inClass:
		p.pos++
		v, err := p.number(start, 8, 3, 1)
		if err == nil && v > 0xff {
			err = p.errorAt(start, "too big number")
		}
		return byte(v & 0xff), true, err
	case c >= '1' && c <= '7':
		p.pos++
		digits := p.digits()
		if n := parseCount(digits); n < 10 || n < len(p.names) {
			p.pos = start
			return 0, false, nil
		}
		p.pos = start + 1
		v, err := p.number(start, 8, 3, 1)
		return byte(v & 0xff), true, err
	}
	return 0, false, nil
}

// byteCharacter reads the rest of the character whose first byte, first,
// came from the escape at start, and returns its bytes. A character needs
// as many bytes as its first byte says, each from a byte escape of its own.
// Outside a class whitespace and comments may sit between them. A byte
// from \x80 to \xbf cannot start a character outside a class, and a byte
// from \xf5 up is a whole character that no rune encodes.
func (p *parser) byteCharacter(start int, first byte, inClass bool) ([]byte, error) {
	seq := []byte{first}
	want := sequenceLength(first)
	if want == 0 && !inClass {
		return nil, p.errorAt(start, "invalid code point value")
	}
	for len(seq) < want {
		if !inClass {
			p.skipBetweenTokens()
		}
		v, ok, err := p.byteEscape(inClass)
		switch {
		case err != nil:
			return nil, err
		case !ok:
			return nil, p.errorAt(start, "too short multibyte code string")
		case v&0xc0 != 0x80:
			return nil, p.errorAt(start, "invalid code point value")
		}
		seq = append(seq, v)
	}
	return seq, nil
}

// byteAtom reads a run of byte escapes outside a class, from the escape at
// start whose value is first, as one character. Bytes that UTF-8 gives no
// rune for, such as an overlong form or a lone \xff, make an empty class,
// because a jq string never holds them.
func (p *parser) byteAtom(start int, first byte) (*node, error) {
	if first < utf8.RuneSelf {
		return p.literal(rune(first)), nil
	}
	seq, err := p.byteCharacter(start, first, false)
	if err != nil {
		return nil, err
	}
	if r, size := utf8.DecodeRune(seq); size == len(seq) && r != utf8.RuneError {
		return p.literal(r), nil
	}
	return &node{kind: kindClass}, nil
}

// classBytePoint reads a run of byte escapes inside a class, from the
// escape at p.pos, as one range end. A single byte keeps its value, and a
// longer character takes the value its bits spell even when UTF-8 forbids
// that form.
func (p *parser) classBytePoint() (classPoint, bool, error) {
	start := p.pos
	first, ok, err := p.byteEscape(true)
	if !ok || err != nil || sequenceLength(first) <= 1 {
		return classPoint{v: rune(first)}, ok, err
	}
	seq, err := p.byteCharacter(start, first, true)
	if err != nil {
		return classPoint{}, true, err
	}
	return classPoint{v: looseDecode(seq), multi: true}, true, nil
}

// skipBetweenTokens skips what may separate two byte escapes of one
// character outside a class, which is Extended's whitespace and comments
// and (?#...) comments.
func (p *parser) skipBetweenTokens() {
	for {
		p.skipExtended()
		if !p.lookingAt("(?#") {
			return
		}
		end := strings.IndexByte(p.src[p.pos:], ')')
		if end < 0 {
			return
		}
		p.pos += end + 1
	}
}

// sequenceLength returns how many bytes the UTF-8 character that starts
// with b has, as jq's Oniguruma counts them. It returns 0 for a byte that
// only continues a character and 1 for a byte from \xf5 up, which Oniguruma
// takes as a whole character.
func sequenceLength(b byte) int {
	switch {
	case b < 0x80:
		return 1
	case b < 0xc0:
		return 0
	case b < 0xe0:
		return 2
	case b < 0xf0:
		return 3
	case b < 0xf5:
		return 4
	}
	return 1
}

// looseDecode returns the value the bits of a multibyte sequence spell,
// with no check that UTF-8 allows the form.
func looseDecode(seq []byte) rune {
	v := rune(seq[0]) & (0x7f >> len(seq))
	for _, b := range seq[1:] {
		v = v<<6 | rune(b&0x3f)
	}
	return v
}
