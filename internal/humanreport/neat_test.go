package humanreport

import (
	"testing"

	"github.com/fivetwenty-io/graft/internal/termstyle"
	"github.com/fivetwenty-io/graft/internal/yamlgolden"
	"github.com/fivetwenty-io/graft/internal/yamlnode"
)

func modeNamed(name string) termstyle.Mode {
	switch name {
	case "truecolor":
		return termstyle.Mode{Color: true, TrueColor: true}
	case "ansi16":
		return termstyle.Mode{Color: true}
	default:
		return termstyle.Mode{}
	}
}

func TestNeatMatchesOracle(t *testing.T) {
	var vectors []struct {
		In      string `json:"in"`
		Palette string `json:"palette"`
		Mode    string `json:"mode"`
		Out     string `json:"out"`
	}
	yamlgolden.ReadJSON(t, "testdata/golden/neat.json", &vectors)
	if len(vectors) != 36 {
		t.Fatalf("neat.json holds %d vectors, want 36", len(vectors))
	}
	for _, v := range vectors {
		node := parseRoot(t, v.In)
		var got string
		var err error
		switch v.Palette {
		case "greenish":
			got, err = neatYAML(node, greenish, modeNamed(v.Mode))
		case "reddish":
			got, err = neatYAML(node, reddish, modeNamed(v.Mode))
		default:
			got, err = yamlString(node, modeNamed(v.Mode))
		}
		if err != nil || got != v.Out {
			t.Errorf("%s %s:\n got %q\nwant %q (err %v)", v.Palette, v.Mode, got, v.Out, err)
		}
	}
}

func TestNeedsQuotes(t *testing.T) {
	for value, want := range map[string]bool{
		"true": true, "null": true, ".inf": true, "-x": true, "1.2.3": true, "+1": true, "e": true, ".": true,
		"a b": true, "*x": true, "&x": true, "a:b": true, "a,b": true,
		"plain": false, "a#b": false, "yes": false, "café": false, "\t": false,
	} {
		n := &yamlnode.Node{Kind: yamlnode.ScalarNode, Tag: "!!str", Value: value}
		if got := needsQuotes(n); got != want {
			t.Errorf("needsQuotes(%q) = %v, want %v", value, got, want)
		}
	}
	if needsQuotes(&yamlnode.Node{Kind: yamlnode.ScalarNode, Tag: "!!int", Value: "-1"}) {
		t.Error("only !!str scalars are ever quoted")
	}
}

func TestYamlStringNil(t *testing.T) {
	for _, n := range []*yamlnode.Node{nil, {Kind: yamlnode.ScalarNode, Tag: "!!null", Value: "~"}} {
		if got, err := yamlString(n, termstyle.Mode{}); err != nil || got != "<nil>" {
			t.Errorf("yamlString(%v) = %q, %v; want <nil>", n, got, err)
		}
	}
}
