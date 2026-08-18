package graft

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/token"
)

// injectKeyStandaloneRe matches <<<: as a standalone map key.
var injectKeyStandaloneRe = regexp.MustCompile(`(?m)^(\s*(?:- )?)<<<:`)

// injectKeyDottedRe matches <<<: at the end of a dotted path key (e.g., host.web1.<<<:).
var injectKeyDottedRe = regexp.MustCompile(`(?m)^(\s*(?:- )?)(\S+\.<<<):`)

// QuoteInjectKeys pre-processes YAML bytes to quote the graft-specific
// <<<: inject key, which goccy/go-yaml rejects when unquoted because
// it interprets <<< as a variant of the YAML merge key <<.
// Handles both standalone (<<<:) and dotted path (foo.<<<:) forms.
func QuoteInjectKeys(data []byte) []byte {
	// Both regexes require a literal "<<<"; almost no document contains
	// one, so skip the two full-buffer regex passes when none is present
	// and hand the caller back the original slice.
	if !bytes.Contains(data, []byte("<<<")) {
		return data
	}

	// First quote dotted paths (must be first to avoid double-quoting)
	data = injectKeyDottedRe.ReplaceAll(data, []byte(`${1}"${2}":`))
	// Then quote standalone <<<:
	data = injectKeyStandaloneRe.ReplaceAll(data, []byte(`${1}"<<<":`))
	return data
}

// NormalizeMap deep-converts any map[interface{}]interface{} values
// to map[string]interface{} throughout the tree. This is needed because
// yaml.v3 produces map[interface{}]interface{} when maps contain
// non-string keys (e.g., integer keys like 1:, 2:).
func NormalizeMap(data map[string]interface{}) map[string]interface{} {
	if data == nil {
		return nil
	}
	for k, v := range data {
		data[k] = normalizeValue(v)
	}
	return data
}

func normalizeValue(v interface{}) interface{} {
	switch val := v.(type) {
	case map[interface{}]interface{}:
		converted := make(map[string]interface{}, len(val))
		for k, v := range val {
			converted[fmt.Sprintf("%v", k)] = normalizeValue(v)
		}
		return converted
	case map[string]interface{}:
		return NormalizeMap(val)
	case []interface{}:
		for i, item := range val {
			val[i] = normalizeValue(item)
		}
		return val
	case uint64:
		if val > uint64(^uint(0)>>1) {
			return val // preserve for values exceeding int range
		}
		return int(val)
	case int64:
		return int(val)
	case float32:
		return float64(val)
	default:
		return v
	}
}

// YAMLCompat controls YAML 1.1 backward compatibility behavior.
type YAMLCompat struct {
	// ConvertYAML11Booleans converts "yes"/"no"/"on"/"off" strings to booleans.
	ConvertYAML11Booleans bool
}

// DefaultYAMLCompat returns compat settings with YAML 1.1 booleans enabled.
func DefaultYAMLCompat() *YAMLCompat {
	return &YAMLCompat{ConvertYAML11Booleans: true}
}

// ConvertValue applies YAML 1.1 compatibility conversions to a string value.
func (c *YAMLCompat) ConvertValue(s string) interface{} {
	if !c.ConvertYAML11Booleans {
		return s
	}
	switch s {
	case "yes", "Yes", "YES", "on", "On", "ON":
		return true
	case "no", "No", "NO", "off", "Off", "OFF":
		return false
	default:
		return s
	}
}

// ConvertMapValues recursively applies YAML 1.1 compatibility conversions.
func (c *YAMLCompat) ConvertMapValues(data map[string]interface{}) map[string]interface{} {
	if !c.ConvertYAML11Booleans {
		return data
	}
	for k, v := range data {
		data[k] = c.convertAny(v)
	}
	return data
}

func (c *YAMLCompat) convertAny(v interface{}) interface{} {
	switch val := v.(type) {
	case string:
		return c.ConvertValue(val)
	case uint64:
		if val > uint64(^uint(0)>>1) {
			return val
		}
		return int(val)
	case int64:
		return int(val)
	case float32:
		return float64(val)
	case map[string]interface{}:
		return c.ConvertMapValues(val)
	case map[interface{}]interface{}:
		// yaml.v3 produces this for maps with non-string keys (e.g., integer keys)
		for k, v := range val {
			val[k] = c.convertAny(v)
		}
		return val
	case []interface{}:
		return c.convertSlice(val)
	default:
		return v
	}
}

func (c *YAMLCompat) convertSlice(s []interface{}) []interface{} {
	for i, v := range s {
		s[i] = c.convertAny(v)
	}
	return s
}

// ConvertAndUnprotect applies ConvertMapValues and
// UnprotectYAML11QuotedBools in a single walk. The parse path always
// runs the two back to back - each a full tree traversal - and their
// composition per value is order-independent to fuse: a marker-tagged
// string never matches ConvertValue's bare words (the marker prefixes
// it), so stripping the marker and skipping coercion is exactly what
// the sequential pair produced. Numeric normalization stays gated on
// ConvertYAML11Booleans, as in ConvertMapValues; marker stripping is
// unconditional, as in UnprotectYAML11QuotedBools.
func (c *YAMLCompat) ConvertAndUnprotect(data map[string]interface{}) map[string]interface{} {
	for k, v := range data {
		data[k] = c.convertAndUnprotectAny(v)
	}
	return data
}

