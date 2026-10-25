package load

import (
	"bufio"
	"bytes"
	"errors"
	"io"

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
	next     int
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
	case r.json != nil:
		v, line, err := r.json.Next()
		if err != nil {
			return Document{}, err
		}
		return Document{Line: line, Value: v}, nil
	}
	return r.nextYAML()
}

func (r *Reader) start() error {
	r.started = true
	if r.opts.Stream {
		return errors.New("--stream is not supported by the loader yet")
	}
	if r.src.Reader == nil {
		r.src.Reader = bytes.NewReader(nil)
	}
	r.br = bufio.NewReader(r.src.Reader)
	if r.opts.RawLines {
		r.raw = true
		return nil
	}
	format := r.opts.Format
	if format == FormatAuto {
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
	r.docs, err = yamlnode.Parse(data)
	if err != nil {
		return r.yamlError(err)
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
	return Document{Line: line, Value: v, Anchors: anchors, LeadingContent: n.HeadComment}, nil
}
