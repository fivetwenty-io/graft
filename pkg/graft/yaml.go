package graft

import (
	"bytes"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
)

// MarshalYAML serializes a value to YAML with 2-space indentation,
// matching the output format expected by BOSH and CF ecosystem tools.
// Map keys are emitted in spruce's two-tier order (see spruceKeyLess).
// String values that cannot be written as plain scalars for syntax
// reasons are single-quoted like spruce's emitter (see
// prefersSingleQuote); type-lookalike strings keep goccy's double
// quotes, which is also what spruce emits for those.
func MarshalYAML(v interface{}) ([]byte, error) {
	v = prepareForEncode(v)

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf, yaml.Indent(2))
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// specialFloatLookalikeRe narrows down candidate strings before paying
// the cost of a real parse-verification check in needsExplicitQuote: an
// optional sign followed by a case-insensitive "inf" or "nan" behind a
// leading dot, e.g. ".nan", "-.Inf", ".INF".
var specialFloatLookalikeRe = regexp.MustCompile(`(?i)^[-+]?\.(inf|nan)$`)

// needsExplicitQuote reports whether s, written as a bare (unquoted)
// YAML plain scalar, would be re-parsed back as something other than a
// string. goccy/go-yaml v1.19.2 recognizes ".nan"/".inf"/"-.inf" (and
// their case variants) as reserved float keywords when *parsing*
// (token.reservedInfKeywords / reservedNanKeywords are registered into
// its parse-time keyword table), but its encode-time quoting-need check
// never learns about them -- that same init() only copies the null and
// standard bool keyword tables into its encode-time lookup, not the
// inf/nan ones. The result: goccy writes these strings back out
// unquoted, and a re-parse silently turns the original string into a
// float. spruce (yaml.v2-family) quotes them. This performs an actual
// round-trip check through goccy's own parser rather than hardcoding
// its keyword table, so the guard keeps working even if a future goccy
// version changes that table -- it only pays the parse cost for strings
// that already look like plausible candidates.
func needsExplicitQuote(s string) bool {
	if !specialFloatLookalikeRe.MatchString(s) {
		return false
	}
	var reparsed interface{}
	if err := yaml.Unmarshal([]byte(s), &reparsed); err != nil {
		return false // not a parseable bare scalar; goccy handles quoting some other way
	}
	_, isString := reparsed.(string)
	return !isString
}

// forcedQuoteString marshals as an explicitly double-quoted YAML plain
// scalar via goccy's BytesMarshaler hook, bypassing goccy's own
// (incomplete) quoting-need heuristic for values needsExplicitQuote has
// verified would otherwise re-parse as a different type.
type forcedQuoteString string

// MarshalYAML implements github.com/goccy/go-yaml's BytesMarshaler.
func (s forcedQuoteString) MarshalYAML() ([]byte, error) {
	return []byte(strconv.Quote(string(s))), nil
}

// prefersSingleQuote reports whether s is a string spruce's emitter
// writes single-quoted: one that cannot be written as a plain scalar
// for syntax reasons (a leading `*`, `&`, `%`, an opening bracket, and
// so on — anything goccy's parser rejects outright as a bare scalar)
// yet contains nothing that needs double-quote escapes. Type-lookalike
// strings ("1.0", "yes", "null") parse fine as plain scalars — just to
// a different type — and are excluded here: both spruce and goccy
// double-quote those. The distinction matters beyond aesthetics:
// genesis's Credhub entombment step regex-replaces `((...))` with `""`
// inside the rendered manifest and re-parses it, which stays valid
// YAML inside single quotes (`'*.uaa.""'`) but is malformed inside
// double quotes (`"*.uaa."""`).
func prefersSingleQuote(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false // needs escapes; leave to goccy's double-quote style
		}
	}
	var reparsed interface{}
	return yaml.Unmarshal([]byte(s), &reparsed) != nil
}

// singleQuotedString marshals as an explicitly single-quoted YAML
// scalar via goccy's BytesMarshaler hook, for values prefersSingleQuote
// has classified as spruce's single-quote class.
type singleQuotedString string

// MarshalYAML implements github.com/goccy/go-yaml's BytesMarshaler.
func (s singleQuotedString) MarshalYAML() ([]byte, error) {
	return []byte("'" + strings.ReplaceAll(string(s), "'", "''") + "'"), nil
}

// prepareForEncode walks v, rebuilding the tree for the encoder: every
// map becomes a yaml.MapSlice with its keys in spruceKeyLess order
// (goccy's own encoder would sort them purely lexicographically), and
// any string value that needsExplicitQuote flags becomes a
// forcedQuoteString so it survives a marshal/re-parse round trip as a
// string. It returns a new tree; the input is not mutated.
func prepareForEncode(v interface{}) interface{} {
	switch val := v.(type) {
	case string:
		if needsExplicitQuote(val) {
			return forcedQuoteString(val)
		}
		if prefersSingleQuote(val) {
			return singleQuotedString(val)
		}
		return val
	case map[string]interface{}:
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool {
			return spruceKeyLess(keys[i], keys[j])
		})
		out := make(yaml.MapSlice, 0, len(keys))
		for _, k := range keys {
			out = append(out, yaml.MapItem{Key: k, Value: prepareForEncode(val[k])})
		}
		return out
	case map[interface{}]interface{}:
		// Stringify keys before building MapItems: goccy's MapSlice
		// encoder type-asserts each key .(string) unchecked and would
		// panic on anything else.
		converted := make(map[string]interface{}, len(val))
		for k, item := range val {
			converted[fmt.Sprintf("%v", k)] = item
		}
		return prepareForEncode(converted)
	case []interface{}:
		out := make([]interface{}, len(val))
		for i, item := range val {
			out[i] = prepareForEncode(item)
		}
		return out
	default:
		return v
	}
}

