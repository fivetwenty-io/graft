package yaml_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

// TestUnmarshalReadsTagTabs checks a tag with a tab after it, which
// goccy's scanner read as part of the tag, so the value went missing and
// a key on the next line moved under the tagged key.
func TestUnmarshalReadsTagTabs(t *testing.T) {
	checkScannerCases(t, []scannerCase{
		{"str", "a: !!str\tx\nb: 1\n", map[string]any{"a": "x", "b": 1}},
		{"tab inside the value", "a: !!str\tx\ty\n", map[string]any{"a": "x\ty"}},
		{"int", "a: !!int\t12\nb: 1\n", map[string]any{"a": 12, "b": 1}},
		{"bool", "a: !!bool\ttrue\nb: 1\n", map[string]any{"a": true, "b": 1}},
		{"str on a boolean word", "a: !!str\tyes\nb: 1\n", map[string]any{"a": "yes", "b": 1}},
		{"str on a number", "a: !!str\t12\nb: 1\n", map[string]any{"a": "12", "b": 1}},
		{"custom", "a: !custom\tbar\nb: 1\n", map[string]any{"a": "bar", "b": 1}},
		{"verbatim", "a: !<tag:yaml.org,2002:str>\tx\nb: 1\n", map[string]any{"a": "x", "b": 1}},
		{"non-specific", "a: !\tx\nb: 1\n", map[string]any{"a": "x", "b": 1}},
		{"double-quoted with a tab", "a: !!str\t\"p\tq\"\nb: 1\n", map[string]any{"a": "p\tq", "b": 1}},
		{"single-quoted", "a: !!str\t'p'\nb: 1\n", map[string]any{"a": "p", "b": 1}},
		{"comment after the value", "a: !!str\tx\t# c\nb: 1\n", map[string]any{"a": "x", "b": 1}},
		{"tab and space before a comment", "a: !!str\tx \t# c\nb: 1\n", map[string]any{"a": "x", "b": 1}},
		{"two tabs", "a: !!str\t\tx\nb: 1\n", map[string]any{"a": "x", "b": 1}},
		{"block scalar", "a: !!str\t|\n  x\nb: 1\n", map[string]any{"a": "x\n", "b": 1}},
		{"line break after the tab", "a: !!str\t\n  x\nb: 1\n", map[string]any{"a": "x", "b": 1}},
		{"flow mapping", "a: !!map\t{k: v}\nb: 1\n", map[string]any{"a": map[string]any{"k": "v"}, "b": 1}},
		{"flow sequence", "a: !!seq\t[x, z]\nb: 1\n", map[string]any{"a": []any{"x", "z"}, "b": 1}},
		{"block mapping", "a: !!map\t\n  k: v\nb: 1\n", map[string]any{"a": map[string]any{"k": "v"}, "b": 1}},
		{"key", "!!str\tk: v\nb: 1\n", map[string]any{"k": "v", "b": 1}},
		{"explicit key", "? !!str\tk\n: v\nb: 1\n", map[string]any{"k": "v", "b": 1}},
		{"nested", "m:\n  a: !!str\tx\n  b: 1\nc: 2\n", map[string]any{"m": map[string]any{"a": "x", "b": 1}, "c": 2}},
		{"block sequence item", "a:\n- !!str\tx\n- z\nb: 1\n", map[string]any{"a": []any{"x", "z"}, "b": 1}},
		{"flow sequence item", "a: [!!str\tx, z]\nb: 1\n", map[string]any{"a": []any{"x", "z"}, "b": 1}},
		{"flow sequence over two lines", "a: [!!str\tx,\n  !!int\t3]\nb: 1\n", map[string]any{"a": []any{"x", 3}, "b": 1}},
		{"flow mapping value", "a: {k: !!str\tx, m: z}\nb: 1\n", map[string]any{"a": map[string]any{"k": "x", "m": "z"}, "b": 1}},
		{"flow mapping key", "a: {!!str\tk: v}\nb: 1\n", map[string]any{"a": map[string]any{"k": "v"}, "b": 1}},
		{"double-quoted flow mapping key", "a: {!!str\t\"p\tq\": v}\nb: 1\n", map[string]any{"a": map[string]any{"p\tq": "v"}, "b": 1}},
		{"CRLF", "a: !!str\tx\r\nb: 1\r\n", map[string]any{"a": "x", "b": 1}},
		{"end of the input", "a: !!str\tx", map[string]any{"a": "x"}},
	})
}

