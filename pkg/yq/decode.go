package yq

import (
	"errors"
	"io"

	"github.com/fivetwenty-io/graft/internal/query/load"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// Format names a text syntax for input or output.
type Format int

// The formats. The zero Format is YAML, which also reads JSON, because
// JSON is YAML's flow style.
const (
	FormatYAML Format = iota
	FormatJSON
)

// Document is where one input document came from. A Decoder records the
// same four facts in the Meta of every value it returns, as DocIndex,
// FileIndex, and Filename, and the Meta of the root also holds the
// document's line.
type Document struct {
	// Value is the document's root.
	Value *value.Value
	// Index is the document's position in its file, counted from zero.
	Index int
	// FileIndex is the file's position among the inputs, counted from zero.
	FileIndex int
	// Filename names the file in error messages and in the filename
	// operator.
	Filename string
}

// DecodeOptions selects how a Decoder reads its source.
type DecodeOptions struct {
	// Format is the input syntax. FormatYAML reads JSON too.
	Format Format
	// Filename names the source in error messages and in the filename
	// operator.
	Filename string
	// FileIndex is the source's position among the inputs, which the
	// file_index operator reports and which decides where an Encoder
	// writes a "---" line.
	FileIndex int
	// FrontMatter is yq's --front-matter setting, either "extract",
	// "process", or empty for a source with no front matter. A value
	// other than empty makes Next fail until the release that completes
	// graft yq supports it.
	FrontMatter string
}

// Decoder reads documents from a source one at a time. Nothing is read
// until the first Next, and a document that does not parse is reported only
// when Next reaches it.
type Decoder struct {
	r   *load.Reader
	err error
}

// NewDecoder returns a Decoder that reads r as opts describe. The values it
// returns carry their comments, styles, anchors, tags, line, column, and
// document index in Meta, which is what lets an expression read them and an
// Encoder write them back.
func NewDecoder(r io.Reader, opts DecodeOptions) *Decoder {
	d := &Decoder{}
	if opts.FrontMatter != "" {
		d.err = errors.New("front matter is not supported yet")
		return d
	}
	lo := load.LoadOptions{Format: load.FormatYAML, KeepMeta: true}
	if opts.Format == FormatJSON {
		lo.Format = load.FormatJSON
	}
	d.r = load.Open(load.Source{Name: opts.Filename, Reader: r}, opts.FileIndex, lo)
	return d
}

// Next returns the next document's root, and io.EOF after the last one. A
// comment block before the first node stays with the document as its
// leading content, so an Encoder prints it. An error sticks, so later calls
// return it again.
func (d *Decoder) Next() (*value.Value, error) {
	if d.err != nil {
		return nil, d.err
	}
	doc, err := d.r.Next()
	if err != nil {
		d.err = err
		return nil, err
	}
	return rooted(doc), nil
}

// rooted is the document's root carrying the position and the leading
// content that the document held beside it.
func rooted(doc load.Document) *value.Value {
	var m value.Meta
	if dm := doc.Value.Meta(); dm != nil {
		m = *dm
	}
	m.DocIndex, m.FileIndex, m.Filename = doc.DocIndex, doc.FileIndex, doc.Source
	m.LeadingContent = doc.LeadingContent
	if m.Line == 0 {
		m.Line = doc.Line
	}
	return doc.Value.WithMeta(&m)
}
