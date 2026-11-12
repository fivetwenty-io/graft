package yaml

import (
	"errors"
	"math"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/yamlnode"
)

// mustSpan returns the span of the scalar loc names.
func mustSpan(t *testing.T, d *Document, loc located) span {
	t.Helper()
	sp, err := d.scalarSpan(loc.node)
	if err != nil {
		t.Fatal(err)
	}
	return sp
}

// textSpan returns the span of text, which must occur once in src.
func textSpan(t *testing.T, src, text string) span {
	t.Helper()
	if strings.Count(src, text) != 1 {
		t.Fatalf("%q is not in %q exactly once", text, src)
	}
	start := strings.Index(src, text)
	return span{start, start + len(text)}
}

// whole is a span over all of src, which leaves the line comparison to
// see every change on its own.
func whole(src string) span { return span{0, len(src)} }

func TestCommitRefusesWhenAnotherNodeMoves(t *testing.T) {
	d := mustParse(t, "a: 1\nb: 2 # keep\n")
	loc, err := d.resolve("set", "a")
	if err != nil {
		t.Fatal(err)
	}
	sp := mustSpan(t, d, loc)
	accept := func(n *yamlnode.Node) error { return nil }
	for _, next := range []string{
		"a: 9\nb: 3 # keep\n",
		"a: 9\nb: 2 # gone\n",
		"a: 9\nb: '2' # keep\n",
		"a: 9\n---\nb: 2 # keep\n",
		"a: [9\nb: 2 # keep\n",
	} {
		err := d.commit("a", []byte(next), loc, whole(string(d.Bytes())), accept)
		if !errors.Is(err, ErrUnsupportedEdit) || string(d.Bytes()) != "a: 1\nb: 2 # keep\n" {
			t.Errorf("%q: commit = %v, Bytes = %q", next, err, d.Bytes())
		}
		err = d.commit("a", []byte(next), loc, sp, accept)
		if !errors.Is(err, ErrUnsupportedEdit) || string(d.Bytes()) != "a: 1\nb: 2 # keep\n" {
			t.Errorf("%q: commit = %v, Bytes = %q", next, err, d.Bytes())
		}
	}
	if err := d.commit("a", []byte("a: 9\nb: 2 # keep\n"), loc, sp, accept); err != nil {
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
	if err := d.commit("a", []byte("a: 2\n"), loc, mustSpan(t, d, loc), refuse); !errors.Is(err, ErrUnsupportedEdit) {
		t.Errorf("commit = %v", err)
	}
	if string(d.Bytes()) != "a: 1\n" {
		t.Errorf("Bytes = %q", d.Bytes())
	}
}

// TestCommitComparesTheTextOutsideTheNode covers edits that leave the
// tree as it was but change bytes outside the edited node, which only
// the comparison of the lines around the node can see. The span is the
// whole input, so the comparison of the bytes around the span passes
// every one of them.
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
		err = d.commit("a", []byte(next), loc, whole(src), accept)
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
	for _, c := range []struct{ src, path, old, next string }{
		{"a: |\n  x\nb: 1 # b\n", "a", "|\n  x\n", "a: |\n  x\n  y\n  z\nb: 1 # b\n"},
		{"a: |\n  x\n  y\nb: 1\n", "a", "|\n  x\n  y\n", "a: |\n  y\nb: 1\n"},
		{"l:\n- |\n  x\n- 2\n", "l.0", "|\n  x\n", "l:\n- |\n  x\n\n  w\n- 2\n"},
		{"l: [1, two, x] # c\n", "l.1", "two", "l: [1, three, x] # c\n"},
		{"a: 1\n", "a", "1", "a: 1000\n"},
	} {
		d := mustParse(t, c.src)
		loc, err := d.resolve("set", c.path)
		if err != nil {
			t.Fatal(err)
		}
		if err := d.commit(c.path, []byte(c.next), loc, textSpan(t, c.src, c.old), accept); err != nil {
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
	if err := d.commit("a.b", []byte("a:\n  b: 'x' # one\nc: 2\n"), loc, mustSpan(t, d, loc), check); err != nil {
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
		if err := d.commit("a", []byte(next), loc, mustSpan(t, d, loc), accept); !errors.Is(err, ErrUnsupportedEdit) {
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
	if sameTree(parse("p: &x 1\nq: &y 1\nr: *x\n"), parse("p: &x 1\nq: &y 1\nr: *y\n"), nil, new(*yamlnode.Node)) {
		t.Error("an alias that changed only its target matched")
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
// the edited node shows the change. The span is the whole input, so the
// line comparison has to see it.
func TestCommitSeesACommentTheTreeDrops(t *testing.T) {
	d := mustParse(t, "x: &a 1\ny: 2\nz: *a # c\n")
	loc, err := d.resolve("set", "y")
	if err != nil {
		t.Fatal(err)
	}
	accept := func(n *yamlnode.Node) error { return nil }
	if err := d.commit("y", []byte("x: &a 1\ny: 3\nz: *a # d\n"), loc, whole(string(d.Bytes())), accept); !errors.Is(err, ErrUnsupportedEdit) {
		t.Errorf("commit = %v", err)
	}
	if err := d.commit("y", []byte("x: &a 1\ny: 3\nz: *a # c\n"), loc, mustSpan(t, d, loc), accept); err != nil {
		t.Errorf("a clean edit: %v", err)
	}
}

// TestCommitKeepsTheBytesAroundTheSpan covers changes on the edited
// node's own lines that the tree can't see and the line comparison
// exempts. Only the comparison of the bytes before and after the old
// span refuses them. What the edit writes in place of the span is left
// to the caller's check.
func TestCommitKeepsTheBytesAroundTheSpan(t *testing.T) {
	accept := func(n *yamlnode.Node) error { return nil }
	for _, c := range []struct{ src, path, old, next string }{
		{"a: 1 # c\n", "a", "1", "a : 9 # c\n"},
		{"a: 1 # c\n", "a", "1", "a:9 # c\n"},
		{"l: [1, two, x]\n", "l.1", "two", "l: [1,three,x,]\n"},
		{"l: [1, two, x]\n", "l.1", "two", "l: [1, three,x]\n"},
		{"l: [1, two, x]\n", "l.1", "two", "l: [1, three, x ]\n"},
		{"a: 1\n...\n", "a", "1", "a: 9\n"},
		{"a: 1\n", "a", "1", "a: 9"},
	} {
		d := mustParse(t, c.src)
		loc, err := d.resolve("set", c.path)
		if err != nil {
			t.Fatal(err)
		}
		if err := d.commit(c.path, []byte(c.next), loc, textSpan(t, c.src, c.old), accept); !errors.Is(err, ErrUnsupportedEdit) {
			t.Errorf("%q to %q: commit = %v", c.src, c.next, err)
		}
		if string(d.Bytes()) != c.src {
			t.Errorf("%q: Bytes = %q after a refused commit", c.src, d.Bytes())
		}
	}
	d := mustParse(t, "a: 1 # c\n")
	loc, err := d.resolve("set", "a")
	if err != nil {
		t.Fatal(err)
	}
	for _, sp := range []span{{3, 99}, {5, 3}, {-1, 4}} {
		if err := d.commit("a", []byte("a: 9 # c\n"), loc, sp, accept); !errors.Is(err, ErrUnsupportedEdit) {
			t.Errorf("span %v: commit = %v", sp, err)
		}
	}
	if err := d.commit("a", []byte("a: 9 # c\n"), loc, span{3, 4}, accept); err != nil {
		t.Errorf("a clean edit: %v", err)
	}
}

// TestCommitRefusesAStaleLoc checks that a loc resolved before an
// earlier edit, whose node the Document no longer holds, can't commit.
func TestCommitRefusesAStaleLoc(t *testing.T) {
	d := mustParse(t, "a: 1\nb: 2\n")
	loc, err := d.resolve("set", "a")
	if err != nil {
		t.Fatal(err)
	}
	sp := mustSpan(t, d, loc)
	accept := func(n *yamlnode.Node) error { return nil }
	if err := d.commit("a", []byte("a: 9\nb: 2\n"), loc, sp, accept); err != nil {
		t.Fatal(err)
	}
	if err := d.commit("a", []byte("a: 7\nb: 2\n"), loc, sp, accept); !errors.Is(err, ErrUnsupportedEdit) {
		t.Errorf("a stale loc: commit = %v", err)
	}
	if string(d.Bytes()) != "a: 9\nb: 2\n" {
		t.Errorf("Bytes = %q", d.Bytes())
	}
}

// TestSetCheckComparesWhatSetMeantToWrite runs Set's check through
// commit against edited texts that hold a node other than the one Set
// meant to write, which commit alone accepts.
func TestSetCheckComparesWhatSetMeantToWrite(t *testing.T) {
	const src = "a: 1 # c\nb: 2\n"
	nine := setText{text: "9", value: "9", tag: "!!int"}
	for _, c := range []struct {
		name  string
		w     setText
		value any
		next  string
		ok    bool
	}{
		{"the text Set wrote", nine, 9, "a: 9 # c\nb: 2\n", true},
		{"a quoted number", nine, 9, "a: '9' # c\nb: 2\n", false},
		{"another quote style", setText{text: `"x"`, value: "x", tag: "!!str", style: yamlnode.StyleDouble}, "x", "a: 'x' # c\nb: 2\n", false},
		{"the quote style Set wrote", setText{text: `"x"`, value: "x", tag: "!!str", style: yamlnode.StyleDouble}, "x", "a: \"x\" # c\nb: 2\n", true},
		{"another value", nine, 9, "a: 9.0 # c\nb: 2\n", false},
		{"another tag", setText{text: "9", value: "9", tag: "!!str"}, "9", "a: 9 # c\nb: 2\n", false},
		{"padding before the text", nine, 9, "a:  9 # c\nb: 2\n", false},
		{"a value that decodes to another", nine, 10, "a: 9 # c\nb: 2\n", false},
		{"a value that does not fit the type", setText{text: "300", value: "300", tag: "!!int"}, uint8(44), "a: 300 # c\nb: 2\n", false},
		{"a float an interface reads as a string", setText{text: "1e+06", value: "1e+06", tag: "!!float"}, 1e6, "a: 1e+06 # c\nb: 2\n", false},
		{"a float with a decimal point", setText{text: "1.0e+06", value: "1.0e+06", tag: "!!float"}, 1e6, "a: 1.0e+06 # c\nb: 2\n", true},
		{"a NaN", setText{text: ".nan", value: ".nan", tag: "!!float"}, math.NaN(), "a: .nan # c\nb: 2\n", true},
		{"a NaN written as infinity", setText{text: ".inf", value: ".inf", tag: "!!float"}, math.NaN(), "a: .inf # c\nb: 2\n", false},
	} {
		d := mustParse(t, src)
		loc, err := d.resolve("set", "a")
		if err != nil {
			t.Fatal(err)
		}
		sp := mustSpan(t, d, loc)
		next := []byte(c.next)
		wrote := span{sp.start, sp.start + len(c.w.text)}
		err = d.commit("a", next, loc, sp, func(n *yamlnode.Node) error {
			return c.w.check(next, wrote, n, c.value)
		})
		if (err == nil) != c.ok {
			t.Errorf("%s: commit = %v, want ok %v", c.name, err, c.ok)
		}
	}
}

// TestCommitAcceptsAnUnchangedText checks that an edit that writes the
// text it replaces commits, at any position in the input.
func TestCommitAcceptsAnUnchangedText(t *testing.T) {
	accept := func(n *yamlnode.Node) error { return nil }
	for _, c := range []struct{ src, path string }{
		{"a: x\n", "a"},
		{"a: 1 # c\nb: 2\n", "a"},
		{"a: 1\nb: 2\nc: 3\n", "b"},
		{"# h\nl: [1, two]\n\n# f\n", "l.1"},
	} {
		d := mustParse(t, c.src)
		loc, err := d.resolve("set", c.path)
		if err != nil {
			t.Fatal(err)
		}
		if err := d.commit(c.path, []byte(c.src), loc, mustSpan(t, d, loc), accept); err != nil {
			t.Errorf("%q at %q: %v", c.src, c.path, err)
		}
	}
	if !sameOutside([]byte("a\nb\n"), []int{0, 2}, []byte("a\nb\n"), []int{0, 2}, 1, 1, 1) {
		t.Error("sameOutside refuses two equal texts")
	}
}

// TestSameOutsideCountsTheLinesAfterTheNode checks that a line added
// after the edited node is refused, even where the lines the two texts
// share at the start and at the end cover every line.
func TestSameOutsideCountsTheLinesAfterTheNode(t *testing.T) {
	a, b := []byte("x\nx\n"), []byte("x\nx\nx\n")
	if sameOutside(a, newLineIndex(a), b, newLineIndex(b), 1, 1, 1) {
		t.Error("sameOutside accepts a line added after the node")
	}
	c := []byte("x\ny\nx\n")
	if !sameOutside(a, newLineIndex(a), c, newLineIndex(c), 1, 1, 2) {
		t.Error("sameOutside refuses a node that grew by one line")
	}
}

// TestCommitChecksBlockPlacement checks that a block scalar edit is
// refused unless the new node's body sits exactly on the bytes Set
// wrote, under the same header and at the same indentation, even where
// the value reads back the same.
func TestCommitChecksBlockPlacement(t *testing.T) {
	const src = "a: |\n  one\nb: 1\n"
	d := mustParse(t, src)
	loc, err := d.resolve("set", "a")
	if err != nil {
		t.Fatal(err)
	}
	h, sp, indent, err := d.blockSlot(loc.node)
	if err != nil {
		t.Fatal(err)
	}
	w := blockText{header: h, body: "\n  x", value: "x\n", indent: indent, of: loc.node}
	for _, c := range []struct {
		name, next string
		ok         bool
	}{
		{"the text Set wrote", "a: |\n  x\nb: 1\n", true},
		{"a deeper indentation", "a: |\n   x\nb: 1\n", false},
		{"a leading blank line", "a: |\n\n  x\nb: 1\n", false},
		{"another chomping indicator", "a: |+\n  x\nb: 1\n", false},
		{"a folded header", "a: >\n  x\nb: 1\n", false},
	} {
		next := []byte(c.next)
		wrote := span{sp.start, sp.start + len(w.body)}
		err := d.commit("a", next, loc, sp, func(n *yamlnode.Node) error {
			return w.check(next, wrote, n)
		})
		if (err == nil) != c.ok {
			t.Errorf("%s: commit = %v, want ok %v", c.name, err, c.ok)
		}
		if c.ok {
			d = mustParse(t, src)
			if loc, err = d.resolve("set", "a"); err != nil {
				t.Fatal(err)
			}
		}
	}
}