// TestUnmarshalReadsAnchorTabs checks an anchor with a tab after its
// name, which goccy's scanner read as part of the name, so the anchor
// held nothing and the alias that named it failed.
func TestUnmarshalReadsAnchorTabs(t *testing.T) {
	checkScannerCases(t, []scannerCase{
		{"value", "a: &n\tx\nb: *n\n", map[string]any{"a": "x", "b": "x"}},
		{"tab inside the value", "a: &n\tx\ty\nb: *n\n", map[string]any{"a": "x\ty", "b": "x\ty"}},
		{"double-quoted with a tab", "a: &n\t\"p\tq\"\nb: *n\n", map[string]any{"a": "p\tq", "b": "p\tq"}},
		{"flow mapping", "a: &n\t{k: v}\nb: *n\n", map[string]any{"a": map[string]any{"k": "v"}, "b": map[string]any{"k": "v"}}},
		{"block mapping", "a: &n\t\n  k: v\nb: *n\n", map[string]any{"a": map[string]any{"k": "v"}, "b": map[string]any{"k": "v"}}},
		{"comment before a block mapping", "a: &n\t# c\n  k: v\nb: *n\n", map[string]any{"a": map[string]any{"k": "v"}, "b": map[string]any{"k": "v"}}},
		{"comment and no value", "a: &n\t# c\nb: 1\n", map[string]any{"a": nil, "b": 1}},
		{"comment after the value", "a: &n\tx\t# c\nb: *n\n", map[string]any{"a": "x", "b": "x"}},
		{"two tabs", "a: &n\t\tx\nb: *n\n", map[string]any{"a": "x", "b": "x"}},
		{"block scalar", "a: &n\t|\n  x\nb: *n\n", map[string]any{"a": "x\n", "b": "x\n"}},
		{"key", "&n\tk: v\nb: 1\n", map[string]any{"k": "v", "b": 1}},
		{"block sequence item", "a:\n- &n\tx\n- *n\n", map[string]any{"a": []any{"x", "x"}}},
		{"flow sequence item", "a: [&n\tx, *n]\n", map[string]any{"a": []any{"x", "x"}}},
		{"flow mapping value", "a: {k: &n\tx, m: *n}\n", map[string]any{"a": map[string]any{"k": "x", "m": "x"}}},
		{"end of the input", "a: &n\tx", map[string]any{"a": "x"}},
		{"anchor then tag", "a: &n\t!!str\tx\nb: *n\n", map[string]any{"a": "x", "b": "x"}},
		{"tag then anchor", "a: !!str\t&n\tx\nb: *n\n", map[string]any{"a": "x", "b": "x"}},
		{"anchor, space, and tag", "a: &n !!str\tyes\nb: *n\n", map[string]any{"a": "yes", "b": "yes"}},
		{"tag, tab, and anchor", "a: !!str\t&n yes\nb: *n\n", map[string]any{"a": "yes", "b": "yes"}},
		{"anchor and tag in a flow sequence", "a: [&n\t!!str\tyes, *n]\n", map[string]any{"a": []any{"yes", "yes"}}},
	})
}

