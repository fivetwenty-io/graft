package load

import (
	"bufio"
	"bytes"
)

// detect decides between JSON and YAML from the first token and never
// retries. It peeks without consuming, and it waits only for as much of the
// first line as it needs.
func detect(br *bufio.Reader) Format {
	prefix := peekFirstLine(br)
	if bytes.HasPrefix(prefix, []byte{0xFF, 0xFE}) || bytes.HasPrefix(prefix, []byte{0xFE, 0xFF}) {
		return FormatYAML
	}
	prefix = bytes.TrimPrefix(prefix, []byte{0xEF, 0xBB, 0xBF})
	prefix = bytes.TrimLeft(prefix, " \t\r\n\x1e")
	if len(prefix) == 0 {
		return FormatYAML
	}
	line := prefix
	if i := bytes.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	switch c := line[0]; c {
	case '{', '[', '"':
		return FormatJSON
	case '%', '#', '&', '*', '!', '|', '>', '\'', '?':
		return FormatYAML
	case '-':
		if len(line) == 1 || line[1] == ' ' || line[1] == '\t' {
			return FormatYAML
		}
	}
	if bytes.Contains(line, []byte(": ")) || bytes.HasSuffix(bytes.TrimRight(line, " \t\r"), []byte(":")) {
		return FormatYAML
	}
	return scalarFormat(prefix)
}

// peekFirstLine returns what the reader has buffered, reading on until the
// prefix holds a whole non-blank line, the buffer fills, or the input ends.
func peekFirstLine(br *bufio.Reader) []byte {
	if _, err := br.Peek(1); err != nil {
		return nil
	}
	for {
		b, _ := br.Peek(br.Buffered())
		trimmed := bytes.TrimLeft(b, " \t\r\n\x1e")
		if bytes.IndexByte(trimmed, '\n') >= 0 || len(b) >= br.Size() {
			return b
		}
		more, err := br.Peek(len(b) + 1)
		if err != nil {
			return more
		}
	}
}

// scalarFormat applies the bare-scalar rule. A complete JSON number,
// true, false, or null that ends the input, ends a line, or sits before
// another JSON value means JSON, and anything else means YAML.
func scalarFormat(prefix []byte) Format {
	end := 0
	for end < len(prefix) && isTokenByte(prefix[end]) {
		end++
	}
	tok := string(prefix[:end])
	if !isJSONLiteral(tok) {
		return FormatYAML
	}
	rest := bytes.TrimLeft(prefix[end:], " \t")
	if len(rest) == 0 {
		return FormatJSON
	}
	switch rest[0] {
	case '\n', '\r', '{', '[', '"', '-', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
		return FormatJSON
	}
	for _, w := range []string{"true", "false", "null"} {
		if bytes.HasPrefix(rest, []byte(w)) {
			return FormatJSON
		}
	}
	return FormatYAML
}

func isTokenByte(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '+' || c == '-' || c == '.'
}

func isJSONLiteral(tok string) bool {
	return tok == "true" || tok == "false" || tok == "null" || isJSONNumber(tok)
}

// isJSONNumber reports whether s is a number in JSON's grammar.
func isJSONNumber(s string) bool {
	i := 0
	if i < len(s) && s[i] == '-' {
		i++
	}
	switch {
	case i < len(s) && s[i] == '0':
		i++
	case i < len(s) && s[i] >= '1' && s[i] <= '9':
		i = skipDigits(s, i)
	default:
		return false
	}
	if i < len(s) && s[i] == '.' {
		j := skipDigits(s, i+1)
		if j == i+1 {
			return false
		}
		i = j
	}
	_, end, ok := scanExponent(s, i)
	return ok && end == len(s)
}

// scanExponent reads an optional exponent that starts at s[i]. It returns
// the exponent's text, the index after it, and false when an e is not
// followed by digits.
func scanExponent(s string, i int) (string, int, bool) {
	if i >= len(s) || (s[i] != 'e' && s[i] != 'E') {
		return "", i, true
	}
	j := i + 1
	if j < len(s) && (s[j] == '+' || s[j] == '-') {
		j++
	}
	k := skipDigits(s, j)
	if k == j {
		return "", i, false
	}
	return s[i:k], k, true
}

func skipDigits(s string, i int) int {
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return i
}
