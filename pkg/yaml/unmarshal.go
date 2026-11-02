// Package yaml decodes YAML the way graft's merge engine reads it.
package yaml

// Unmarshal decodes the first YAML document in data into v, reading
// yes, no, on, and off as booleans the way graft's merge engine does.
//
// v must be a non-nil pointer. A *any, *map[string]any, or *[]any
// receives maps as map[string]any, sequences as []any, integers as int,
// and floats as float64. Empty input, or input that holds only comments
// or a null, leaves v unchanged. Every error is a *DecodeError.
//
// Any other v, such as a pointer to a struct, a typed map, or a typed
// slice, is decoded by goccy/go-yaml, which honors the yaml tag options
// ,omitempty, ,inline, and ,flow. A struct field with no yaml tag takes
// its key from its json tag, and then from its name in lower case. A
// plain yes, no, on, or off decodes into a bool or *bool field as a
// boolean, while a quoted one is refused with an ErrorKindType error, as
// yaml.v2 refuses it. A string, including a map key of a string type,
// holds a scalar as written, so 1.10 stays "1.10" and 0x1F stays "0x1F",
// as in yaml.v2 and yaml.v3. A key that reads as a number, a boolean, or
// a null still matches a struct field by its text. A value of type any,
// map[string]any, or []any anywhere inside v holds exactly what a
// top-level decode of the same text gives, and so do the keys of such a
// map. A failed decode can leave v partly filled, with those values
// already converted.
//
// These limits apply to that route:
//
//   - A named bool type, such as type Flag bool, follows goccy's YAML 1.2
//     rule, so only true and false decode into it and a plain yes is a
//     type error.
//   - A type with an UnmarshalText method receives goccy's formatted text
//     of a scalar, so 1.10 arrives as "1.1". So does a string that a
//     type's own UnmarshalYAML method decodes through its callback.
//   - A type's own UnmarshalYAML method that keeps what it decodes in an
//     unexported field, or in a local variable, gets goccy's raw values.
//     In YAML 1.1 mode, when v also holds a value of interface type, a
//     quoted yes, no, on, or off can reach that method with an internal
//     marker character in front of it.
//   - An ,inline map receives every key of its mapping, including keys
//     that name another field of the struct.
//   - In a << merge key's list of maps, a later map wins over an earlier
//     one, as on the generic route, where yaml.v2 and yaml.v3 let the
//     earlier map win.
//   - A value of interface type that v already held is converted as a
//     decoded generic value is, even where the document sets nothing.
func Unmarshal(data []byte, v any) error {
	return UnmarshalWith(data, v, DecodeOptions{})
}

// UnmarshalWith decodes the first YAML document in data into v with opts.
func UnmarshalWith(data []byte, v any, opts DecodeOptions) error {
	if err := checkTarget(v); err != nil {
		return err
	}
	switch p := v.(type) {
	case *any:
		return decodeInto(data, opts, func(got any) error {
			*p = got
			return nil
		})
	case *map[string]any:
		return decodeInto(data, opts, func(got any) error {
			m, ok := got.(map[string]any)
			if !ok {
				return mismatch(got, *p)
			}
			*p = m
			return nil
		})
	case *[]any:
		return decodeInto(data, opts, func(got any) error {
			s, ok := got.([]any)
			if !ok {
				return mismatch(got, *p)
			}
			*p = s
			return nil
		})
	default:
		return decodeTyped(data, v, opts)
	}
}

// decodeInto runs the generic route and hands a non-nil result to set,
// so a document that holds nothing leaves the target unchanged.
func decodeInto(data []byte, opts DecodeOptions, set func(any) error) error {
	got, err := decodeGeneric(data, opts)
	if err != nil {
		return err
	}
	if got == nil {
		return nil
	}
	return set(got)
}

// mismatch reports a decoded root whose type does not fit the target,
// naming both Go types and quoting no value.
func mismatch(got, target any) error {
	return typeError("cannot unmarshal %T into Go value of type %T", got, target)
}