// bareDashLineRe matches a block-sequence item line consisting only of a
// dash (i.e. no value token at all), optionally followed by a comment.
var bareDashLineRe = regexp.MustCompile(`^-[ \t]*(#.*)?$`)

// mapKeyLineRe matches a plain, single-, or double-quoted YAML mapping
// key at the start of a line.
var mapKeyLineRe = regexp.MustCompile(`^(?:"[^"]*"|'[^']*'|[^-#\s][^:]*?):(\s|$)`)

// blockScalarHeaderRe matches a line that opens a literal (|) or folded
// (>) block scalar, optionally preceded by a mapping key or sequence
// dash, and optionally followed by chomping/indent indicators and a
// trailing comment.
var blockScalarHeaderRe = regexp.MustCompile(`(?:^|:|-)[ \t]*[|>][+-]?\d*[ \t]*(#.*)?$`)

// sanitizeBareSequenceTerminators works around a goccy/go-yaml v1.19.2
// parser bug (confirmed against a standalone goccy repro; v1.19.2 is the
// latest available release, so no upstream version bump fixes it): a
// block-sequence item consisting of a bare "-" with no value token,
// immediately followed by a sibling mapping key at the same or a
// shallower indent, gets misparsed -- the sibling key is silently
// nested inside the empty sequence item instead of terminating the
// sequence, corrupting the document structure.
//
// spruce (yaml.v2-family) parses the same bare "-" as an explicit null
// list entry and keeps the following key as a sibling. This function
// rewrites the bare "-" line to "- ~" (an explicit null) before the
// data reaches goccy's parser, matching spruce's semantics and closing
// off the misparse for the specific pattern that triggers it.
//
// It tracks literal/folded block scalars (| and >) by indentation and
// skips lines inside them, so a "-" appearing as literal text inside a
// multi-line string is never rewritten. A bare dash followed by another
// sequence item (rather than a mapping key) is left untouched, since
// that shape does not trigger the goccy bug.
//
// This is a text-level heuristic, not a full YAML parse: it does not
// track flow-style collections or tag/anchor edge cases. Those shapes
// do not exhibit the misparse being guarded against here.
func sanitizeBareSequenceTerminators(data []byte) []byte {
	if len(data) == 0 {
		return data
	}

	lines := strings.Split(string(data), "\n")

	inBlockScalar := false
	blockScalarIndent := 0

	for i, line := range lines {
		trimmed := strings.TrimRight(line, " \t\r")
		content := strings.TrimLeft(trimmed, " \t")
		indent := len(trimmed) - len(content)

		if inBlockScalar {
			if content == "" {
				continue // blank lines never terminate a block scalar
			}
			if indent > blockScalarIndent {
				continue // still inside the scalar body
			}
			inBlockScalar = false // dedented back out; fall through and reprocess this line
		}

		if blockScalarHeaderRe.MatchString(content) {
			inBlockScalar = true
			blockScalarIndent = indent
			continue
		}

		if !bareDashLineRe.MatchString(content) {
			continue
		}

		if bareDashEndsSequence(lines, i+1, indent) {
			lines[i] = strings.Repeat(" ", indent) + "- ~"
		}
	}

	return []byte(strings.Join(lines, "\n"))
}

// bareDashEndsSequence reports whether the bare dash at indent, whose
// following lines start at index from, is the trailing item goccy
// misparses: the next significant line has to be a mapping key at or
// outside the dash's own indentation. Anything else -- end of document,
// the dash's own more-indented value, a sibling sequence item, a document
// boundary, or a line that is not a mapping key at all -- parses
// correctly as written and is left alone.
func bareDashEndsSequence(lines []string, from, indent int) bool {
	next, nextIndent := nextSignificantLine(lines, from)
	switch {
	case next == "":
		return false
	case nextIndent > indent:
		return false
	case next == "-" || strings.HasPrefix(next, "- ") || strings.HasPrefix(next, "-\t"):
		return false
	case next == "---" || next == "...":
		return false
	}
	return mapKeyLineRe.MatchString(next)
}

// nextSignificantLine returns the trimmed content and indentation of the
// next non-blank, non-comment-only line starting at index from, or ""
// if none remain.
func nextSignificantLine(lines []string, from int) (content string, indent int) {
	for i := from; i < len(lines); i++ {
		raw := strings.TrimRight(lines[i], " \t\r")
		c := strings.TrimLeft(raw, " \t")
		if c == "" || strings.HasPrefix(c, "#") {
			continue
		}
		return c, len(raw) - len(c)
	}
	return "", 0
}
