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

// blockHeaderLineRe matches a line that ends in a block scalar header,
// such as "k: |", "k: |2-", "- |2+", "- k: >1", or "? |2", optionally
// followed by a comment. Group 1 is the line's indentation, group 2 the
// "-", "?", and ":" indicators before the header, and group 3 a key the
// header is the value of. Groups 4 and 5 hold the indentation indicator
// digit, before or after a chomping indicator, when the header has one.
var blockHeaderLineRe = regexp.MustCompile(
	`^( *)((?:[-?:] +)*)((?:"(?:[^"\\]|\\.)*"|'(?:[^']|'')*'|[^ #'"\-?:][^#]*?|[-?:][^ #][^#]*?) *: +)?` +
		`(?:[!&][^ ]* +)*[|>](?:([1-9])[+-]?|[+-]([1-9])?)? *(?:#.*)?$`)

// blockHeader describes the block scalar whose header ends a line. Its
// content runs on while each line that is not blank is indented by at
// least end spaces. pad is the indentation padBlockScalarLines pads the
// block's blank lines to, or 0 when it leaves them alone.
type blockHeader struct {
	end, pad int
}

// padBlockScalarLines pads every line that holds only spaces, fewer than
// the indentation of the block scalar it belongs to, to that
// indentation, for each block scalar with an indentation indicator
// inside a block collection. A block at the top level of a document is
// left alone, since goccy counts its indentation differently. The
// content of every block scalar is skipped when looking for headers, so
// a line inside a block that looks like a header, such as "k: |2-" in a
// stringified map, never causes padding, and the content of a block
// without an indicator is never changed. The last line of data counts
// only when a line break ends it. It reports whether it changed
// anything.
func padBlockScalarLines(data []byte) ([]byte, bool) {
	lines := bytes.Split(data, []byte("\n"))
	// The piece after the final line break, or a last line with no line
	// break after it, is never padded.
	last := len(lines) - 1
	changed := false
	for i := 0; i < last; i++ {
		h, ok := parseBlockHeader(string(lines[i]))
		if !ok {
			continue
		}
		j := i + 1
		for ; j < last; j++ {
			line := lines[j]
			spaces := len(line) - len(bytes.TrimLeft(line, " "))
			if spaces == len(line) {
				if spaces < h.pad {
					lines[j] = bytes.Repeat([]byte(" "), h.pad)
					changed = true
				}
				continue
			}
			if spaces < h.end {
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

// parseBlockHeader reports whether line ends in a block scalar header
// and returns the extent of the block's content. The indentation counts
// from the enclosing block collection's: the column of the key the
// block is the value of, or of the last "-", "?", or ":" indicator
// before the header. With an indentation indicator inside a block
// collection, the content indentation libyaml gives the block is that
// column plus the indicator, and the block's blank lines are padded to
// it. Any other block's content is every line indented past that
// column, or past column 0 at the top level of a document, which a
// "--- " before the header marks.
func parseBlockHeader(line string) (blockHeader, bool) {
	top := strings.HasPrefix(line, "--- ")
	if top {
		line = strings.TrimLeft(line[len("---"):], " ")
	}
	m := blockHeaderLineRe.FindStringSubmatch(line)
	if m == nil {
		return blockHeader{}, false
	}
	parent := -1
	switch {
	case top:
	case m[3] != "":
		parent = len(m[1]) + len(m[2])
	case m[2] != "":
		indicators := strings.TrimRight(m[2], " ")
		parent = len(m[1]) + strings.LastIndexAny(indicators, "-?:")
	}
	if digit := m[4] + m[5]; digit != "" && parent >= 0 {
		indent := parent + int(digit[0]-'0')
		return blockHeader{end: indent, pad: indent}, true
	}
	return blockHeader{end: max(parent+1, 1)}, true
}
