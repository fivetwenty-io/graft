package yaml_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

// TestMarshalQuotesLiteralMergeKeyName marshals a map and a MapSlice
// whose key is the string "<<". The key is written quoted, and the
// output decodes back to the same value.
func TestMarshalQuotesLiteralMergeKeyName(t *testing.T) {
	cases := []struct {
		name string
		in   any
		want any
	}{
		{"map", map[string]any{"<<": map[string]any{"x": 1}, "b": 1}, map[string]any{"<<": map[string]any{"x": 1}, "b": 1}},
		{"map scalar", map[string]any{"<<": "s"}, map[string]any{"<<": "s"}},
		{"mapslice", yaml.MapSlice{{Key: "<<", Value: "s"}, {Key: "c", Value: 1}}, map[string]any{"<<": "s", "c": 1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			out, err := yaml.Marshal(c.in)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(out), `"<<":`) {
				t.Errorf("output %q has no quoted \"<<\" key", out)
			}
			var got any
			if err := yaml.Unmarshal(out, &got); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("decoded %#v, want %#v", got, c.want)
			}
		})
	}
}
