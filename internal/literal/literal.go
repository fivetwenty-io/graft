// Package literal holds the opaque wrapper that graft's merge and
// evaluation treat as a single scalar. pkg/graft exports the type as
// LiteralValue and builds one with NewLiteral; this package keeps the
// helpers that every tree walker needs, so that pkg/graft, its tree and
// merger packages, and the YAML encoders can all recognize a wrapper
// without importing each other.
package literal

import (
	"encoding/json"
	"fmt"
	"strings"
)

// maxDepth bounds the recursion in Strip and Copy. A document deeper
// than this is far past anything graft can merge, and the bound keeps a
// cyclic value from overflowing the stack.
const maxDepth = 10000

// Value wraps a value that graft must keep whole. A merge replaces it as
// a unit and never merges into it, the evaluator never scans inside it,
// and every output strips the wrapper so that only the plain value is
// written. The zero Value wraps nil.
//
// Value holds its content behind a pointer so that two Values compare
// with == without a runtime panic, whatever they hold.
type Value struct {
	b *box
}

type box struct {
	v any
}

// Wrap returns v wrapped as a Value. A v that is already a Value is
// returned unchanged, so wrapping twice is the same as wrapping once.
func Wrap(v any) Value {
	if l, ok := v.(Value); ok {
		return l
	}
	return Value{b: &box{v: v}}
}

// Value returns the wrapped value, which may itself hold other Values.
func (l Value) Value() any {
	if l.b == nil {
		return nil
	}
	return l.b.v
}

// MarshalYAML returns the wrapped value with every wrapper inside it
// removed, so a YAML encoder that honors the method writes plain data.
func (l Value) MarshalYAML() (any, error) {
	return Strip(l.Value()), nil
}

// MarshalJSON writes the wrapped value as JSON, with every wrapper
// inside it removed.
func (l Value) MarshalJSON() ([]byte, error) {
	return json.Marshal(Strip(l.Value()))
}

// Is reports whether v is a Value.
func Is(v any) bool {
	_, ok := v.(Value)
	return ok
}

// Unwrap returns the content of v when v is a Value, and v itself
// otherwise. The bool reports whether v was a Value.
func Unwrap(v any) (any, bool) {
	if l, ok := v.(Value); ok {
		return l.Value(), true
	}
	return v, false
}

// Strip returns v with every Value at any depth replaced by its content.
// It copies only the maps and lists on the way to a Value, so a v that
// holds no Value comes back as the very same container, and the caller's
// data is never modified.
func Strip(v any) any {
	out, _ := strip(v, 0)
	return out
}

// strip is Strip, and it also reports whether anything changed.
func strip(v any, depth int) (any, bool) {
	if depth > maxDepth {
		return v, false
	}
	switch t := v.(type) {
	case Value:
		inner, _ := strip(t.Value(), depth+1)
		return inner, true
	case map[string]any:
		return stripMap(t, depth)
	case map[any]any:
		return stripMap(t, depth)
	case []any:
		return stripList(t, depth)
	default:
		return v, false
	}
}

// stripMap is strip for a map. It copies m only when a value in it
// changes.
func stripMap[K comparable](m map[K]any, depth int) (any, bool) {
	var out map[K]any
	for k, e := range m {
		s, changed := strip(e, depth+1)
		if !changed {
			continue
		}
		if out == nil {
			out = make(map[K]any, len(m))
			for k2, e2 := range m {
				out[k2] = e2
			}
		}
		out[k] = s
	}
	if out == nil {
		return m, false
	}
	return out, true
}

// stripList is strip for a list. It copies l only when an item in it
// changes.
func stripList(l []any, depth int) (any, bool) {
	var out []any
	for i, e := range l {
		s, changed := strip(e, depth+1)
		if !changed {
			continue
		}
		if out == nil {
			out = make([]any, len(l))
			copy(out, l)
		}
		out[i] = s
	}
	if out == nil {
		return l, false
	}
	return out, true
}

// Contains reports whether v is a Value or holds one at any depth.
func Contains(v any) bool {
	return contains(v, 0)
}

func contains(v any, depth int) bool {
	if depth > maxDepth {
		return false
	}
	switch t := v.(type) {
	case Value:
		return true
	case map[string]any:
		for _, e := range t {
			if contains(e, depth+1) {
				return true
			}
		}
	case map[any]any:
		for _, e := range t {
			if contains(e, depth+1) {
				return true
			}
		}
	case []any:
		for _, e := range t {
			if contains(e, depth+1) {
				return true
			}
		}
	}
	return false
}

// Copy returns a deep copy of v's maps and lists. A Value stays a Value
// and gets a copy of its content, so the copy shares no container with
// v. Any other value is returned as it is.
func Copy(v any) any {
	return deepCopy(v, 0)
}

func deepCopy(v any, depth int) any {
	if depth > maxDepth {
		return v
	}
	switch t := v.(type) {
	case Value:
		return Value{b: &box{v: deepCopy(t.Value(), depth+1)}}
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, e := range t {
			out[k] = deepCopy(e, depth+1)
		}
		return out
	case map[any]any:
		out := make(map[any]any, len(t))
		for k, e := range t {
			out[k] = deepCopy(e, depth+1)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = deepCopy(e, depth+1)
		}
		return out
	default:
		return v
	}
}

// GuardText returns v with every string that holds "((" wrapped as a
// Value, so the evaluator never runs it as an operator. It copies only
// the maps and lists on the way to a string it wraps, leaves a Value
// that is already in v as it is, and reports whether anything changed.
func GuardText(v any) (any, bool) {
	return guardText(v, 0)
}

func guardText(v any, depth int) (any, bool) {
	if depth > maxDepth {
		return v, false
	}
	switch t := v.(type) {
	case string:
		if strings.Contains(t, "((") {
			return Wrap(t), true
		}
		return v, false
	case map[string]any:
		var out map[string]any
		for k, e := range t {
			g, changed := guardText(e, depth+1)
			if !changed {
				continue
			}
			if out == nil {
				out = make(map[string]any, len(t))
				for k2, e2 := range t {
					out[k2] = e2
				}
			}
			out[k] = g
		}
		if out == nil {
			return v, false
		}
		return out, true
	case []any:
		var out []any
		for i, e := range t {
			g, changed := guardText(e, depth+1)
			if !changed {
				continue
			}
			if out == nil {
				out = make([]any, len(t))
				copy(out, t)
			}
			out[i] = g
		}
		if out == nil {
			return v, false
		}
		return out, true
	default:
		return v, false
	}
}

// Texts returns the text of every Value in v at any depth: each string
// it holds, each map key, and each other scalar as fmt prints it. The
// engine uses it to keep that text out of an error message.
func Texts(v any) []string {
	var out []string
	collectTexts(v, false, 0, &out)
	return out
}

func collectTexts(v any, inside bool, depth int, out *[]string) {
	if depth > maxDepth {
		return
	}
	switch t := v.(type) {
	case Value:
		collectTexts(t.Value(), true, depth+1, out)
	case map[string]any:
		for k, e := range t {
			if inside {
				*out = append(*out, k)
			}
			collectTexts(e, inside, depth+1, out)
		}
	case map[any]any:
		for k, e := range t {
			if inside {
				*out = append(*out, fmt.Sprint(k))
			}
			collectTexts(e, inside, depth+1, out)
		}
	case []any:
		for _, e := range t {
			collectTexts(e, inside, depth+1, out)
		}
	case nil:
	default:
		if inside {
			*out = append(*out, fmt.Sprint(t))
		}
	}
}
