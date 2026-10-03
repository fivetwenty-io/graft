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
// comments, yields no documents. CRLF and lone CR line breaks become LF
// before anything lexes. goccy counts a CRLF comment line as two lines,
// and libyaml reads a lone CR as a line break, so the document splitter
// has to count it the way goccy's lexer does. Nesting deeper than
// yaml.v3 allows fails before anything tokenizes the whole stream. A
// panic inside goccy comes back as a ParseError.
func Parse(data []byte) (docs []*Node, err error) {
	defer func() {
		if r := recover(); r != nil {
			docs, err = nil, &ParseError{Message: fmt.Sprintf("internal parser error: %v", r)}
		}
	}()
	src, probed, err := readStream(data)
	if err != nil {
		return nil, err
	}
	prepared, nulled := yamlprep.Prepare(normalizeLineBreaks(src))
	text, addedNewline := streamText(prepared)
	b := newBuilder(nulled)

	// The probe's text differs from Parse's only where the bare-dash or
	// placeholder rewrite changed something, and then it tokenizes again.
	toks := probed.toks
	if toks == nil || probed.text != text {
		toks = lexer.Tokenize(text)
	}
	if err := checkDepth(toks); err != nil {
		return nil, err
	}
	chunks := splitTokens(text, toks)
	docs = make([]*Node, 0, len(chunks))
	ends := make([]docSlots, 0, len(chunks))
	endsInBlock := false
	for i, c := range chunks {
		c.unterminated = i == len(chunks)-1 && addedNewline
		if c.bareLine > 0 {
			return nil, documentStartError(c.bareLine)
		}
		doc, d, inBlock, err := b.parseChunk(c)
		if err != nil {
			return nil, err
		}
		ends = append(ends, d)
		if doc != nil {
			docs, endsInBlock = append(docs, doc), inBlock
		}
	}
	setDocumentComments(docs, ends)

	// A clip or keep scalar that ran to the end of a stream with no final
	// line break gained one above, so take it back.
	if endsInBlock && addedNewline && b.lastBlock != nil && !b.lastBlockStrips {
		b.lastBlock.Value = strings.TrimSuffix(b.lastBlock.Value, "\n")
	}
	return docs, nil
}

// parseChunk parses one chunk into its document and the document's own
// comments, and reports whether the chunk ends inside a block scalar. A
// tail chunk holds no document, only comments for the one before it.
func (b *builder) parseChunk(c chunk) (*Node, docSlots, bool, error) {
	if c.tail {
		_, d := commentSlots(nil, c, b.nulledLines)
		return nil, d, false, nil
	}
	text, endsInBlock := normalizeChunkEnd(c.text, c.unterminated)
	c.text = text
	file, err := parser.ParseBytes([]byte(c.text), 0, parser.AllowDuplicateMapKey())
	if err != nil {
		return nil, docSlots{}, false, toParseError(err, c.lineOffset())
	}
	doc, err := b.document(file, c)
	if err != nil {
		return nil, docSlots{}, false, err
	}
	slots, d := commentSlots(file, c, b.nulledLines)
	applySlots(doc, "d0", slots)
	return doc, d, endsInBlock, nil
}

// normalizeChunkEnd works around goccy's handling of a block scalar that
// runs to the end of what it parses. Parse hands goccy one document at a
// time, so every chunk ends a stream as far as goccy can tell. With an
// indentation indicator, goccy rejects a trailing line shorter than the
// content indentation. Such a line holds only a line break, which clip
// and strip chomping discard, so we drop it. We leave the
// final chunk alone when Parse added the stream's last line break,
// because that chunk then ends on content. Without that line break, goccy
// drops trailing spaces and stops folding, which is why Parse adds it.
// normalizeChunkEnd reports whether the chunk ends inside a block scalar.
func normalizeChunkEnd(text string, addedNewline bool) (string, bool) {
	header, endsInBlock := finalBlockScalarHeader(text)
	if endsInBlock && !addedNewline && indentIndicator(header.Value) > 0 && !strings.Contains(header.Value, "+") {
		text = dropTrailingBlankLines(text, blockIndent(header, nil))
	}
	return text, endsInBlock
}

