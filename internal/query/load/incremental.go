package load

import (
	"errors"
	"io"
	"strings"

	"github.com/fivetwenty-io/graft/internal/yamlnode"
)

// incremental holds what a YAML reader needs to read its source a chunk at
// a time, where a chunk is the text of one document or a few.
type incremental struct {
	held    *heldChunk                // parsed, not yet handed out
	err     error                     // a failure to report after the held documents
	anchors map[string]*yamlnode.Node // shared by every chunk
	ahead   []sourceLine              // lines read past the end of a chunk, which open the next
	total   int                       // how many documents the reader has yielded
	done    bool
}

// sourceLine is a line of the source and its number.
type sourceLine struct {
	text string
	n    int
}

// startIncremental reads the leading content of a source and nothing
// beyond the first chunk, so that a document is parsed and yielded as soon
// as the one after it begins, rather than when the source ends.
func (r *Reader) startIncremental() error {
	r.inc = &incremental{anchors: map[string]*yamlnode.Node{}}
	if r.opts.NodeComments {
		return nil
	}
	var head strings.Builder
	for {
		line, err := r.br.ReadString('\n')
		if line != "" {
			head.WriteString(line)
			r.lines++
		}
		atEnd := err != nil
		if atEnd && !errors.Is(err, io.EOF) {
			return err
		}
		if atEnd || !leadingLine(line) {
			lead, data, shift := splitLeadingContent([]byte(head.String()))
			if atEnd || strings.TrimSpace(string(data)) != "" {
				r.leading, r.leadLines = lead, shift
				for i, l := range strings.SplitAfter(string(data), "\n") {
					if l != "" {
						r.inc.ahead = append(r.inc.ahead, sourceLine{l, i + 1})
					}
				}
				if atEnd {
					r.inc.done = true
				}
				return nil
			}
		}
	}
}

// leadingLine reports whether line is plainly part of the leading content,
// which lets the reader skip the scan until a line that might not be.
func leadingLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	left := strings.TrimLeft(line, yamlSpace)
	return trimmed == "" || trimmed == "---" || strings.HasPrefix(left, "#") || strings.HasPrefix(left, "%YAML")
}

// isDocStart reports whether line opens a document with "---".
func isDocStart(line string) bool {
	return strings.HasPrefix(line, "---") && (len(line) == 3 || strings.ContainsRune(" \t\r\n", rune(line[3])))
}

// isContent reports whether line holds something a document could be made
// of, which is anything but a blank, a comment, a directive, or a marker.
func isContent(line string) bool {
	trimmed := strings.TrimSpace(line)
	switch {
	case trimmed == "" || strings.HasPrefix(trimmed, "#"):
		return false
	case isDocStart(line):
		rest := strings.TrimSpace(line[3:])
		return rest != "" && !strings.HasPrefix(rest, "#")
	case strings.HasPrefix(line, "...") || strings.HasPrefix(line, "%"):
		return false
	}
	return true
}

// isDocEnd reports whether line closes a document with "...".
func isDocEnd(line string) bool {
	return strings.HasPrefix(line, "...") && (len(line) == 3 || strings.ContainsRune(" \t\r\n", rune(line[3])))
}

// nextChunk reads the lines of the next chunk, which ends before the "---"
// or the line after a "..." that opens the document after one with content. The chunk
// is cut only once a line with content follows the opening, so that a
// "---" followed by nothing but comments stays with the document before
// it, as it does when the whole source is parsed at once. It returns
// io.EOF when the source has nothing more. startLine is the number of the
// chunk's first line.
//
//nolint:gocyclo // one pass over the lines that decides where each chunk ends
func (r *Reader) nextChunk() (text string, startLine int, err error) {
	in := r.inc
	var b strings.Builder
	content, ended := false, false
	add := func(l sourceLine) {
		if b.Len() == 0 {
			startLine = l.n
		}
		b.WriteString(l.text)
		content = content || isContent(l.text)
		ended = isDocEnd(l.text)
	}
	read := func() (sourceLine, bool, error) {
		line, rerr := r.br.ReadString('\n')
		if line != "" {
			r.lines++
		}
		if rerr != nil {
			in.done = true
			if !errors.Is(rerr, io.EOF) {
				return sourceLine{}, false, rerr
			}
		}
		return sourceLine{line, r.lines}, line != "", nil
	}
	for _, l := range in.ahead {
		add(l)
	}
	in.ahead = nil
	// opens is whether a line may begin the next document. After a "..."
	// that is a line that holds no content, and the document starts with
	// the "---" that follows.
	opens := func(line string) bool { return isDocStart(line) || (ended && !isContent(line)) }
	for !in.done {
		l, ok, rerr := read()
		if rerr != nil {
			return "", 0, rerr
		}
		if !ok {
			break
		}
		if !content || !opens(l.text) {
			add(l)
			continue
		}
		// l may open the next document. Look on for the line that tells.
		ahead := []sourceLine{l}
		started := isDocStart(l.text)
		if isContent(l.text) {
			in.ahead = ahead
			return b.String(), startLine, nil
		}
		for !in.done {
			nl, ok, rerr := read()
			if rerr != nil {
				return "", 0, rerr
			}
			if !ok {
				break
			}
			if started && isDocStart(nl.text) && !isContent(nl.text) && hasBlank(ahead) {
				// A second "---" after a blank line with no content between:
				// the first one opens an empty document whose comments
				// belong to the document before, so it stays in this chunk.
				for _, al := range ahead {
					add(al)
				}
				ahead = nil
			}
			ahead = append(ahead, nl)
			started = started || isDocStart(nl.text)
			if isContent(nl.text) {
				if started {
					in.ahead = ahead
					return b.String(), startLine, nil
				}
				break
			}
		}
		for _, al := range ahead {
			add(al)
		}
	}
	if b.Len() == 0 {
		return "", 0, io.EOF
	}
	return b.String(), startLine, nil
}

