package yamlnode_test

import (
	"errors"
	"testing"

	"github.com/fivetwenty-io/graft/internal/yamlgolden"
	"github.com/fivetwenty-io/graft/internal/yamlnode"
)

func TestFromValueMatchesMarshalReparse(t *testing.T) {
	var vectors []struct {
		Name  string           `json:"name"`
		Node  *yamlgolden.Node `json:"node"`
		Error string           `json:"error"`
		Panic string           `json:"panic"`
	}
	yamlgolden.ReadJSON(t, "testdata/golden/values.json", &vectors)
	cat := valueCatalog()
	if len(vectors) != len(cat) {
		t.Fatalf("values.json has %d vectors and the catalog %d values; run make oracle-vectors", len(vectors), len(cat))
	}
	for _, v := range vectors {
		t.Run(v.Name, func(t *testing.T) {
			got, err := yamlnode.FromValue(cat[v.Name])
			var unsupported *yamlnode.UnsupportedTypeError
			switch {
			case v.Panic != "":
				if !errors.As(err, &unsupported) || err.Error() != v.Panic {
					t.Fatalf("err = %v, want *UnsupportedTypeError %q", err, v.Panic)
				}
			case v.Error != "":
				if err == nil || err.Error() != v.Error || errors.As(err, &unsupported) {
					t.Fatalf("err = %v, want the unwrapped error %q", err, v.Error)
				}
			default:
				if err != nil {
					t.Fatalf("FromValue: %v", err)
				}
				want := yamlgolden.Strip(v.Node, false, false)
				if d := yamlgolden.Diff(want, yamlgolden.Strip(yamlgolden.FromNode(got), false, false)); d != "" {
					t.Fatal(d)
				}
			}
		})
	}
}

func TestFromValueNeverProducesAnchors(t *testing.T) {
	shared := map[string]interface{}{"k": "v"}
	doc, err := yamlnode.FromValue(map[string]interface{}{"a": shared, "b": shared})
	if err != nil {
		t.Fatal(err)
	}
	var walk func(n *yamlnode.Node)
	walk = func(n *yamlnode.Node) {
		if n.Anchor != "" || n.Kind == yamlnode.AliasNode {
			t.Fatalf("FromValue produced an anchor or alias: %+v", n)
		}
		for _, c := range n.Content {
			walk(c)
		}
	}
	walk(doc)
}

type yamlMarshalerValue struct{ out interface{} }

func (m yamlMarshalerValue) MarshalYAML() (interface{}, error) { return m.out, nil }

type inlineBase struct {
	A int `yaml:"a,omitempty"`
}

type inlineHolder struct {
	inlineBase `yaml:",inline"`
	Extra      map[string]string `yaml:",inline"`
	Name       string            `yaml:"name,omitempty,flow"`
}

type badFlag struct {
	X int `yaml:"x,bogus"`
}

func scalarPairs(t *testing.T, v interface{}) []string {
	t.Helper()
	doc, err := yamlnode.FromValue(v)
	if err != nil {
		t.Fatalf("FromValue: %v", err)
	}
	var out []string
	for _, c := range doc.Content[0].Content {
		out = append(out, c.Value)
	}
	return out
}

func TestFromValueHonorsYAMLMarshaler(t *testing.T) {
	got := scalarPairs(t, yamlMarshalerValue{out: map[string]int{"k": 1}})
	if len(got) != 2 || got[0] != "k" || got[1] != "1" {
		t.Fatalf("pairs = %q", got)
	}
	doc, err := yamlnode.FromValue(yamlMarshalerValue{})
	if err != nil || doc.Content[0].Tag != "!!null" {
		t.Fatalf("a nil MarshalYAML result must become !!null, got %+v, %v", doc, err)
	}
}

func TestFromValueStructInlineAndOmitEmpty(t *testing.T) {
	got := scalarPairs(t, inlineHolder{Extra: map[string]string{"z": "1", "y": "2"}})
	want := []string{"y", "2", "z", "1"}
	if len(got) != len(want) {
		t.Fatalf("pairs = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("pairs = %q, want %q", got, want)
		}
	}
	got = scalarPairs(t, inlineHolder{inlineBase: inlineBase{A: 4}, Name: "n"})
	want = []string{"a", "4", "name", "n"}
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Fatalf("pairs = %q, want %q", got, want)
		}
	}
}

func TestFromValueStructErrors(t *testing.T) {
	if _, err := yamlnode.FromValue(badFlag{}); err == nil || err.Error() != `unsupported flag "bogus" in tag "x,bogus" of type yamlnode_test.badFlag` {
		t.Fatalf("err = %v", err)
	}
	clash := inlineHolder{Extra: map[string]string{"name": "x"}}
	if _, err := yamlnode.FromValue(clash); err == nil || err.Error() != `cannot have key "name" in inlined map: conflicts with struct field` {
		t.Fatalf("err = %v", err)
	}
}
