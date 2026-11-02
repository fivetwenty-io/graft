package yaml

import "reflect"

// interfaceMapType is map[interface{}]interface{}, which Unmarshal
// refuses wherever it sits in a target type.
var interfaceMapType = reflect.TypeFor[map[interface{}]interface{}]()

// checkTarget refuses a target Unmarshal cannot decode into, before
// anything parses. That is a nil v, a v that is not a pointer, a nil
// pointer, and a type that holds map[interface{}]interface{} anywhere a
// decoder can reach, which is through pointers, slice and array elements,
// map values, and exported or embedded struct fields.
func checkTarget(v any) *DecodeError {
	if v == nil {
		return typeError("Unmarshal(nil)")
	}
	rv := reflect.ValueOf(v)
	t := rv.Type()
	if t.Kind() != reflect.Pointer {
		return typeError("Unmarshal(non-pointer %s)", t)
	}
	if rv.IsNil() {
		return typeError("Unmarshal(nil %s)", t)
	}
	w := targetWalk{seen: make(map[reflect.Type]bool)}
	if where, found := w.find(t.Elem(), t.Elem().String()); found {
		return typeError("Unmarshal does not support map[interface {}]interface {}, which %s holds at %s; use map[string]any instead", t, where)
	}
	return nil
}

// targetWalk searches a type for map[interface{}]interface{}. seen holds
// every type the walk has entered, so a recursive type such as
// struct{ Next *T } is walked once and the walk ends.
type targetWalk struct {
	seen map[reflect.Type]bool
}

// find reports whether t holds map[interface{}]interface{}, and where,
// as a path that starts at path, the name of t.
func (w targetWalk) find(t reflect.Type, path string) (string, bool) {
	if w.seen[t] {
		return "", false
	}
	w.seen[t] = true
	if t.Kind() == reflect.Map && t.Key() == interfaceMapType.Key() && t.Elem() == interfaceMapType.Elem() {
		return path, true
	}
	switch t.Kind() {
	case reflect.Pointer:
		return w.find(t.Elem(), path)
	case reflect.Slice, reflect.Array, reflect.Map:
		return w.find(t.Elem(), path+"[]")
	case reflect.Struct:
		for i := range t.NumField() {
			f := t.Field(i)
			if !f.IsExported() && !f.Anonymous {
				continue
			}
			if where, found := w.find(f.Type, path+"."+f.Name); found {
				return where, true
			}
		}
	default:
	}
	return "", false
}
