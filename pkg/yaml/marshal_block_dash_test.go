package yaml_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

// TestMarshalMultilineIndicatorLiteral pins that a multi-line string
// that starts with an indicator character, such as a PEM certificate, is
// written as a literal block and reads back equal, both as a map value
// and as a list item. A string a block cannot hold stays double-quoted.
func TestMarshalMultilineIndicatorLiteral(t *testing.T) {
	cases := []struct {
		name  string
		s     string
		block bool
	}{
		{"cert with trailing newline", "-----BEGIN CERTIFICATE-----\nMIIBabc\nDEF==\n-----END CERTIFICATE-----\n", true},
		{"cert without trailing newline", "-----BEGIN X-----\nabc", true},
		{"first line dash space", "- a\nb\n", true},
		{"first line dash space no newline", "- a\nb", true},
		{"dash only first line", "-\nb\n", true},
		{"question first", "?x\ny\n", true},
		{"question space first", "? x\ny\n", true},
		{"colon first", ":x\ny\n", true},
		{"colon space first", ": x\ny\n", true},
		{"document start line", "---\nx\n", true},
		{"document end line", "...\nx\n", true},
		{"document start prefix", "---x\ny\n", true},
		{"hash first", "#x\ny\n", true},
		{"bracket first", "[x\ny\n", true},
		{"brace first", "{x\ny\n", true},
		{"ampersand first", "&x\ny\n", true},
		{"star first", "*x\ny\n", true},
		{"bang first", "!x\ny\n", true},
		{"pipe first", "|x\ny\n", true},
		{"gt first", ">x\ny\n", true},
		{"percent first", "%x\ny\n", true},
		{"at first", "@x\ny\n", true},
		{"backtick first", "`x\ny\n", true},
		{"quote first", "'x\ny\n", true},
		{"dquote first", "\"x\ny\n", true},
		{"dash with blank line", "-x\n\ny\n", true},
		{"dash with deeper line", "-x\n  y\nz\n", true},
		{"dash number", "-5\nx\n", true},
		{"trailing space stays quoted", "-x \ny\n", false},
		{"dash plain single line", "-x", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			for shape, v := range map[string]any{
				"map":    map[string]any{"cert": c.s},
				"list":   map[string]any{"l": []any{c.s}},
				"nested": map[string]any{"a": map[string]any{"b": []any{map[string]any{"c": c.s}}}},
			} {
				out, err := yaml.Marshal(v)
				if err != nil {
					t.Fatalf("%s: Marshal: %v", shape, err)
				}
				if got := strings.Contains(string(out), "|"); got != c.block {
					t.Errorf("%s: literal block = %v, want %v:\n%s", shape, got, c.block, out)
				}
				var back any
				if err := yaml.Unmarshal(out, &back); err != nil {
					t.Fatalf("%s: Unmarshal: %v\n%s", shape, err, out)
				}
				if !reflect.DeepEqual(back, v) {
					t.Errorf("%s: round trip = %#v, want %#v\n%s", shape, back, v, out)
				}
			}
		})
	}
}

// TestMarshalMultilineIndicatorRoundTrip builds multi-line strings that
// start with an indicator character from the pieces that make blocks
// hard to read, and checks that each reads back equal wherever it sits.
func TestMarshalMultilineIndicatorRoundTrip(t *testing.T) {
	starts := []string{"-", "- ", "--", "---", "-----BEGIN X-----", "?", "? ", "?x", ":", ": ", ":x", "...", "-\t", "-5", "-.5", "-.inf", "-1e3", "-0x1F", "-12:30"}
	pieces := []string{"\n", "\n\n", "a", " ", "  ", "\t", "- ", "-", ": ", ":", "#", " #", "\r", "\r\n", "é", "\u0085", "---", "...", "? ", "'", "\"", "|", "x"}
	p := newPRNG(7)
	for i := 0; i < 20000; i++ {
		var b strings.Builder
		b.WriteString(starts[p.Intn(len(starts))])
		b.WriteString("\n")
		for j, n := 0, p.Intn(6); j < n; j++ {
			b.WriteString(pieces[p.Intn(len(pieces))])
		}
		s := b.String()
		for _, v := range []any{
			map[string]any{"k": s},
			[]any{s, s},
			map[string]any{"a": map[string]any{"b": []any{map[string]any{"c": s, "d": 1}}}},
		} {
			out, err := yaml.Marshal(v)
			if err != nil {
				t.Fatalf("Marshal(%q): %v", s, err)
			}
			var back any
			if err := yaml.Unmarshal(out, &back); err != nil {
				t.Fatalf("Unmarshal of %q written as\n%s\nfailed: %v", s, out, err)
			}
			if !reflect.DeepEqual(back, v) {
				t.Fatalf("%q written as\n%s\nreads back as %#v", s, out, back)
			}
		}
	}
}
