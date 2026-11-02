package yaml_test

import (
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

// TestUnmarshalReadsTagThenAnchor checks a scalar core tag followed by an
// anchor whose scalar starts on a later line. The parser read the scalar
// and then skipped the token after it, so the next key went missing with
// no error. Each value is the one spruce v1.35.17 gives.
func TestUnmarshalReadsTagThenAnchor(t *testing.T) {
	checkScannerCases(t, []scannerCase{
		{"next line", "a: !!str &n\n  x\nb: 1\n", map[string]any{"a": "x", "b": 1}},
		{"tab after the tag", "a: !!str\t&n\n  x\nb: 1\n", map[string]any{"a": "x", "b": 1}},
		{"comment after the anchor", "a: !!str &n # c\n  x\nb: 1\n", map[string]any{"a": "x", "b": 1}},
		{"tabs and a comment", "a: !!str\t&n\t# c\n  x\nb: 1\n", map[string]any{"a": "x", "b": 1}},
		{"comment line before the value", "a: !!str &n\n  # c\n  x\nb: 1\n", map[string]any{"a": "x", "b": 1}},
		{"anchor on the next line", "a: !!str\n  &n\n  x\nb: 1\n", map[string]any{"a": "x", "b": 1}},
		{"comment after the tag", "a: !!str # c\n  &n\n    x\nb: 1\n", map[string]any{"a": "x", "b": 1}},
		{"two lines", "a: !!str &n\n  x\n  y\nb: 1\n", map[string]any{"a": "x y", "b": 1}},
		{"int and an alias", "a: !!int &n\n  12\nb: *n\n", map[string]any{"a": 12, "b": 12}},
		{"bool and tabs", "a: !!bool\t&n\t\n  yes\nb: *n\n", map[string]any{"a": true, "b": true}},
		{"nested", "m:\n  a: !!str &n\n    yes\n  b: 1\nc: 2\n", map[string]any{"m": map[string]any{"a": "yes", "b": 1}, "c": 2}},
	})
}

// TestTagThenAnchorReadsAsAnchorThenTag checks that a scalar core tag
// followed by an anchor gives the same tree as the anchor followed by
// the tag, node for node and place for place.
func TestTagThenAnchorReadsAsAnchorThenTag(t *testing.T) {
	for _, c := range []struct{ doc, swapped string }{
		{"a: !!str &n\n  x\nb: 1\n", "a: &n !!str\n  x\nb: 1\n"},
		{"a: !!str\t&n\n  x\nb: 1\n", "a: &n\t!!str\n  x\nb: 1\n"},
		{"a: !!str &n # c\n  x\nb: 1\n", "a: &n !!str # c\n  x\nb: 1\n"},
		{"a: !!int &n\n  12\nb: *n\n", "a: &n !!int\n  12\nb: *n\n"},
		{"a: !!str &n {k: v}\nb: 1\n", "a: &n !!str {k: v}\nb: 1\n"},
		{"a: !!str\t&n\t[x]\nb: 1\n", "a: &n\t!!str\t[x]\nb: 1\n"},
		{"a: !!str &n\n  k: v\nb: 1\n", "a: &n !!str\n  k: v\nb: 1\n"},
	} {
		var got, want yaml.Node
		if err := yaml.Unmarshal([]byte(c.doc), &got); err != nil {
			t.Errorf("Unmarshal(%q): %v", c.doc, err)
			continue
		}
		if err := yaml.Unmarshal([]byte(c.swapped), &want); err != nil {
			t.Fatalf("Unmarshal(%q): %v", c.swapped, err)
		}
		sameNodes(t, c.doc, &got, &want)
	}
}

// TestTagThenAnchorOnCollectionFails checks that a scalar core tag on a
// collection is a syntax error when an anchor sits between them, with a
// space or a tab around the anchor, as it is with no anchor and with the
// anchor first. The parser used to read the collection and drop the key
// after it with no error. The error gives a line and a column and quotes
// nothing from the input. The columns follow goccy's count, which places
// the token after a tag at the blank before it and counts no tab between
// two tokens.
func TestTagThenAnchorOnCollectionFails(t *testing.T) {
	for _, c := range []struct {
		doc          string
		line, column int
	}{
		{"a: !!str &n {secret: hidden}\nb: 1\n", 1, 12},
		{"a: !!str\t&n\t{secret: hidden}\nb: 1\n", 1, 11},
		{"a: !!str\t&n\t {secret: hidden}\nb: 1\n", 1, 11},
		{"a: !!str\t&n \t{secret: hidden}\nb: 1\n", 1, 11},
		{"a: !!str &n [secret]\nb: 1\n", 1, 12},
		{"a: !!str\t&n\t[secret]\nb: 1\n", 1, 11},
		{"a: !!int &n {secret: hidden}\nb: 1\n", 1, 12},
		{"a: !!str &n {secret: hidden}\n", 1, 12},
		{"m:\n  a: !!str &n {secret: hidden}\n  b: 1\nc: 2\n", 2, 14},
		{"a: !!str &n\n  secret: hidden\nb: 1\n", 2, 3},
		{"a: !!str\t&n\n  secret: hidden\nb: 1\n", 2, 3},
		{"a: !!str &n\n- secret\nb: 1\n", 2, 1},
		{"a: !!str\n  &n {secret: hidden}\nb: 1\n", 2, 6},
		// The anchor first, and no anchor, fail in the same places.
		{"a: &n !!str {secret: hidden}\nb: 1\n", 1, 12},
		{"a: &n\t!!str\t{secret: hidden}\nb: 1\n", 1, 11},
		{"a: &n !!str\n  secret: hidden\nb: 1\n", 2, 3},
		{"a: !!str    {secret: hidden}\nb: 1\n", 1, 12},
		{"a: !!str\n  secret: hidden\nb: 1\n", 2, 3},
	} {
		var v any
		err := yaml.Unmarshal([]byte(c.doc), &v)
		de := wantDecodeError(t, err, yaml.ErrorKindSyntax, c.line)
		if de.Column != c.column {
			t.Errorf("Unmarshal(%q): column %d, want %d (%v)", c.doc, de.Column, c.column, de)
		}
		if strings.Contains(de.Error(), "secret") || strings.Contains(de.Error(), "hidden") {
			t.Errorf("Unmarshal(%q): error %q quotes the input", c.doc, de.Error())
		}
	}
}

// TestTagThenAnchorWithNoValueReadsEmpty checks a scalar core tag and an
// anchor with nothing after them before the next key, which reads as the
// empty string, as it does in spruce, where the parser used to move the
// next key under the tagged one.
func TestTagThenAnchorWithNoValueReadsEmpty(t *testing.T) {
	checkScannerCases(t, []scannerCase{
		{"space", "a: !!str &n\nb: 1\n", map[string]any{"a": "", "b": 1}},
		{"tabs and a comment", "a: !!str\t&n\t# c\nb: 1\n", map[string]any{"a": "", "b": 1}},
	})
}
