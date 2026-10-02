package yamlnode

import (
	"strings"

	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/token"
)

// chunk is one YAML document's slice of the source, padded with leading
// newlines so goccy reports the original line numbers.
type chunk struct {
	text      string
	startLine int  // 1-based line where the document starts
	index     int  // the document's position in the stream
	marker    bool // a "---" follows the chunk, or it ends with "..."

	// unterminated marks the stream's last chunk when Parse added the
	// stream's final line break.
	unterminated bool
}

// splitDocuments cuts a YAML stream into one chunk per document the way
// yaml.v3 counts documents. goccy's parser merges or drops documents
// around empty "---" sections, but its lexer reports every "---" and "..."
// marker with a correct line, so we cut at those lines.
//
// A section that starts with "---" is always a document, even when it is
// empty. A section that does not (the text before the first "---", or
// the text after a "...") is a document only when it holds a token other
// than a comment or a directive. Directive lines (%YAML, %TAG) travel with
// the "---" section that follows them, and so do the comments of a
// section that is not a document, because yaml.v3 gives them to the next
// document's first node.
func splitDocuments(src string) []chunk {
	s := &splitter{segStart: 1, textStart: 1, directiveLines: map[int]bool{}}
	s.lines = strings.SplitAfter(src, "\n")
	if n := len(s.lines); n > 0 && s.lines[n-1] == "" {
		s.lines = s.lines[:n-1]
	}
	toks := lexer.Tokenize(src)
	for _, t := range toks {
		if t.Type == token.DirectiveType {
			s.directiveLines[t.Position.Line] = true
		}
	}
	for _, t := range toks {
		s.token(t)
	}
	s.emit(len(s.lines), false)
	return s.chunks
}

type splitter struct {
	lines            []string
	directiveLines   map[int]bool
	chunks           []chunk
	segStart         int
	textStart        int // first line of the chunk text, before segStart when comments carry over
	explicit         bool
	hasContent       bool
	pendingDirective int // first directive line waiting for its "---"
}

func (s *splitter) token(t *token.Token) {
	line := t.Position.Line
	switch {
	case t.Type == token.DocumentHeaderType:
		textStart := line
		if !s.emit(line-1, true) && !s.explicit {
			textStart = s.textStart // carry the section's comments over
		}
		s.textStart, s.segStart, s.explicit, s.hasContent = textStart, line, true, false
		if s.pendingDirective > 0 {
			s.segStart, s.pendingDirective = s.pendingDirective, 0
			s.textStart = min(s.textStart, s.segStart)
		}
	case t.Type == token.DocumentEndType:
		s.emit(line, true)
		s.textStart, s.segStart, s.explicit, s.hasContent = line+1, line+1, false, false
	case t.Type == token.DirectiveType:
		if s.pendingDirective == 0 {
			s.pendingDirective = line
		}
	case t.Type == token.CommentType || s.directiveLines[line]:
		// Comments and directive arguments never make a document.
	default:
		s.hasContent = true
	}
}

// emit closes the section that ends at endLine and reports whether it
// was a document. It clamps endLine to the last line, so a marker line
// the lexer counts past the end of the text can never slice beyond it.
func (s *splitter) emit(endLine int, marker bool) bool {
	endLine = min(endLine, len(s.lines))
	if endLine < s.segStart || s.textStart > endLine+1 || (!s.explicit && !s.hasContent) {
		return false
	}
	s.chunks = append(s.chunks, chunk{
		text:      strings.Repeat("\n", s.textStart-1) + strings.Join(s.lines[s.textStart-1:endLine], ""),
		startLine: s.segStart,
		index:     len(s.chunks),
		marker:    marker,
	})
	return true
}
