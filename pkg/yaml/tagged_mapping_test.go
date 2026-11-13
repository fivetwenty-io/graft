package yaml_test

import (
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
