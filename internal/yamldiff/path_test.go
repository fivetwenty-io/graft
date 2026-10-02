package yamldiff

import (
	"testing"

	"github.com/fivetwenty-io/graft/internal/yamlnode"
)

func mustParse(t *testing.T, src string) []*yamlnode.Node {
	t.Helper()
	docs, err := yamlnode.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	return docs
}

func TestToDotStyleSkipsEmptyNames(t *testing.T) {
	p := Path{PathElements: []PathElement{
		{Idx: -1, Name: "a"}, {Idx: -1, Name: ""}, {Idx: 2}, {Idx: -1, Key: "name", Name: "web"}, {Idx: -1},
	}}
	if got := p.ToDotStyle(); got != "a.2.web" {
		t.Errorf("ToDotStyle = %q, want %q", got, "a.2.web")
	}
	if got := p.ToGoPatchStyle(); got != "/a/-1/2/name=web/-1" {
		t.Errorf("ToGoPatchStyle = %q, want %q", got, "/a/-1/2/name=web/-1")
	}
	empty := Path{}
	if empty.ToDotStyle() != "" || empty.ToGoPatchStyle() != "/" {
		t.Errorf("empty path renders %q and %q, want \"\" and \"/\"", empty.ToDotStyle(), empty.ToGoPatchStyle())
	}
	dotted := NewPathWithNamedElement(Path{}, "a.b")
	if got := dotted.ToDotStyle(); got != "a.b" {
		t.Errorf("a key with a dot renders %q, want it unescaped", got)
	}
}

func TestRootDescriptionUsesNames(t *testing.T) {
	f := &InputFile{Names: []string{"v1/ConfigMap/a"}}
	for _, c := range []struct {
		p    Path
		want string
	}{
		{Path{Root: f, DocumentIdx: 0}, "v1/ConfigMap/a"},
		{Path{Root: f, DocumentIdx: 1}, "document #2"},
		{Path{}, "document #1"},
	} {
		if got := c.p.RootDescription(); got != c.want {
			t.Errorf("RootDescription = %q, want %q", got, c.want)
		}
	}
}

func TestPathConstructorsNeverShareElements(t *testing.T) {
	base := NewPathWithNamedElement(Path{}, "a")
	left := NewPathWithIndexedListElement(base, 0)
	right := NewPathWithNamedListElement(base, "name", "web")
	if left.ToDotStyle() != "a.0" || right.ToDotStyle() != "a.web" || base.ToDotStyle() != "a" {
		t.Fatalf("paths leaked into each other: %q %q %q", base.ToDotStyle(), left.ToDotStyle(), right.ToDotStyle())
	}
	if right.PathElements[1].Key != "name" || left.PathElements[1].Idx != 0 || base.PathElements[0].Idx != -1 {
		t.Fatalf("unexpected elements: %+v %+v", left.PathElements, right.PathElements)
	}
}

func TestMaxDepthMatchesYtbx(t *testing.T) {
	for src, want := range map[string]int{
		"x\n":                                    0,
		"{}\n":                                   0,
		"a: 1\n":                                 1,
		"a: {b: {c: 1}}\n":                       3,
		"l: [{name: x, v: {w: 1}}, {name: y}]\n": 4,
		"l: [[1, 2], 3]\n":                       3,
		"a: &x {b: 1}\nc: *x\n":                  2,
		"l: [{name: x}, {name: y, deep: {z: 1}}]\n": 4,
		"a: {}\n":            0,
		"a: {b: {}}\nc: 1\n": 1,
		"a: {b: []}\n":       0,
		"l: [[], {}]\n":      0,
		"l: [{name: x}]\n":   0,
		"l: [[], 1]\n":       2,
	} {
		if got := MaxDepth(mustParse(t, src)[0].Content[0]); got != want {
			t.Errorf("MaxDepth(%q) = %d, want %d", src, got, want)
		}
	}
}

func TestNamedListIdentifier(t *testing.T) {
	for src, want := range map[string]string{
		"- {name: a}\n- {name: b, id: 1}\n":      "name",
		"- {key: a, id: 1}\n- {key: b, id: 2}\n": "key",
		"- {id: 1}\n- {x: 2}\n":                  "",
		"- a\n- b\n":                             "",
	} {
		if got := NamedListIdentifier(mustParse(t, src)[0].Content[0]); got != want {
			t.Errorf("NamedListIdentifier(%q) = %q, want %q", src, got, want)
		}
	}
}

func TestListKeysAndValueByKey(t *testing.T) {
	m := mustParse(t, "b: 1\na: 2\nb: 3\n")[0].Content[0]
	if got := ListKeys(m); len(got) != 3 || got[0] != "b" || got[1] != "a" || got[2] != "b" {
		t.Errorf("ListKeys = %v, want [b a b]", got)
	}
	if v, ok := ValueByKey(m, "b"); !ok || v.Value != "1" {
		t.Errorf("ValueByKey(b) = %v, %v; want the first match, 1", v, ok)
	}
	if _, ok := ValueByKey(m, "c"); ok {
		t.Error("ValueByKey(c) must report a miss")
	}
}
