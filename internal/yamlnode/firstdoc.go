package yamlnode

import (
	"bytes"
	"unicode/utf8"

	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/token"
)

// FirstDocument returns the first document of src, raw YAML bytes, for
// callers that parse with goccy and read only the first document, as
// graft's merge does. goccy parses every document in a stream, so
// without the cut a syntax error or deep nesting in a later document
// would fail a merge that never reads it. FirstDocument fails a first
// document that nests deeper than yaml.v3 allows, with yaml.v3's message
// and line, as CheckFirstDocumentDepth does.
//
// The result is a prefix of src, so each line and column in it is the
// one src has. It ends where the line holding the "---" that ends the
// first document starts, or where the line after a "..." that ends it
// starts. Content after that "..." needs a "---" before it, as yaml.v3
// reads the stream, so FirstDocument fails content that comes first with
// yaml.v3's message and line, as Parse does, rather than drop it. It
// also fails a directive after that "..." with no "---" to follow it.
// The marker is the token the depth probe confirms in two prefixes, or
// in the whole input. When nothing ends the first
// document, or when the cut cannot be proved, FirstDocument returns src
// as it stands. See documentEnd for the proof. goccy then parses every
// document in src, so FirstDocument fails nesting past maxDepth in any
// of them, not only in the first.
//
// It tokenizes the way probeDepth does. Input that cannot nest past
// maxDepth is tokenized once, whole, and other input in prefixes that
// double until the first document ends or the depth scan trips. Input
// with no marker line that could end the first document, which is most
// input, gets only the depth probe. See hasLaterMarker.
func FirstDocument(src []byte) ([]byte, error) {
	if !hasLaterMarker(src) {
		return wholeStream(src)
	}
	cut := probeCut(src)
	if cut < 0 {
		cut = len(src)
	}
	var prev token.Tokens
	for {
		for cut < len(src) && !utf8.RuneStart(src[cut]) {
			cut++
		}
		whole := cut == len(src)
		toks := lexer.Tokenize(probeText(src[:cut], whole))
		at, err := scanDepth(toks, true)
		if at >= 0 && (whole || sameToken(prev, toks, at)) {
			if err != nil {
				return nil, err
			}
			end := documentEnd(src, toks, at)
			if end < 0 {
				return wholeStream(src)
			}
			if toks[at].Type != token.DocumentEndType {
				return src[:end], nil
			}
			if line, decided := bareAfterEnd(src, prev, toks, at, whole); decided {
				if line > 0 {
					return nil, documentStartError(line)
				}
				return src[:end], nil
			}
		} else if whole {
			// No token ended the first document, so the depth scan
			// covered every token in src.
			return src, nil
		}
		prev, cut = toks, min(2*cut, len(src))
	}
}

// bareAfterEnd looks past toks[at], the "..." that ends the first
// document, for content that comes before any "---", which yaml.v3
// rejects. See endWatch. A "..." or the end of the input after a
// directive fails the same way. It returns the line yaml.v3 fails on,
// or 0 when a "---" or the end of the input comes first, and reports
// whether the tokens decide it. In a prefix, only a token that prev
// holds at the same index decides it, as with the marker, and the end
// of the prefix decides nothing.
func bareAfterEnd(src []byte, prev, toks token.Tokens, at int, whole bool) (int, bool) {
	var w endWatch
	w.reset(true)
	for i := at + 1; i < len(toks); i++ {
		if w.directive && toks[i].Type == token.DocumentEndType {
			return toks[i].Position.Line, whole || sameToken(prev, toks, i)
		}
		w.token(toks[i])
		if !w.armed {
			return w.line, whole || sameToken(prev, toks, i)
		}
	}
	if whole && w.directive {
		return endOfInputLine(src), true
	}
	return 0, whole
}

// endOfInputLine is the line libyaml gives the end of src. It counts a
// last line with no line break as ended, as libyaml does.
func endOfInputLine(src []byte) int {
	line := bytes.Count(src, []byte("\n")) + 1
	if len(src) > 0 && src[len(src)-1] != '\n' {
		line++
	}
	return line
}

// wholeStream returns src, the whole stream FirstDocument hands goccy,
// after it fails nesting past maxDepth in any document of src. Input
// that cannot nest that deep costs one pass over its bytes.
func wholeStream(src []byte) ([]byte, error) {
	if _, err := probeDepth(src, false); err != nil {
		return nil, err
	}
	return src, nil
}

