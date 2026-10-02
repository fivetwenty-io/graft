package yamlnode

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/token"

	"github.com/fivetwenty-io/graft/internal/yamlprep"
)

// ParseError is a YAML syntax error in yaml.v3's "yaml: line N: message"
// form. Message is goccy's text, so only the prefix and the line number
// match what spruce prints.
type ParseError struct {
	Line    int
	Message string
}

func (e *ParseError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("yaml: line %d: %s", e.Line, e.Message)
	}
	return "yaml: " + e.Message
}

// Parse reads a YAML stream and returns one DocumentNode per document,
// the way yaml.v3's Decoder does. An empty stream, or one holding only
// comments, yields no documents. CRLF line breaks become LF before
// anything lexes, because goccy counts a CRLF comment line as two lines.
func Parse(data []byte) ([]*Node, error) {
	src, err := decodeEncoding(data)
	if err != nil {
		return nil, err
	}
	if err := checkUTF8(src); err != nil {
		return nil, err
	}
	src = bytes.ReplaceAll(src, []byte("\r\n"), []byte("\n"))
	prepared, nulled := yamlprep.Prepare(src)
	text, addedNewline := string(prepared), false
	if text != "" && !strings.HasSuffix(text, "\n") {
		text, addedNewline = text+"\n", true
	}
	b := newBuilder(nulled)

	chunks := splitDocuments(text)
	docs := make([]*Node, 0, len(chunks))
	endsInBlock := false
	for i, c := range chunks {
		c.unterminated = i == len(chunks)-1 && addedNewline
		c.text, endsInBlock = normalizeChunkEnd(c.text, c.unterminated)
		file, err := parser.ParseBytes([]byte(c.text), 0, parser.AllowDuplicateMapKey())
		if err != nil {
			return nil, toParseError(err)
		}
		doc, err := b.document(file, c)
		if err != nil {
			return nil, err
		}
		docs = append(docs, doc)
	}

	// A clip or keep scalar that ran to the end of a stream with no final
	// line break gained one above, so take it back.
	if endsInBlock && addedNewline && b.lastBlock != nil && !b.lastBlockStrips {
		b.lastBlock.Value = strings.TrimSuffix(b.lastBlock.Value, "\n")
	}
	return docs, nil
}

// normalizeChunkEnd works around goccy's handling of a block scalar that
// runs to the end of what it parses. Parse hands goccy one document at a
// time, so every chunk ends a stream as far as goccy can tell. With an
// indentation indicator, goccy rejects trailing blank lines, which carry
// no content under clip or strip chomping, so we drop them. We leave the
// final chunk alone when Parse added the stream's last line break,
// because that chunk then ends on content. Without that line break, goccy
// drops trailing spaces and stops folding, which is why Parse adds it.
// normalizeChunkEnd reports whether the chunk ends inside a block scalar.
func normalizeChunkEnd(text string, addedNewline bool) (string, bool) {
	header, endsInBlock := finalBlockScalarHeader(text)
	if endsInBlock && !addedNewline && strings.ContainsAny(header, "123456789") && !strings.Contains(header, "+") {
		text = dropTrailingBlankLines(text)
	}
	return text, endsInBlock
}

// finalBlockScalarHeader reports whether the last token of src is the
// body of a literal or folded block scalar, so the stream ends inside it,
// and returns that scalar's header, such as "|2-". goccy's lexer marks
// the body Invalid when it is about to reject it, so both types count.
func finalBlockScalarHeader(src string) (string, bool) {
	toks := lexer.Tokenize(src)
	n := len(toks)
	if n < 2 || (toks[n-1].Type != token.StringType && toks[n-1].Type != token.InvalidType) {
		return "", false
	}
	for k := n - 2; k >= 0; k-- {
		switch toks[k].Type {
		case token.CommentType:
			continue
		case token.LiteralType, token.FoldedType:
			return toks[k].Value, true
		default:
			return "", false
		}
	}
	return "", false
}

