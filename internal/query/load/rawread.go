package load

import (
	"bufio"
	"errors"
	"io"
	"unicode/utf8"
)

// readRawLine returns the next line split on \n alone, without the \n, so
// a \r stays in the text. A final line with no newline is still a line,
// and io.EOF follows it.
func readRawLine(br *bufio.Reader) (string, error) {
	line, err := br.ReadBytes('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	if len(line) == 0 && errors.Is(err, io.EOF) {
		return "", io.EOF
	}
	if n := len(line); n > 0 && line[n-1] == '\n' {
		line = line[:n-1]
	}
	return toValidUTF8(line), nil
}

// toValidUTF8 replaces each invalid byte with U+FFFD, as jq does.
func toValidUTF8(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	out := make([]byte, 0, len(b)+8)
	for i := 0; i < len(b); {
		r, size := utf8.DecodeRune(b[i:])
		if r == utf8.RuneError && size == 1 {
			out = utf8.AppendRune(out, utf8.RuneError)
		} else {
			out = append(out, b[i:i+size]...)
		}
		i += size
	}
	return string(out)
}
