package load

import (
	"testing"

	"github.com/fivetwenty-io/graft/pkg/query/value"
)

func TestFromNodeStringKeysRejectCollision(t *testing.T) {
	n := mustParseYAML(t, "1: a\n\"1\": b\n")
	_, err := FromNode(n, FromNodeOptions{StringKeys: true})
	wantErrContains(t, err, `duplicate key "1"`)
	v, err := FromNode(n, FromNodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, len(v.Entries()), 2) // yq keeps both and prints the last
	got, _ := v.Get("1")
	wantEqual(t, got.Str(), "b")
}

func TestFromNodeKeepMetaCarriesCommentsAndAnchors(t *testing.T) {
	n := mustParseYAML(t, "a: &x 1 # one\nb: *x\n")
	v, err := FromNode(n, FromNodeOptions{KeepMeta: true})
	if err != nil {
		t.Fatal(err)
	}
	a, _ := v.Get("a")
	wantEqual(t, a.Meta().Anchor, "x")
	wantEqual(t, a.Meta().LineComment, "# one")
	b, _ := v.Get("b")
	wantEqual(t, b.Meta().AliasOf, "x") // alias nodes stay unexpanded under KeepMeta
	wantEqual(t, b.Kind(), value.KindNull)
}

func TestFromNodeExpandsAliasesWithoutMeta(t *testing.T) {
	n := mustParseYAML(t, "a: &x [1]\nb: *x\n")
	v, err := FromNode(n, FromNodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	b, _ := v.Get("b")
	wantEqual(t, b.Len(), 1)
}

func TestFromNodeAliasBudget(t *testing.T) {
	n := mustParseYAML(t, billionLaughs(8))
	_, err := FromNode(n, FromNodeOptions{AliasBudget: 10000})
	wantErrContains(t, err, "alias expansion limit")
}

func TestFromNodeScalarTags(t *testing.T) {
	v, err := FromNode(mustParseYAML(t, "i: 10\nf: 1.5\nb: true\nn: ~\ns: 2001-12-14\nq: \"7\"\nh: 0x10\no: 012\nu: 1_000\ninf: .inf\n"), FromNodeOptions{KeepMeta: true})
	if err != nil {
		t.Fatal(err)
	}
	wantKinds(t, v, map[string]value.Kind{"i": value.KindNumber, "f": value.KindNumber, "b": value.KindTrue, "n": value.KindNull, "s": value.KindString, "q": value.KindString, "h": value.KindNumber, "o": value.KindNumber, "u": value.KindNumber, "inf": value.KindNumber})
	i, _ := v.Get("i")
	wantEqual(t, i.Literal(), "10")
	h, _ := v.Get("h")
	wantEqual(t, h.Literal(), "")
	wantEqual(t, h.Float(), 16.0)
	wantEqual(t, h.Meta().Text, "0x10")
	o, _ := v.Get("o")
	wantEqual(t, o.Float(), 10.0)
	u, _ := v.Get("u")
	wantEqual(t, u.Float(), 1000.0)
	wantEqual(t, u.Meta().Text, "1_000")
}

func TestFromNodeFoldsMergeKeysUnderStringKeys(t *testing.T) {
	n := mustParseYAML(t, "base: &b {x: 1, y: 2}\nuse:\n  <<: *b\n  y: 3\n  z: 4\n")
	v, err := FromNode(n, FromNodeOptions{StringKeys: true})
	if err != nil {
		t.Fatal(err)
	}
	use := mustGet(v, "use")
	wantEqual(t, use.Keys(), []string{"x", "y", "z"})
	wantEqual(t, mustGet(use, "y").Float(), 3.0)

	kept, err := FromNode(n, FromNodeOptions{KeepMeta: true})
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, mustGet(kept, "use").Keys(), []string{"<<", "y", "z"})
}

func TestFromNodeKeepsOverBoundLiteralAsText(t *testing.T) {
	v, err := FromNode(mustParseYAML(t, "n: !!float 1e1000000000000\n"), FromNodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	n := mustGet(v, "n")
	wantEqual(t, n.Literal(), "1e1000000000000")
	wantEqual(t, n.Bounded(), false)
}

func TestFromNodeFillsAnchorsAndPositions(t *testing.T) {
	docs := mustReadAll(t, "a: &x 1\nb: *x\n", LoadOptions{KeepMeta: true})
	wantEqual(t, len(docs), 1)
	wantEqual(t, docs[0].Anchors["x"].Literal(), "1")
	a := mustGet(docs[0].Value, "a")
	wantEqual(t, a.Meta().Line, 1)
	wantEqual(t, a.Meta().Filename, "f")
}

func TestFromNodeYes(t *testing.T) {
	v, err := FromNode(mustParseYAML(t, "b: yes\nc: !!bool yes\n"), FromNodeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, mustGet(v, "b").Kind(), value.KindString) // yq 4.54.1 reads a plain yes as a string
	wantEqual(t, mustGet(v, "c").Kind(), value.KindTrue)
}

func TestMetaStyleTagAndKeyMeta(t *testing.T) {
	v, err := FromNode(mustParseYAML(t, "'a': !!str 1\n"), FromNodeOptions{KeepMeta: true})
	if err != nil {
		t.Fatal(err)
	}
	e := v.Entries()[0]
	wantEqual(t, e.Key.Meta().Style, value.SingleQuoted)
	wantEqual(t, e.Key.Meta().Tag, "")
	wantEqual(t, e.Value.Kind(), value.KindString)
	wantEqual(t, e.Value.Meta().Style, value.Tagged)
	wantEqual(t, e.Value.Meta().Tag, "!!str")
	wantEqual(t, e.Value.Meta().Text, "1")
}

func TestMetaStyleMapsEveryFlag(t *testing.T) {
	v, err := FromNode(mustParseYAML(t, "a: 'x'\nb: \"y\"\nc: |\n  z\nd: >\n  w\ne: [1]\nf: !!int 1\ng: !!str 'yes'\n!local h: {}\ni: plain\n"), FromNodeOptions{KeepMeta: true})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]value.StyleFlags{
		"a": value.SingleQuoted,
		"b": value.DoubleQuoted,
		"c": value.Literal,
		"d": value.Folded,
		"e": value.Flow,
		"f": value.Tagged,
		"g": value.Tagged | value.SingleQuoted,
		"h": value.Flow,
		"i": 0,
	}
	for key, style := range want {
		if got := mustGet(v, key).Meta().Style; got != style {
			t.Errorf("value of %q has style %06b, want %06b", key, got, style)
		}
	}
	wantEqual(t, mustGet(v, "f").Meta().Tag, "!!int")
	wantEqual(t, mustGet(v, "i").Meta().Tag, "") // an implied tag is not recorded
	wantEqual(t, mustGet(v, "c").Meta().Text, "z\n")
	h := v.Entries()[7].Key.Meta()
	wantEqual(t, h.Style, value.Tagged)
	wantEqual(t, h.Tag, "!local")
}
