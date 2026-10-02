package yamldiff

import (
	"testing"

	"github.com/fivetwenty-io/graft/internal/yamlgolden"
	"github.com/fivetwenty-io/graft/internal/yamlnode"
)

func TestLoadTOMLReadsLocalTimesInCurrentZone(t *testing.T) {
	yamlgolden.PinLocal(t)
	docs, err := loadTOMLDocuments([]byte("d = 1979-05-27\n" +
		"dt = 1979-05-27T07:32:00\n" +
		"tm = 07:32:00\n" +
		"off = 1979-05-27T07:32:00+02:00\n" +
		"utc = 1979-05-27T07:32:00Z\n" +
		"list = [1979-05-27T07:32:00]\n" +
		"[table]\nnested = 1979-05-28\n" +
		"[[tables]]\nwhen = 1979-05-29T01:02:03\n"))
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	var walk func(prefix string, n *yamlnode.Node)
	walk = func(prefix string, n *yamlnode.Node) {
		switch n.Kind {
		case yamlnode.MappingNode:
			for i := 0; i+1 < len(n.Content); i += 2 {
				walk(prefix+n.Content[i].Value+".", n.Content[i+1])
			}
		case yamlnode.SequenceNode:
			for i, c := range n.Content {
				walk(prefix+string(rune('0'+i))+".", c)
			}
		case yamlnode.ScalarNode:
			got[prefix] = n.Value
		default:
			t.Fatalf("unexpected node kind %v at %s", n.Kind, prefix)
		}
	}
	walk("", docs[0].Content[0])
	want := map[string]string{
		"d.":             "1979-05-27T00:00:00-04:00",
		"dt.":            "1979-05-27T07:32:00-04:00",
		"tm.":            "0000-01-01T07:32:00-04:00",
		"off.":           "1979-05-27T07:32:00+02:00",
		"utc.":           "1979-05-27T07:32:00Z",
		"list.0.":        "1979-05-27T07:32:00-04:00",
		"table.nested.":  "1979-05-28T00:00:00-04:00",
		"tables.0.when.": "1979-05-29T01:02:03-04:00",
	}
	for k, w := range want {
		if got[k] != w {
			t.Errorf("%s = %q, want %q", k, got[k], w)
		}
	}
}
