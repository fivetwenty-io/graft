package load

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"

	"github.com/fivetwenty-io/graft/internal/yamlnode"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// Reader yields the documents of one source, or of several chained ones, a
// document at a time. Nothing is read until the first Next.
type Reader struct {
	src       Source
	fileIndex int
	opts      LoadOptions

	chain []*Reader
	cur   int

	started  bool
	br       *bufio.Reader
	json     *JSONReader
	raw      bool
	docs     []*yamlnode.Node
	leading  string
	next     int
	evq      []*value.Value // streaming events of a loaded document
	evLine   int
	lines    int
	docIndex int
	err      error
}

// Open starts reading src, which is the fileIndex-th input. The format is
// decided on the first Next.
func Open(src Source, fileIndex int, opts LoadOptions) *Reader {
	return &Reader{src: src, fileIndex: fileIndex, opts: opts}
}

// Chain yields every reader's documents in order.
func Chain(readers ...*Reader) *Reader {
	if readers == nil {
		readers = []*Reader{}
	}
	return &Reader{chain: readers}
}

// Line returns the number of lines consumed so far, which is what
// input_line_number reports.
func (r *Reader) Line() int {
	if r.chain != nil {
		if r.cur < len(r.chain) {
			return r.chain[r.cur].Line()
		}
		if len(r.chain) > 0 {
			return r.chain[len(r.chain)-1].Line()
		}
		return 0
	}
	if r.json != nil {
		return r.json.Line()
	}
	return r.lines
}

// Next returns the next document, and io.EOF after the last one. A failure
// sticks, so later calls return the same error.
func (r *Reader) Next() (Document, error) {
	if r.chain != nil {
		return r.nextChained()
	}
	if r.err != nil {
		return Document{}, r.err
	}
	d, err := r.read()
	if err != nil {
		r.err = err
		return Document{}, err
	}
	d.Source, d.FileIndex, d.DocIndex = r.src.Name, r.fileIndex, r.docIndex
	r.docIndex++
	return d, nil
}

// RawText returns everything a raw reader has not yet read as one string,
// newlines included, with each invalid UTF-8 byte replaced as it is in a
// raw line, and leaves the reader at its end. A chain joins the rest of
// each of its readers in order. This is how a raw slurp keeps the text
// exactly, which joining lines cannot, because a line does not say whether
// a newline ended it.
func (r *Reader) RawText() (string, error) {
	if r.chain != nil {
		var b strings.Builder
		for ; r.cur < len(r.chain); r.cur++ {
			text, err := r.chain[r.cur].RawText()
			if err != nil {
				return "", err
			}
			b.WriteString(text)
		}
		if len(r.chain) > 0 {
			r.cur = len(r.chain) - 1
		}
		return b.String(), nil
	}
	if errors.Is(r.err, io.EOF) {
		return "", nil // a reader with nothing left still has its empty rest
	}
	if r.err != nil {
		return "", r.err
	}
	if !r.started {
		if err := r.start(); err != nil {
			r.err = err
			return "", err
		}
	}
	if !r.raw {
		return "", errors.New("RawText needs a reader of raw lines")
	}
	data, err := io.ReadAll(r.br)
	if err != nil {
		r.err = err
		return "", err
	}
	r.lines += bytes.Count(data, []byte{'\n'})
	return toValidUTF8(data), nil
}

func (r *Reader) nextChained() (Document, error) {
	for r.cur < len(r.chain) {
		d, err := r.chain[r.cur].Next()
		if errors.Is(err, io.EOF) {
			if r.cur == len(r.chain)-1 {
				return Document{}, io.EOF
			}
			r.cur++
			continue
		}
		return d, err
	}
	return Document{}, io.EOF
}

func (r *Reader) read() (Document, error) {
	if !r.started {
		if err := r.start(); err != nil {
			return Document{}, err
		}
	}
	switch {
	case r.raw:
		line, err := readRawLine(r.br)
		if err != nil {
			return Document{}, err
		}
		r.lines++
		return Document{Line: r.lines, Value: value.Str(line)}, nil
	case r.json != nil && r.opts.Stream:
		return r.nextStreamEvent()
	case r.json != nil:
		v, line, err := r.json.Next()
		if err != nil {
			return Document{}, err
		}
		return Document{Line: line, Value: v}, nil
	}
	if r.opts.Stream {
		return r.nextYAMLEvent()
	}
	return r.nextYAML()
}

// nextStreamEvent reads one event from a JSON source.
func (r *Reader) nextStreamEvent() (Document, error) {
	ev, err := r.json.NextEvent()
	var se *streamError
	switch {
	case err == nil:
		return Document{Line: r.json.Line(), Value: ev}, nil
	case !errors.As(err, &se):
		return Document{}, err
	}
	return Document{}, &SyntaxError{Source: r.src.Name, Line: se.line, Col: se.col, Msg: se.msg}
}

// nextYAMLEvent reads one event from a document that has been loaded whole.
func (r *Reader) nextYAMLEvent() (Document, error) {
	for len(r.evq) == 0 {
		d, err := r.nextYAML()
		if err != nil {
			return Document{}, err
		}
		r.evq, r.evLine = StreamEvents(d.Value), d.Line
	}
	ev := r.evq[0]
	r.evq = r.evq[1:]
	return Document{Line: r.evLine, Value: ev}, nil
}

// readBuffer is how much input the reader holds ahead, which is enough for
// a whole piece of a long line to be in view when the piece is taken.
const readBuffer = 1 << 16