// TestPropertyTabsReadAsSpaces checks that a document with a tab after a
// tag or an anchor reads as the same document with a space there, node
// for node and place for place, and that one the parser refuses with a
// space fails at the same place with a tab, where spruce reads a tag with
// a comment after it as the empty string.
func TestPropertyTabsReadAsSpaces(t *testing.T) {
	for _, doc := range []string{
		"a: !!str\tx\nb: 1\n",
		"a: !custom\tbar\nb: [!!int\t3, z]\n",
		"a: &n\t{k: v}\nb: *n\n",
		"a: !!str\t&n\tx\nb: *n\n",
		"m:\n  a: !!str\tx\n  b: {k: &n\tv, m: *n}\nc:\n- !!str\tz\n",
		"a: !!str\t\"p\tq\"\nb: 1\n",
		"a: [!!str\tx,\n  !!int\t3]\nb: 1\n",
		"a: [!!int\t3, !!str\t\tz, &n\tq]\nb: *n\n",
		"a: !!str\t\tx\nb: 1\n",
		"a:\t!!str\tx\nb: 1\n",
		"a: [&n\t!!str\tz, *n]\n",
		"!!str\tk: v\nb: 1\n",
		"a: !<tag:yaml.org,2002:str>\tx\nb: &n\t{k: !!str\tv}\nc: *n\n",
		"a: ['p\tq', !!str\tz, &n\tq]\nb: *n\n",
		"a: [\"p\\\"\tq\", !!str\tz]\n",
		"a: ['it''s\tx', !!str\tz]\n",
		"a: [x\ty, !!str\tz]\n",
	} {
		spaced := strings.ReplaceAll(doc, "\t", " ")
		var got, want yaml.Node
		if err := yaml.Unmarshal([]byte(doc), &got); err != nil {
			t.Errorf("Unmarshal(%q): %v", doc, err)
			continue
		}
		if err := yaml.Unmarshal([]byte(spaced), &want); err != nil {
			t.Fatalf("Unmarshal(%q): %v", spaced, err)
		}
		sameNodes(t, doc, &got, &want)
	}

	sameError := func(doc string, v any) {
		t.Helper()
		err := yaml.Unmarshal([]byte(doc), v)
		wantErr := yaml.Unmarshal([]byte(strings.ReplaceAll(doc, "\t", " ")), v)
		var de, wantDE *yaml.DecodeError
		if !errors.As(err, &de) || !errors.As(wantErr, &wantDE) || *de != *wantDE {
			t.Errorf("Unmarshal(%q) = %v, want %v", doc, err, wantErr)
		}
	}
	var v any
	sameError("a: !!str\t# c\nb: 1\n", &v)
	var one struct{ A, B int }
	sameError("a: 1\nb: !!str\tz\n", &one)

	// A type error gives the column of the scalar's token, which counts
	// no tab between tokens, as it counts none after a comma.
	for _, c := range []struct {
		doc          string
		line, column int
	}{
		{"a: [3,\tz]\n", 1, 7},
		{"a: [&n\tz, 3]\n", 1, 7},
		{"a: [!!int\t3, !!str\t\tz]\n", 1, 12},
		{"b: [1]\na: [!!int\t3, &n\tz]\n", 2, 15},
	} {
		var ints struct{ A, B []int }
		err := yaml.Unmarshal([]byte(c.doc), &ints)
		var de *yaml.DecodeError
		if !errors.As(err, &de) || de.Kind != yaml.ErrorKindType || de.Line != c.line || de.Column != c.column {
			t.Errorf("Unmarshal(%q) = %#v, want a type error at %d:%d", c.doc, err, c.line, c.column)
		}
	}
}

// sameNodes reports where got, read from doc, differs from want in a
// kind, a tag, a value, or a place.
func sameNodes(t *testing.T, doc string, got, want *yaml.Node) {
	t.Helper()
	if got.Kind != want.Kind || got.Tag != want.Tag || strings.ReplaceAll(got.Value, "\t", " ") != want.Value ||
		got.Line != want.Line || got.Column != want.Column || len(got.Content) != len(want.Content) {
		t.Errorf("%q: node %v %s %q at %d:%d, want %v %s %q at %d:%d", doc,
			got.Kind, got.Tag, got.Value, got.Line, got.Column,
			want.Kind, want.Tag, want.Value, want.Line, want.Column)
		return
	}
	for i := range got.Content {
		sameNodes(t, doc, got.Content[i], want.Content[i])
	}
}
