package jq

import (
	"errors"
	"io"
	"strings"

	"github.com/fivetwenty-io/graft/internal/query/load"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// Format names a text syntax for input or output.
type Format int

// The formats. FormatAuto reads JSON or YAML, choosing from the first
// token of each source, and writes JSON.
const (
	FormatAuto Format = iota
	FormatJSON
	FormatYAML
)

// DecodeOptions selects how a Decoder reads its source.
type DecodeOptions struct {
	// Format is the input syntax, and FormatAuto chooses JSON or YAML from
	// the source's first token, as --input-format auto does.
	Format Format
	// Raw reads one string per line instead of parsing, as -R does.
	Raw bool
	// Seq reads RS-separated JSON texts, as --seq does.
	Seq bool
	// Stream reads the streaming event form, as --stream does: every
	// value comes out as the [path, leaf] events jq's tostream makes of it,
	// followed by a closing [path] event for each container. A JSON source
	// is read as it arrives, so an event comes out as soon as the text
	// after it shows that it is complete.
	Stream bool
	// StreamErrors is Stream with a failure to parse reported as a final
	// [message, path] event, as --stream-errors does, instead of as an
	// error from Next.
	StreamErrors bool
	// Warn receives the message for each damaged text that Seq skips
	// over, which jq reports on stderr as it reads on. It may be nil.
	Warn func(message string)
	// Filename names the source in error messages.
	Filename string
	// DepthLimit bounds how deeply a value may nest, and zero means jq's
	// limit of 10,000.
	DepthLimit int
	// AliasBudget bounds the nodes that YAML alias expansion may produce,
	// and zero means one million.
	AliasBudget int
}

// Decoder reads values from a source one at a time. Nothing is read until
// the first Next, and a value that does not parse is reported only when
// Next reaches it.
type Decoder struct {
	r   *load.Reader
	raw bool

	// A Decoder from Slurp has slurp set and reads its parts.
	slurp bool
	parts []*Decoder
	cur   int
	done  bool
	err   error
}

// NewDecoder returns a Decoder that reads r as opts describe.
func NewDecoder(r io.Reader, opts DecodeOptions) *Decoder {
	lo := load.LoadOptions{
		Format:       loadFormat(opts.Format),
		RawLines:     opts.Raw,
		Seq:          opts.Seq,
		Stream:       opts.Stream || opts.StreamErrors,
		StreamErrors: opts.StreamErrors,
		Warn:         opts.Warn,
		DepthLimit:   opts.DepthLimit,
		AliasBudget:  opts.AliasBudget,
	}
	return &Decoder{r: load.Open(load.Source{Name: opts.Filename, Reader: r}, 0, lo), raw: opts.Raw}
}

func loadFormat(f Format) load.Format {
	switch f {
	case FormatJSON:
		return load.FormatJSON
	case FormatYAML:
		return load.FormatYAML
	default:
		return load.FormatAuto
	}
}

// Slurp returns a Decoder whose one value holds everything decoders read,
// in order, as -s does across every input file. When every decoder is raw
// that value is a string holding their text exactly, newlines included,
// and otherwise it is an array of their values. Its Next returns io.EOF
// after that value. With no decoders the value is an empty array.
func Slurp(decoders ...*Decoder) *Decoder {
	raw := len(decoders) > 0
	for _, d := range decoders {
		raw = raw && d.raw
	}
	return &Decoder{raw: raw, slurp: true, parts: decoders}
}

// Next returns the next value, and io.EOF after the last one. An error
// sticks, so later calls return it again.
func (d *Decoder) Next() (*value.Value, error) {
	if !d.slurp {
		doc, err := d.r.Next()
		if err != nil {
			return nil, err
		}
		return doc.Value, nil
	}
	if d.err != nil {
		return nil, d.err
	}
	if d.done {
		return nil, io.EOF
	}
	v, err := d.drain()
	if err != nil {
		d.err = err
		return nil, err
	}
	d.done = true
	return v, nil
}

// drain reads every part into one value.
func (d *Decoder) drain() (*value.Value, error) {
	if d.raw {
		var b strings.Builder
		for ; d.cur < len(d.parts); d.cur++ {
			text, err := d.parts[d.cur].rawText()
			if err != nil {
				return nil, err
			}
			b.WriteString(text)
		}
		return value.Str(b.String()), nil
	}
	arr := value.NewArrBuilder(0)
	for ; d.cur < len(d.parts); d.cur++ {
		for {
			v, err := d.parts[d.cur].Next()
			if errors.Is(err, io.EOF) {
				break
			}
			if err != nil {
				return nil, err
			}
			arr.Append(v)
		}
	}
	return arr.Seal(), nil
}

// rawText returns the rest of a raw Decoder's text as one string.
func (d *Decoder) rawText() (string, error) {
	if !d.slurp {
		return d.r.RawText()
	}
	v, err := d.Next()
	if errors.Is(err, io.EOF) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return v.Str(), nil
}

// Line returns the number of lines read so far from the source the last
// value came from, which is what input_line_number reports.
func (d *Decoder) Line() int {
	if !d.slurp {
		return d.r.Line()
	}
	if len(d.parts) == 0 {
		return 0
	}
	return d.parts[min(d.cur, len(d.parts)-1)].Line()
}
