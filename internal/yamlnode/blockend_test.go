package yamlnode

import (
	"reflect"
	"strings"
	"testing"

	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/token"
)

// TestParseBytesFoldsBlockAtEnd parses block scalars whose last line ends
// the input with no line break, and checks each value against what spruce
// v1.35.17 merges the document to. goccy leaves the line break before a
// last line of one character unfolded, and drops the blanks that end the
// last line.
func TestParseBytesFoldsBlockAtEnd(t *testing.T) {
	cases := []struct {
		name, in string
		want     []string
	}{
		{"folded", "a: >\n  x\n  y", []string{`str "a"`, `block "x y"`}},
		{"strip", "a: >-\n  x\n  y", []string{`str "a"`, `block "x y"`}},
		{"keep", "a: >+\n  x\n  y", []string{`str "a"`, `block "x y"`}},
		{"nested", "m:\n  a: >\n    x\n    y", []string{`str "m"`, `str "a"`, `block "x y"`}},
		{"sequence item", "a:\n- >\n  x\n  y", []string{`str "a"`, `block "x y"`}},
		{"longer first line", "a: >\n  xx\n  y", []string{`str "a"`, `block "xx y"`}},
		{"strip with a longer first line", "a: >-\n  xxxx\n  y", []string{`str "a"`, `block "xxxx y"`}},
		{"three lines", "a: >\n  x\n  y\n  z", []string{`str "a"`, `block "x y z"`}},
		{"three longer lines", "a: >\n  aa\n  bb\n  c", []string{`str "a"`, `block "aa bb c"`}},
		{"empty line", "a: >\n  x\n  y\n\n  z", []string{`str "a"`, `block "x y\nz"`}},
		{"keep with an empty line", "a: >+\n  x\n\n  y", []string{`str "a"`, `block "x\ny"`}},
		{"multi-byte last line", "a: >\n  x\n  é", []string{`str "a"`, `block "x é"`}},
		{"comment after the header", "a: > # c\n  x\n  y", []string{`str "a"`, `block "x y"`}},
		{"last line of two characters", "a: >\n  x\n  yy", []string{`str "a"`, `block "x yy"`}},
		{"lines of two characters", "a: >\n  xx\n  yy", []string{`str "a"`, `block "xx yy"`}},
		{"single line", "a: >\n  y", []string{`str "a"`, `block "y"`}},
		{"more indented last line", "a: >\n  x\n    y", []string{`str "a"`, `block "x\n  y"`}},
		{"indentation indicator", "a: >2\n   x\n  y", []string{`str "a"`, `block " x\ny"`}},
		{"comment after the header and a longer line", "a: > # c\n  xx\n  y", []string{`str "a"`, `block "xx y"`}},
		{"line breaks of CR and LF", "a: >\r\n  x\r\n  y", []string{`str "a"`, `block "x y"`}},
		{"last line that looks like a comment", "a: >\n  x\n  #y", []string{`str "a"`, `block "x #y"`}},
		{"last line ending in a hash", "a: >\n  x\n  y # c", []string{`str "a"`, `block "x y # c"`}},
		{"strip with a last line of two characters", "a: >-\n  x\n  yy", []string{`str "a"`, `block "x yy"`}},
		{"trailing space", "a: >\n  x\n  y ", []string{`str "a"`, `block "x y "`}},
		{"trailing tab", "a: >\n  x\n  y\t", []string{`str "a"`, `block "x y\t"`}},
		{"literal", "a: |\n  x\n  y", []string{`str "a"`, `block "x\ny"`}},
		{"literal with a trailing space", "a: |\n  x\n  y ", []string{`str "a"`, `block "x\ny "`}},
		{"strip literal", "a: |-\n  x\n  y", []string{`str "a"`, `block "x\ny"`}},
		{"anchored", "a: &f >\n  x\n  y", []string{`str "a"`, `block "x y"`}},
		{"last line of spaces", "a: >\n  x\n  ", []string{`str "a"`, `block "x\n"`}},
		{"keep with a last line of spaces", "a: >+\n  x\n  ", []string{`str "a"`, `block "x\n"`}},
		{"final line break", "a: >\n  x\n  y\n", []string{`str "a"`, `block "x y\n"`}},
		{"key after the block", "m:\n  a: >\n    x\n    y\nb: 1", []string{`str "m"`, `str "a"`, `block "x y\n"`, `str "b"`, `*ast.IntegerNode 1`}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parsedScalars(t, c.in); !reflect.DeepEqual(got, c.want) {
				t.Errorf("ParseBytes(%q) scalars = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestFoldBlockAtEndKeepsPositions checks that the fix-up changes only
// the value of the block's content token, so every token keeps the
// position goccy gives it in the text as written.
func TestFoldBlockAtEndKeepsPositions(t *testing.T) {
	for _, in := range []string{"a: >\n  x\n  y", "m:\n  a: |\n    x\n    y ", "- >-\n  xx\n  y\t"} {
		toks := lexer.Tokenize(in)
		want := make([]token.Position, len(toks))
		origins := make([]string, len(toks))
		for i, tk := range toks {
			want[i], origins[i] = *tk.Position, tk.Origin
		}
		foldBlockAtEnd(in, toks)
		for i, tk := range toks {
			if *tk.Position != want[i] || tk.Origin != origins[i] {
				t.Errorf("foldBlockAtEnd(%q) token %d at %+v %q, want %+v %q", in, i, *tk.Position, tk.Origin, want[i], origins[i])
			}
		}
		if v := toks[len(toks)-1].Value; strings.HasSuffix(v, "\n") || !strings.Contains(v, "y") {
			t.Errorf("foldBlockAtEnd(%q) content = %q", in, v)
		}
	}
}

// TestEndsInBlockScalar checks the inputs foldBlockAtEnd leaves alone.
func TestEndsInBlockScalar(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"a: >\n  x\n  y", true},
		{"a: | # c\n  x\n  y", true},
		{"a: >\n  x\n  y\n", false},
		{"a: >\n  x\n  y\r", false},
		{"a: >\n  x\n  ", false},
		{"a: >\n  x\n  \t", true},
		{"a: >\n  x\n  \t ", true},
		{"a: >\n  x\n  y\nb: c", false},
		{"a: x", false},
		{"a: >\n  x\n  y\n# c", false},
		{"", false},
	}
	for _, c := range cases {
		if got := endsInBlockScalar(c.in, lexer.Tokenize(c.in)); got != c.want {
			t.Errorf("endsInBlockScalar(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
