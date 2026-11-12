package yaml

import "testing"

func TestOffsetOfLineAndColumn(t *testing.T) {
	d := &Document{src: []byte("a: 1\nné: x\n\nz")}
	d.lines = newLineIndex(d.src)
	cases := []struct {
		line, col, want int
		ok              bool
	}{
		{1, 1, 0, true},
		{1, 4, 3, true},
		{2, 1, 5, true},
		{2, 5, 10, true},
		{3, 1, 12, true},
		{4, 1, 13, true},
		{5, 1, 0, false},
		{2, 9, 0, false},
		{0, 1, 0, false},
	}
	for _, c := range cases {
		got, ok := d.offset(c.line, c.col)
		if got != c.want || ok != c.ok {
			t.Errorf("offset(%d, %d) = %d, %v; want %d, %v", c.line, c.col, got, ok, c.want, c.ok)
		}
	}
}

// TestLineIndexCountsLineBreaksAsTheParserDoes checks that CRLF, a lone
// CR, and U+0085 each end one line, as they do for yamlnode.Parse, so a
// node's Line finds its line in the input as written.
func TestLineIndexCountsLineBreaksAsTheParserDoes(t *testing.T) {
	src := []byte("a\r\nb\rc\u0085d\n")
	if got, want := newLineIndex(src), []int{0, 3, 5, 8, 10}; !equalInts(got, want) {
		t.Fatalf("newLineIndex = %v, want %v", got, want)
	}
	d := &Document{src: src, lines: newLineIndex(src)}
	for _, c := range []struct{ line, col, want int }{{1, 2, 1}, {2, 2, 4}, {3, 2, 6}, {4, 2, 9}} {
		if got, ok := d.offset(c.line, c.col); !ok || got != c.want {
			t.Errorf("offset(%d, %d) = %d, %v; want %d", c.line, c.col, got, ok, c.want)
		}
	}
	if _, ok := d.offset(1, 3); ok {
		t.Error("offset ran past the end of a CRLF line")
	}
}

func equalInts(a, b []int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestResolveRecordsAliasesAndAnchors(t *testing.T) {
	d, err := ParseDocument([]byte("plain: v\nanch: &a v\nali: *a\nbase: &b\n  k: v\nuse: *b\nroot:\n  in: v\n"))
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		path                          string
		isAlias, throughAlias, anchor bool
	}{
		{"plain", false, false, false},
		{"anch", false, false, true},
		{"ali", true, false, true},
		{"base.k", false, false, true},
		{"use.k", false, true, true},
		{"root.in", false, false, false},
	}
	for _, c := range cases {
		loc, err := d.resolve("get", c.path)
		if err != nil {
			t.Errorf("resolve(%q): %v", c.path, err)
			continue
		}
		if loc.isAlias != c.isAlias || loc.throughAlias != c.throughAlias || loc.anchor != c.anchor {
			t.Errorf("resolve(%q) = isAlias %v, throughAlias %v, anchor %v; want %v, %v, %v",
				c.path, loc.isAlias, loc.throughAlias, loc.anchor, c.isAlias, c.throughAlias, c.anchor)
		}
		if loc.parent.Content[loc.index] != loc.node && !loc.isAlias {
			t.Errorf("resolve(%q): parent.Content[index] is not the node", c.path)
		}
	}
}

func TestResolveRecordsTheEntry(t *testing.T) {
	d, err := ParseDocument([]byte("m:\n  k: v\nl: [x, y]\n"))
	if err != nil {
		t.Fatal(err)
	}
	loc, err := d.resolve("get", "m.k")
	if err != nil || loc.key == nil || loc.key.Value != "k" || loc.index != 1 || loc.node.Value != "v" {
		t.Errorf("resolve(m.k) = %+v, %v", loc, err)
	}
	loc, err = d.resolve("get", "l.1")
	if err != nil || loc.key != nil || loc.index != 1 || loc.node.Value != "y" {
		t.Errorf("resolve(l.1) = %+v, %v", loc, err)
	}
	loc, err = d.resolve("get", "")
	if err != nil || loc.parent != d.docs[0] || loc.index != 0 || loc.node != d.docs[0].Content[0] {
		t.Errorf("resolve(\"\") = %+v, %v; want the root inside its document", loc, err)
	}
}
