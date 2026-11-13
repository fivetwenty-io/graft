package yaml_test

import (
	"reflect"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

type taggedInner struct {
	B int `yaml:"b"`
}

type taggedTarget struct {
	A  int         `yaml:"a"`
	In taggedInner `yaml:"in"`
}

type tagHook struct{ Val string }

func (h *tagHook) UnmarshalYAML(n *yaml.Node) error {
	h.Val = "hook:" + n.Value
	return nil
}

type taggedWithHook struct {
	H tagHook `yaml:"h"`
	A int     `yaml:"a"`
}

func TestTaggedMappingIntoStructAndMap(t *testing.T) {
	for _, src := range []string{"!foo {a: 1}\n", "!!map {a: 1}\n", "!foo\na: 1\n", "&x !foo {a: 1}\n"} {
		var s taggedTarget
		if err := yaml.Unmarshal([]byte(src), &s); err != nil || s.A != 1 {
			t.Errorf("%q into a struct: %+v, %v", src, s, err)
		}
		var m map[string]int
		if err := yaml.Unmarshal([]byte(src), &m); err != nil || m["a"] != 1 {
			t.Errorf("%q into a map: %v, %v", src, m, err)
		}
	}
}

func TestTaggedMappingInNestedPlaces(t *testing.T) {
	var s taggedTarget
	if err := yaml.Unmarshal([]byte("a: 1\nin: !foo {b: 2}\n"), &s); err != nil || s.In.B != 2 {
		t.Errorf("field: %+v, %v", s, err)
	}
	var m map[string]taggedInner
	if err := yaml.Unmarshal([]byte("k: !foo {b: 3}\n"), &m); err != nil || m["k"].B != 3 {
		t.Errorf("map value: %v, %v", m, err)
	}
	var l []taggedInner
	if err := yaml.Unmarshal([]byte("- !foo {b: 4}\n"), &l); err != nil || len(l) != 1 || l[0].B != 4 {
		t.Errorf("list item: %v, %v", l, err)
	}
}

func TestTaggedMappingReachesHooks(t *testing.T) {
	var v taggedWithHook
	if err := yaml.Unmarshal([]byte("!foo {h: x, a: 1}\n"), &v); err != nil || v.A != 1 || v.H.Val != "hook:x" {
		t.Errorf("got %+v, %v", v, err)
	}
}

// A tagged mapping keeps what the decode of an untagged one gives. A
// parsed Node decodes twice, once into a struct and once into a map, an
// interface{} target still gets a plain map, a tagged sequence still
// can't fill a struct, and an alias of an anchored tagged mapping fills
// its field as the anchor does.
func TestTaggedMappingKeepsDecodeProperties(t *testing.T) {
	var n yaml.Node
	if err := yaml.Unmarshal([]byte("!foo {a: 1}\n"), &n); err != nil {
		t.Fatal(err)
	}
	var s taggedTarget
	if err := n.Decode(&s); err != nil || s.A != 1 {
		t.Errorf("first decode into a struct: %+v, %v", s, err)
	}
	var m map[string]int
	if err := n.Decode(&m); err != nil || m["a"] != 1 {
		t.Errorf("second decode into a map: %v, %v", m, err)
	}

	var a, plain interface{}
	if err := yaml.Unmarshal([]byte("{a: 1}\n"), &plain); err != nil {
		t.Fatal(err)
	}
	if err := yaml.Unmarshal([]byte("!foo {a: 1}\n"), &a); err != nil ||
		!reflect.DeepEqual(a, plain) {
		t.Errorf("into interface{}: %#v, want %#v, %v", a, plain, err)
	}
	if _, ok := plain.(map[string]interface{}); !ok {
		t.Errorf("untagged into interface{} gave %T", plain)
	}

	var bad taggedTarget
	if err := yaml.Unmarshal([]byte("!foo [1]\n"), &bad); err == nil {
		t.Errorf("tagged sequence into a struct: err = nil, got %+v", bad)
	}

	var two struct {
		X taggedInner `yaml:"x"`
		Y taggedInner `yaml:"y"`
	}
	if err := yaml.Unmarshal([]byte("x: &k !foo {b: 1}\ny: *k\n"), &two); err != nil ||
		two.X.B != 1 || two.Y.B != 1 {
		t.Errorf("anchor and alias: %+v, %v", two, err)
	}
}

type taggedHookHolder struct {
	In taggedWithHook `yaml:"in"`
}

// A hook inside a tagged mapping runs wherever the mapping sits: under a
// struct field, as a map value, or as a list item.
func TestTaggedMappingReachesNestedHooks(t *testing.T) {
	var f taggedHookHolder
	if err := yaml.Unmarshal([]byte("in: !foo {h: x, a: 1}\n"), &f); err != nil ||
		f.In.H.Val != "hook:x" || f.In.A != 1 {
		t.Errorf("field: %+v, %v", f, err)
	}
	var m map[string]taggedWithHook
	if err := yaml.Unmarshal([]byte("k: !foo {h: y, a: 2}\n"), &m); err != nil ||
		m["k"].H.Val != "hook:y" || m["k"].A != 2 {
		t.Errorf("map value: %+v, %v", m, err)
	}
	var l []taggedWithHook
	if err := yaml.Unmarshal([]byte("- !foo {h: z, a: 3}\n"), &l); err != nil ||
		len(l) != 1 || l[0].H.Val != "hook:z" || l[0].A != 3 {
		t.Errorf("list item: %+v, %v", l, err)
	}
}
