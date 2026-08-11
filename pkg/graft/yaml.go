package graft

import (
	"bytes"
	"regexp"
	"strconv"
	"strings"

	"github.com/goccy/go-yaml"
)

// MarshalYAML serializes a value to YAML with 2-space indentation,
// matching the output format expected by BOSH and CF ecosystem tools.
func MarshalYAML(v interface{}) ([]byte, error) {
	v = quoteSpecialFloatLookalikes(v)

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

// quoteSpecialFloatLookalikes walks v, replacing any string value that
// needsExplicitQuote flags with a forcedQuoteString so it survives a
// marshal/re-parse round trip as a string. It returns a new tree; the
// input is not mutated.
func quoteSpecialFloatLookalikes(v interface{}) interface{} {
	switch val := v.(type) {
	case string:
		if needsExplicitQuote(val) {
			return forcedQuoteString(val)
		}
		return val
	case map[string]interface{}:
		out := make(map[string]interface{}, len(val))
		for k, item := range val {
			out[k] = quoteSpecialFloatLookalikes(item)
		}
		return out
	case map[interface{}]interface{}:
		out := make(map[interface{}]interface{}, len(val))
		for k, item := range val {
			out[k] = quoteSpecialFloatLookalikes(item)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(val))
		for i, item := range val {
			out[i] = quoteSpecialFloatLookalikes(item)
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
var blockScalarHeaderRe = regexp.MustCompile(`(?:^|:|-)[ \t]*[|>][+-]?[0-9]*[ \t]*(#.*)?$`)

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

		next, nextIndent := nextSignificantLine(lines, i+1)
		if next == "" {
			continue // nothing follows -- a trailing bare dash at end-of-document parses correctly
		}
		if nextIndent > indent {
			continue // more-indented content is this item's own value, not the bug trigger
		}
		if next == "-" || strings.HasPrefix(next, "- ") || strings.HasPrefix(next, "-\t") {
			continue // sibling sequence item, not a mapping key -- parses correctly already
		}
		if next == "---" || next == "..." {
			continue // document boundary marker, not a mapping key
		}
		if !mapKeyLineRe.MatchString(next) {
			continue // doesn't look like a mapping key line; leave alone
		}

		lines[i] = strings.Repeat(" ", indent) + "- ~"
	}

	return []byte(strings.Join(lines, "\n"))
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