// heldChunk is a chunk read and parsed on its own, waiting for the chunk
// after it. A document's foot comment is whatever the next document
// carries back to it, so the documents go out only once the chunk that
// follows them has been read.
type heldChunk struct {
	text  string
	start int
	docs  []*yamlnode.Node
}

// loadChunk parses the next chunk and queues the documents of the one
// before it in r.docs. It returns io.EOF once the source has no more
// documents.
//
//nolint:gocyclo // reads a chunk, settles the comments between neighbors, and ends the stream
func (r *Reader) loadChunk() error {
	in := r.inc
	for {
		if in.err != nil {
			return in.err
		}
		text, start, err := r.nextChunk()
		var cur *heldChunk
		if err == nil {
			docs, perr := yamlnode.ParseWith([]byte(text), r.parseOptions())
			if perr != nil {
				err = r.yamlErrorAt(perr, start-1)
			} else {
				for _, d := range docs {
					shiftLines(d, start-1)
				}
				cur = &heldChunk{text: text, start: start, docs: docs}
			}
		}
		prev := in.held
		in.held = cur
		if prev != nil {
			docs := prev.docs
			if cur != nil && len(docs) > 0 && (carriesComments(cur.text) || trailsComments(prev.text)) {
				// Parse the two together to learn the comments the later
				// chunk takes from the earlier one and hands back to it.
				if wd, werr := yamlnode.ParseWith([]byte(prev.text+cur.text), r.parseOptions()); werr == nil && len(wd) == len(docs)+len(cur.docs) {
					for _, d := range wd {
						shiftLines(d, prev.start-1)
					}
					for i, d := range wd[:len(docs)] {
						d.HeadComment = docs[i].HeadComment
					}
					docs, cur.docs = wd[:len(docs)], wd[len(docs):]
				}
			}
			if err != nil && !errors.Is(err, io.EOF) {
				in.err = err
			}
			if len(docs) > 0 {
				r.docs, r.next = docs, 0
				return nil
			}
		}
		if errors.Is(err, io.EOF) {
			if in.total == 0 && r.leading != "" && len(r.docs) == 0 && !r.nulled {
				// yq reads a source that holds only leading content as
				// one null document carrying it.
				r.nulled = true
				r.docs, r.next = []*yamlnode.Node{nullDocument()}, 0
				return nil
			}
			return io.EOF
		}
		if err != nil {
			return err
		}
	}
}

// carriesComments reports whether a comment comes before the first line of
// content in text, which is what lets a chunk hand foot comments back to
// the one before it.
func carriesComments(text string) bool {
	for text != "" {
		line, rest, _ := strings.Cut(text, "\n")
		text = rest
		switch {
		case strings.HasPrefix(strings.TrimSpace(line), "#"):
			return true
		case isContent(line):
			return false
		}
	}
	return false
}

// shiftLines moves the line of n and everything under it down by by lines,
// which puts a chunk's nodes where they stood in the whole source.
func shiftLines(n *yamlnode.Node, by int) {
	if by == 0 || n == nil {
		return
	}
	if n.Line > 0 {
		n.Line += by
	}
	for _, c := range n.Content {
		shiftLines(c, by)
	}
}

// nullDocument is the document of a source that holds only leading
// content.
func nullDocument() *yamlnode.Node {
	null := &yamlnode.Node{Kind: yamlnode.ScalarNode, Tag: yamlnode.TagNull, Line: 1, Column: 1}
	return &yamlnode.Node{Kind: yamlnode.DocumentNode, Line: 1, Column: 1, Content: []*yamlnode.Node{null}}
}

// hasBlank reports whether any of lines is empty.
func hasBlank(lines []sourceLine) bool {
	for _, l := range lines {
		if strings.TrimSpace(l.text) == "" {
			return true
		}
	}
	return false
}

// trailsComments reports whether a comment follows the last line of
// content in text, which a document keeps as its foot comment only when
// another document comes after it.
func trailsComments(text string) bool {
	trailing := false
	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(strings.TrimSpace(line), "#"):
			trailing = true
		case isContent(line):
			trailing = false
		}
	}
	return trailing
}
