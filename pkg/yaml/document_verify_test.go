package yaml

import (
	"errors"
	"testing"

	"github.com/fivetwenty-io/graft/internal/yamlnode"
)

func TestCommitRefusesWhenAnotherNodeMoves(t *testing.T) {
	d := mustParse(t, "a: 1\nb: 2 # keep\n")
	loc, err := d.resolve("set", "a")
	if err != nil {
		t.Fatal(err)
	}
	accept := func(n *yamlnode.Node) error { return nil }
	for _, next := range []string{
		"a: 9\nb: 3 # keep\n",
		"a: 9\nb: 2 # gone\n",
		"a: 9\nb: '2' # keep\n",
		"a: 9\n---\nb: 2 # keep\n",
		"a: [9\nb: 2 # keep\n",
	} {
		err := d.commit("set", "a", []byte(next), loc, accept)
		if !errors.Is(err, ErrUnsupportedEdit) || string(d.Bytes()) != "a: 1\nb: 2 # keep\n" {
			t.Errorf("%q: commit = %v, Bytes = %q", next, err, d.Bytes())
		}
	}
	if err := d.commit("set", "a", []byte("a: 9\nb: 2 # keep\n"), loc, accept); err != nil {
		t.Fatalf("a clean edit: %v", err)
	}
	if string(d.Bytes()) != "a: 9\nb: 2 # keep\n" {
		t.Errorf("Bytes = %q", d.Bytes())
	}
	if n, err := d.Get("a"); err != nil || n.Value != "9" || n.Line != 1 {
		t.Errorf("Get after commit = %+v, %v", n, err)
	}
}

func TestCommitRunsTheTargetCheck(t *testing.T) {
	d := mustParse(t, "a: 1\n")
	loc, _ := d.resolve("set", "a")
	refuse := func(n *yamlnode.Node) error { return errors.New("no") }
	if err := d.commit("set", "a", []byte("a: 2\n"), loc, refuse); !errors.Is(err, ErrUnsupportedEdit) {
		t.Errorf("commit = %v", err)
	}
	if string(d.Bytes()) != "a: 1\n" {
		t.Errorf("Bytes = %q", d.Bytes())
	}
}

// TestCommitComparesTheTextOutsideTheNode covers edits that leave the
// tree as it was but change bytes outside the edited node, which only
// the comparison of the lines around the node can see.
func TestCommitComparesTheTextOutsideTheNode(t *testing.T) {
	const src = "# top\na: 1\n\nb: 2\nc: [x, y]\n"
	accept := func(n *yamlnode.Node) error { return nil }
	for _, next := range []string{
		"# top\na: 9\nb: 2\nc: [x, y]\n",
		"# top\na: 9\n\nb:  2\nc: [x, y]\n",
		"# top\na: 9\n\nb: 2\nc: [x,y]\n",
		"#  top\na: 9\n\nb: 2\nc: [x, y]\n",
		"# top\na: 9\n\nb: 2\nc: [x, y]",
		"# top\n\na: 9\n\nb: 2\nc: [x, y]\n",
		"# top\na: 9\n\nb: 2\nc: [x, y] # new\n",
	} {
		d := mustParse(t, src)
		loc, err := d.resolve("set", "a")
		if err != nil {
			t.Fatal(err)
		}
		var de *DocumentError
		err = d.commit("set", "a", []byte(next), loc, accept)
		if !errors.As(err, &de) || de.Reason != "the edit would change another part of the document" || de.Line != 2 {
			t.Errorf("%q: commit = %v", next, err)
		}
		if string(d.Bytes()) != src {
			t.Errorf("%q: Bytes = %q after a refused commit", next, d.Bytes())
		}
	}
}

// TestCommitLetsTheNodeChangeItsLineCount checks that lines after the
// edited node may move by the number of lines the node gained or lost.
func TestCommitLetsTheNodeChangeItsLineCount(t *testing.T) {
	accept := func(n *yamlnode.Node) error { return nil }
	for _, c := range []struct{ src, path, next string }{
		{"a: |\n  x\nb: 1 # b\n", "a", "a: |\n  x\n  y\n  z\nb: 1 # b\n"},
		{"a: |\n  x\n  y\nb: 1\n", "a", "a: |\n  y\nb: 1\n"},
		{"l:\n- |\n  x\n- 2\n", "l.0", "l:\n- |\n  x\n\n  w\n- 2\n"},
		{"l: [1, two, x] # c\n", "l.1", "l: [1, three, x] # c\n"},
		{"a: 1\n", "a", "a: 1000\n"},
	} {
		d := mustParse(t, c.src)
		loc, err := d.resolve("set", c.path)
		if err != nil {
			t.Fatal(err)
		}
		if err := d.commit("set", c.path, []byte(c.next), loc, accept); err != nil {
			t.Errorf("%q to %q: %v", c.src, c.next, err)
			continue
		}
		if string(d.Bytes()) != c.next {
			t.Errorf("Bytes = %q, want %q", d.Bytes(), c.next)
		}
	}
}

