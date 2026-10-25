package sortnode_test

import (
	"reflect"
	"testing"

	"github.com/fivetwenty-io/graft/internal/sortnode"
	"github.com/fivetwenty-io/graft/internal/yamlnode"
	"github.com/fivetwenty-io/graft/pkg/sortkeys"
)

func mustNode(t *testing.T, src string) *yamlnode.Node {
	t.Helper()
	docs, err := yamlnode.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return docs[0].Content[0]
}

func mappingKeys(n *yamlnode.Node) []string {
	var keys []string
	for i := 0; i < len(n.Content); i += 2 {
		keys = append(keys, n.Content[i].Value)
	}
	return keys
}

func wantKeys(t *testing.T, n *yamlnode.Node, want ...string) {
	t.Helper()
	if got := mappingKeys(n); !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestSortNodeMapping(t *testing.T) {
	n := mustNode(t, "b: 1\na:\n  d: 1\n  c: 2\n")
	sortnode.Sort(n, sortkeys.Lexical)
	wantKeys(t, n, "a", "b")
	wantKeys(t, n.Content[1], "c", "d")
}

func TestSortNodeReachesSequences(t *testing.T) {
	n := mustNode(t, "l:\n- {z: 1, y: 2}\n")
	sortnode.Sort(n, sortkeys.Lexical)
	wantKeys(t, n.Content[1].Content[0], "y", "z")
}

func TestSortNodeNoneAndSpruce(t *testing.T) {
	n := mustNode(t, "b: 1\n10: x\n2: y\na: 2\n")
	sortnode.Sort(n, sortkeys.None)
	wantKeys(t, n, "b", "10", "2", "a")
	sortnode.Sort(n, sortkeys.Spruce)
	wantKeys(t, n, "2", "10", "a", "b")
	if n.Content[1].Value != "y" {
		t.Errorf("value did not follow its key: %q", n.Content[1].Value)
	}
}
