package yamlgolden

import (
	"testing"
	"time"

	"github.com/fivetwenty-io/graft/internal/yamlnode"
)

func TestFromNodeAndStrip(t *testing.T) {
	key := &yamlnode.Node{Kind: yamlnode.ScalarNode, Tag: "!!str", Value: "a", Line: 3, HeadComment: "# h"}
	val := &yamlnode.Node{Kind: yamlnode.ScalarNode, Tag: "!!int", Value: "1", Line: 3, LineComment: "# l"}
	key2 := &yamlnode.Node{Kind: yamlnode.ScalarNode, Tag: "!!str", Value: "b", Line: 4}
	alias := &yamlnode.Node{Kind: yamlnode.AliasNode, Value: "x", Alias: val, Line: 4}
	doc := &yamlnode.Node{Kind: yamlnode.DocumentNode, Content: []*yamlnode.Node{
		{Kind: yamlnode.MappingNode, Tag: "!!map", Line: 3, Content: []*yamlnode.Node{key, val, key2, alias}},
	}}

	want := &Node{Kind: "document", Content: []*Node{{Kind: "mapping", Tag: "!!map", Content: []*Node{
		{Kind: "scalar", Tag: "!!str", Value: "a", Line: 3},
		{Kind: "scalar", Tag: "!!int", Value: "1"},
		{Kind: "scalar", Tag: "!!str", Value: "b", Line: 4},
		{Kind: "alias", Value: "x", Alias: "x"},
	}}}}
	if d := Diff(want, Strip(FromNode(doc), false, true)); d != "" {
		t.Fatal(d)
	}
	if got := Strip(FromNode(doc), true, false).Content[0].Content[0].HeadComment; got != "# h" {
		t.Fatalf("keepComments lost the head comment, got %q", got)
	}
}

func TestFromValueSortsMapEntriesAndKeepsTypes(t *testing.T) {
	PinLocal(t)
	when := time.Date(2001, 12, 14, 21, 59, 43, 0, time.UTC)
	got := FromValue(map[interface{}]interface{}{"b": 1.5, 1: []interface{}{nil, when}})
	want := Typed{T: "map[interface {}]interface {}", M: [][2]Typed{
		{{T: "int", V: "1"}, {T: "[]interface {}", L: []Typed{{T: "nil"}, {T: "time.Time", V: "2001-12-14T21:59:43Z|UTC"}}}},
		{{T: "string", V: "b"}, {T: "float64", V: "1.5"}},
	}}
	if d := Diff(want, got); d != "" {
		t.Fatal(d)
	}
}

func TestLineErrorPrefix(t *testing.T) {
	for _, tc := range []struct {
		msg, want string
		ok        bool
	}{
		{"yaml: line 2: did not find expected key", "yaml: line 2: ", true},
		{"unable to parse data from a.yml: yaml: line 13: mapping values are not allowed in this context", "unable to parse data from a.yml: yaml: line 13: ", true},
		{"yaml: mapping values are not allowed in this context", "", false},
		{"yaml: line two: bad", "", false},
		{"yaml: line 3", "", false},
	} {
		got, ok := LineErrorPrefix(tc.msg)
		if got != tc.want || ok != tc.ok {
			t.Errorf("LineErrorPrefix(%q) = %q, %v; want %q, %v", tc.msg, got, ok, tc.want, tc.ok)
		}
	}
}
