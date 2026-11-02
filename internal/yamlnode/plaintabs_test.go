package yamlnode

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/goccy/go-yaml/ast"
)

// typedScalars records every scalar it visits, keys included, in
// document order, as its Go type and value. A block scalar counts once.
type typedScalars struct{ out *[]string }

func (c typedScalars) Visit(n ast.Node) ast.Visitor {
	switch x := n.(type) {
	case *ast.AnchorNode:
		ast.Walk(c, x.Value)
		return nil
	case *ast.TagNode:
		ast.Walk(c, x.Value)
		return nil
	case *ast.AliasNode:
		*c.out = append(*c.out, "alias")
		return nil
	case *ast.LiteralNode:
		*c.out = append(*c.out, fmt.Sprintf("block %q", x.Value.Value))
		return nil
	case *ast.StringNode:
		*c.out = append(*c.out, fmt.Sprintf("str %q", x.Value))
	case ast.ScalarNode:
		*c.out = append(*c.out, fmt.Sprintf("%T %v", x, x.GetValue()))
	}
	return c
}

func parsedScalars(t *testing.T, in string) []string {
	t.Helper()
	file, err := ParseBytes([]byte(in), 0)
	if err != nil {
		t.Fatalf("ParseBytes(%q): %v", in, err)
	}
	var got []string
	for _, doc := range file.Docs {
		ast.Walk(typedScalars{out: &got}, doc)
	}
	return got
}

// TestParseBytesKeepsPlainTabs checks that a tab between two characters
// of a plain scalar stays in its value, and that the value is then a
// string, against what spruce v1.35.17 merges each document to. A tab at
// the end of a line, before a comment, or at the start of a continuation
// line is a separator, which spruce drops as well.
func TestParseBytesKeepsPlainTabs(t *testing.T) {
	cases := []struct {
		name, in string
		want     []string
	}{
		{"value", "a: x\ty\n", []string{`str "a"`, `str "x\ty"`}},
		{"value before a comment", "a: x\ty\t# c\n", []string{`str "a"`, `str "x\ty"`}},
		{"trailing tab", "a: x\t\n", []string{`str "a"`, `str "x"`}},
		{"tab before a comment", "a: x\t#c\n", []string{`str "a"`, `str "x"`}},
		{"hash after a tab inside the text", "a: x\ty#z\n", []string{`str "a"`, `str "x\ty#z"`}},
		{"colon after a tab", "a: x\t:y\n", []string{`str "a"`, `str "x\t:y"`}},
		{"tab after a space", "a: x \ty\n", []string{`str "a"`, `str "x \ty"`}},
		{"run of tabs", "a: x\t\t\ty\n", []string{`str "a"`, `str "x\t\t\ty"`}},
		{"tabs and a space", "a: x\t\ty z\n", []string{`str "a"`, `str "x\t\ty z"`}},
		{"tab separates the value", "a:\tx\n", []string{`str "a"`, `str "x"`}},
		{"multi-line", "a: x\ty\n  p\tq\n", []string{`str "a"`, `str "x\ty p\tq"`}},
		{"multi-line with an empty line", "a: x\ty\n\n  p\tq\n", []string{`str "a"`, `str "x\ty\np\tq"`}},
		{"multi-line trailing tab", "a: x\t\n  y\n", []string{`str "a"`, `str "x y"`}},
		{"multi-line leading tab", "a: x\n \ty\n", []string{`str "a"`, `str "x y"`}},
		{"flow sequence", "a: [x\ty]\n", []string{`str "a"`, `str "x\ty"`}},
		{"flow sequence over two lines", "a: [x\ty,\n  p\tq]\n", []string{`str "a"`, `str "x\ty"`, `str "p\tq"`}},
		{"flow mapping", "a: {k: x\ty}\n", []string{`str "a"`, `str "k"`, `str "x\ty"`}},
		{"anchor and alias", "a: &n x\ty\nb: *n\n", []string{`str "a"`, `str "x\ty"`, `str "b"`, "alias"}},
		{"key", "x\ty: 1\n", []string{`str "x\ty"`, `*ast.IntegerNode 1`}},
		{"explicit key and value", "? x\ty\n: z\tw\n", []string{`str "x\ty"`, `str "z\tw"`}},
		{"tagged", "a: !!str x\ty\n", []string{`str "a"`, `str "x\ty"`}},
		{"would be an integer", "a: 1\t2\n", []string{`str "a"`, `str "1\t2"`}},
		{"would be a boolean", "a: true\tx\n", []string{`str "a"`, `str "true\tx"`}},
		{"would be null", "a: ~\tx\n", []string{`str "a"`, `str "~\tx"`}},
		{"integer with a trailing tab", "a: 12\t\n", []string{`str "a"`, `*ast.IntegerNode 12`}},
		{"multi-byte characters", "a: é\tü\n", []string{`str "a"`, `str "é\tü"`}},
		{"literal block", "a: |\n  x\ty\n", []string{`str "a"`, `block "x\ty\n"`}},
		{"single-quoted", "a: 'x\ty'\n", []string{`str "a"`, `str "x\ty"`}},
		{"comment holding a tab", "a: x\ty\n# c\td\nb: 1\n", []string{`str "a"`, `str "x\ty"`, `str "b"`, `*ast.IntegerNode 1`}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parsedScalars(t, c.in); !reflect.DeepEqual(got, c.want) {
				t.Errorf("ParseBytes(%q) scalars = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestPlainValue(t *testing.T) {
	cases := []struct {
		origin, want string
		ok           bool
	}{
		{" x\ty", "x\ty", true},
		{" x\ty\t", "x\ty", true},
		{"\n  p\tq", "p\tq", true},
		{" x\ty\n  p\tq\n", "x\ty p\tq", true},
		{" x\ty\n\n\n  p\tq", "x\ty\n\np\tq", true},
		{" x\r\n  y\r\n", "x y", true},
		{" \t\n", "", false},
	}
	for _, c := range cases {
		got, ok := plainValue(c.origin)
		if got != c.want || ok != c.ok {
			t.Errorf("plainValue(%q) = %q, %v; want %q, %v", c.origin, got, ok, c.want, c.ok)
		}
	}
}
