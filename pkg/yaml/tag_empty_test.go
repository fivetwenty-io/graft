package yaml_test

import "testing"

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