func (c *YAMLCompat) convertAndUnprotectAny(v interface{}) interface{} {
	switch val := v.(type) {
	case string:
		if strings.HasPrefix(val, yaml11QuotedBoolMarker) {
			return strings.TrimPrefix(val, yaml11QuotedBoolMarker)
		}
		if c.ConvertYAML11Booleans {
			return c.ConvertValue(val)
		}
		return val
	case uint64, int64, float32:
		return c.convertAndUnprotectNumber(val)
	case map[string]interface{}:
		for k, item := range val {
			val[k] = c.convertAndUnprotectAny(item)
		}
		return val
	case map[interface{}]interface{}:
		for k, item := range val {
			val[k] = c.convertAndUnprotectAny(item)
		}
		return val
	case []interface{}:
		for i, item := range val {
			val[i] = c.convertAndUnprotectAny(item)
		}
		return val
	default:
		return v
	}
}

// convertAndUnprotectNumber applies the compat pass's numeric
// normalizations: int-sized integers become int, float32 widens to
// float64, and an integer too large for int stays uint64 untouched.
func (c *YAMLCompat) convertAndUnprotectNumber(v interface{}) interface{} {
	if !c.ConvertYAML11Booleans {
		return v
	}
	switch val := v.(type) {
	case uint64:
		if val > uint64(^uint(0)>>1) {
			return val
		}
		return int(val)
	case int64:
		return int(val)
	case float32:
		return float64(val)
	default:
		return v
	}
}

// yaml11QuotedBoolMarker prefixes a decoded string value that came from
// an explicitly quoted YAML 1.1 boolean-lookalike scalar (e.g. "yes",
// 'On'). ConvertValue's coercion switch matches on the bare words only,
// so a tagged value falls through untouched; UnprotectYAML11QuotedBools
// removes the prefix afterward, unconditionally, restoring the original
// string. U+E0DA is a Private Use Area code point that cannot appear in
// hand-authored deployment YAML.
const yaml11QuotedBoolMarker = "\uE0DA"

// yaml11BoolLookalikeWords are the exact-cased tokens ConvertValue
// coerces to a boolean. Quoting one of them (single or double) is an
// author's explicit request, honored by both spruce and YAML 1.1, to
// keep the value a string -- that request must survive the compat
// coercion pass below.
var yaml11BoolLookalikeWords = map[string]bool{
	"yes": true, "Yes": true, "YES": true,
	"no": true, "No": true, "NO": true,
	"on": true, "On": true, "ON": true,
	"off": true, "Off": true, "OFF": true,
}

// quotedBoolTagger is an ast.Visitor that mutates the Value of every
// explicitly-quoted (single- or double-quoted) *ast.StringNode whose
// content is a YAML 1.1 boolean-lookalike word, prefixing it with
// yaml11QuotedBoolMarker. Plain (unquoted) scalars, and quoted scalars
// embedded inside literal/folded block content (which the parser
// represents as a different node shape, never a quoted StringNode), are
// left untouched.
type quotedBoolTagger struct{}

func (quotedBoolTagger) Visit(n ast.Node) ast.Visitor {
	if n == nil {
		return nil
	}
	if sn, ok := n.(*ast.StringNode); ok {
		if sn.Token != nil && isQuotedScalarToken(sn.Token.Type) && yaml11BoolLookalikeWords[sn.Value] {
			sn.Value = yaml11QuotedBoolMarker + sn.Value
		}
	}
	return quotedBoolTagger{}
}

func isQuotedScalarToken(t token.Type) bool {
	return t == token.SingleQuoteType || t == token.DoubleQuoteType
}

// ParseYAML11CompatAware parses data the same way yaml.Unmarshal(data,
// &interface{}{}) would, except every explicitly-quoted YAML 1.1
// boolean-lookalike scalar ("yes", 'On', "OFF", ...) is protected from
// YAMLCompat's later yes/no/on/off -> bool coercion. Unquoted
// occurrences of those words decode as plain strings exactly as before,
// so ConvertMapValues keeps converting them.
//
// Quoting information only exists at the token level -- once a document
// is decoded into interface{} values, a quoted and an unquoted "yes"
// are indistinguishable Go strings. This parses data into goccy's AST,
// tags quoted matches using the AST's own token type (not a text-level
// guess), then decodes the (locally mutated) AST back into the same
// generic shape a direct yaml.Unmarshal call would produce.
//
// Callers must run the result through UnprotectYAML11QuotedBools after
// applying YAMLCompat, unconditionally, so a tagged value that YAMLCompat
// left alone (e.g. compat disabled) still comes out as the original
// word rather than the internal marker-prefixed string.
func ParseYAML11CompatAware(data []byte) (interface{}, error) {
	file, err := parser.ParseBytes(data, 0)
	if err != nil {
		return nil, err
	}
	if len(file.Docs) == 0 || file.Docs[0].Body == nil {
		return nil, nil
	}

	ast.Walk(quotedBoolTagger{}, file.Docs[0].Body)

	var result interface{}
	if err := yaml.NodeToValue(file.Docs[0].Body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// UnprotectYAML11QuotedBools reverses the tagging ParseYAML11CompatAware
// applied: it walks v, stripping yaml11QuotedBoolMarker from any string
// value that still carries it, restoring the original bare word. Mutates
// map and slice values in place; returns the (possibly replaced) root
// value, mirroring ConvertMapValues' in-place style.
func UnprotectYAML11QuotedBools(v interface{}) interface{} {
	switch val := v.(type) {
	case string:
		if strings.HasPrefix(val, yaml11QuotedBoolMarker) {
			return strings.TrimPrefix(val, yaml11QuotedBoolMarker)
		}
		return val
	case map[string]interface{}:
		for k, item := range val {
			val[k] = UnprotectYAML11QuotedBools(item)
		}
		return val
	case map[interface{}]interface{}:
		for k, item := range val {
			val[k] = UnprotectYAML11QuotedBools(item)
		}
		return val
	case []interface{}:
		for i, item := range val {
			val[i] = UnprotectYAML11QuotedBools(item)
		}
		return val
	default:
		return v
	}
}
