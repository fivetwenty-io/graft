package yamlnode_test

import (
	"testing"

	"github.com/fivetwenty-io/graft/internal/yamlgolden"
	"github.com/fivetwenty-io/graft/internal/yamlnode"
)

func TestResolvePlainTagMatchesYAMLv3(t *testing.T) {
	var vectors []struct {
		In  string `json:"in"`
		Tag string `json:"tag"`
	}
	yamlgolden.ReadJSON(t, "testdata/golden/tags.json", &vectors)
	if len(vectors) < 1700 {
		t.Fatalf("tags.json holds %d vectors, want at least 1700; run make oracle-vectors", len(vectors))
	}
	for _, v := range vectors {
		if got := yamlnode.ResolvePlainTag(v.In); got != v.Tag {
			t.Errorf("ResolvePlainTag(%q) = %q, want %q", v.In, got, v.Tag)
		}
	}
}

func TestShortTag(t *testing.T) {
	for in, want := range map[string]string{
		"tag:yaml.org,2002:str":    "!!str",
		"tag:yaml.org,2002:binary": "!!binary",
		"tag:yaml.org,2002:custom": "!!custom",
		"!!int":                    "!!int",
		"!foo":                     "!foo",
		"tag:example.com,2000:foo": "tag:example.com,2000:foo",
		"":                         "",
	} {
		if got := yamlnode.ShortTag(in); got != want {
			t.Errorf("ShortTag(%q) = %q, want %q", in, got, want)
		}
	}
}
