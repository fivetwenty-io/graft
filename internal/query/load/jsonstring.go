package load

import (
	"unicode/utf16"
	"unicode/utf8"
)

// decodeString resolves the escapes in the text between a string's quotes,
// joining a surrogate pair, turning a lone low surrogate or a bad UTF-8
// byte into U+FFFD, and returning jq's message when the text is bad. Both
// readers decode a string this way, after they have found its closing quote.
func decodeString(raw []byte) (string, string) {
	out := make([]byte, 0, len(raw))
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		switch {
		case c < 0x20:
			return "", "Invalid string: control characters from U+0000 through U+001F must be escaped"
		case c != '\\':
			out = append(out, c)
			continue
		}
		i++
		var esc byte
		switch raw[i] {
		case '"', '\\', '/':
			esc = raw[i]
		case 'b':
			esc = '\b'
		case 'f':
			esc = '\f'
		case 'n':
			esc = '\n'
		case 'r':
			esc = '\r'
		case 't':
			esc = '\t'
		case 'u':
			rn, n, msg := unicodeAt(raw[i+1:])
			if msg != "" {
				return "", msg
			}
			i += n
			out = utf8.AppendRune(out, rn)
			continue
		default:
			return "", "Invalid escape"
		}
		out = append(out, esc)
	}
	return toValidUTF8(out), ""
}

// unicodeAt reads the four hex digits after \u in rest, which ends where
// the string does. A high surrogate must be followed by a \uXXXX escape of
// a low surrogate, which joins it. It returns the rune and the bytes of
// rest it used, or jq's message.
func unicodeAt(rest []byte) (rune, int, string) {
	if len(rest) < 4 {
		return 0, 0, "Invalid \\uXXXX escape"
	}
	hi, ok := parseHex4(rest)
	if !ok {
		return 0, 0, "Invalid characters in \\uXXXX escape"
	}
	switch {
	case utf16.IsSurrogate(hi) && hi < 0xDC00:
		const pairErr = "Invalid \\uXXXX\\uXXXX surrogate pair escape"
		if len(rest) < 10 || rest[4] != '\\' || rest[5] != 'u' {
			return 0, 0, pairErr
		}
		lo, ok := parseHex4(rest[6:])
		if !ok || lo < 0xDC00 || lo > 0xDFFF {
			return 0, 0, pairErr
		}
		return utf16.DecodeRune(hi, lo), 10, ""
	case utf16.IsSurrogate(hi):
		return utf8.RuneError, 4, ""
	}
	return hi, 4, ""
}
