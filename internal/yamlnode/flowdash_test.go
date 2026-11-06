package yamlnode

import (
	"strings"
	"testing"
)

// TestFlowBlockEntryRefused pins that a "-" followed by a blank inside a
// flow collection, which libyaml reads as a block sequence entry, fails
// the parse at the dash, as it fails in spruce. goccy read most of these
// as a nested list. A dash folded into a plain scalar that continues
// onto the dash's line, as in "[x" over "- w]", fails here too, where
// spruce reads the text "x - w".
func TestFlowBlockEntryRefused(t *testing.T) {
	const msg = " a block sequence entry cannot appear inside a flow collection"
	cases := []struct{ doc, at string }{
		{"a: [x,\n- w]\n", "[2:1]"},
		{"a: [x,\n  - w]\n", "[2:3]"},
		{"a: [\n- w]\n", "[2:1]"},
		{"a: [x,\n  -\n]\n", "[2:3]"},
		{"a: [x, - w]\n", "[1:8]"},
		{"a: [x,\n- w, y]\nb: 1\n", "[2:1]"},
		{"a: {b: [x,\n- w]}\n", "[2:1]"},
		{"a: [x,\n\n- w]\n", "[3:1]"},
		{"a: [x, # c\n- w]\n", "[2:1]"},
		{"a: [[x,\n- w]]\n", "[2:1]"},
		{"a: [x,\n-\tw]\n", "[2:1]"},
		{"a: [x, {k: - w}]\n", "[1:12]"},
		{"a: {b: x,\n- w}\n", "[2:1]"},
		{"a: {k: v,\n- w: 1}\n", "[2:1]"},
		{"a: {b: [x\n- w]}\n", "[2:1]"},
		{"a:\n  - [w\n    - w]\n", "[3:5]"},
		{"a: [x\n  - w]\n", "[2:3]"},
	}
	for _, c := range cases {
		t.Run(c.doc, func(t *testing.T) {
			_, err := ParseBytes([]byte(c.doc), 0)
			if err == nil {
				t.Fatal("ParseBytes accepted the document")
			}
			got, _, _ := strings.Cut(err.Error(), "\n")
			if want := c.at + msg; got != want {
				t.Fatalf("ParseBytes = %q, want %q", got, want)
			}
		})
	}
}

// TestFlowDashScalarsKept pins the dashes in a flow collection that
// libyaml reads as text, which are a dash not followed by a blank and a
// dash inside quotes, and a dash line after a block collection's flow
// value ends.
func TestFlowDashScalarsKept(t *testing.T) {
	for _, doc := range []string{
		"a: [x, -]\n",
		"a: [-]\n",
		"a: {k: -}\n",
		"a: [-, x]\n",
		"a: [x,\n-w]\n",
		"a: [x, -w,\n -v]\n",
		"a: [x,\n  y - w]\n",
		"a: [\"x\n- w\"]\n",
		"a: [x, \"- w\"]\n",
		"a: [x]\nb:\n- w\n",
		"- [x]\n- {a: 1}\n- w\n",
		"a: [x]\n---\n- w\n",
	} {
		if _, err := ParseBytes([]byte(doc), 0); err != nil {
			t.Errorf("%q: %v", doc, err)
		}
	}
}
