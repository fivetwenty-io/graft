package yaml_test

import (
	"reflect"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

// scannerCase is a document and the value spruce v1.35.17 merges it to.
type scannerCase struct {
	name, doc string
	want      any
}

// checkScannerCases decodes each case with Unmarshal, and with Decode on
// the Node that Unmarshal returns, and compares both with the case's
// value.
func checkScannerCases(t *testing.T, cases []scannerCase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var got any
			if err := yaml.Unmarshal([]byte(c.doc), &got); err != nil {
				t.Fatalf("Unmarshal(%q): %v", c.doc, err)
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("Unmarshal(%q) = %#v, want %#v", c.doc, got, c.want)
			}
			var n yaml.Node
			if err := yaml.Unmarshal([]byte(c.doc), &n); err != nil {
				t.Fatalf("Unmarshal(%q) into a Node: %v", c.doc, err)
			}
			var decoded any
			if err := n.Decode(&decoded); err != nil {
				t.Fatalf("Decode(%q): %v", c.doc, err)
			}
			if !reflect.DeepEqual(decoded, c.want) {
				t.Errorf("Decode(%q) = %#v, want %#v", c.doc, decoded, c.want)
			}
		})
	}
}

func TestUnmarshalKeepsPlainScalarTabs(t *testing.T) {
	checkScannerCases(t, []scannerCase{
		{"value", "a: x\ty\n", map[string]any{"a": "x\ty"}},
		{"before a comment", "a: x\ty\t# c\n", map[string]any{"a": "x\ty"}},
		{"trailing tab", "a: x\t\n", map[string]any{"a": "x"}},
		{"multi-line", "a: x\ty\n  p\tq\n", map[string]any{"a": "x\ty p\tq"}},
		{"flow", "a: [x\ty, {k: p\tq}]\n", map[string]any{"a": []any{"x\ty", map[string]any{"k": "p\tq"}}}},
		{"key", "x\ty: 1\n", map[string]any{"x\ty": 1}},
		{"would be an integer", "a: 1\t2\n", map[string]any{"a": "1\t2"}},
		{"would be a boolean", "a: true\tx\n", map[string]any{"a": "true\tx"}},
	})
}