func TestCommitHandsTheCheckTheNewNode(t *testing.T) {
	d := mustParse(t, "a:\n  b: 1 # one\nc: 2\n")
	loc, err := d.resolve("set", "a.b")
	if err != nil {
		t.Fatal(err)
	}
	var seen *yamlnode.Node
	check := func(n *yamlnode.Node) error { seen = n; return nil }
	if err := d.commit("set", "a.b", []byte("a:\n  b: 'x' # one\nc: 2\n"), loc, check); err != nil {
		t.Fatal(err)
	}
	if seen == nil || seen.Value != "x" || seen.Style != yamlnode.StyleSingle || seen.Line != 2 {
		t.Errorf("check saw %+v", seen)
	}
	if loc2, err := d.resolve("get", "a.b"); err != nil || loc2.node != seen {
		t.Errorf("the Document does not hold the node the check saw: %v", err)
	}
}

func TestCommitRefusesANewCommentOrAnchorOnTheNode(t *testing.T) {
	accept := func(n *yamlnode.Node) error { return nil }
	for _, next := range []string{"a: 9 # new\nb: 2\n", "a: &n 9\nb: 2\n"} {
		d := mustParse(t, "a: 1\nb: 2\n")
		loc, err := d.resolve("set", "a")
		if err != nil {
			t.Fatal(err)
		}
		if err := d.commit("set", "a", []byte(next), loc, accept); !errors.Is(err, ErrUnsupportedEdit) {
			t.Errorf("%q: commit = %v", next, err)
		}
	}
}

func TestSameTree(t *testing.T) {
	parse := func(src string) *yamlnode.Node {
		docs, err := yamlnode.Parse([]byte(src))
		if err != nil || len(docs) != 1 {
			t.Fatalf("%q: %v", src, err)
		}
		return docs[0]
	}
	base := "a: &x [1, 'two']\nb: *x\nc: !!str 3 # c\n"
	old := parse(base)
	if !sameTree(old, parse("a:   &x [1,    'two']\nb:  *x\nc: !!str   3   # c\n"), nil, new(*yamlnode.Node)) {
		t.Error("spacing alone made the trees differ")
	}
	for _, other := range []string{
		"a: &y [1, 'two']\nb: *y\nc: !!str 3 # c\n",
		"a: &x [1, \"two\"]\nb: *x\nc: !!str 3 # c\n",
		"a: &x [1, 'two']\nb: *x\nc: !!str 3 # d\n",
		"a: &x [1, 'two']\nb: *x\nc: 3 # c\n",
		"a: &x [1, 'two', 3]\nb: *x\nc: !!str 3 # c\n",
		"a: &x {1: 'two'}\nb: *x\nc: !!str 3 # c\n",
	} {
		if sameTree(old, parse(other), nil, new(*yamlnode.Node)) {
			t.Errorf("%q matched %q", other, base)
		}
	}
	var found *yamlnode.Node
	skip := old.Content[0].Content[1].Content[1]
	if !sameTree(old, parse("a: &x [1, {k: v}]\nb: *x\nc: !!str 3 # c\n"), skip, &found) ||
		found == nil || found.Kind != yamlnode.MappingNode {
		t.Errorf("sameTree with skip found %+v", found)
	}
}

// TestCommitSeesACommentTheTreeDrops checks a line comment after an
// alias, which the parsed tree does not keep, so only the text outside
// the edited node shows the change.
func TestCommitSeesACommentTheTreeDrops(t *testing.T) {
	d := mustParse(t, "x: &a 1\ny: 2\nz: *a # c\n")
	loc, err := d.resolve("set", "y")
	if err != nil {
		t.Fatal(err)
	}
	accept := func(n *yamlnode.Node) error { return nil }
	if err := d.commit("set", "y", []byte("x: &a 1\ny: 3\nz: *a # d\n"), loc, accept); !errors.Is(err, ErrUnsupportedEdit) {
		t.Errorf("commit = %v", err)
	}
	if err := d.commit("set", "y", []byte("x: &a 1\ny: 3\nz: *a # c\n"), loc, accept); err != nil {
		t.Errorf("a clean edit: %v", err)
	}
}
