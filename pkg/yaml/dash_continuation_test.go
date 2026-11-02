package yaml_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

// TestPlainScalarDashContinuation pins a plain scalar with a continuation
// line that starts with "-", which reads as spruce v1.35.17 reads it on
// every route. A comment ends the scalar, the blanks at the ends of each
// line go, and each line break between two lines of text folds to a
// space, as on any other continuation line.
func TestPlainScalarDashContinuation(t *testing.T) {
	cases := []struct {
		doc  string
		want any
	}{
		{"a: v\n  - x #\tc\n", map[string]any{"a": "v - x"}},
		{"a: v\n  - x\t\n", map[string]any{"a": "v - x"}},
		{"a: v\n  - x\t#c\n", map[string]any{"a": "v - x"}},
		{"a: v\n  - x #c\n", map[string]any{"a": "v - x"}},
		{"a: v\n  -x\t\n", map[string]any{"a": "v -x"}},
		{"a: v\n  - x\ty\t\n", map[string]any{"a": "v - x\ty"}},
		{"a: v\n  - x\t\n  y\n", map[string]any{"a": "v - x y"}},
		{"a: v\n  - x\n    y\n", map[string]any{"a": "v - x y"}},
		{"a: v\n  - x\n\n  y\n", map[string]any{"a": "v - x\ny"}},
		{"a: v\n  - x\t\n  - y\t\n", map[string]any{"a": "v - x - y"}},
		{"a: v\n  - x\n   - y\n", map[string]any{"a": "v - x - y"}},
		{"a: v\n  w\n  - x\t\n", map[string]any{"a": "v w - x"}},
		{"a: v\n  - x #c\n  # d\nb: 1\n", map[string]any{"a": "v - x", "b": 1}},
		{"a: v\n  - x\n  #c\nb: 1\n", map[string]any{"a": "v - x", "b": 1}},
		{"k:\n  a: v\n    - x\t#c\n  b: 1\n", map[string]any{"k": map[string]any{"a": "v - x", "b": 1}}},
		{"k:\n- v\n  - x #c\n- w\n", map[string]any{"k": []any{"v - x", "w"}}},
		{"? v\n  - x #c\n: 1\n", map[string]any{"v - x": 1}},
		{"a: &n v\n  - x\t\nb: *n\n", map[string]any{"a": "v - x", "b": "v - x"}},
		{"a: !!str v\n  - x #c\n", map[string]any{"a": "v - x"}},
		{"a: 1\n  - 2\n", map[string]any{"a": "1 - 2"}},
		{"a: v\n  - x:y\n", map[string]any{"a": "v - x:y"}},
		{"a: v\n  - \"x\"\n", map[string]any{"a": "v - \"x\""}},
	}
	for _, c := range cases {
		t.Run(c.doc, func(t *testing.T) {
			r := decodeEveryRoute(t, c.doc)
			if r.strictErr != nil || r.nodeErr != nil {
				t.Fatalf("strict routes refused: %v and %v", r.strictErr, r.nodeErr)
			}
			for name, got := range map[string]any{"Unmarshal": r.strict, "Node.Decode": r.viaNode, "callback any": r.cbAny, "callback Node": r.cbNode} {
				if !reflect.DeepEqual(got, c.want) {
					t.Errorf("%s = %#v, want %#v", name, got, c.want)
				}
			}
		})
	}
}

// TestPlainScalarDashContinuationRefused pins the plain scalars with a
// continuation line that starts with "-" that spruce v1.35.17 refuses,
// because text follows a comment in them or a line of them holds a
// mapping value. Each fails at the place spruce stops, and the error
// never quotes the input.
func TestPlainScalarDashContinuationRefused(t *testing.T) {
	cases := []struct {
		doc       string
		line, col int
		msg       string
	}{
		{"a: s3cr3t\n  - x #c\n  y\n", 3, 3, "a plain scalar cannot continue after a comment"},
		{"a: s3cr3t\n  - x\n  # c\n  y\n", 4, 3, "a plain scalar cannot continue after a comment"},
		{"a: s3cr3t\n  - x #c\n  - y\n", 3, 3, "a plain scalar cannot continue after a comment"},
		{"a: s3cr3t\n  - x: y\n", 2, 6, "mapping values are not allowed in this context"},
		{"a: s3cr3t\n  - x\n  y: z\n", 3, 4, "mapping values are not allowed in this context"},
		{"a: s3cr3t\n  - x:\n", 2, 6, "mapping values are not allowed in this context"},
		{"k:\n  a: s3cr3t\n    - x #c\n    y\n", 4, 5, "a plain scalar cannot continue after a comment"},
	}
	for _, c := range cases {
		t.Run(c.doc, func(t *testing.T) {
			var v any
			err := yaml.Unmarshal([]byte(c.doc), &v)
			var de *yaml.DecodeError
			if !errors.As(err, &de) || de.Line != c.line || de.Column != c.col || de.Msg != c.msg {
				t.Errorf("Unmarshal err = %#v, want %q at %d:%d", err, c.msg, c.line, c.col)
			}
			var n yaml.Node
			nerr := yaml.Unmarshal([]byte(c.doc), &n)
			if !errors.As(nerr, &de) || de.Line != c.line || de.Msg != c.msg {
				t.Errorf("Unmarshal into a Node err = %v, want %q at line %d", nerr, c.msg, c.line)
			}
			for _, e := range []error{err, nerr} {
				if e != nil && strings.Contains(e.Error(), "s3cr3t") {
					t.Errorf("error quotes the input: %v", e)
				}
			}
		})
	}
}
