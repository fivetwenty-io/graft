package yaml_test

import (
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

// TestUnmarshalReadsEmptyTagAtEnd checks a tag with nothing after it on
// the last line of a document that ends with a line break. spruce reads
// the empty string under any tag but !!null and the number and boolean
// tags, and graft used to read null under a tag that isn't a core one.
// Each value is the one spruce v1.35.17 gives.
func TestUnmarshalReadsEmptyTagAtEnd(t *testing.T) {
	checkScannerCases(t, []scannerCase{
		{"custom", "a: !custom\n", map[string]any{"a": ""}},
		{"bare", "a: !\n", map[string]any{"a": ""}},
		{"verbatim", "a: !<tag:x.y,2020:z>\n", map[string]any{"a": ""}},
		{"set", "a: !!set\n", map[string]any{"a": ""}},
		{"map", "a: !!map\n", map[string]any{"a": ""}},
		{"seq", "a: !!seq\n", map[string]any{"a": ""}},
		{"anchor first", "a: &n !custom\n", map[string]any{"a": ""}},
		{"comment", "a: !custom # c\n", map[string]any{"a": ""}},
		{"comment line", "a: !custom\n# c\n", map[string]any{"a": ""}},
		{"blank line", "a: !custom\n\n", map[string]any{"a": ""}},
		{"nested", "a:\n  b: !custom\n", map[string]any{"a": map[string]any{"b": ""}}},
		{"list item", "l:\n- !custom\n", map[string]any{"l": []any{""}}},
	})
}

// TestUnmarshalReadsEmptyTagBeforeSibling checks a tag with no value
// before the next key or item at its own level. The parser read that key
// or item, and everything after it at that level, as the tag's value, so
// "a: !foo" then "b: 1" read as a: {b: 1}, and a core tag such as !!str
// there failed the document. Each value is the one spruce v1.35.17 gives.
func TestUnmarshalReadsEmptyTagBeforeSibling(t *testing.T) {
	ab := map[string]any{"a": "", "b": 1, "c": 2}
	checkScannerCases(t, []scannerCase{
		{"custom", "a: !custom\nb: 1\nc: 2\n", ab},
		{"custom and anchor", "a: !custom &n\nb: 1\nc: 2\n", ab},
		{"anchor and custom", "a: &n !custom\nb: 1\nc: 2\n", ab},
		{"verbatim", "a: !<tag:x.y,2020:z>\nb: 1\nc: 2\n", ab},
		{"verbatim and anchor", "a: !<tag:x.y,2020:z> &n\nb: 1\nc: 2\n", ab},
		{"anchor and verbatim", "a: &n !<tag:x.y,2020:z>\nb: 1\nc: 2\n", ab},
		{"bare", "a: !\nb: 1\nc: 2\n", ab},
		{"bare and anchor", "a: ! &n\nb: 1\nc: 2\n", ab},
		{"set", "a: !!set\nb: 1\nc: 2\n", ab},
		{"comment", "a: !custom # c\nb: 1\nc: 2\n", ab},
		{"anchor and comment", "a: !custom &n # c\nb: 1\nc: 2\n", ab},
		{"trailing tab", "a: !custom\t\nb: 1\nc: 2\n", ab},
		{"anchor and trailing tab", "a: !custom &n\t\nb: 1\nc: 2\n", ab},
		{"tab before the anchor", "a: !custom\t&n\nb: 1\nc: 2\n", ab},
		{"trailing space", "a: !custom &n \nb: 1\nc: 2\n", ab},
		{"blank lines", "a: !custom\n\n\nb: 1\nc: 2\n", ab},
		{"comment line", "a: !custom\n# c\nb: 1\nc: 2\n", ab},
		{"list after", "a: !custom &n\nb:\n- x\n- z\nc: 2\n", map[string]any{"a": "", "b": []any{"x", "z"}, "c": 2}},
		{"nested", "m:\n  a: !custom &n\n  b: 1\nc: 2\n", map[string]any{"m": map[string]any{"a": "", "b": 1}, "c": 2}},
		{"quoted key", "\"a\": !custom\nb: 1\n", map[string]any{"a": "", "b": 1}},
		{"anchored key", "&k a: !custom\nb: 1\n", map[string]any{"a": "", "b": 1}},
		{"tag on the next line", "a:\n  !custom\nb: 1\n", map[string]any{"a": "", "b": 1}},
		{"alias to an earlier anchor", "a: &n 5\nb: !custom\nc: *n\nd: 2\n", map[string]any{"a": 5, "b": "", "c": 5, "d": 2}},
		{"alias to the tagged node", "a: !custom &n\nb: *n\nc: 1\n", map[string]any{"a": "", "b": "", "c": 1}},
		{"alias, anchor first", "a: &n !custom\nb: *n\n", map[string]any{"a": "", "b": ""}},
		{"end of the document", "a: !custom &n\n", map[string]any{"a": ""}},
		{"end, no line break", "a: !custom &n", map[string]any{"a": ""}},
		{"str", "a: !!str\nb: 1\n", map[string]any{"a": "", "b": 1}},
		{"str and anchor", "a: !!str &n\nb: 1\n", map[string]any{"a": "", "b": 1}},
		{"anchor and str", "a: &n !!str\nb: 1\n", map[string]any{"a": "", "b": 1}},
		{"str, tab, and anchor", "a: !!str\t&n\nb: 1\n", map[string]any{"a": "", "b": 1}},
		{"str and comment", "a: !!str # c\nb: 1\n", map[string]any{"a": "", "b": 1}},
		{"str on the next line", "k: # c\n  !!str\nj: 1\n", map[string]any{"k": "", "j": 1}},
		{"str and an alias", "a: !!str &n\nb: *n\n", map[string]any{"a": "", "b": ""}},
		{"null", "a: !!null\nb: 1\n", map[string]any{"a": nil, "b": 1}},
		{"binary", "a: !!binary\nb: 1\n", map[string]any{"a": "", "b": 1}},
		{"timestamp", "a: !!timestamp\nb: 1\n", map[string]any{"a": "", "b": 1}},
		{"map", "a: !!map\nb: 1\n", map[string]any{"a": "", "b": 1}},
		{"seq", "a: !!seq\nb: 1\n", map[string]any{"a": "", "b": 1}},
		{"omap", "a: !!omap\nb: 1\n", map[string]any{"a": "", "b": 1}},
		{"pairs", "a: !!pairs\nb: 1\n", map[string]any{"a": "", "b": 1}},
		{"map and anchor", "a: !!map &n\nb: 1\n", map[string]any{"a": "", "b": 1}},
		{"seq and anchor", "a: !!seq &n\nb: 1\n", map[string]any{"a": "", "b": 1}},
		{"omap and anchor", "a: !!omap &n\nb: 1\n", map[string]any{"a": "", "b": 1}},
	})
}

// TestUnmarshalReadsEmptyTaggedItem checks a tagged list item with no
// value before the next item, or before a key that ends the list, which
// the parser read as holding the rest of the list or that key. Each value
// is the one spruce v1.35.17 gives.
func TestUnmarshalReadsEmptyTaggedItem(t *testing.T) {
	checkScannerCases(t, []scannerCase{
		{"custom", "l:\n- !custom\n- 2\n- 3\n", map[string]any{"l": []any{"", 2, 3}}},
		{"two in a row", "l:\n- !custom\n- !custom\n- 3\n", map[string]any{"l": []any{"", "", 3}}},
		{"anchor", "l:\n- !custom &n\n- 2\nc: 2\n", map[string]any{"l": []any{"", 2}, "c": 2}},
		{"comment", "l:\n- !custom # c\n- 3\n", map[string]any{"l": []any{"", 3}}},
		{"trailing tab", "l:\n- !custom\t\n- 3\n", map[string]any{"l": []any{"", 3}}},
		{"anchor and comment", "l:\n- !custom &n # c\n- 3\n", map[string]any{"l": []any{"", 3}}},
		{"key after the list", "l:\n- !custom &n\nx: 1\n", map[string]any{"l": []any{""}, "x": 1}},
		{"in a mapping item", "l:\n- a: !custom\n  b: 1\n- a: !custom\n- 3\n", map[string]any{"l": []any{map[string]any{"a": "", "b": 1}, map[string]any{"a": ""}, 3}}},
		{"str", "l:\n- !!str\n- 2\n", map[string]any{"l": []any{"", 2}}},
		{"str and anchor", "l:\n- !!str &n\n- 2\n", map[string]any{"l": []any{"", 2}}},
		{"set", "l:\n- !!set\n- 2\n", map[string]any{"l": []any{"", 2}}},
		{"map", "l:\n- !!map\n- a\n", map[string]any{"l": []any{"", "a"}}},
	})
}

// TestUnmarshalReadsIndentedValueUnderTag checks that a node on the line
// after a tag stays the tag's value when it is indented under the tag's
// key or item, and when it is a list at the key's own indentation. Each
// value is the one spruce v1.35.17 gives.
func TestUnmarshalReadsIndentedValueUnderTag(t *testing.T) {
	checkScannerCases(t, []scannerCase{
		{"mapping", "a: !custom\n  x: 1\nb: 2\n", map[string]any{"a": map[string]any{"x": 1}, "b": 2}},
		{"scalar", "a: !custom\n  x\nb: 2\n", map[string]any{"a": "x", "b": 2}},
		{"comment line first", "a: !custom\n   # c\n  x\nb: 2\n", map[string]any{"a": "x", "b": 2}},
		{"anchor and an alias", "a: !custom &n\n  x: 1\nb: *n\n", map[string]any{"a": map[string]any{"x": 1}, "b": map[string]any{"x": 1}}},
		{"list at the key's column", "a: !custom\n- 1\n- 2\nb: 3\n", map[string]any{"a": []any{1, 2}, "b": 3}},
		{"list in a mapping item", "l:\n- a: !custom\n  - x\n", map[string]any{"l": []any{map[string]any{"a": []any{"x"}}}}},
		{"under a list item", "l:\n- !custom\n  x: 1\n- 2\n", map[string]any{"l": []any{map[string]any{"x": 1}, 2}}},
		{"tag on the next line", "a:\n  !custom\n  b: 1\n", map[string]any{"a": map[string]any{"b": 1}}},
		{"anchored key", "&k a: !custom\n  x: 1\nb: 1\n", map[string]any{"a": map[string]any{"x": 1}, "b": 1}},
		{"map", "a: !!map\n  k: v\nb: 1\n", map[string]any{"a": map[string]any{"k": "v"}, "b": 1}},
		{"seq", "a: !!seq\n- 1\nb: 1\n", map[string]any{"a": []any{1}, "b": 1}},
		{"set", "a: !!set\n  ? x\nb: 1\n", map[string]any{"a": map[string]any{"x": nil}, "b": 1}},
	})
}

// TestEmptyNumberTagBeforeSiblingFails checks that !!int, !!float, and
// !!bool with no value before the next key fail the document, as they do
// in spruce, at the tag's line and column, and that the error quotes
// nothing from the input.
func TestEmptyNumberTagBeforeSiblingFails(t *testing.T) {
	for _, c := range []struct {
		doc          string
		line, column int
	}{
		{"secret: !!int\nhidden: 1\n", 1, 9},
		{"secret: !!float\nhidden: 1\n", 1, 9},
		{"secret: !!bool\nhidden: 1\n", 1, 9},
		{"secret: !!int &n\nhidden: 1\n", 1, 9},
		{"l:\n- !!str\n- !!int\n- hidden\n", 3, 3},
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

// TestEmptyTagNodePlace checks the node Unmarshal gives a tag with no
// value before the next key: an empty scalar that keeps the tag as
// written, at the tag's place, with the next key at its own place.
func TestEmptyTagNodePlace(t *testing.T) {
	for _, c := range []struct {
		doc, tag     string
		line, column int
	}{
		{"a: !custom\nb: 1\n", "!custom", 1, 4},
		{"a: !custom &n # c\nb: 1\n", "!custom", 1, 4},
		{"a: !!map\nb: 1\n", "!!map", 1, 4},
		{"a: !!set &n\nb: 1\n", "!!set", 1, 4},
	} {
		var n yaml.Node
		if err := yaml.Unmarshal([]byte(c.doc), &n); err != nil {
			t.Fatalf("Unmarshal(%q): %v", c.doc, err)
		}
		m := n.Content[0]
		if len(m.Content) != 4 {
			t.Fatalf("Unmarshal(%q): %d nodes in the mapping, want 4", c.doc, len(m.Content))
		}
		v, b := m.Content[1], m.Content[2]
		if v.Kind != yaml.ScalarNode || v.Tag != c.tag || v.Value != "" || v.Line != c.line || v.Column != c.column {
			t.Errorf("Unmarshal(%q): value %v %s %q at %d:%d, want a scalar %s \"\" at %d:%d", c.doc, v.Kind, v.Tag, v.Value, v.Line, v.Column, c.tag, c.line, c.column)
		}
		if b.Value != "b" || b.Line != 2 || b.Column != 1 {
			t.Errorf("Unmarshal(%q): next key %q at %d:%d, want \"b\" at 2:1", c.doc, b.Value, b.Line, b.Column)
		}
	}
}
