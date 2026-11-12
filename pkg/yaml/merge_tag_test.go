package yaml_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

func TestUnmarshalTaggedMergeKeys(t *testing.T) {
	var merged map[string]map[string]int
	src := "base: &b {a: 1}\nm:\n  ? !!merge <<\n  : *b\n  c: 2\n"
	if err := yaml.Unmarshal([]byte(src), &merged); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(merged["m"], map[string]int{"a": 1, "c": 2}) {
		t.Errorf("m = %v", merged["m"])
	}
	var block map[string]map[string]int
	if err := yaml.Unmarshal([]byte("base: &b {a: 1}\nm:\n  !!merge <<: *b\n"), &block); err != nil || block["m"]["a"] != 1 {
		t.Errorf("block form: %v, %v", block, err)
	}
	var plain map[string]map[string]map[string]int
	if err := yaml.Unmarshal([]byte("m:\n  !!str <<: {a: 1}\n"), &plain); err != nil {
		t.Fatal(err)
	}
	if plain["m"]["<<"]["a"] != 1 {
		t.Errorf("!!str << = %v", plain)
	}
}

// TestTaggedMergeKeyErrorPositions pins the place a parse error reports
// when it follows a tagged merge key, on the key's line or a later one,
// so dropping the tag moves no other token.
func TestTaggedMergeKeyErrorPositions(t *testing.T) {
	cases := []struct {
		src          string
		line, column int
	}{
		{"m:\n  !!merge <<: {a: 1\n", 2, 14},
		{"m:\n  !!merge <<: {a: 1}\n  c: 2\nd: [1, 2\n", 4, 4},
		{"m:\n  ? !!merge <<\n  : {a: 1}\n  c: 2\nd: [1, 2\n", 5, 4},
		{"m:\n  !!str <<: {a: 1}\n  c: 2\nd: [1, 2\n", 4, 4},
	}
	for _, c := range cases {
		var v any
		err := yaml.Unmarshal([]byte(c.src), &v)
		var de *yaml.DecodeError
		if !errors.As(err, &de) || de.Kind != yaml.ErrorKindSyntax {
			t.Errorf("%q: got %v, want a syntax DecodeError", c.src, err)
			continue
		}
		if de.Line != c.line || de.Column != c.column {
			t.Errorf("%q: error at %d:%d, want %d:%d", c.src, de.Line, de.Column, c.line, c.column)
		}
	}
}

// TestDocumentTaggedMergeKeys reads, edits, and writes back a Document
// whose merge keys carry tags. Bytes gives the input back unchanged,
// Get finds a key beside the merge key and one under its value, and Set
// changes only the value it names.
func TestDocumentTaggedMergeKeys(t *testing.T) {
	for _, src := range []string{
		"base: &b {a: 1}\nm:\n  !!merge <<: *b\n  c: 2\n",
		"base: &b {a: 1}\nm:\n  ? !!merge <<\n  : *b\n  c: 2\n",
		"m:\n  !!str <<: {a: 1}\n  c: 2\n",
		"m:\n  !!merge <<: {a: 1}\n  c: 2\n",
	} {
		d, err := yaml.ParseDocument([]byte(src))
		if err != nil {
			t.Errorf("%q: %v", src, err)
			continue
		}
		if got := string(d.Bytes()); got != src {
			t.Errorf("Bytes() = %q, want %q", got, src)
		}
		if n, err := d.Get("m.c"); err != nil || n.Value != "2" {
			t.Errorf("%q: Get m.c = %v, %v", src, n, err)
		}
		if n, err := d.Get("m.<<.a"); err != nil || n.Value != "1" {
			t.Errorf("%q: Get m.<<.a = %v, %v", src, n, err)
		}
		if err := d.Set("m.c", 3); err != nil {
			t.Errorf("%q: Set: %v", src, err)
			continue
		}
		want := src[:len(src)-2] + "3\n"
		if got := string(d.Bytes()); got != want {
			t.Errorf("after Set, Bytes() = %q, want %q", got, want)
		}
	}
}
