package yaml_test

import (
	"sort"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

// ptrDupHook is a Node hook beside the pointer-keyed map, so the decode
// of the struct around the map takes the hook route.
type ptrDupHook struct{ Val string }

func (h *ptrDupHook) UnmarshalYAML(n *yaml.Node) error {
	h.Val = "hook:" + n.Value
	return nil
}

type ptrDupTarget struct {
	M map[*int]string `yaml:"m"`
	N ptrDupHook      `yaml:"n"`
}

// ptrDupValues returns the map's values in order.
func ptrDupValues(m map[*int]string) []string {
	out := make([]string, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// A map keyed by pointers holds each repeat of a key under a pointer of
// its own, so with OnDuplicateKey the struct route keeps both entries,
// as goccy does, and reports the repeat once. The route through a Node
// keeps one pair for the key, with the last value, and reports the
// repeat too, so neither route drops an entry without a word.
func TestOnDuplicateKeyPointerKeyedMap(t *testing.T) {
	doc := []byte("m: {1: a, 1: b}\nn: x\n")

	var got []yaml.DuplicateKey
	var s ptrDupTarget
	if err := yaml.UnmarshalWith(doc, &s, collect(&got)); err != nil {
		t.Fatalf("struct: %v", err)
	}
	if vals := ptrDupValues(s.M); len(vals) != 2 || vals[0] != "a" || vals[1] != "b" {
		t.Fatalf("struct map values = %v, want [a b]", vals)
	}
	if s.N.Val != "hook:x" {
		t.Fatalf("hook gave %q, want hook:x", s.N.Val)
	}
	if len(got) != 1 || got[0].Path != "m.1" {
		t.Fatalf("struct reports = %+v, want one for m.1", got)
	}

	got = nil
	var n yaml.Node
	if err := yaml.UnmarshalWith(doc, &n, collect(&got)); err != nil {
		t.Fatalf("node: %v", err)
	}
	if len(got) != 1 || got[0].Path != "m.1" {
		t.Fatalf("node reports = %+v, want one for m.1", got)
	}
	var back ptrDupTarget
	if err := n.Decode(&back); err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if vals := ptrDupValues(back.M); len(vals) != 1 || vals[0] != "b" {
		t.Fatalf("Decode map values = %v, want [b]", vals)
	}
	if back.N.Val != "hook:x" {
		t.Fatalf("Decode hook gave %q, want hook:x", back.N.Val)
	}
}
