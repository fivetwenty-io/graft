package yaml

import "github.com/fivetwenty-io/graft/internal/yamlcodec"

// decodeGeneric decodes the first document in data into the values graft
// merge reads it as, which are map[string]any, []any, int, float64,
// bool, string, and nil, with YAML 1.1 booleans unless opts.YAML12 is set.
// An integer too large for int stays uint64. A nil value with a nil error
// means data holds no document, or a null one. Every error it returns is a
// *DecodeError.
//
// It runs the same codec helpers as graft's engine, which are
// yamlcodec.FirstMergeDocument and yamlcodec.DecodeGeneric, so a document
// decodes here exactly as a merge reads it, minus the engine's control-flow
// expansion of operators.
func decodeGeneric(data []byte, opts DecodeOptions) (any, error) {
	doc, err := yamlcodec.FirstMergeDocument(data)
	if err != nil {
		return nil, translate(err)
	}
	compat := &yamlcodec.YAMLCompat{ConvertYAML11Booleans: !opts.YAML12}
	v, err := yamlcodec.DecodeGeneric(doc, compat, true)
	if err != nil {
		return nil, translate(err)
	}
	return v, nil
}
