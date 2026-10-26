package yq

import (
	"io"

	"github.com/fivetwenty-io/graft/internal/query/jqout"
	"github.com/fivetwenty-io/graft/internal/query/yamlout"
	"github.com/fivetwenty-io/graft/pkg/query/value"
	"github.com/fivetwenty-io/graft/pkg/sortkeys"
)

// defaultIndent is yq's indentation when no -I is given.
const defaultIndent = 2

// EncodeOptions selects how an Encoder writes values.
type EncodeOptions struct {
	// Format is the output syntax, and the zero Format is YAML.
	Format Format
	// Indent is the spaces per level. Zero means yq's default of two. A
	// negative Indent means -I 0, which writes JSON on one line with no
	// spaces, and which YAML clamps to its minimum of two.
	Indent int
	// Colors writes JSON in color, as -C does. A YAML Encoder does not
	// color its output yet.
	Colors bool
	// UnwrapScalar writes a top-level string as its bare text, as -r
	// does, which is yq's default at the command line.
	UnwrapScalar bool
	// NoDocSeparators leaves out the "---" line between YAML documents,
	// as -N does.
	NoDocSeparators bool
	// PrettyPrint drops the quoting, flow, and block styles of YAML
	// output while keeping explicit tags, as -P does.
	PrettyPrint bool
	// SortKeys orders object keys. The empty mode keeps input order.
	SortKeys sortkeys.Mode
}

// Encoder writes values to a writer. It buffers its output, so the caller
// calls Flush when it is done.
type Encoder struct {
	yaml   yamlout.Encoder
	json   jqout.Encoder
	pretty bool
}

// NewEncoder returns an Encoder that writes to w as opts describe.
func NewEncoder(w io.Writer, opts EncodeOptions) *Encoder {
	indent := opts.Indent
	compact := indent < 0
	if indent == 0 {
		indent = defaultIndent
	}
	if opts.Format == FormatJSON {
		return &Encoder{json: jqout.New(w, jqout.Options{
			Indent:   max(indent, 0),
			Compact:  compact,
			Raw:      opts.UnwrapScalar,
			SortKeys: opts.SortKeys,
			Color:    opts.Colors,
			Palette:  jqout.DefaultPalette,
		})}
	}
	return &Encoder{
		yaml: yamlout.New(w, yamlout.Options{
			Indent:       indent,
			NoDoc:        opts.NoDocSeparators,
			UnwrapScalar: opts.UnwrapScalar,
			SortKeys:     opts.SortKeys,
		}),
		pretty: opts.PrettyPrint,
	}
}

// Encode writes v followed by its terminator. A YAML Encoder writes the
// document leading content and the "---" line that the value's Meta calls
// for, which is what a value from a Decoder or an Evaluator carries.
func (e *Encoder) Encode(v *value.Value) error {
	if v == nil {
		v = value.Null()
	}
	if e.json != nil {
		return e.json.Write(v)
	}
	if e.pretty {
		v = plain(v)
	}
	return e.yaml.WriteDocument(documentOf(v), v)
}

// Flush writes any buffered output to the writer.
func (e *Encoder) Flush() error {
	if e.json != nil {
		return e.json.Flush()
	}
	return e.yaml.Flush()
}

// plain is v with every quoting, flow, and block style cleared, as
// "... style=\"\"" leaves it. An explicit tag stays.
func plain(v *value.Value) *value.Value {
	out := v
	switch v.Kind() {
	case value.KindArray:
		b := value.NewArrBuilderFrom(v)
		for i, e := range v.Elems() {
			b.Set(i, plain(e))
		}
		out = b.Seal()
	case value.KindObject:
		b := value.NewObjBuilderFrom(v)
		for _, ent := range v.Entries() {
			b.SetEntry(plain(ent.Key), plain(ent.Value))
		}
		out = b.Seal()
	default:
	}
	m := out.Meta()
	if m == nil || m.Style&^value.Tagged == 0 {
		return out
	}
	c := *m
	c.Style &= value.Tagged
	return out.WithMeta(&c)
}
