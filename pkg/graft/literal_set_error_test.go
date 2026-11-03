package graft_test

import (
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// TestSetAndDeleteIntoLiteralNameTheExportedType runs Set and Delete
// paths into a literal. The error names the value as a literal, which a
// caller can look up, and not the internal type behind it.
func TestSetAndDeleteIntoLiteralNameTheExportedType(t *testing.T) {
	newData := func() map[string]interface{} {
		return map[string]interface{}{
			"params": graft.NewLiteral(map[string]interface{}{"x": "y"}),
			"list":   []interface{}{graft.NewLiteral(map[string]interface{}{"name": "a"})},
		}
	}
	tests := []struct {
		name string
		run  func(map[string]interface{}) error
	}{
		{"DeepSet field", func(d map[string]interface{}) error { return graft.DeepSet(d, "params.x", "z") }},
		{"DeepSet key match", func(d map[string]interface{}) error { return graft.DeepSet(d, "params[name=a]", "z") }},
		{"DeepDelete at root", func(d map[string]interface{}) error { return graft.DeepDelete(graft.NewLiteral(d), "params") }},
		{"DeepDelete field", func(d map[string]interface{}) error { return graft.DeepDelete(d, "params.x") }},
		{"Document.Set", func(d map[string]interface{}) error { return graft.NewDocument(d).Set("params.x", "z") }},
		{"Document.Delete", func(d map[string]interface{}) error { return graft.NewDocument(d).Delete("params.x") }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run(newData())
			if err == nil {
				t.Fatal("a path into a literal must fail")
			}
			for _, text := range []string{err.Error(), graft.DetailedError(err)} {
				if strings.Contains(text, "literal.Value") {
					t.Errorf("the error names the internal type: %q", text)
				}
				if !strings.Contains(text, "literal value") {
					t.Errorf("the error does not say the path runs into a literal value: %q", text)
				}
			}
		})
	}
}
