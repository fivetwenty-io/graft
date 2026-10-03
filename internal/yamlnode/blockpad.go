package yamlnode

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// ParseBytes parses data with goccy, as parser.ParseBytes does, and
// works around goccy's handling of block scalars with an indentation
// indicator. libyaml reads a line that holds only spaces, fewer than
// the block's indentation, as an empty line of the block. goccy fails
// such a line when it is the last line of its input ("invalid number of
// indent is specified in the multi-line header"), and fails a block
// whose lines are all empty when a less indented line follows it
// ("could not find multi-line content"). spruce writes both shapes, for
// a value such as "\na" or "\n" that starts with a line break, and reads
// them back. When goccy fails with either message, ParseBytes pads those
// lines to the block's indentation, which leaves them empty to libyaml
// and lets goccy read them, and parses again. It returns goccy's first
// error when the padded text fails too. Padding never moves a line, so
// positions in the result are those of data.
//
// When data holds a LINE SEPARATOR or PARAGRAPH SEPARATOR, ParseBytes
// also gives the single-quoted and literal block scalars that hold one
// the values libyaml reads for them, as fixLineSeparators describes.
func ParseBytes(data []byte, mode parser.Mode, opts ...parser.Option) (*ast.File, error) {
	file, err := parseBlockPadded(data, mode, opts...)
	if err == nil && file != nil && hasLineSeparator(data) {
		fixLineSeparators(file)
	}
	return file, err
}

// parseBlockPadded parses data with goccy, padding block scalar lines
// and parsing again when goccy fails with an error that
// padBlockScalarLines works around.
func parseBlockPadded(data []byte, mode parser.Mode, opts ...parser.Option) (*ast.File, error) {
	file, err := parser.ParseBytes(data, mode, opts...)
	if err == nil || !isBlockIndentError(err) {
		return file, err
	}
	padded, ok := padBlockScalarLines(data)
	if !ok {
		return file, err
	}
	retried, retryErr := parser.ParseBytes(padded, mode, opts...)
	if retryErr != nil {
		return file, err
	}
	return retried, nil
}

// isBlockIndentError reports whether err is one of the two goccy errors
// that padBlockScalarLines works around.
func isBlockIndentError(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "invalid number of indent is specified in the multi-line header") ||
		strings.Contains(msg, "could not find multi-line content")
}

// blockHeaderLineRe matches a line that ends in a block scalar header
// with an indentation indicator, such as "k: |2-", "- |2+", "- k: >1",
// or "? |2", optionally followed by a comment. Group 1 is the line's
// indentation, group 2 the "-", "?", and ":" indicators before the
// header, and group 3 a key the header is the value of. Groups 4 and 5
// hold the indicator digit, before or after a chomping indicator.
var blockHeaderLineRe = regexp.MustCompile(
	`^( *)((?:[-?:] +)*)((?:"(?:[^"\\]|\\.)*"|'(?:[^']|'')*'|[^ #'"\-?:][^#]*?|[-?:][^ #][^#]*?) *: +)?` +
		`(?:[!&][^ ]* +)*[|>](?:([1-9])[+-]?|[+-]([1-9])) *(?:#.*)?$`)

// padBlockScalarLines pads every line that holds only spaces, fewer than
// the indentation of the block scalar it belongs to, to that
// indentation, for each block scalar with an indentation indicator
// inside a block collection. A block at the top level of a document is
// left alone, since goccy counts its indentation differently. The last
// line of data counts only when a line break ends it. It reports
// whether it changed anything.
func padBlockScalarLines(data []byte) ([]byte, bool) {
	lines := bytes.Split(data, []byte("\n"))
	// The piece after the final line break, or a last line with no line
	// break after it, is never padded.
	last := len(lines) - 1
	changed := false
	for i := 0; i < last; i++ {
		indent, ok := blockHeaderIndent(string(lines[i]))
		if !ok {
			continue
		}
		j := i + 1
		for ; j < last; j++ {
			line := lines[j]
			spaces := len(line) - len(bytes.TrimLeft(line, " "))
			if spaces == len(line) {
				if spaces < indent {
					lines[j] = bytes.Repeat([]byte(" "), indent)
					changed = true
				}
				continue
			}
			if spaces < indent {
				break
			}
		}
		i = j - 1
	}
	if !changed {
		return data, false
	}
	return bytes.Join(lines, []byte("\n")), true
}

// blockHeaderIndent returns the content indentation libyaml gives the
// block scalar whose header ends line, when the header has an
// indentation indicator and the block sits inside a block collection.
// The indentation counts from the collection's: the column of the key
// the block is the value of, or of the last "-", "?", or ":" indicator
// before the header.
func blockHeaderIndent(line string) (int, bool) {
	m := blockHeaderLineRe.FindStringSubmatch(line)
	if m == nil {
		return 0, false
	}
	digit := m[4] + m[5]
	parent := -1
	switch {
	case m[3] != "":
		parent = len(m[1]) + len(m[2])
	case m[2] != "":
		indicators := strings.TrimRight(m[2], " ")
		parent = len(m[1]) + strings.LastIndexAny(indicators, "-?:")
	}
	if parent < 0 {
		return 0, false
	}
	return parent + int(digit[0]-'0'), true
}
