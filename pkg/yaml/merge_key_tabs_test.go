package yaml_test

import (
	"reflect"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

// TestMergeKeyBeforeTab pins a << key with a tab before or after its
// colon, which spruce v1.35.17 reads as a merge key, as it reads one
// with a space there.
func TestMergeKeyBeforeTab(t *testing.T) {
	const base = "base: &base {k: v}\n"
	cases := []struct {
		doc  string
		want any
	}{
		{"<<:\t{k: v}\na: 1\n", map[string]any{"a": 1, "k": "v"}},
		{base + "m:\n  <<:\t*base\n  z: 1\n", map[string]any{"k": "v", "z": 1}},
		{base + "m:\n  <<\t: *base\n  z: 1\n", map[string]any{"k": "v", "z": 1}},
		{base + "m: {<<:\t*base, z: 1}\n", map[string]any{"k": "v", "z": 1}},
		{base + "m:\n  <<:\t\n    k: w\n", map[string]any{"k": "w"}},
	}
	for _, c := range cases {
		t.Run(c.doc, func(t *testing.T) {
			var a any
			if err := yaml.Unmarshal([]byte(c.doc), &a); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			got := a
			if m, ok := a.(map[string]any)["m"]; ok {
				got = m
			}
			if !reflect.DeepEqual(got, c.want) {
				t.Errorf("Unmarshal = %v, want %v", got, c.want)
			}
			var n yaml.Node
			if err := yaml.Unmarshal([]byte(c.doc), &n); err != nil {
				t.Fatalf("Unmarshal into a Node: %v", err)
			}
			var back any
			if err := n.Decode(&back); err != nil || !reflect.DeepEqual(back, a) {
				t.Errorf("Node.Decode = %v, %v, want %v", back, err, a)
			}
		})
	}
}

// TestMergeKeyBeforeTabNeverReported pins two << keys with a tab after
// the colon, which the duplicate-key callback never hears of, since
// they are merge keys.
func TestMergeKeyBeforeTabNeverReported(t *testing.T) {
	doc := "x: &x {k: v}\nw: &w {q: 1}\nm:\n  <<:\t*x\n  <<:\t*w\n"
	for _, target := range []any{new(any), new(yaml.Node)} {
		var got []yaml.DuplicateKey
		if err := yaml.UnmarshalWith([]byte(doc), target, collect(&got)); err != nil {
			t.Fatalf("%T: %v", target, err)
		}
		if len(got) != 0 {
			t.Errorf("%T: reports = %+v, want none", target, got)
		}
	}
}
