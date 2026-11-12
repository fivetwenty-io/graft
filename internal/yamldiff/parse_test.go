package yamldiff

import (
	"testing"

	"github.com/fivetwenty-io/graft/internal/yamlnode"
)

func TestParseDocuments(t *testing.T) {
	cases := []struct {
		in    string
		count int
		kind  yamlnode.Kind
	}{
		{"", 1, 0},
		{"---\n", 1, 0},
		{"a: 1\n", 1, yamlnode.MappingNode},
		{`{"a": [1, 2]}`, 1, yamlnode.MappingNode},
		{"[1, 2]", 1, yamlnode.SequenceNode},
		{"a: 1\n---\nb: 2\n", 2, yamlnode.MappingNode},
		{"a = 1\n", 1, yamlnode.ScalarNode},
	}
	for _, c := range cases {
		docs, err := ParseDocuments([]byte(c.in))
		if err != nil || len(docs) != c.count {
			t.Errorf("ParseDocuments(%q) = %d docs, %v; want %d", c.in, len(docs), err, c.count)
			continue
		}
		if c.kind != 0 && (len(docs[0].Content) == 0 || docs[0].Content[0].Kind != c.kind) {
			t.Errorf("ParseDocuments(%q) first document is not kind %v", c.in, c.kind)
		}
	}
	if _, err := ParseDocuments([]byte("a: [1\n")); err == nil {
		t.Error("broken YAML parsed")
	}
}