// finalBlockScalarHeader reports whether the last token of src is the
// body of a literal or folded block scalar, so the stream ends inside it,
// and returns that scalar's header token, such as "|2-". goccy's lexer
// marks the body Invalid when it is about to reject it, so both types
// count.
func finalBlockScalarHeader(src string) (*token.Token, bool) {
	toks := lexer.Tokenize(src)
	n := len(toks)
	if n < 2 || (toks[n-1].Type != token.StringType && toks[n-1].Type != token.InvalidType) {
		return nil, false
	}
	for k := n - 2; k >= 0; k-- {
		switch toks[k].Type {
		case token.CommentType:
			continue
		case token.LiteralType, token.FoldedType:
			return toks[k], true
		default:
			return nil, false
		}
	}
	return nil, false
}

// dropTrailingBlankLines removes the lines at the end of src that hold
// nothing but up to indent spaces, and leaves exactly one final line
// break. A whitespace-only line longer than indent is content, so it
// stays, and so does any line before it.
func dropTrailingBlankLines(src string, indent int) string {
	body := strings.TrimRight(src, "\n")
	for {
		i := strings.LastIndexByte(body, '\n')
		if i < 0 {
			break
		}
		if last := body[i+1:]; len(last) > indent || strings.Trim(last, " ") != "" {
			break
		}
		body = body[:i]
	}
	return body + "\n"
}

// streamText returns the prepared stream as the text Parse tokenizes,
// with a final line break added when a stream that is not empty lacks
// one, and reports whether it added the line break. It copies the bytes
// once.
func streamText(prepared []byte) (string, bool) {
	if len(prepared) == 0 || prepared[len(prepared)-1] == '\n' {
		return string(prepared), false
	}
	var b strings.Builder
	b.Grow(len(prepared) + 1)
	b.Write(prepared)
	b.WriteByte('\n')
	return b.String(), true
}

// readStream decodes data and runs the checks that come before anything
// tokenizes the whole stream: the encoding, UTF-8, and the depth probe.
// It also returns what the probe tokenized, for Parse to reuse.
func readStream(data []byte) ([]byte, probeResult, error) {
	src, err := decodeEncoding(data)
	if err != nil {
		return nil, probeResult{}, err
	}
	if err := checkUTF8(src); err != nil {
		return nil, probeResult{}, err
	}
	probed, err := probeDepth(src, false)
	if err != nil {
		return nil, probeResult{}, err
	}
	return src, probed, nil
}

// normalizeLineBreaks turns CRLF and lone CR line breaks into LF. Input
// with no CR comes back as the same slice, not a copy.
func normalizeLineBreaks(src []byte) []byte {
	if bytes.IndexByte(src, '\r') < 0 {
		return src
	}
	src = bytes.ReplaceAll(src, []byte("\r\n"), []byte("\n"))
	return bytes.ReplaceAll(src, []byte("\r"), []byte("\n"))
}

// utf8BOM is the UTF-8 byte order mark.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// TrimBOM strips one leading UTF-8 byte order mark from src, as libyaml
// does, and returns src as it stands when it has none. The result shares
// src's bytes.
func TrimBOM(src []byte) []byte {
	return bytes.TrimPrefix(src, utf8BOM)
}

// decodeEncoding strips a UTF-8 byte order mark and transcodes UTF-16
// input that starts with a byte order mark, as libyaml does.
func decodeEncoding(data []byte) ([]byte, error) {
	switch {
	case bytes.HasPrefix(data, utf8BOM):
		return TrimBOM(data), nil
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

// toParseError converts a goccy error from a chunk whose lines start
// lineOffset lines into the stream.
func toParseError(err error, lineOffset int) error {
	var se *yaml.SyntaxError
	if errors.As(err, &se) && se.Token != nil {
		line := se.Token.Position.Line
		if line > 0 {
			line += lineOffset
		}
		return &ParseError{Line: line, Message: se.Message}
	}
	return &ParseError{Message: err.Error()}
}
