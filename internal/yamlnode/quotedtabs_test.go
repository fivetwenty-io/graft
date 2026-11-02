package yamlnode

import (
	"reflect"
	"testing"
)

// TestParseBytesReadsQuotedTabs checks quoted scalars holding a literal
// tab, against what spruce v1.35.17 merges each document to. goccy's
// scanner lost its place after a tab inside a double-quoted scalar, and
// failed the document or read the rest of the line wrong. A
// single-quoted scalar never had the trouble.
func TestParseBytesReadsQuotedTabs(t *testing.T) {
	cases := []struct {
		name, in string
		want     []string
	}{
		{"value", "a: \"p\tq\"\nb: 1\n", []string{`str "a"`, `str "p\tq"`, `str "b"`, `*ast.IntegerNode 1`}},
		{"value before a comment", "a: \"p\tq\" # c\nb:  1\n", []string{`str "a"`, `str "p\tq"`, `str "b"`, `*ast.IntegerNode 1`}},
		{"flow sequence", "a: [\"p\tq\"]\n", []string{`str "a"`, `str "p\tq"`}},
		{"flow sequence of two", "a: [\"p\tq\", \"r\ts\"]\n", []string{`str "a"`, `str "p\tq"`, `str "r\ts"`}},
		{"flow mapping", "a: {k: \"p\tq\"}\nb: 1\n", []string{`str "a"`, `str "k"`, `str "p\tq"`, `str "b"`, `*ast.IntegerNode 1`}},
		{"two tabs", "a: \"p\tq\tr\"\nb: 1\n", []string{`str "a"`, `str "p\tq\tr"`, `str "b"`, `*ast.IntegerNode 1`}},
		{"run of tabs", "a: \"p\t\tq\"\nb: 1\n", []string{`str "a"`, `str "p\t\tq"`, `str "b"`, `*ast.IntegerNode 1`}},
		{"escaped tab", "a: \"p\\tq\"\nb: 1\n", []string{`str "a"`, `str "p\tq"`, `str "b"`, `*ast.IntegerNode 1`}},
		{"escaped backslash", "a: \"p\\\\tq\"\nb: 1\n", []string{`str "a"`, `str "p\\tq"`, `str "b"`, `*ast.IntegerNode 1`}},
		{"escaped quote", "a: \"x\\\"p\tq\\\"\"\nb: 1\n", []string{`str "a"`, `str "x\"p\tq\""`, `str "b"`, `*ast.IntegerNode 1`}},
		{"key", "\"p\tq\": 1\nb: 2\n", []string{`str "p\tq"`, `*ast.IntegerNode 1`, `str "b"`, `*ast.IntegerNode 2`}},
		{"multi-line", "a: \"p\tq\n  r\ts\"\nb: 1\n", []string{`str "a"`, `str "p\tq r\ts"`, `str "b"`, `*ast.IntegerNode 1`}},
		{"tab before the line break", "a: \"p\t\n  q\"\nb: 1\n", []string{`str "a"`, `str "p q"`, `str "b"`, `*ast.IntegerNode 1`}},
		{"tab after an escaped line break", "a: \"p\\\n  \tq\"\nb: 1\n", []string{`str "a"`, `str "pq"`, `str "b"`, `*ast.IntegerNode 1`}},
		{"end of the input", "a: \"p\tq\"", []string{`str "a"`, `str "p\tq"`}},
		{"tab before the closing quote", "a: \"p\t\"\nb: 1\n", []string{`str "a"`, `str "p\t"`, `str "b"`, `*ast.IntegerNode 1`}},
		{"multi-byte characters", "a: \"é\tü\"\nb: 1\n", []string{`str "a"`, `str "é\tü"`, `str "b"`, `*ast.IntegerNode 1`}},
		{"in a literal block", "a: |\n  \"p\tq\"\nb:  1\n", []string{`str "a"`, `block "\"p\tq\"\n"`, `str "b"`, `*ast.IntegerNode 1`}},
		{"in a comment", "# \"p\tq\"\nb: 1\n", []string{`str "b"`, `*ast.IntegerNode 1`}},
		{"in a plain scalar", "a: x\"p\tq\"\nb: 1\n", []string{`str "a"`, `str "x\"p\tq\""`, `str "b"`, `*ast.IntegerNode 1`}},
		{"in a single-quoted scalar", "a: '\"p\tq\"'\nb: 1\n", []string{`str "a"`, `str "\"p\tq\""`, `str "b"`, `*ast.IntegerNode 1`}},
		{"anchored", "a: &n \"p\tq\"\nb: *n\n", []string{`str "a"`, `str "p\tq"`, `str "b"`, "alias"}},
		{"tagged", "a: !!str \"p\tq\"\nb: 1\n", []string{`str "a"`, `str "p\tq"`, `str "b"`, `*ast.IntegerNode 1`}},
		{"single-quoted", "a: 'p\tq'\nb: 1\n", []string{`str "a"`, `str "p\tq"`, `str "b"`, `*ast.IntegerNode 1`}},
		{"single-quoted in a flow sequence", "a: ['p\tq']\n", []string{`str "a"`, `str "p\tq"`}},
		{"single-quoted in a flow mapping", "a: {k: 'p\tq'}\nb: 1\n", []string{`str "a"`, `str "k"`, `str "p\tq"`, `str "b"`, `*ast.IntegerNode 1`}},
		{"single-quoted multi-line", "a: 'p\tq\n  r\ts'\nb: 1\n", []string{`str "a"`, `str "p\tq r\ts"`, `str "b"`, `*ast.IntegerNode 1`}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := parsedScalars(t, c.in); !reflect.DeepEqual(got, c.want) {
				t.Errorf("ParseBytes(%q) scalars = %q, want %q", c.in, got, c.want)
			}
		})
	}
}
