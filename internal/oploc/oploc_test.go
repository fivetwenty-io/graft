package oploc

import (
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

func cursors(t *testing.T, paths ...string) []*tree.Cursor {
	t.Helper()
	out := make([]*tree.Cursor, 0, len(paths))
	for _, p := range paths {
		c, err := tree.ParseCursor(p)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, c)
	}
	return out
}

func names(cs []*tree.Cursor) string {
	parts := make([]string, 0, len(cs))
	for _, c := range cs {
		parts = append(parts, c.String())
	}
	return strings.Join(parts, ",")
}

func TestIndexUnderAndAbove(t *testing.T) {
	root := map[string]interface{}{
		"meta": map[string]interface{}{"a": "x", "b": "y"},
		"list": []interface{}{map[string]interface{}{"name": "web", "v": "z"}},
		"a.b":  "dotted",
	}
	locs := cursors(t, "meta.a", "meta.b", "list.web.v", "top")
	locs = append(locs, &tree.Cursor{Nodes: []string{"a.b"}})
	idx := New(locs, root)

	if got := names(idx.Under([]string{"meta"}, nil)); got != "meta.a,meta.b" {
		t.Fatalf("under meta is %s", got)
	}
	if got := names(idx.Under([]string{"meta"}, []string{"meta", "a"})); got != "meta.b" {
		t.Fatalf("under meta without meta.a is %s", got)
	}
	if got := names(idx.Under([]string{"list", "0"}, nil)); got != "list.web.v" {
		t.Fatalf("under list.0 is %s, want the entry named web", got)
	}
	if got := len(idx.Under(nil, nil)); got != len(locs) {
		t.Fatalf("under the root holds %d paths, want %d", got, len(locs))
	}
	if got := idx.Under([]string{"a"}, nil); len(got) != 0 {
		t.Fatalf("under a is %s, want nothing, since a.b is one node", names(got))
	}

	if !idx.AtOrAbove([]string{"meta", "a", "deep"}) {
		t.Fatal("meta.a.deep sits under an operator")
	}
	if !idx.AtOrAbove([]string{"top"}) {
		t.Fatal("top is an operator")
	}
	if idx.AtOrAbove([]string{"meta"}) {
		t.Fatal("meta holds operators but is not one")
	}
	if idx.AtOrAbove(nil) {
		t.Fatal("the root is never an operator")
	}

	if !idx.At([]string{"meta", "a"}) || !idx.At([]string{"list", "0", "v"}) {
		t.Fatal("meta.a and list.0.v are operators")
	}
	if idx.At([]string{"meta"}) || idx.At([]string{"meta", "a", "deep"}) {
		t.Fatal("At names only the operator's own path")
	}
	var none *Index
	if none.At([]string{"top"}) {
		t.Fatal("a nil index holds nothing")
	}
}

func TestIndexCovers(t *testing.T) {
	locs := cursors(t, "a", "b")
	idx := New(locs, map[string]interface{}{})
	if !idx.Covers(locs) {
		t.Fatal("an index covers the slice it was built from")
	}
	if idx.Covers(cursors(t, "a", "b")) {
		t.Fatal("an index does not cover another slice")
	}
	if idx.Covers(locs[:1]) {
		t.Fatal("an index does not cover a shorter slice")
	}
	var none *Index
	if none.Covers(locs) || none.AtOrAbove([]string{"a"}) || none.Under(nil, nil) != nil {
		t.Fatal("a nil index holds nothing")
	}
}
