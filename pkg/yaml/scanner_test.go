package yaml_test

import (
	"errors"
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

// TestUnmarshalFoldsBlockAtEnd checks block scalars whose last line ends
// the document with no line break after it.
func TestUnmarshalFoldsBlockAtEnd(t *testing.T) {
	checkScannerCases(t, []scannerCase{
		{"folded", "a: >\n  x\n  y", map[string]any{"a": "x y"}},
		{"strip", "a: >-\n  x\n  y", map[string]any{"a": "x y"}},
		{"keep", "a: >+\n  x\n  y", map[string]any{"a": "x y"}},
		{"longer lines", "a: >\n  aa\n  bb\n  c", map[string]any{"a": "aa bb c"}},
		{"last line of two characters", "a: >\n  x\n  yy", map[string]any{"a": "x yy"}},
		{"nested", "m:\n  a: >\n    x\n    y", map[string]any{"m": map[string]any{"a": "x y"}}},
		{"sequence item", "a:\n- >\n  x\n  y", map[string]any{"a": []any{"x y"}}},
		{"empty line", "a: >\n  x\n  y\n\n  z", map[string]any{"a": "x y\nz"}},
		{"trailing space", "a: >\n  x\n  y ", map[string]any{"a": "x y "}},
		{"literal with a trailing space", "a: |\n  x\n  y ", map[string]any{"a": "x\ny "}},
		{"last line of spaces", "a: >\n  x\n  ", map[string]any{"a": "x\n"}},
		{"keep with a last line of spaces", "a: >+\n  x\n  ", map[string]any{"a": "x\n"}},
		{"literal keep with a last line of spaces", "a: |+\n  x\n  ", map[string]any{"a": "x\n"}},
		{"nested keep with a last line of spaces", "m:\n  a: |+\n    x\n    ", map[string]any{"m": map[string]any{"a": "x\n"}}},
		{"keep with an empty line and a last line of spaces", "a: |+\n  x\n\n  ", map[string]any{"a": "x\n\n"}},
		{"keep with a last line of more spaces", "a: |+\n  x\n     ", map[string]any{"a": "x\n   "}},
		{"clip with a last line of more spaces", "a: |\n  x\n     ", map[string]any{"a": "x\n   "}},
		{"final line break", "a: >\n  x\n  y\n", map[string]any{"a": "x y\n"}},
		{"keep with a last line of a tab", "a: |+\n  x\n  \t", map[string]any{"a": "x\n\t"}},
		{"clip with a last line of a tab", "a: |\n  x\n  \t", map[string]any{"a": "x\n\t"}},
		{"strip with a last line of a tab", "a: |-\n  x\n  \t", map[string]any{"a": "x\n\t"}},
		{"folded keep with a last line of a tab", "a: >+\n  x\n  \t", map[string]any{"a": "x\n\t"}},
		{"folded with a last line of a tab", "a: >\n  x\n  y\n  \t", map[string]any{"a": "x y\n\t"}},
		{"last line of two tabs", "a: |+\n  x\n  \t\t", map[string]any{"a": "x\n\t\t"}},
		{"last line of tabs and a space", "a: |+\n  x\n  \t \t", map[string]any{"a": "x\n\t \t"}},
		{"last line of a space and a tab", "a: |\n  x\n   \t", map[string]any{"a": "x\n \t"}},
		{"empty line and a last line of a tab", "a: |+\n  x\n\n  \t", map[string]any{"a": "x\n\n\t"}},
		{"sequence item with a last line of a tab", "l:\n- |\n  x\n  \t", map[string]any{"l": []any{"x\n\t"}}},
	})
}

// TestUnmarshalReadsQuotedTabs checks quoted scalars holding a literal
// tab, which goccy's scanner failed on in a double-quoted scalar.
func TestUnmarshalReadsQuotedTabs(t *testing.T) {
	checkScannerCases(t, []scannerCase{
		{"value", "a: \"p\tq\"\nb: 1\n", map[string]any{"a": "p\tq", "b": 1}},
		{"flow sequence", "a: [\"p\tq\", x]\n", map[string]any{"a": []any{"p\tq", "x"}}},
		{"flow mapping", "a: {k: \"p\tq\", m: x}\nb: 1\n", map[string]any{"a": map[string]any{"k": "p\tq", "m": "x"}, "b": 1}},
		{"key", "\"p\tq\": 1\nb: 2\n", map[string]any{"p\tq": 1, "b": 2}},
		{"two tabs", "a: \"p\tq\tr\"\nb: 1\n", map[string]any{"a": "p\tq\tr", "b": 1}},
		{"multi-line", "a: \"p\tq\n  r\ts\"\nb: 1\n", map[string]any{"a": "p\tq r\ts", "b": 1}},
		{"tagged", "a: !!str \"p\tq\"\nb: 1\n", map[string]any{"a": "p\tq", "b": 1}},
		{"end of the input", "a: \"p\tq\"", map[string]any{"a": "p\tq"}},
		{"single-quoted", "a: 'p\tq'\nb: 1\n", map[string]any{"a": "p\tq", "b": 1}},
		{"single-quoted in a flow mapping", "a: {k: 'p\tq'}\nb: 1\n", map[string]any{"a": map[string]any{"k": "p\tq"}, "b": 1}},
	})
}

// TestQuotedTabPositions checks the places of the nodes after a
// double-quoted scalar holding a tab, and of an error there. A tab counts
// as one column, as yaml.v3 counts it.
func TestQuotedTabPositions(t *testing.T) {
	for _, c := range []struct {
		doc          string
		path         []int
		line, column int
	}{
		{"a: {k: \"p\tq\", m: x}\n", []int{0, 1, 1}, 1, 8},
		{"a: {k: \"p\tq\", m: x}\n", []int{0, 1, 2}, 1, 15},
		{"a: {k: \"p\tq\", m: x}\n", []int{0, 1, 3}, 1, 18},
		{"\"p\tq\": 1\nb: 2\n", []int{0, 1}, 1, 8},
		{"\"p\tq\": 1\nb: 2\n", []int{0, 2}, 2, 1},
		{"- [\"p\tq\", x]\n", []int{0, 0, 1}, 1, 11},
	} {
		var n yaml.Node
		if err := yaml.Unmarshal([]byte(c.doc), &n); err != nil {
			t.Fatalf("Unmarshal(%q): %v", c.doc, err)
		}
		at := &n
		for _, i := range c.path {
			at = at.Content[i]
		}
		if at.Line != c.line || at.Column != c.column {
			t.Errorf("%q at %v: %d:%d, want %d:%d", c.doc, c.path, at.Line, at.Column, c.line, c.column)
		}
	}

	var v struct {
		A struct {
			K string
			M int
		}
	}
	err := yaml.Unmarshal([]byte("a: {k: \"p\tq\", m: x}\n"), &v)
	var de *yaml.DecodeError
	if !errors.As(err, &de) || de.Line != 1 || de.Column != 18 {
		t.Errorf("Unmarshal error = %#v, want one at 1:18", err)
	}
}
