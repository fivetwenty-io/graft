package yamlnode

import (
	"fmt"

	"github.com/goccy/go-yaml/token"
)

// maxDepth is yaml.v3's nesting limit, libyaml's max_flow_level and
// max_indents. Each one counts on its own, so a document may hold 10,000
// open block collections and 10,000 open flow collections inside them.
const maxDepth = 10000

// checkDepth fails a token stream that nests deeper than yaml.v3 allows,
// with yaml.v3's message and line. It has to run on the lexer's tokens
// before goccy parses anything, because goccy's parser recurses once per
// level and slows down faster than linearly. 200,000 levels take it
// seconds, and 2,000,000 take it longer than anyone waits.
//
// The scan follows libyaml's scanner. A "[" or "{" raises the flow level.
// Outside flow collections, every token first closes the block levels
// indented past its column, and a "-", a "?", or a key's ":" opens a new
// level when it stands right of the innermost one.
func checkDepth(toks token.Tokens) error {
	s := &depthScan{}
	for _, t := range toks {
		if err := s.token(t); err != nil {
			return err
		}
	}
	return nil
}

type depthScan struct {
	flow    int
	indents []int // the columns of the open block collections
	keyLine int   // the last line where a key could have started outside flow collections
	dirLine int   // the line of the last directive
}

func (s *depthScan) token(t *token.Token) error {
	line := t.Position.Line
	switch t.Type {
	case token.CommentType:
		return nil
	case token.DirectiveType:
		s.dirLine, s.indents = line, s.indents[:0]
		return nil
	case token.DocumentHeaderType, token.DocumentEndType:
		s.indents = s.indents[:0]
		return nil
	default:
		if line == s.dirLine {
			return nil // a directive's arguments
		}
	}
	if s.flow == 0 {
		s.unroll(t.Position.Column)
	}
	switch t.Type {
	case token.SequenceStartType, token.MappingStartType:
		return s.openFlow(line)
	case token.SequenceEndType, token.MappingEndType:
		s.flow = max(s.flow-1, 0)
	case token.SequenceEntryType, token.MappingKeyType:
		return s.roll(t.Position.Column, line)
	case token.MappingValueType:
		return s.roll(keyColumn(t), line)
	case token.CollectEntryType, token.LiteralType, token.FoldedType:
		// These never start a key.
	default:
		if s.flow == 0 && !isBlockBody(t) {
			s.keyLine = line
		}
	}
	return nil
}

// openFlow enters a flow collection. libyaml reports the error on the
// line of the bracket that went too deep.
func (s *depthScan) openFlow(line int) error {
	if s.flow == 0 {
		s.keyLine = line
	}
	s.flow++
	if s.flow > maxDepth {
		return depthError(line)
	}
	return nil
}

func (s *depthScan) unroll(col int) {
	for n := len(s.indents); n > 0 && s.indents[n-1] > col; n-- {
		s.indents = s.indents[:n-1]
	}
}

// roll opens a block collection at col when col stands right of the
// innermost one. libyaml reports the error on the line of the last token
// that could have started a key, and when that is the first line, on the
// line it is reading.
func (s *depthScan) roll(col, line int) error {
	if s.flow > 0 {
		return nil
	}
	if n := len(s.indents); n > 0 && s.indents[n-1] >= col {
		return nil
	}
	s.indents = append(s.indents, col)
	if len(s.indents) <= maxDepth {
		return nil
	}
	if s.keyLine > 1 {
		return depthError(s.keyLine)
	}
	return depthError(line)
}

// keyColumn returns the column where the key of the ":" token t starts,
// which is the first token on its line after any "-", "?", or earlier
// ":". Stopping at an earlier ":" keeps the walk linear on a line that
// repeats "a: a: a:".
func keyColumn(t *token.Token) int {
	start := t
	for p := t.Prev; p != nil && p.Position.Line == t.Position.Line; p = p.Prev {
		if p.Type == token.SequenceEntryType || p.Type == token.MappingKeyType || p.Type == token.MappingValueType {
			break
		}
		start = p
	}
	return start.Position.Column
}

// isBlockBody reports whether t is the body of a literal or folded
// scalar, which libyaml never treats as a possible key.
func isBlockBody(t *token.Token) bool {
	return t.Prev != nil && (t.Prev.Type == token.LiteralType || t.Prev.Type == token.FoldedType)
}

// depthError is yaml.v3's depth error. libyaml numbers lines from 0 and
// gives a line only when it is not the first.
func depthError(line int) error {
	if line <= 1 {
		line = 0
	}
	return &ParseError{Line: line, Message: fmt.Sprintf("exceeded max depth of %d", maxDepth)}
}
