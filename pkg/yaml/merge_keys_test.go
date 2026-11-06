package yaml_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

// mergeTargets decodes doc into a map, a struct, and a Node that is
// then decoded, and returns each m it read.
func mergeTargets(t *testing.T, doc string, opts yaml.DecodeOptions) map[string]map[string]int {
	t.Helper()
	out := make(map[string]map[string]int)
	var a map[string]map[string]int
	if err := yaml.UnmarshalWith([]byte(doc), &a, opts); err != nil {
		t.Fatalf("map: %v", err)
	}
	out["map"] = a["m"]
	var s struct {
		M map[string]int `yaml:"m"`
	}
	if err := yaml.UnmarshalWith([]byte(doc), &s, opts); err != nil {
		t.Fatalf("struct: %v", err)
	}
	out["struct"] = s.M
	var n yaml.Node
	if err := yaml.UnmarshalWith([]byte(doc), &n, opts); err != nil {
		t.Fatalf("Node: %v", err)
	}
	var back struct {
		M map[string]int `yaml:"m"`
	}
	if err := n.Decode(&back); err != nil {
		t.Fatalf("Node.Decode: %v", err)
	}
	out["Node.Decode"] = back.M
	return out
}

// TestMergeKeysFollowSpruce pins merge keys on every target, strict and
// with OnDuplicateKey set. The first map of a list wins, a merge wins
// over a key written before it and loses to a key written after it, and
// a second merge key wins over the first, as in spruce.
func TestMergeKeysFollowSpruce(t *testing.T) {
	cases := []struct {
		name, doc string
		want      map[string]int
	}{
		{"first map of a list wins",
			"b: &b {k1: 1, k2: 2}\no: &o {k2: 9, k3: 9}\nm:\n  <<: [*b, *o]\n",
			map[string]int{"k1": 1, "k2": 2, "k3": 9}},
		{"merge wins over a key before it",
			"m:\n  k2: 5\n  <<: [{k2: 1}, {k2: 2}]\n",
			map[string]int{"k2": 1}},
		{"a key after the merge wins",
			"m:\n  <<: {a: 1, b: 1}\n  a: 2\n",
			map[string]int{"a": 2, "b": 1}},
		{"second merge key wins",
			"b: &b {a: 1, b: 1}\nm:\n  <<: *b\n  <<: {a: 2}\n  c: 9\n",
			map[string]int{"a": 2, "b": 1, "c": 9}},
	}
	for _, c := range cases {
		for name, opts := range map[string]yaml.DecodeOptions{"strict": {}, "callback": collect(new([]yaml.DuplicateKey))} {
			t.Run(c.name+"/"+name, func(t *testing.T) {
				for target, got := range mergeTargets(t, c.doc, opts) {
					if !reflect.DeepEqual(got, c.want) {
						t.Errorf("%s m = %v, want %v", target, got, c.want)
					}
				}
			})
		}
	}
}

// TestNodeKeepsMergeKeysAsWritten pins that a Node target is lossless.
// Its mapping holds each merge key as written, tagged !!merge, with the
// alias it names beside it, and only a decode of the Node merges.
func TestNodeKeepsMergeKeysAsWritten(t *testing.T) {
	doc := "b: &b {a: 1, b: 1}\nm:\n  <<: *b\n  <<: {a: 2}\n"
	var n yaml.Node
	if err := yaml.Unmarshal([]byte(doc), &n); err != nil {
		t.Fatal(err)
	}
	m := n.Content[0].Content[3]
	if len(m.Content) != 4 {
		t.Fatalf("m holds %d nodes, want 4", len(m.Content))
	}
	for i, kind := range []yaml.Kind{yaml.AliasNode, yaml.MappingNode} {
		key, val := m.Content[2*i], m.Content[2*i+1]
		if key.Value != "<<" || key.Tag != "!!merge" || val.Kind != kind {
			t.Errorf("pair %d = %q %s and kind %v, want << !!merge and kind %v", i, key.Value, key.Tag, val.Kind, kind)
		}
	}
	var back struct {
		M map[string]int `yaml:"m"`
	}
	if err := n.Decode(&back); err != nil {
		t.Fatal(err)
	}
	if want := map[string]int{"a": 2, "b": 1}; !reflect.DeepEqual(back.M, want) {
		t.Fatalf("Node.Decode m = %v, want %v", back.M, want)
	}
}

// TestOnDuplicateKeyMergeOverride pins that a key a merge brings in and
// a key written in the mapping then overrides repeat nothing, while two
// keys written in the mapping still repeat, with a merge between them.
func TestOnDuplicateKeyMergeOverride(t *testing.T) {
	for _, target := range []any{new(map[string]any), new(yaml.Node)} {
		var got []yaml.DuplicateKey
		doc := "b: &b {a: 1}\nm:\n  <<: *b\n  a: 2\nn:\n  a: 3\n  <<: {a: 4}\n"
		if err := yaml.UnmarshalWith([]byte(doc), target, collect(&got)); err != nil {
			t.Fatalf("%T: %v", target, err)
		}
		if len(got) != 0 {
			t.Errorf("%T: a merged key overridden reported %+v", target, got)
		}

		got = nil
		doc = "m:\n  a: 1\n  <<: {a: 2}\n  a: 3\n"
		if err := yaml.UnmarshalWith([]byte(doc), target, collect(&got)); err != nil {
			t.Fatalf("%T: %v", target, err)
		}
		want := []yaml.DuplicateKey{{Path: "m.a", Line: 4, Column: 3, FirstLine: 2, FirstColumn: 3}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%T: reports = %+v, want %+v", target, got, want)
		}
	}
	var m map[string]map[string]int
	if err := yaml.UnmarshalWith([]byte("m:\n  a: 1\n  <<: {a: 2}\n  a: 3\n"), &m, collect(new([]yaml.DuplicateKey))); err != nil {
		t.Fatal(err)
	}
	if m["m"]["a"] != 3 {
		t.Fatalf("m = %v, want a 3", m["m"])
	}
}

// TestMergeKeyValueRefusedOnEveryTarget pins the error for a merge
// value that is neither a map nor a list of maps. It carries the merge
// key's line and quotes nothing of the document, and a Node target,
// which merges nothing, takes the document.
func TestMergeKeyValueRefusedOnEveryTarget(t *testing.T) {
	for _, doc := range []string{"m:\n  <<: s3kr1t\n", "s: &s [{a: 1}]\nm:\n  <<: *s\n"} {
		line := strings.Count(doc, "\n")
		for _, target := range []any{new(map[string]any), new(struct{ M map[string]any })} {
			err := yaml.Unmarshal([]byte(doc), target)
			var de *yaml.DecodeError
			if !errors.As(err, &de) || de.Line != line || !strings.Contains(err.Error(), "map merge requires map or sequence of maps as the value") {
				t.Errorf("%q into %T: error = %v, want the merge error at line %d", doc, target, err, line)
			}
			if err != nil && strings.Contains(err.Error(), "s3kr1t") {
				t.Errorf("%q into %T: error quotes the value", doc, target)
			}
		}
		var n yaml.Node
		if err := yaml.Unmarshal([]byte(doc), &n); err != nil {
			t.Fatalf("%q into Node: %v", doc, err)
		}
		var v any
		if err := n.Decode(&v); err == nil || !strings.Contains(err.Error(), "map merge") {
			t.Errorf("%q: Node.Decode error = %v, want the merge error", doc, err)
		}
	}
}
