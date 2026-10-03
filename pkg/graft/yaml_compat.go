package graft

import (
	"fmt"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/token"

	"github.com/fivetwenty-io/graft/internal/yamlnode"
	"github.com/fivetwenty-io/graft/internal/yamlprep"
)

// QuoteInjectKeys pre-processes YAML bytes to quote the graft-specific
// <<<: inject key, which goccy/go-yaml rejects when unquoted because it
// reads <<< as a variant of the YAML merge key <<. It handles both the
// standalone (<<<:) and dotted path (foo.<<<:) forms.
func QuoteInjectKeys(data []byte) []byte {
	return yamlprep.QuoteInjectKeys(data)
}

// NormalizeMap deep-converts any map[interface{}]interface{} values
// to map[string]interface{} throughout the tree. This is needed because
// values that other decoders produce, such as yaml.v2 through go-patch,
// use map[interface{}]interface{} when maps contain non-string keys
// (e.g., integer keys like 1:, 2:). goccy decodes those same maps into
// map[string]interface{} with stringified keys.
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
// Its yes, no, on, and off table serves merge. That differs on purpose from
// the YAML 1.2 core forms in internal/yamlnode and from the YAML 1.1 set
// that toBool in internal/yamldiff accepts for nodes tagged !!bool, so the
// three tables must not be merged.
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
		// Other decoders produce this for maps with non-string keys (e.g.,
		// integer keys); goccy itself stringifies them.
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
//
// Library callers pass raw input through FirstMergeDocument first, since
// goccy's parser stalls on nesting that function fails cheaply.
func ParseYAML11CompatAware(data []byte) (interface{}, error) {
	file, err := parser.ParseBytes(data, 0)
	if err != nil {
		return nil, err
	}
	body := firstBody(file)
	if body == nil {
		return nil, nil
	}
	if err := checkAnchors(body); err != nil {
		return nil, err
	}

	ast.Walk(quotedBoolTagger{}, body)

	var result interface{}
	if err := yaml.NodeToValue(body, &result); err != nil {
		return nil, err
	}
	return result, nil
}

// firstBody returns the root node of file's first document, or nil when
// it has none. goccy gives the directives before a "---" a document of
// their own, whose body is the directive, so firstBody skips those and
// returns the body of the document the directives belong to, as libyaml
// and yaml.Unmarshal read it.
func firstBody(file *ast.File) ast.Node {
	for _, doc := range file.Docs {
		if _, ok := doc.Body.(*ast.DirectiveNode); !ok {
			return doc.Body
		}
	}
	return nil
}

// maxRecursionMessage is the text CheckForCycles gives for a merged tree
// nested past its limit. spruce gives the same text for any merge input
// nested past 4,096 levels.
const maxRecursionMessage = "Hit max recursion depth. You seem to have a self-referencing dataset"

// maxRecursionError reports merge input nested too deep to parse. It
// reads as maxRecursionMessage and wraps the depth error underneath.
type maxRecursionError struct {
	cause error
}

func (e *maxRecursionError) Error() string { return maxRecursionMessage }

func (e *maxRecursionError) Unwrap() error { return e.cause }

// FirstMergeDocument returns the first document of data, raw YAML bytes
// about to be merged, since the merge reads no other, as spruce's does.
// goccy parses every document it is given, so a syntax error or deep
// nesting in a later document would otherwise fail the merge. The result
// is a prefix of data, and it is data itself when nothing ends the first
// document. See yamlnode.FirstDocument.
//
// It fails a first document that nests deeper than 10,000 levels, and
// it runs before anything else tokenizes data, because goccy's parser
// slows down faster than linearly with depth, and 20,000 unclosed
// brackets cost it hundreds of megabytes before it fails. The error
// reads as the text a merge gives for a tree nested past 4,096 levels,
// so every over-deep merge fails with one message whatever its depth.
func FirstMergeDocument(data []byte) ([]byte, error) {
	first, err := yamlnode.FirstDocument(data)
	if err != nil {
		return nil, &maxRecursionError{cause: err}
	}
	return first, nil
}

// CheckSelfContainingAnchors fails data, YAML bytes about to be decoded,
// when an alias in its first document sits inside the collection its
// anchor names, with the message spruce gives. goccy decodes such an
// alias as null, so without the check the value would quietly vanish. A
// syntax error passes the check, so the caller's own decode reports it.
// Library callers pass raw input through FirstMergeDocument first, since
// goccy's parser stalls on nesting that function fails cheaply.
func CheckSelfContainingAnchors(data []byte) error {
	if file, err := parser.ParseBytes(data, 0); err == nil {
		if body := firstBody(file); body != nil {
			return checkAnchors(body)
		}
	}
	return nil
}

// checkAnchors walks body, a document's root node, for an alias that
// sits inside the collection its anchor names.
func checkAnchors(body ast.Node) error {
	w := &anchorWalk{
		bound: make(map[string]*ast.AnchorNode),
		open:  make(map[*ast.AnchorNode]bool),
	}
	return w.walk(body)
}

// anchorWalk tracks anchors the way yaml.v3 resolves aliases. A name
// binds to its latest definition in document order, from the moment the
// definition starts, so an alias inside its own anchor's collection
// finds that collection still open on the walk's path.
type anchorWalk struct {
	bound map[string]*ast.AnchorNode // each name's latest definition
	open  map[*ast.AnchorNode]bool   // the definitions the walk is inside
}

func (w *anchorWalk) walk(n ast.Node) error {
	switch x := n.(type) {
	case *ast.AnchorNode:
		return w.anchor(x)
	case *ast.AliasNode:
		return w.alias(x)
	case *ast.TagNode:
		return w.walk(x.Value)
	case *ast.MappingKeyNode:
		return w.walk(x.Value)
	case *ast.MappingValueNode:
		if err := w.walk(x.Key); err != nil {
			return err
		}
		return w.walk(x.Value)
	case *ast.MappingNode:
		for _, v := range x.Values {
			if err := w.walk(v); err != nil {
				return err
			}
		}
	case *ast.SequenceNode:
		for _, v := range x.Values {
			if err := w.walk(v); err != nil {
				return err
			}
		}
	}
	return nil
}

func (w *anchorWalk) anchor(a *ast.AnchorNode) error {
	w.bound[a.Name.GetToken().Value] = a
	w.open[a] = true
	err := w.walk(a.Value)
	delete(w.open, a)
	return err
}

func (w *anchorWalk) alias(a *ast.AliasNode) error {
	name := a.Value.GetToken().Value
	if w.open[w.bound[name]] {
		return fmt.Errorf("anchor '%s' value contains itself", name)
	}
	return nil
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
