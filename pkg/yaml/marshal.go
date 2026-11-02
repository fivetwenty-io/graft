package yaml

import (
	"fmt"
	"reflect"

	"github.com/fivetwenty-io/graft/internal/yamlcodec"
	"github.com/fivetwenty-io/graft/pkg/sortkeys"
)

// Marshal encodes v as YAML, with every map's keys in spruce's order and
// two spaces of indentation, as graft merge writes a document.
//
// A struct is encoded by goccy/go-yaml, which honors the yaml tag options
// ,omitempty, ,inline, and ,flow. A string is written in the style spruce
// picks for it, so one that would read back as another type is quoted and
// a multi-line one is a literal block. A float64 that holds a whole
// number is written as an integer, so 3.0 becomes 3, while NaN, the
// infinities, a float with a fraction, and a float too large to print
// without an exponent keep their float form. A string that a YAML 1.1
// reader resolves as a timestamp, such as 2026-01-02, is double-quoted.
//
// Marshal refuses a v that holds a Node, because writing a Node tree
// back is not supported, and a v that holds a cycle. Its errors name a
// Go type and never quote a value from v. A value the encoder cannot
// write, such as a channel, is an error and not a panic, but a panic from
// a type's own MarshalYAML method is not recovered.
func Marshal(v any) ([]byte, error) {
	return MarshalWith(v, EncodeOptions{})
}

// MarshalWith is Marshal with the options given. It returns an error
// and no output for a KeyOrder other than the zero value, which means
// sortkeys.Spruce, and sortkeys.Lexical.
func MarshalWith(v any, opts EncodeOptions) (out []byte, err error) {
	mode := opts.KeyOrder
	switch mode {
	case "":
		mode = sortkeys.Spruce
	case sortkeys.Spruce, sortkeys.Lexical:
	default:
		return nil, &encodeError{msg: fmt.Sprintf("MarshalWith does not support key order %q; use the zero value, sortkeys.Spruce, or sortkeys.Lexical", string(mode))}
	}
	if err := checkValue(v); err != nil {
		return nil, err
	}

	defer func() {
		r := recover()
		if r == nil {
			return
		}
		if !panicFromEncoder() {
			panic(r)
		}
		out, err = nil, encoderPanicError(v)
	}()
	out, err = yamlcodec.MarshalYAMLTyped(v, sortkeys.Less(mode))
	if err != nil {
		return nil, &encodeError{msg: "cannot encode Go value of type " + typeName(v)}
	}
	return out, nil
}

// encodeError is the error Marshal returns. Its text names a Go type or a
// field path and never quotes a value from the input.
type encodeError struct {
	msg string
}

func (e *encodeError) Error() string { return "yaml: " + e.msg }

// encoderPanicError is the error an encode returns in place of a panic in
// the encoder, which names only the Go type of v.
func encoderPanicError(v any) error {
	return &encodeError{msg: "cannot encode Go value of type " + typeName(v)}
}

func typeName(v any) string {
	if v == nil {
		return "nil"
	}
	return reflect.TypeOf(v).String()
}

// nodeType is Node, which Marshal refuses wherever it finds one.
var nodeType = reflect.TypeFor[Node]()

// checkValue refuses a v that holds a Node or a cycle, before anything
// encodes. It walks the value as Marshal's encoder would reach it, which
// is through pointers, interfaces, slice and array elements, map values,
// and exported or embedded struct fields.
func checkValue(v any) error {
	if v == nil {
		return nil
	}
	w := valueWalk{onPath: make(map[walkID]bool), done: make(map[walkID]bool)}
	rv := reflect.ValueOf(v)
	if where, why := w.find(rv, rv.Type().String()); why != "" {
		return &encodeError{msg: fmt.Sprintf("Marshal does not support %s, which %s holds at %s", why, rv.Type(), where)}
	}
	return nil
}

// walkID identifies a pointer, map, or slice the walk entered.
type walkID struct {
	ptr uintptr
	typ reflect.Type
	len int
}

// valueWalk searches a value for a Node and for a cycle. onPath holds
// what the walk is inside of, so reaching one again is a cycle, and done
// holds what it has finished, so a value shared by many parents is walked
// once.
type valueWalk struct {
	onPath map[walkID]bool
	done   map[walkID]bool
}

// find returns where in v a Node or a cycle sits, as a path that starts
// at path, and which of the two it is. why is empty when there is none. A
// map's value adds "{value}" to the path, and an element adds "[]".
func (w valueWalk) find(v reflect.Value, path string) (where, why string) {
	switch v.Kind() {
	case reflect.Interface:
		if v.IsNil() {
			return "", ""
		}
		return w.find(v.Elem(), path)
	case reflect.Pointer:
		if v.IsNil() {
			return "", ""
		}
		if v.Type().Elem() == nodeType {
			return path, "yaml.Node"
		}
		return w.enter(walkID{ptr: v.Pointer(), typ: v.Type()}, path, func() (string, string) {
			return w.find(v.Elem(), path)
		})
	case reflect.Map:
		if v.IsNil() {
			return "", ""
		}
		return w.enter(walkID{ptr: v.Pointer(), typ: v.Type()}, path, func() (string, string) {
			return w.mapValues(v, path)
		})
	case reflect.Slice:
		if v.IsNil() || v.Len() == 0 {
			return "", ""
		}
		return w.enter(walkID{ptr: v.Pointer(), typ: v.Type(), len: v.Len()}, path, func() (string, string) {
			return w.elements(v, path)
		})
	case reflect.Array:
		return w.elements(v, path)
	case reflect.Struct:
		if v.Type() == nodeType {
			return path, "yaml.Node"
		}
		return w.fields(v, path)
	default:
	}
	return "", ""
}

// mapValues searches the values of a map v. Its keys are not searched,
// because the encoder writes a key as text.
func (w valueWalk) mapValues(v reflect.Value, path string) (where, why string) {
	iter := v.MapRange()
	for iter.Next() {
		if where, why := w.find(iter.Value(), path+"{value}"); why != "" {
			return where, why
		}
	}
	return "", ""
}

// fields searches the exported and embedded fields of a struct v.
func (w valueWalk) fields(v reflect.Value, path string) (where, why string) {
	t := v.Type()
	for i := range t.NumField() {
		f := t.Field(i)
		if !f.IsExported() && !f.Anonymous {
			continue
		}
		if where, why := w.find(v.Field(i), path+"."+f.Name); why != "" {
			return where, why
		}
	}
	return "", ""
}

// elements searches the items of a slice or array v.
func (w valueWalk) elements(v reflect.Value, path string) (where, why string) {
	if !canHold(v.Type().Elem()) {
		return "", ""
	}
	for i := range v.Len() {
		if where, why := w.find(v.Index(i), path+"[]"); why != "" {
			return where, why
		}
	}
	return "", ""
}

// canHold reports whether a value of type t can hold something find
// looks into, so a slice of numbers or strings is not walked item by item.
func canHold(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Interface, reflect.Pointer, reflect.Map, reflect.Slice, reflect.Array, reflect.Struct:
		return true
	default:
		return false
	}
}

// enter runs search for the pointer, map, or slice at id, reporting a
// cycle when the walk is already inside it, and skipping it when it has
// been searched.
func (w valueWalk) enter(id walkID, path string, search func() (string, string)) (where, why string) {
	if w.onPath[id] {
		return path, "a cycle"
	}
	if w.done[id] {
		return "", ""
	}
	w.onPath[id] = true
	where, why = search()
	delete(w.onPath, id)
	if why == "" {
		w.done[id] = true
	}
	return where, why
}
