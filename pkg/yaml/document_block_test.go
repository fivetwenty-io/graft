package yaml_test

import (
	"errors"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

func TestDocumentSetBlockScalars(t *testing.T) {
	cases := []struct {
		name, src, path, v, want string
	}{
		{"literal clip", "a: |\n  one\n  two\nb: 1\n", "a", "x\ny\n", "a: |\n  x\n  y\nb: 1\n"},
		{"literal keeps four-space indent", "a: |\n    one\nb: 1\n", "a", "x\n", "a: |\n    x\nb: 1\n"},
		{"literal strip", "a: |-\n  one\nb: 1\n", "a", "x\ny", "a: |-\n  x\n  y\nb: 1\n"},
		{"literal keep", "a: |+\n  one\n\nb: 1\n", "a", "x\n\n\n", "a: |+\n  x\n\n\nb: 1\n"},
		{"blank line inside", "a: |\n  one\nb: 1\n", "a", "x\n\ny\n", "a: |\n  x\n\n  y\nb: 1\n"},
		{"folded", "a: >\n  one\n  two\nb: 1\n", "a", "x y\nz\n", "a: >\n  x y\n\n  z\nb: 1\n"},
		{"folded strip", "a: >-\n  one\n", "a", "x\n\ny", "a: >-\n  x\n\n\n  y\n"},
		{"sequence item", "l:\n  - |\n    one\n  - two\n", "l.0", "x\n", "l:\n  - |\n    x\n  - two\n"},
		{"header comment kept", "a: | # note\n  one\n", "a", "x\n", "a: | # note\n  x\n"},
		{"blank line after clip kept", "a: |\n  one\n\nb: 1\n", "a", "x\n", "a: |\n  x\n\nb: 1\n"},
		{"empty clip value", "a: |\n  one\nb: 1\n", "a", "", "a: |\nb: 1\n"},
		{"end of file without newline", "a: |-\n  one", "a", "x", "a: |-\n  x"},
		{"leading line break", "a: |\n  one\n", "a", "\nx\n", "a: |\n\n  x\n"},
		{"folded keep", "a: >+\n  one\n", "a", "x\ny\n\n", "a: >+\n  x\n\n  y\n\n"},
		{"keep at the end of the input", "a: |+\n  one\n\n", "a", "x\n", "a: |+\n  x\n"},
		{"empty body", "a: |\nb: 1\n", "a", "x\n", "a: |\n  x\nb: 1\n"},
		{"empty body in a sequence", "l:\n  - |\n  - two\n", "l.0", "x\n", "l:\n  - |\n    x\n  - two\n"},
		{"empty strip body at the end of the input", "a: |-", "a", "x", "a: |-\n  x"},
		{"tab inside a line", "a: |\n  one\n", "a", "x\ty\n", "a: |\n  x\ty\n"},
		{"comment after the body kept", "a: |\n  one\n# c\nb: 1\n", "a", "x\ny\n", "a: |\n  x\n  y\n# c\nb: 1\n"},
		{"line count shrinks", "a: |\n  one\n  two\n  three\nb: 1\n", "a", "x\n", "a: |\n  x\nb: 1\n"},
	}
	for _, c := range cases {
		d := mustDoc(t, c.src)
		if err := d.Editable(c.path); err != nil {
			t.Errorf("%s: Editable: %v", c.name, err)
		}
		if err := d.Set(c.path, c.v); err != nil {
			t.Errorf("%s: Set: %v", c.name, err)
			continue
		}
		if got := string(d.Bytes()); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
		if n, err := d.Get(c.path); err != nil || n.Value != c.v {
			t.Errorf("%s: Get = %+v, %v", c.name, n, err)
		}
	}
}

// TestDocumentSetBlockScalarSameValue checks that setting a block scalar
// to the value it holds leaves the bytes alone, even where Set would
// write that value another way.
func TestDocumentSetBlockScalarSameValue(t *testing.T) {
	src := "a: >\n  one\n  two\nb: 1\n"
	d := mustDoc(t, src)
	if err := d.Set("a", "one two\n"); err != nil {
		t.Fatal(err)
	}
	if got := string(d.Bytes()); got != src {
		t.Errorf("got %q", got)
	}
}

func TestDocumentSetBlockScalarRefusals(t *testing.T) {
	cases := map[string]struct {
		src    string
		v      any
		reason string
	}{
		"clip needs one final newline":    {"a: |\n  one\n", "x", "the value does not fit the block scalar's header"},
		"clip refuses two":                {"a: |\n  one\n", "x\n\n", "the value does not fit the block scalar's header"},
		"strip has no final newline":      {"a: |-\n  one\n", "x\n", "the value does not fit the block scalar's header"},
		"keep needs a final newline":      {"a: |+\n  one\n", "x", "the value does not fit the block scalar's header"},
		"leading space":                   {"a: |\n  one\n", " x\n", "the value does not fit the block scalar's header"},
		"folded more-indented line":       {"a: >\n  one\n", "x\n y\n", "the value does not fit the block scalar's header"},
		"indentation indicator":           {"a: |2\n  one\n", "x\n", "the block scalar has an indentation indicator"},
		"non-string":                      {"a: |\n  one\n", 5, "a block scalar takes a string"},
		"leading tab":                     {"a: |\n  one\n", "\tx\n", "the value does not fit the block scalar's header"},
		"folded tab line":                 {"a: >\n  one\n", "x\n\ty\n", "the value does not fit the block scalar's header"},
		"keep with only line breaks":      {"a: |+\n  one\n", "\n\n", "the value does not fit the block scalar's header"},
		"clip with only a line break":     {"a: |\n  one\n", "\n", "the value does not fit the block scalar's header"},
		"keep refuses an empty value":     {"a: |+\n  one\n", "", "the value does not fit the block scalar's header"},
		"no line break at the end":        {"a: |\n  one", "x\n", "the value does not fit the block scalar's header"},
		"nil":                             {"a: |\n  one\n", nil, "a block scalar takes a string"},
		"carriage return":                 {"a: |\n  one\n", "x\ry\n", "the value does not fit the block scalar's header"},
		"next line":                       {"a: |\n  one\n", "x\u0085y\n", "the value does not fit the block scalar's header"},
		"indentation indicator and chomp": {"a: >-1\n one\n", "x", "the block scalar has an indentation indicator"},
	}
	for name, c := range cases {
		d := mustDoc(t, c.src)
		err := d.Set("a", c.v)
		var de *yaml.DocumentError
		if !errors.Is(err, yaml.ErrUnsupportedEdit) || !errors.As(err, &de) || de.Reason != c.reason {
			t.Errorf("%s: got %v, want %q", name, err, c.reason)
		}
		if string(d.Bytes()) != c.src {
			t.Errorf("%s: bytes changed to %q", name, d.Bytes())
		}
	}
	var de *yaml.DocumentError
	if err := mustDoc(t, "a: |2\n  one\n").Editable("a"); !errors.As(err, &de) ||
		de.Reason != "the block scalar has an indentation indicator" {
		t.Errorf("Editable of an indentation indicator = %v", err)
	}
}
