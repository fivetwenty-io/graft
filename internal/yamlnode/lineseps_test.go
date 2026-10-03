package yamlnode

import (
	"testing"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"
)

// scalarCollector records the value of every scalar it visits, in
// document order, with a literal block scalar's value read once.
type scalarCollector struct{ values *[]string }

func (c scalarCollector) Visit(n ast.Node) ast.Visitor {
	switch x := n.(type) {
	case *ast.StringNode:
		*c.values = append(*c.values, x.Value)
	case *ast.LiteralNode:
		*c.values = append(*c.values, x.Value.Value)
		return nil
	}
	return c
}

// TestParseBytesReadsLineSeparators parses scalars that hold a LINE
// SEPARATOR or PARAGRAPH SEPARATOR, and checks each value against what
// spruce v1.35.17 reads for the same text.
func TestParseBytesReadsLineSeparators(t *testing.T) {
	const ls, ps = "\u2028", "\u2029"
	cases := []struct{ name, in, want string }{
		{"spaces after", "k: 'a" + ls + "  b'\n", "a" + ls + "b"},
		{"spaces before", "k: 'a  " + ls + "b'\n", "a" + ls + "b"},
		{"tab after", "k: 'a" + ls + "\tb'\n", "a" + ls + "b"},
		{"line break after", "k: 'a" + ls + "\n  b'\n", "a" + ls + "\nb"},
		{"two line breaks after", "k: 'a" + ls + "\n\n  b'\n", "a" + ls + "\n\nb"},
		{"two separators", "k: 'a" + ls + ls + "  b'\n", "a" + ls + ls + "b"},
		{"separator last", "k: 'a" + ls + "'\n", "a" + ls},
		{"separator first", "k: '" + ls + "  a'\n", ls + "a"},
		{"escaped quote", "k: 'it''s" + ls + " x'\n", "it's" + ls + "x"},
		{"CRLF fold", "k: 'a  \r\n b" + ls + "'\n", "a b" + ls},
		{"paragraph separator", "k: 'a " + ps + " \n b'\n", "a" + ps + "\nb"},
		{"literal", "k: |\n  a" + ls + "  b\n", "a" + ls + "b\n"},
		{"literal with content past the indentation", "k: |\n  a" + ls + "   b\n", "a" + ls + " b\n"},
		{"literal clip after a separator", "k: |\n  a" + ls + "  b\n  c" + ls + "\n", "a" + ls + "b\nc" + ls},
		{"literal strip", "k: |-\n  a" + ls + "\n  b\n", "a" + ls + "\nb"},
		{"literal strip after a separator", "k: |-\n  a" + ls + "\n", "a"},
		{"literal keep after a separator", "k: |+\n  a" + ls + "\n", "a" + ls + "\n"},
		{"literal clip after two separators", "k: |\n  a" + ls + ls + "\n\n", "a" + ls},
		{"literal strip of a separator line", "k: |-\n  a" + ls + "\n  " + ls + "\n", "a"},
		{"literal clip of a separator line", "k: |\n  a\n  " + ls + "\n", "a\n"},
		{"literal with an indentation indicator", "k: |2-\n  " + ls + "  a\n  b\n", ls + "a\nb"},
		{"literal in a sequence", "l:\n- |\n    a" + ls + "    b\n", "a" + ls + "b\n"},
		{"literal with an empty line", "k: |\n  a" + ls + "\n\n  b\n", "a" + ls + "\n\nb\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			file, err := ParseBytes([]byte(c.in), parser.ParseComments)
			if err != nil {
				t.Fatalf("ParseBytes(%q): %v", c.in, err)
			}
			var values []string
			for _, doc := range file.Docs {
				ast.Walk(scalarCollector{&values}, doc)
			}
			if len(values) == 0 || values[len(values)-1] != c.want {
				t.Errorf("ParseBytes(%q) scalars = %q, want the last to be %q", c.in, values, c.want)
			}
		})
	}
}
