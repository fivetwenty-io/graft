// Package yaml decodes YAML the way graft's merge engine reads it.
package yaml

// Unmarshal decodes the first YAML document in data into v, reading
// yes, no, on, and off as booleans the way graft's merge engine does.
//
// v must be a non-nil pointer. A *any, *map[string]any, or *[]any
// receives maps as map[string]any, sequences as []any, integers as int,
// and floats as float64. Empty input, or input that holds only comments,
// leaves v unchanged. Every error is a *DecodeError.
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
		return typeError("cannot unmarshal into Go value of type %T", v)
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
