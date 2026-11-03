package graft

import "github.com/fivetwenty-io/graft/internal/literal"

// LiteralValue is a value that graft keeps whole. NewLiteral builds one,
// and Value returns what it wraps.
//
// A merge treats a LiteralValue as a single scalar. It replaces whatever
// value it lands on, whole, and a later document replaces it whole in
// turn, so nothing inside it is deep-merged, merged by name, appended
// to, or touched by an (( append )) or (( replace )) marker. The
// evaluator never looks inside it, so a "(( ... ))" string in it stays
// text, even when it is malformed. A path that reads into it, such as
// (( grab params.size )), (( keys params )), or a || fallback, sees
// through the wrapper, and a value that (( grab )) or (( inject )) copies
// out of it stays a LiteralValue at its new place.
//
// Every output is plain data. RawData, GetData, ToMap, Get and the other
// getters, ToYAML, ToJSON, ToJSONIndent, MarshalYAML, pkg/yaml Marshal,
// and encoding/json all remove the wrapper at any depth, and they leave
// the document itself unchanged.
//
// The zero LiteralValue wraps nil. Two LiteralValues compare equal with
// == only when one is a copy of the other.
type LiteralValue = literal.Value

// NewLiteral wraps a deep copy of v so that graft keeps it whole. A v
// that is already a LiteralValue is returned unchanged, and v may be nil.
// Because NewLiteral copies every map and list in v, a later change to v
// does not reach the document, and nothing graft does to the document
// reaches v.
//
// On the way NewLiteral turns each map[interface{}]interface{} in v into
// a map[string]interface{}, with every key printed by fmt, so a path into
// the result always sees maps of type map[string]interface{} and lists of
// type []interface{}, the shapes that ParseYAML and encoding/json produce.
// Put the result anywhere in a document tree, as a map value or a list
// item.
func NewLiteral(v any) LiteralValue {
	return literal.New(v)
}

// plainValue returns v with every LiteralValue at any depth replaced by
// its content. It copies only the containers on the way to a wrapper.
func plainValue(v interface{}) interface{} {
	return literal.Strip(v)
}

// plainMap is plainValue for a document root.
func plainMap(m map[string]interface{}) map[string]interface{} {
	out, _ := literal.Strip(m).(map[string]interface{})
	return out
}

// treeOf returns doc's own tree with every LiteralValue still in place.
// The engine reads documents through it instead of RawData, which strips
// the wrappers that merge and evaluation depend on.
func treeOf(doc Document) interface{} {
	if d, ok := doc.(*document); ok {
		return d.data
	}
	return doc.RawData()
}