// documentEnd returns the offset in src where the first document ends,
// given toks[at], the marker that ends it. That is the start of the
// marker's line for a "---", and the start of the next line for a
// "...", or len(src) when the "..." line is the last. It returns -1 when
// it cannot prove the marker starts its line.
//
// The tokens come from text whose line breaks are all LF, and goccy
// counts lines by LF alone, so the marker's line number counts the line
// breaks before it. lineStart counts the same breaks in src, where a
// CRLF, a lone CR, or an LF is one break, as normalizeLineBreaks makes
// it one LF. The "<<<" key rewrite only adds quotes inside a line. A cut
// stands only when that line of src starts with the marker itself,
// followed by a space, a tab, a line break, or the end of src.
//
// goccy also ends a document at a "---" inside a quoted scalar or a
// flow collection that runs past it, and then fails the whole stream.
// The cut would hide that error, or swap it for another, so an invalid
// token or an open flow collection before the marker leaves src whole.
func documentEnd(src []byte, toks token.Tokens, at int) int {
	t := toks[at]
	if t.Position.Column != 1 || !closedBefore(toks[:at]) {
		return -1
	}
	marker := []byte("---")
	if t.Type == token.DocumentEndType {
		marker = []byte("...")
	}
	off := lineStart(src, t.Position.Line)
	if off < 0 || !bytes.HasPrefix(src[off:], marker) {
		return -1
	}
	if rest := src[off+len(marker):]; len(rest) > 0 && bytes.IndexByte([]byte(" \t\r\n"), rest[0]) < 0 {
		return -1
	}
	if t.Type == token.DocumentEndType {
		// The "..." line belongs to the document it ends.
		if next := lineStart(src, t.Position.Line+1); next >= 0 {
			return next
		}
		return len(src)
	}
	return off
}

// closedBefore reports whether toks hold no invalid token and leave no
// flow collection open.
func closedBefore(toks token.Tokens) bool {
	flow := 0
	for _, t := range toks {
		switch t.Type {
		case token.InvalidType:
			return false
		case token.SequenceStartType, token.MappingStartType:
			flow++
		case token.SequenceEndType, token.MappingEndType:
			flow = max(flow-1, 0)
		default:
			// Other tokens leave the flow level as it stands.
		}
	}
	return flow == 0
}

// lineStart returns the offset in src where line, numbered from 1,
// starts, or -1 when src has fewer lines. A CRLF, a lone CR, and an LF
// each end one line.
func lineStart(src []byte, line int) int {
	off := 0
	for n := 1; n < line; n++ {
		i := bytes.IndexAny(src[off:], "\r\n")
		if i < 0 {
			return -1
		}
		off += i + 1
		if src[off-1] == '\r' && off < len(src) && src[off] == '\n' {
			off++
		}
	}
	return off
}

// hasLaterMarker reports whether a line of src that comes after its
// first content or marker line starts with a "---" or a "...". A token
// that ends the first document needs content or a "---" before it, and
// documentEnd needs its line to start with the marker, so without such
// a line FirstDocument would return src whole and need not tokenize it.
// Lines of spaces and tabs, comment lines, and directive lines are not
// content. Taking a line for content when goccy does not only makes the
// answer true more often, which costs a tokenize and never a wrong cut.
func hasLaterMarker(src []byte) bool {
	started := false
	for len(src) > 0 {
		end := bytes.IndexAny(src, "\r\n")
		if end < 0 {
			end = len(src)
		}
		line := src[:end]
		switch {
		case isMarkerLine(line):
			if started {
				return true
			}
			started = true
		case !started && isPreamble(line):
		default:
			started = true
		}
		src = src[min(end+1, len(src)):]
	}
	return false
}

// isMarkerLine reports whether line starts with a "---" or a "..."
// followed by a space, a tab, or nothing.
func isMarkerLine(line []byte) bool {
	if !bytes.HasPrefix(line, []byte("---")) && !bytes.HasPrefix(line, []byte("...")) {
		return false
	}
	return len(line) == 3 || line[3] == ' ' || line[3] == '\t'
}

// isPreamble reports whether line, before any content, is blank, a
// comment, or a directive.
func isPreamble(line []byte) bool {
	rest := bytes.TrimLeft(line, " \t")
	return len(rest) == 0 || rest[0] == '#' || line[0] == '%'
}
