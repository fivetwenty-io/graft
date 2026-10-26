// Package load turns JSON, YAML, and raw input into the query value model.
// It reads one document at a time, so a broken value is reported only when
// the caller asks for it.
package load

import (
	"fmt"
	"io"
	"strings"

	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// Format names an input syntax.
type Format int

// The input formats. FormatAuto commits to one of the other two from the
// first token and never retries.
const (
	FormatAuto Format = iota
	FormatJSON
	FormatYAML
)

// Source is a named stream of input.
type Source struct {
	Name   string
	Reader io.Reader
}

// LoadOptions selects how a source is read. Slurping is the command's
// business and has no option here.
//
//nolint:revive // LoadOptions is the name the loader's callers are specified against.
type LoadOptions struct {
	Format   Format
	RawLines bool
	KeepMeta bool
	// NodeComments leaves the comments before a YAML source's first node
	// to the parser, as yq's load and from_yaml do, rather than cutting
	// them off as leading content.
	NodeComments bool
	StringKeys   bool
	Seq          bool
	Stream       bool
	DepthLimit   int
	AliasBudget  int
}

// Document is one value read from a source.
type Document struct {
	Source    string
	FileIndex int
	DocIndex  int
	Line      int
	Value     *value.Value
	Anchors   map[string]*value.Value
	// LeadingContent is what comes before the first node of a source
	// under KeepMeta, which is its comment lines, blank lines, %YAML
	// directives, and "---" lines, with each "---" written as
	// "$yqDocSeparator$". A later document holds only that marker, for the
	// "---" that opened it. It is empty without KeepMeta.
	LeadingContent string
}

// SyntaxError is a parse failure with a position. Col is 0 when the
// parser reports only a line.
type SyntaxError struct {
	Source    string
	Line, Col int
	Msg       string
}

func (e *SyntaxError) Error() string {
	var b strings.Builder
	if e.Source != "" {
		b.WriteString(e.Source)
		b.WriteString(": ")
	}
	switch {
	case e.Line > 0 && e.Col > 0:
		fmt.Fprintf(&b, "line %d, column %d: ", e.Line, e.Col)
	case e.Line > 0:
		fmt.Fprintf(&b, "line %d: ", e.Line)
	}
	b.WriteString(e.Msg)
	return b.String()
}
