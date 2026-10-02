package humanreport

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/yamldiff"
	"github.com/fivetwenty-io/graft/internal/yamlnode"
)

func parseRoot(t *testing.T, src string) *yamlnode.Node {
	t.Helper()
	docs, err := yamlnode.Parse([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return docs[0].Content[0]
}

func TestRestructureMatchesSpruceOrder(t *testing.T) {
	var b strings.Builder
	b.WriteString("name: n\ndeep:\n  a: 1\n")
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&b, "k%02d: %d\n", i, i)
	}
	m := parseRoot(t, b.String())
	restructureObject(m)
	want := []string{"name", "k09", "k10", "k01", "k02", "k03", "k04", "k05", "k06", "k07", "k08",
		"k00", "k11", "k19", "k12", "k13", "k14", "k15", "k16", "k17", "k18", "deep"}
	if got := yamldiff.ListKeys(m); !reflect.DeepEqual(got, want) {
		t.Fatalf("order %v\nwant  %v", got, want)
	}
}

func TestRestructureKnownOrders(t *testing.T) {
	m := parseRoot(t, "instance_groups: []\nother: 1\nname: x\nreleases: []\ndirector_uuid: u\n")
	restructureObject(m)
	if got := yamldiff.ListKeys(m); !reflect.DeepEqual(got, []string{"name", "director_uuid", "releases", "instance_groups", "other"}) {
		t.Errorf("BOSH order = %v", got)
	}
	k := parseRoot(t, "spec: {}\nmetadata: {}\nkind: X\napiVersion: v1\n")
	restructureObject(k)
	if got := yamldiff.ListKeys(k); !reflect.DeepEqual(got, []string{"apiVersion", "kind", "metadata", "spec"}) {
		t.Errorf("Kubernetes order = %v", got)
	}
	plain := parseRoot(t, "b: 1\na: 2\n")
	restructureObject(plain)
	if got := yamldiff.ListKeys(plain); !reflect.DeepEqual(got, []string{"b", "a"}) {
		t.Errorf("a map with no known key must keep its order, got %v", got)
	}
}

func TestRestructureSortsByLeafDepth(t *testing.T) {
	// ytbx's maxDepth counts only scalar and alias leaves, so an empty
	// collection or a named-list entry holding only its identifier has
	// depth 0, the same as a scalar. These orders come from the real
	// ytbx.RestructureObject.
	for src, want := range map[string][]string{
		"name: n\na:\n  x: {}\nb: 1\n":    {"name", "a", "b"},
		"name: n\nx: {b: {}}\ny: 1\n":     {"name", "x", "y"},
		"name: n\nx: {b: {c: 1}}\ny: 1\n": {"name", "y", "x"},
		"name: n\nl: [{name: x}]\nb: 1\n": {"name", "l", "b"},
	} {
		m := parseRoot(t, src)
		restructureObject(m)
		if got := yamldiff.ListKeys(m); !reflect.DeepEqual(got, want) {
			t.Errorf("restructureObject(%q) order = %v, want %v", src, got, want)
		}
	}
}

func TestRestructureRepeatedKeysTakeTheLastPair(t *testing.T) {
	for src, want := range map[string][]string{
		"x: 1\nname: a\nx: 2\n":    {"name", "a", "x", "2", "x", "2"},
		"name: a\nname: b\nk: 1\n": {"name", "b", "k", "1"},
	} {
		m := parseRoot(t, src)
		restructureObject(m)
		got := make([]string, 0, len(m.Content))
		for _, n := range m.Content {
			got = append(got, n.Value)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("restructureObject(%q) content = %v, want %v", src, got, want)
		}
	}
}

func TestRestructureIsVisibleThroughAliases(t *testing.T) {
	root := parseRoot(t, "base: &b {x: 1, name: n}\nuse: *b\n")
	restructureObject(root)
	target := yamlnode.FollowAlias(root.Content[3])
	if got := yamldiff.ListKeys(target); !reflect.DeepEqual(got, []string{"name", "x"}) {
		t.Fatalf("the alias target must be reordered in place, got %v", got)
	}
}
