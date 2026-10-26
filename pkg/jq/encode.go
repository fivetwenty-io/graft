package jq

import (
	"github.com/fivetwenty-io/graft/internal/query/jqout"
	"github.com/fivetwenty-io/graft/internal/query/yamlout"
	"github.com/fivetwenty-io/graft/pkg/query/value"
	"github.com/fivetwenty-io/graft/pkg/sortkeys"

	"io"
)

// defaultIndent is jq's indentation when no --indent is given.
const defaultIndent = 2

// EncodeOptions selects how an Encoder writes values. The fields after
// Format describe JSON text, and a YAML Encoder honors only Indent, Raw,
// Join, FloatNumbers, SortKeys, and Unbuffered.
type EncodeOptions struct {
	// Format is the output syntax, and FormatAuto writes JSON.
	Format Format
	// Indent is the spaces per level, as --indent sets it. Zero means jq's
	// default of two, and a negative Indent means --indent 0, which breaks
	// lines without indenting them. YAML indents by 2 through 7.
	Indent int
	// Tab indents with one tab per level, as --tab does.
	Tab bool
	// Compact writes each value on one line with no spaces, as -c does.
	Compact bool
	// Color writes JSON in color, as -C does, with jq 1.8's palette unless
	// Colors overrides it.
	Color bool
	// ASCII escapes every non-ASCII character, as -a does.
	ASCII bool
	// Raw writes a top-level string as its bare text, as -r does.
	Raw bool
	// RawNUL is Raw with a NUL byte after each value instead of a newline,
	// as --raw-output0 does, and refuses a string that holds a NUL.
	RawNUL bool
	// Join is Raw with nothing after each value, as -j does.
	Join bool
	// Seq writes an RS byte before each JSON text, as --seq does.
	Seq bool
	// FloatNumbers prints computed numbers the way jq 1.8.2 prints a double,
	// as --float-numbers does.
	FloatNumbers bool
	// SortKeys orders object keys, as -S and --sort-keys do. The empty mode
	// keeps input order, as sortkeys.None does.
	SortKeys sortkeys.Mode
	// Colors is a JQ_COLORS value applied over jq's palette. A value that
	// CheckColors refuses leaves the palette as it is, as jq does.
	Colors string
	// Unbuffered writes each value through to the writer as soon as it is
	// encoded, as --unbuffered does.
	Unbuffered bool
}

// Encoder writes values to a writer. It buffers its output, so the caller
// calls Flush when it is done.
type Encoder struct {
	enc        jqout.Encoder
	unbuffered bool
}

// NewEncoder returns an Encoder that writes to w as opts describe.
func NewEncoder(w io.Writer, opts EncodeOptions) *Encoder {
	indent := opts.Indent
	switch {
	case indent == 0:
		indent = defaultIndent
	case indent < 0:
		indent = 0
	}
	if opts.Format == FormatYAML {
		return &Encoder{
			enc: yamlout.New(w, yamlout.Options{
				Indent:       indent,
				UnwrapScalar: opts.Raw || opts.Join,
				FloatNumbers: opts.FloatNumbers,
				SortKeys:     opts.SortKeys,
			}),
			unbuffered: opts.Unbuffered,
		}
	}
	palette := jqout.DefaultPalette
	if opts.Colors != "" {
		palette, _ = jqout.ParsePalette(opts.Colors)
	}
	return &Encoder{enc: jqout.New(w, jqout.Options{
		Indent:       indent,
		Tab:          opts.Tab,
		Compact:      opts.Compact,
		Raw:          opts.Raw,
		Join:         opts.Join,
		RawNUL:       opts.RawNUL,
		ASCII:        opts.ASCII,
		Seq:          opts.Seq,
		FloatNumbers: opts.FloatNumbers,
		SortKeys:     opts.SortKeys,
		Color:        opts.Color,
		Palette:      palette,
		Unbuffered:   opts.Unbuffered,
	})}
}

// Encode writes v followed by its terminator.
func (e *Encoder) Encode(v *value.Value) error {
	if err := e.enc.Write(v); err != nil {
		return err
	}
	if e.unbuffered {
		return e.enc.Flush()
	}
	return nil
}

// Flush writes any buffered output to the writer.
func (e *Encoder) Flush() error { return e.enc.Flush() }

// CheckColors reports whether jqColors is a JQ_COLORS value jq accepts, so
// a command can warn as jq does before it falls back to the default
// palette.
func CheckColors(jqColors string) error {
	_, err := jqout.ParsePalette(jqColors)
	return err
}
