package graft

import (
	"errors"

	"github.com/fivetwenty-io/graft/internal/yamlcodec"
)

// YAMLHeadComment attaches a block of comment lines (rendered "# "-prefixed,
// one per entry in Lines) directly above one node in MarshalYAMLWithComments'
// output. Path uses graft/spruce's dotted document-path syntax (e.g.
// "meta.password", or "jobs.0.name" for a list index - no leading "$.");
// MarshalYAMLWithComments converts it to the bracket-indexed form
// goccy/go-yaml's own path syntax requires ("$.jobs[0].name") internally,
// so callers never need to know that detail (or depend on goccy/go-yaml's
// types directly).
type YAMLHeadComment = yamlcodec.YAMLHeadComment

// YAMLCompat controls YAML 1.1 backward compatibility behavior.
type YAMLCompat = yamlcodec.YAMLCompat

// MarshalYAML serializes a value to YAML with 2-space indentation, matching
// the output format expected by BOSH and CF ecosystem tools. Map keys are
// emitted in spruce's two-tier order. Every string, key or value, is written
// in the style spruce's emitter picks for it (plain, single-quoted,
// double-quoted, or a literal block), folded at the same column, and keys
// spruce cannot write as simple keys use the explicit "? key" form.
func MarshalYAML(v interface{}) ([]byte, error) {
	return yamlcodec.MarshalYAML(v)
}

// MarshalYAMLWith serializes v exactly like MarshalYAML, but orders every
// map's keys with less instead of spruce's order. A nil less keeps spruce's
// order.
func MarshalYAMLWith(v interface{}, less func(a, b string) bool) ([]byte, error) {
	return yamlcodec.MarshalYAMLWith(v, less)
}

// MarshalYAMLWithComments serializes v exactly like MarshalYAML (same spruce
// key ordering and quoting rules, since both share one implementation and
// MarshalYAML is the call with no comments), then additionally attaches each
// entry in comments as a head comment placed directly above the node at its
// Path.
//
// A comments entry whose Path cannot be converted to a valid goccy/go-yaml
// path is skipped rather than failing the whole encode: comment placement is
// best-effort, never load-bearing for the document's own data. cmd/graft's
// "graft merge --report-deferred=inline" is the only current caller,
// attaching one comment per deferred key directly above that key.
func MarshalYAMLWithComments(v interface{}, comments []YAMLHeadComment) ([]byte, error) {
	return yamlcodec.MarshalYAMLWithComments(v, comments)
}

// MarshalYAMLWithCommentsWith serializes v exactly like
// MarshalYAMLWithComments, but orders every map's keys with less. A nil less
// keeps spruce's order.
func MarshalYAMLWithCommentsWith(v interface{}, comments []YAMLHeadComment, less func(a, b string) bool) ([]byte, error) {
	return yamlcodec.MarshalYAMLWithCommentsWith(v, comments, less)
}

// QuoteInjectKeys pre-processes YAML bytes to quote the graft-specific <<<:
// inject key, which goccy/go-yaml rejects when unquoted because it reads <<<
// as a variant of the YAML merge key <<. It handles both the standalone (<<<:)
// and dotted path (foo.<<<:) forms.
func QuoteInjectKeys(data []byte) []byte {
	return yamlcodec.QuoteInjectKeys(data)
}

// NormalizeMap deep-converts any map[interface{}]interface{} values to
// map[string]interface{} throughout the tree. This is needed because values
// that other decoders produce, such as yaml.v2 through go-patch, use
// map[interface{}]interface{} when maps contain non-string keys (e.g.,
// integer keys like 1:, 2:). goccy decodes those same maps into
// map[string]interface{} with stringified keys.
func NormalizeMap(data map[string]interface{}) map[string]interface{} {
	return yamlcodec.NormalizeMap(data)
}

// DefaultYAMLCompat returns compat settings with YAML 1.1 booleans enabled.
func DefaultYAMLCompat() *YAMLCompat {
	return yamlcodec.DefaultYAMLCompat()
}

// ParseYAML11CompatAware parses data the same way yaml.Unmarshal(data,
// &interface{}{}) would, except every explicitly-quoted YAML 1.1
// boolean-lookalike scalar ("yes", 'On', "OFF", ...) is protected from
// YAMLCompat's later yes/no/on/off -> bool coercion. Unquoted occurrences of
// those words decode as plain strings exactly as before, so ConvertMapValues
// keeps converting them.
//
// Quoting information only exists at the token level -- once a document
// is decoded into interface{} values, a quoted and an unquoted "yes"
// are indistinguishable Go strings. This parses data into goccy's AST,
// tags quoted matches using the AST's own token type (not a text-level
// guess), then decodes the (locally mutated) AST back into the same generic
// shape a direct yaml.Unmarshal call would produce.
//
// Callers must run the result through UnprotectYAML11QuotedBools after
// applying YAMLCompat, unconditionally, so a tagged value that YAMLCompat left
// alone (e.g. compat disabled) still comes out as the original word rather
// than the internal marker-prefixed string.
//
// Library callers pass raw input through FirstMergeDocument first, since
// goccy's parser stalls on nesting that function fails cheaply.
func ParseYAML11CompatAware(data []byte) (interface{}, error) {
	return yamlcodec.ParseYAML11CompatAware(data)
}

// FirstMergeDocument returns the first document of data, raw YAML bytes about
// to be merged, since the merge reads no other, as spruce's does. goccy parses
// every document it is given, so a syntax error or deep nesting in a later
// document would otherwise fail the merge. It strips a leading UTF-8 byte
// order mark first, as spruce does, since goccy reads one as text. The result
// is a prefix of the rest of data, with every CRLF and lone CR turned into
// an LF, and it is all of that rest when nothing ends the first document.
//
// It fails a first document that nests deeper than 10,000 levels, and it
// runs before anything else tokenizes data, because goccy's parser slows
// down faster than linearly with depth, and 20,000 unclosed brackets cost it
// hundreds of megabytes before it fails. The error reads as the text a merge
// gives for a tree nested past 4,096 levels, so every over-deep merge fails
// with one message whatever its depth.
//
// Content after the "..." that ends the first document needs a "---" before
// it, and spruce fails a file without one. The merge must not drop that
// content, so FirstMergeDocument fails it with a parse error that carries
// yaml.v3's message and line.
func FirstMergeDocument(data []byte) ([]byte, error) {
	first, err := yamlcodec.FirstMergeDocument(data)
	if err != nil {
		var tooDeep *yamlcodec.MaxRecursionError
		if errors.As(err, &tooDeep) {
			return nil, err
		}
		return nil, NewParseError(err.Error(), err)
	}
	return first, nil
}

// CheckSelfContainingAnchors fails data, YAML bytes about to be decoded,
// when an alias in its first document sits inside the collection its anchor
// names, with the message spruce gives. goccy decodes such an alias as null,
// so without the check the value would quietly vanish. A syntax error passes
// the check, so the caller's own decode reports it. Library callers pass
// raw input through FirstMergeDocument first, since goccy's parser stalls on
// nesting that function fails cheaply.
func CheckSelfContainingAnchors(data []byte) error {
	return yamlcodec.CheckSelfContainingAnchors(data)
}

// UnprotectYAML11QuotedBools reverses the tagging ParseYAML11CompatAware
// applied: it walks v, stripping the internal marker from any string value
// that still carries it, restoring the original bare word. Mutates map and
// slice values in place; returns the (possibly replaced) root value, mirroring
// ConvertMapValues' in-place style.
func UnprotectYAML11QuotedBools(v interface{}) interface{} {
	return yamlcodec.UnprotectYAML11QuotedBools(v)
}
