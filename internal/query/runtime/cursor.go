package runtime

import (
	"math"

	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// Cursor is where the evaluator is in its input, for builtins that report
// it. Doc is the index of Root among the run's roots. Values hold no
// parent pointers, so yq's parent, key, and path walk Root along Path
// instead.
type Cursor struct {
	Root *value.Value
	Path value.Path
	Doc  int
}

// Child returns the cursor one step further down, at component c. The
// new cursor has a path of its own, so two children of one cursor never
// share storage.
func (c Cursor) Child(component *value.Value) Cursor {
	p := make(value.Path, len(c.Path), len(c.Path)+1)
	copy(p, c.Path)
	c.Path = append(p, component)
	return c
}

// Node returns the value at Path, or nil when Path does not lead to one.
// As in getpath, a key or index the input lacks, or a null along the way,
// gives null.
func (c Cursor) Node() *value.Value {
	v, _ := walk(c.Root, c.Path)
	return v
}

// Parent returns the value that holds the cursor's node, and false at the
// root or when the path does not lead to one.
func (c Cursor) Parent() (*value.Value, bool) {
	if len(c.Path) == 0 {
		return nil, false
	}
	return walk(c.Root, c.Path[:len(c.Path)-1])
}

// Key returns the last path component, which is the key or index of the
// cursor's node in its parent, and false at the root.
func (c Cursor) Key() (*value.Value, bool) {
	if len(c.Path) == 0 {
		return nil, false
	}
	return c.Path[len(c.Path)-1], true
}

// walk follows path from v. It stops with false at a component that
// cannot index the value it meets, such as a string on an array or a
// slice.
func walk(v *value.Value, path value.Path) (*value.Value, bool) {
	if v == nil {
		return nil, false
	}
	for _, k := range path {
		var ok bool
		if v, ok = step(v, k); !ok {
			return nil, false
		}
	}
	return v, true
}

func step(v, k *value.Value) (*value.Value, bool) {
	switch {
	case v.Kind() == value.KindNull:
		return value.Null(), true
	case v.Kind() == value.KindObject && k.Kind() == value.KindString:
		if e, ok := v.Get(k.Str()); ok {
			return e, true
		}
		return value.Null(), true
	case v.Kind() == value.KindObject && k.Kind() != value.KindArray && k.Kind() != value.KindObject:
		// yq keeps keys that are numbers, booleans, or null.
		entries := v.Entries()
		for i := len(entries) - 1; i >= 0; i-- {
			if value.Equal(entries[i].Key, k) {
				return entries[i].Value, true
			}
		}
		return value.Null(), true
	case v.Kind() == value.KindArray && k.Kind() == value.KindNumber:
		if f := math.Floor(k.Float()); math.Abs(f) <= 1<<53 {
			if e, ok := v.Index(int(f)); ok {
				return e, true
			}
		}
		return value.Null(), true
	}
	return nil, false
}