func (r *Reader) start() error {
	r.started = true
	if r.src.Reader == nil {
		r.src.Reader = bytes.NewReader(nil)
	}
	r.br = bufio.NewReaderSize(r.src.Reader, readBuffer)
	if r.opts.RawLines {
		r.raw = true
		return nil
	}
	format := r.opts.Format
	if format == FormatAuto && r.opts.Seq {
		// A text sequence is JSON by definition.
		format = FormatJSON
	} else if format == FormatAuto {
		format = detect(r.br)
	}
	if format == FormatJSON {
		if b, _ := r.br.Peek(3); bytes.Equal(b, []byte{0xEF, 0xBB, 0xBF}) {
			_, _ = r.br.Discard(3)
		}
		r.json = NewJSONReader(r.br, r.src.Name, r.opts)
		return nil
	}
	data, err := io.ReadAll(r.br)
	if err != nil {
		return err
	}
	r.lines = bytes.Count(data, []byte{'\n'})
	if len(data) > 0 && data[len(data)-1] != '\n' {
		r.lines++
	}
	if r.opts.KeepMeta && !r.opts.NodeComments {
		r.leading, data = splitLeadingContent(data)
	}
	r.docs, err = yamlnode.Parse(data)
	if err != nil {
		return r.yamlError(err)
	}
	if len(r.docs) == 0 && r.leading != "" {
		// yq reads a source that holds only leading content as one null
		// document carrying it.
		null := &yamlnode.Node{Kind: yamlnode.ScalarNode, Tag: yamlnode.TagNull, Line: 1, Column: 1}
		r.docs = []*yamlnode.Node{{Kind: yamlnode.DocumentNode, Line: 1, Column: 1, Content: []*yamlnode.Node{null}}}
	}
	return nil
}

func (r *Reader) yamlError(err error) error {
	var pe *yamlnode.ParseError
	if errors.As(err, &pe) {
		return &SyntaxError{Source: r.src.Name, Line: pe.Line, Msg: pe.Message}
	}
	return err
}

func (r *Reader) nextYAML() (Document, error) {
	if r.next >= len(r.docs) {
		return Document{}, io.EOF
	}
	n := r.docs[r.next]
	r.next++
	fo := FromNodeOptions{KeepMeta: r.opts.KeepMeta, StringKeys: r.opts.StringKeys, AliasBudget: r.opts.AliasBudget, DepthLimit: r.opts.DepthLimit}
	v, anchors, err := convertDocument(n, fo, docSite{docIndex: r.docIndex, fileIndex: r.fileIndex, filename: r.src.Name})
	if err != nil {
		var ne *nodeError
		if errors.As(err, &ne) {
			return Document{}, &SyntaxError{Source: r.src.Name, Line: max(ne.Line, n.Line), Msg: ne.Msg}
		}
		return Document{}, &SyntaxError{Source: r.src.Name, Line: n.Line, Msg: err.Error()}
	}
	line := n.Line
	if len(n.Content) > 0 && n.Content[0].Line > 0 {
		line = n.Content[0].Line
	}
	d := Document{Line: line, Value: v, Anchors: anchors}
	switch {
	case !r.opts.KeepMeta:
	case r.next == 1:
		d.LeadingContent = r.leading
	default:
		// Every document after the first starts with a "---".
		d.LeadingContent = yqDocSeparator + "\n"
	}
	return d, nil
}

// yqDocSeparator stands for a "---" line in leading content, as in yq.
const yqDocSeparator = "$yqDocSeparator$"

// splitLeadingContent cuts the leading content off a YAML source the way
// yq does before it parses. That is every comment line, blank line,
// %YAML directive, and "---" before the first line of anything else, with
// each "---" written as yqDocSeparator. It returns the leading content
// and data with those lines left empty, so the parser never attaches the
// comments to the first node and the lines after keep their numbers.
//
// Content after a "---" on the same line is read as a line of its own,
// as yq does, but it keeps its line break, where yq joins it to the next
// line.
func splitLeadingContent(data []byte) (string, []byte) {
	var lead, blank strings.Builder
	rest := string(data)
	for rest != "" {
		line, newline, after := cutLine(rest)
		trimmed := strings.TrimSpace(line)
		if trimmed == "---" {
			lead.WriteString(yqDocSeparator + newline)
			blank.WriteString(newline)
			rest = after
			continue
		}
		if body, ok := cutSeparator(line); ok {
			sep := newline
			if sep == "" {
				sep = "\n"
			}
			lead.WriteString(yqDocSeparator + sep)
			rest = body + newline + after
			continue
		}
		left := strings.TrimLeft(line, yamlSpace)
		if trimmed != "" && !strings.HasPrefix(left, "#") && !strings.HasPrefix(left, "%YAML") {
			break
		}
		lead.WriteString(line + newline)
		blank.WriteString(newline)
		rest = after
	}
	if lead.Len() == 0 {
		return "", data
	}
	return lead.String(), []byte(blank.String() + rest)
}

// yamlSpace is what yq's \s matches.
const yamlSpace = " \t\n\f\r"

// cutLine splits the first line off s, with its line break apart.
func cutLine(s string) (line, newline, rest string) {
	i := strings.IndexByte(s, '\n')
	if i < 0 {
		return s, "", ""
	}
	line, rest = s[:i], s[i+1:]
	if strings.HasSuffix(line, "\r") {
		return line[:len(line)-1], "\r\n", rest
	}
	return line, "\n", rest
}

// cutSeparator returns what follows a "---" and the blanks after it at
// the start of line.
func cutSeparator(line string) (string, bool) {
	s := strings.TrimLeft(line, yamlSpace)
	if !strings.HasPrefix(s, "---") {
		return "", false
	}
	s = s[3:]
	body := strings.TrimLeft(s, yamlSpace)
	if body == s {
		return "", false
	}
	return body, true
}
