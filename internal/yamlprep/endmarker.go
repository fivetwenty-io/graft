package yamlprep

import (
	"bytes"
	"sort"
	"strings"

	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/token"
)

// QuoteEndMarkerScalars single-quotes each plain scalar at column 1 that
// starts with "..." and a character other than a blank, a line break, a
// ".", or a "#", so goccy reads it as the scalar it is.
//
// YAML ends a document at a "..." only when a blank, a line break, or
// the end of the input follows it, and spruce's yaml.v3 reads "...x: 2"
// as the key "...x". goccy v1.19.2 ends the document at the three dots
// whatever follows them, and then drops the key, renames it, or fails on
// it. A "...#" stays as goccy reads it, which is the end of a document.
//
// The rewrite is token-driven. It quotes a line only where goccy's lexer
// puts a document end at column 1 outside every flow collection, so a
// line inside a quoted string or a block scalar never changes. The
// scalar runs to the ": " that makes it a key, and the rewrite quotes the
// key alone. A scalar that is no key runs to a " #" or the end of the
// line, and the rewrite quotes it only when no later line before the
// next marker line could continue it. Any "'" inside the scalar is
// doubled. Only quotes are added, so every line keeps its number, and
// goccy reads the tokens after the scalar a column or more to the right.
//
// goccy counts a CRLF comment line as two lines, so the tokens come from
// a copy with LF line breaks, and each span is mapped back onto data.
// Input with no such line comes back as the original slice.
func QuoteEndMarkerScalars(data []byte) []byte {
	if !hasEndMarkerScalar(data) {
		return data
	}
	src := string(data)
	lf, removed := withoutCR(src)
	spans := endMarkerSpans(lf)
	if len(spans) == 0 {
		return data
	}

	out := make([]byte, 0, len(data)+4*len(spans))
	prev := 0
	for _, sp := range spans {
		start := sp.start + sort.SearchInts(removed, sp.start)
		end := sp.end + sort.SearchInts(removed, sp.end)
		out = append(out, data[prev:start]...)
		out = append(out, '\'')
		out = append(out, bytes.ReplaceAll(data[start:end], []byte("'"), []byte("''"))...)
		out = append(out, '\'')
		prev = end
	}
	return append(out, data[prev:]...)
}

// hasEndMarkerScalar reports whether a line of data starts with "..."
// and a character that isEndMarkerScalar accepts.
func hasEndMarkerScalar(data []byte) bool {
	for len(data) > 0 {
		if isEndMarkerScalar(data) {
			return true
		}
		nl := bytes.IndexByte(data, '\n')
		if nl < 0 {
			return false
		}
		data = data[nl+1:]
	}
	return false
}

// isEndMarkerScalar reports whether line starts with "..." and a fourth
// byte that is not a blank, a line break, a ".", or a "#". goccy reads
// those three dots as the end of a document, and YAML reads them as the
// start of a plain scalar. Four dots are text to goccy too, and a "#"
// right after the dots stays the end of a document.
func isEndMarkerScalar(line []byte) bool {
	return len(line) > 3 && line[0] == '.' && line[1] == '.' && line[2] == '.' &&
		bytes.IndexByte([]byte(" \t\r\n.#"), line[3]) < 0
}

// endMarkerSpans returns the byte spans of src to quote, in order. Each
// one is the scalar at the start of a line where goccy's lexer puts a
// document end at column 1 outside every flow collection.
func endMarkerSpans(src string) []span {
	idx := newByteIndex(src)
	var spans []span
	flow := 0
	for _, tk := range lexer.Tokenize(src) {
		switch tk.Type {
		case token.SequenceStartType, token.MappingStartType:
			flow++
		case token.SequenceEndType, token.MappingEndType:
			flow = max(flow-1, 0)
		case token.DocumentEndType:
			if flow > 0 || tk.Position.Column != 1 {
				continue
			}
			if sp, ok := endMarkerSpan(src, idx, tk.Position.Line); ok {
				spans = append(spans, sp)
			}
		default:
			// Other tokens leave the flow level as it stands.
		}
	}
	return spans
}

// endMarkerSpan returns the span of the plain scalar that starts line,
// numbered from 1, when the line starts with one goccy misreads and the
// scalar's end is certain.
func endMarkerSpan(src string, idx byteIndex, line int) (span, bool) {
	if line < 1 || line > len(idx.lines) {
		return span{}, false
	}
	start := idx.lines[line-1]
	rest := src[start:]
	text, next, _ := strings.Cut(rest, "\n")
	if !isEndMarkerScalar([]byte(text)) {
		return span{}, false
	}
	n, key := plainScalarEnd(text)
	if !key && !endsScalar(next) {
		return span{}, false
	}
	return span{start, start + n}, true
}

// plainScalarEnd returns the length of the plain scalar that starts
// text, a line in block context, with the blanks after it trimmed, and
// whether a ":" after it makes it a key. A ":" ends the scalar when a
// blank or the end of the line follows it, and a "#" ends it when a
// blank comes before it.
func plainScalarEnd(text string) (int, bool) {
	end, key := len(text), false
	for i := 3; i < len(text); i++ {
		if text[i] == ':' && (i+1 == len(text) || isBlank(text[i+1])) {
			end, key = i, true
			break
		}
		if text[i] == '#' && isBlank(text[i-1]) {
			end = i
			break
		}
	}
	return len(strings.TrimRight(text[:end], " \t\r")), key
}

// endsScalar reports whether no line of rest, the text after a line
// holding a plain scalar that is no key, continues that scalar. Lines
// that are blank or hold only a comment are skipped, and a marker line
// or the end of the input ends the scalar.
func endsScalar(rest string) bool {
	for rest != "" {
		var text string
		text, rest, _ = strings.Cut(rest, "\n")
		switch content := strings.Trim(text, " \t\r"); {
		case content == "" || content[0] == '#':
		case strings.HasPrefix(text, "---") || strings.HasPrefix(text, "..."):
			return len(text) == 3 || isBlank(text[3])
		default:
			return false
		}
	}
	return true
}

// isBlank reports whether c is a space, a tab, or a carriage return.
func isBlank(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r'
}
