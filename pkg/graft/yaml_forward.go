package graft

import (
	"errors"

	"github.com/fivetwenty-io/graft/internal/yamlcodec"
)

// YAMLHeadComment names a node by its dotted document path and gives the
// comment lines that MarshalYAMLWithComments places above it.
type YAMLHeadComment = yamlcodec.YAMLHeadComment

// YAMLCompat controls YAML 1.1 backward compatibility behavior.
type YAMLCompat = yamlcodec.YAMLCompat

// MarshalYAML serializes v with spruce's key order and quoting rules.
func MarshalYAML(v interface{}) ([]byte, error) {
	return yamlcodec.MarshalYAML(v)
}

// MarshalYAMLWith serializes v like MarshalYAML, ordering map keys by less.
func MarshalYAMLWith(v interface{}, less func(a, b string) bool) ([]byte, error) {
	return yamlcodec.MarshalYAMLWith(v, less)
}

// MarshalYAMLWithComments serializes v like MarshalYAML and attaches each
// entry in comments as a head comment above the node at its Path.
func MarshalYAMLWithComments(v interface{}, comments []YAMLHeadComment) ([]byte, error) {
	return yamlcodec.MarshalYAMLWithComments(v, comments)
}

// MarshalYAMLWithCommentsWith is MarshalYAMLWithComments with map keys
// ordered by less.
func MarshalYAMLWithCommentsWith(v interface{}, comments []YAMLHeadComment, less func(a, b string) bool) ([]byte, error) {
	return yamlcodec.MarshalYAMLWithCommentsWith(v, comments, less)
}

// QuoteInjectKeys quotes the YAML 1.1 boolean-looking keys in data.
func QuoteInjectKeys(data []byte) []byte {
	return yamlcodec.QuoteInjectKeys(data)
}

// NormalizeMap converts nested map[interface{}]interface{} values in data
// to map[string]interface{}.
func NormalizeMap(data map[string]interface{}) map[string]interface{} {
	return yamlcodec.NormalizeMap(data)
}

// DefaultYAMLCompat returns compat settings with YAML 1.1 booleans enabled.
func DefaultYAMLCompat() *YAMLCompat {
	return yamlcodec.DefaultYAMLCompat()
}

// ParseYAML11CompatAware parses data as YAML 1.1 and keeps quoted
// boolean-looking strings as strings.
func ParseYAML11CompatAware(data []byte) (interface{}, error) {
	return yamlcodec.ParseYAML11CompatAware(data)
}

// FirstMergeDocument returns the first YAML document in data, the one a
// merge reads. An over-deep document fails with the merge's recursion
// message, and content after a bare "..." fails with a parse error.
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

// CheckSelfContainingAnchors fails data when an alias in its first
// document sits inside the collection its own anchor names.
func CheckSelfContainingAnchors(data []byte) error {
	return yamlcodec.CheckSelfContainingAnchors(data)
}

// UnprotectYAML11QuotedBools strips the marker that
// ParseYAML11CompatAware leaves on quoted boolean-looking strings.
func UnprotectYAML11QuotedBools(v interface{}) interface{} {
	return yamlcodec.UnprotectYAML11QuotedBools(v)
}
