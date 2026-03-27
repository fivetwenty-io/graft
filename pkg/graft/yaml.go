package graft

import (
	"bytes"

	"gopkg.in/yaml.v3"
)

// MarshalYAML serializes a value to YAML with 2-space indentation,
// matching the output format expected by BOSH and CF ecosystem tools.
func MarshalYAML(v interface{}) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