// dropTrailingBlankLines removes the empty and whitespace-only lines at the
// end of src and leaves exactly one final line break.
func dropTrailingBlankLines(src string) string {
	body := strings.TrimRight(src, "\n")
	for {
		i := strings.LastIndexByte(body, '\n')
		if i < 0 || strings.TrimSpace(body[i+1:]) != "" {
			break
		}
		body = body[:i]
	}
	return body + "\n"
}

// decodeEncoding strips a UTF-8 byte order mark and transcodes UTF-16
// input that starts with a byte order mark, as libyaml does.
func decodeEncoding(data []byte) ([]byte, error) {
	switch {
	case bytes.HasPrefix(data, []byte{0xEF, 0xBB, 0xBF}):
		return data[3:], nil
	case bytes.HasPrefix(data, []byte{0xFF, 0xFE}):
		return decodeUTF16(data[2:], false)
	case bytes.HasPrefix(data, []byte{0xFE, 0xFF}):
		return decodeUTF16(data[2:], true)
	}
	return data, nil
}

func decodeUTF16(data []byte, bigEndian bool) ([]byte, error) {
	if len(data)%2 != 0 {
		return nil, &ParseError{Message: "incomplete UTF-16 character sequence"}
	}
	units := make([]uint16, len(data)/2)
	for i := range units {
		hi, lo := data[2*i+1], data[2*i]
		if bigEndian {
			hi, lo = lo, hi
		}
		units[i] = uint16(hi)<<8 | uint16(lo)
	}
	return []byte(string(utf16.Decode(units))), nil
}

// checkUTF8 rejects input that is not UTF-8, with the message libyaml's
// reader gives for the first bad sequence (readerc.go). libyaml reports
// no line for these errors, so neither do we.
func checkUTF8(src []byte) error {
	if utf8.Valid(src) {
		return nil
	}
	for i := 0; i < len(src); {
		width := utf8Width(src[i])
		if width == 0 {
			return &ParseError{Message: "invalid leading UTF-8 octet"}
		}
		if i+width > len(src) {
			return &ParseError{Message: "incomplete UTF-8 octet sequence"}
		}
		for k := 1; k < width; k++ {
			if src[i+k]&0xC0 != 0x80 {
				return &ParseError{Message: "invalid trailing UTF-8 octet"}
			}
		}
		if r, size := utf8.DecodeRune(src[i : i+width]); r == utf8.RuneError && size <= 1 {
			return &ParseError{Message: invalidSequence(src[i : i+width])}
		}
		i += width
	}
	return nil
}

// utf8Width returns the sequence length a leading octet announces, or 0
// when the octet cannot start a sequence.
func utf8Width(octet byte) int {
	switch {
	case octet&0x80 == 0x00:
		return 1
	case octet&0xE0 == 0xC0:
		return 2
	case octet&0xF0 == 0xE0:
		return 3
	case octet&0xF8 == 0xF0:
		return 4
	default:
		return 0
	}
}

// invalidSequence names what is wrong with a well-formed sequence that
// still decodes to no rune. Either it is longer than its value needs, or
// its value is a surrogate or lies past U+10FFFF.
func invalidSequence(seq []byte) string {
	value := rune(seq[0] & (0x7F >> len(seq)))
	for _, octet := range seq[1:] {
		value = value<<6 | rune(octet&0x3F)
	}
	if minimum := [...]rune{2: 0x80, 3: 0x800, 4: 0x10000}[len(seq)]; value < minimum {
		return "invalid length of a UTF-8 sequence"
	}
	return "invalid Unicode character"
}

func toParseError(err error) error {
	var se *yaml.SyntaxError
	if errors.As(err, &se) && se.Token != nil {
		return &ParseError{Line: se.Token.Position.Line, Message: se.Message}
	}
	return &ParseError{Message: err.Error()}
}
