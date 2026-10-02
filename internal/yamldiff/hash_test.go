package yamldiff

import (
	"errors"
	"strconv"
	"testing"

	"github.com/fivetwenty-io/graft/internal/yamlgolden"
	"github.com/fivetwenty-io/graft/internal/yamlnode"
)

func TestNodeHashMatchesHashstructure(t *testing.T) {
	var vectors []struct {
		YAML   string   `json:"yaml"`
		Hashes []string `json:"hashes"`
	}
	yamlgolden.ReadJSON(t, "testdata/golden/hash.json", &vectors)
	compared := 0
	for _, v := range vectors {
		items := mustParse(t, v.YAML)[0].Content[0].Content
		if len(items) != len(v.Hashes) {
			t.Fatalf("%q parsed into %d items, want %d", v.YAML, len(items), len(v.Hashes))
		}
		for i, item := range items {
			h, err := nodeHash(item)
			if err != nil {
				t.Fatalf("%q item %d: %v", v.YAML, i, err)
			}
			if got := strconv.FormatUint(h, 10); got != v.Hashes[i] {
				t.Errorf("%q item %d hash %s, want %s", v.YAML, i, got, v.Hashes[i])
			}
			compared++
		}
	}
	if compared != 24 {
		t.Fatalf("compared %d hashes, want 24; regenerate hash.json with make oracle-vectors", compared)
	}
}

func TestNodeHashNestedScalarsIgnoreTags(t *testing.T) {
	items := mustParse(t, "- {a: 1}\n- {a: '1'}\n- 1\n- '1'\n")[0].Content[0].Content
	hashes := make([]uint64, len(items))
	for i, item := range items {
		h, err := nodeHash(item)
		if err != nil {
			t.Fatalf("nodeHash(item %d): %v", i, err)
		}
		hashes[i] = h
	}
	if hashes[0] != hashes[1] {
		t.Error("nested scalars must hash by value only, as dyff's basicType does")
	}
	if hashes[2] == hashes[3] {
		t.Error("top-level scalars must hash by tag and value")
	}
}

func TestNodeHashRejectsCollectionKeys(t *testing.T) {
	key := &yamlnode.Node{Kind: yamlnode.SequenceNode, Tag: "!!seq", Line: 4}
	entry := &yamlnode.Node{Kind: yamlnode.MappingNode, Tag: "!!map", Content: []*yamlnode.Node{key, {Kind: yamlnode.ScalarNode, Tag: "!!int", Value: "1"}}}
	_, err := nodeHash(entry)
	var unhashable *UnhashableKeyError
	if !errors.As(err, &unhashable) || unhashable.Line != 4 {
		t.Fatalf("err = %v, want *UnhashableKeyError at line 4", err)
	}
}
