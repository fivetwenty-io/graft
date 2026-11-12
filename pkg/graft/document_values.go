package graft

import "reflect"

// plainGoValue returns v with every slice or array (except []byte) as
// []interface{} and every map with string keys as
// map[string]interface{}, recursively. Other values come back as they
// are. Values already in the plain form are rebuilt only when something
// inside them changes. A container that holds itself is left as it is,
// so the cycle checks further on still see it.
func plainGoValue(v interface{}) interface{} {
	return (&plainer{active: map[plainVisit]struct{}{}}).value(v)
}

// plainGoMap is plainGoValue for a map[string]interface{}.
func plainGoMap(m map[string]interface{}) map[string]interface{} {
	if out, ok := plainGoValue(m).(map[string]interface{}); ok {
		return out
	}
	return m
}

// plainVisit identifies a map or slice that is being converted.
type plainVisit struct {
	ptr uintptr
	typ reflect.Type
	n   int
}

type plainer struct {
	active map[plainVisit]struct{}
}

// enter marks the container rv as being converted. It reports false
// when rv is already being converted, which means it holds itself.
func (p *plainer) enter(rv reflect.Value) (plainVisit, bool) {
	k := plainVisit{ptr: rv.Pointer(), typ: rv.Type(), n: rv.Len()}
	if _, seen := p.active[k]; seen && k.ptr != 0 {
		return k, false
	}
	p.active[k] = struct{}{}
	return k, true
}

func (p *plainer) value(v interface{}) interface{} {
	switch v.(type) {
	case nil, string, bool, int, int64, float64, []byte:
		return v
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return v
		}
	case reflect.Slice, reflect.Array:
		if rv.Type().Elem().Kind() == reflect.Uint8 {
			return v
		}
	default:
		return v
	}
	if rv.Kind() != reflect.Array {
		k, ok := p.enter(rv)
		if !ok {
			return v
		}
		defer delete(p.active, k)
	}
	switch t := v.(type) {
	case map[string]interface{}:
		return p.plainMap(t)
	case []interface{}:
		return p.plainSlice(t)
	}
	return p.reflected(rv)
}

// plainMap normalizes the values of m, copying m only when one of
// them changes.
func (p *plainer) plainMap(m map[string]interface{}) map[string]interface{} {
	var out map[string]interface{}
	for k, e := range m {
		ne := p.value(e)
		if out == nil && !sameValue(e, ne) {
			out = make(map[string]interface{}, len(m))
			for k2, e2 := range m {
				out[k2] = e2
			}
		}
		if out != nil {
			out[k] = ne
		}
	}
	if out == nil {
		return m
	}
	return out
}

// plainSlice normalizes the elements of s, copying s only when one of
// them changes.
func (p *plainer) plainSlice(s []interface{}) []interface{} {
	var out []interface{}
	for i, e := range s {
		ne := p.value(e)
		if out == nil && !sameValue(e, ne) {
			out = make([]interface{}, len(s))
			copy(out, s)
		}
		if out != nil {
			out[i] = ne
		}
	}
	if out == nil {
		return s
	}
	return out
}

// reflected converts a typed slice, array, or string-keyed map.
func (p *plainer) reflected(rv reflect.Value) interface{} {
	if rv.Kind() == reflect.Map {
		out := make(map[string]interface{}, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			out[iter.Key().String()] = p.value(iter.Value().Interface())
		}
		return out
	}
	out := make([]interface{}, rv.Len())
	for i := range out {
		out[i] = p.value(rv.Index(i).Interface())
	}
	return out
}

// sameValue reports whether plainGoValue left a value untouched. Only
// maps and slices can change, and a rebuilt one is a different object.
func sameValue(a, b interface{}) bool {
	switch x := a.(type) {
	case map[string]interface{}:
		y, ok := b.(map[string]interface{})
		return ok && reflect.ValueOf(x).Pointer() == reflect.ValueOf(y).Pointer()
	case []interface{}:
		y, ok := b.([]interface{})
		if !ok || len(x) != len(y) {
			return false
		}
		return len(x) == 0 || &x[0] == &y[0]
	}
	return reflect.TypeOf(a) == reflect.TypeOf(b)
}
