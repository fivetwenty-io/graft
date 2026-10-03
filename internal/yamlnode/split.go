package yamlnode

import (
	"strings"

	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/token"
)

// chunk is one YAML document's slice of the source. goccy numbers the
// chunk's lines from 1, so the line of anything in it is textLine-1 more
// than goccy says.
type chunk struct {
	text      string
	startLine int  // 1-based line where the document starts
	textLine  int  // 1-based line where text starts, before startLine when comments carry over
	index     int  // the document's position in the stream
	marker    bool // a "---" follows the chunk, or it ends with "..."
	afterEnd  bool // a "..." ended the document before this one

	// endComments holds the comments that sit on the "..." lines that
	// end the document before this one. yaml.v3 gives them to this
	// document as head comments, ahead of any comment above its "---".
	endComments []string

	// tail marks the comments after the last document's "...", which are
	// no document but can still be that document's foot comment.
	tail bool

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
// document's first node. Comments after the last document's "..." come
// back as a tail chunk.
func splitDocuments(src string) []chunk {
	return splitTokens(src, lexer.Tokenize(src))
}

// splitTokens is splitDocuments for a stream the caller has already
// tokenized.
func splitTokens(src string, toks token.Tokens) []chunk {
	s := &splitter{segStart: 1, textStart: 1, directiveLines: map[int]bool{}}
	s.lines = strings.SplitAfter(src, "\n")
	if n := len(s.lines); n > 0 && s.lines[n-1] == "" {
		s.lines = s.lines[:n-1]
	}
	for _, t := range toks {
		if t.Type == token.DirectiveType {
			s.directiveLines[t.Position.Line] = true
		}
	}
	for _, t := range toks {
		s.token(t)
	}
	if !s.emit(len(s.lines), false) && s.afterEnd && s.textStart <= len(s.lines) {
		s.chunks = append(s.chunks, chunk{
			text:     strings.Join(s.lines[s.textStart-1:], ""),
			textLine: s.textStart, startLine: s.textStart,
			index: len(s.chunks), afterEnd: true, tail: true,
		})
	}
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

	// endedByDots records that a "..." ended the last document, and
	// afterEnd that the section being read follows that "...".
	endedByDots bool
	afterEnd    bool

	// endLine is the line of the last "...", and endComments holds the
	// comments that sit on the "..." lines since the last chunk, which
	// the next chunk takes as comment lines of its own.
	endLine     int
	endComments []string
}

func (s *splitter) token(t *token.Token) {
	line := t.Position.Line
	switch {
	case t.Type == token.DocumentHeaderType:
		textStart := line
		if s.emit(line-1, true) {
			s.endedByDots = false
		} else if !s.explicit {
			textStart = s.textStart // carry the section's comments over
		}
		s.afterEnd = s.endedByDots
		s.textStart, s.segStart, s.explicit, s.hasContent = textStart, line, true, false
		if s.pendingDirective > 0 {
			s.segStart, s.pendingDirective = s.pendingDirective, 0
			s.textStart = min(s.textStart, s.segStart)
		}
	case t.Type == token.DocumentEndType:
		if s.emit(line, true) {
			s.endedByDots = true
		}
		s.afterEnd = s.endedByDots
		s.endLine = line
		s.textStart, s.segStart, s.explicit, s.hasContent = line+1, line+1, false, false
	case t.Type == token.DirectiveType:
		if s.pendingDirective == 0 {
			s.pendingDirective = line
		}
	case t.Type == token.CommentType && line == s.endLine && s.endedByDots:
		s.endComments = append(s.endComments, "#"+t.Value)
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
	var endComments []string
	if s.afterEnd {
		endComments = s.endComments
	}
	s.endComments = nil
	s.chunks = append(s.chunks, chunk{
		text:        strings.Join(s.lines[s.textStart-1:endLine], ""),
		endComments: endComments,
		startLine:   s.segStart,
		textLine:    s.textStart,
		index:       len(s.chunks),
		marker:      marker,
		afterEnd:    s.afterEnd,
	})
	return true
}

// lineOffset is what to add to a line goccy reports in the chunk to get
// its line in the stream.
func (c chunk) lineOffset() int {
	return c.textLine - 1
}
