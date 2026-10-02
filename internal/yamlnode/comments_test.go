package yamlnode_test

import (
	"fmt"
	"os"
	"reflect"
	"testing"

	"github.com/fivetwenty-io/graft/internal/yamlgolden"
	"github.com/fivetwenty-io/graft/internal/yamlnode"
)

// printedSlots lists the four comment slots neat prints, plus a scalar
// sequence item's head comment, a root scalar's slots, and a document's
// head (#DH) and foot (#DF), keyed by mapping pair and sequence indexes
// so duplicate keys stay distinct.
func printedSlots(n *yamlgolden.Node, path string, out map[string]string) {
	put := func(k, v string) {
		if v != "" {
			out[k] = v
		}
	}
	switch n.Kind {
	case "document":
		put(path+"#DH", n.HeadComment)
		put(path+"#DF", n.FootComment)
		for _, c := range n.Content {
			if c.Kind == "scalar" {
				put(path+"#VH", c.HeadComment)
				put(path+"#VL", c.LineComment)
				put(path+"#VF", c.FootComment)
			}
			printedSlots(c, path, out)
		}
	case "mapping":
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			kp := fmt.Sprintf("%s{%d}", path, i/2)
			put(kp+"#KH", k.HeadComment)
			put(kp+"#KF", k.FootComment)
			if v.Kind == "scalar" {
				put(kp+"#VL", v.LineComment)
				put(kp+"#VF", v.FootComment)
			}
			printedSlots(v, kp, out)
		}
	case "sequence":
		for i, c := range n.Content {
			ip := fmt.Sprintf("%s[%d]", path, i)
			if c.Kind == "scalar" {
				put(ip+"#VH", c.HeadComment)
				put(ip+"#VL", c.LineComment)
				put(ip+"#VF", c.FootComment)
			}
			printedSlots(c, ip, out)
		}
	}
}

func TestCommentsMatchYAMLv3PrintedSlots(t *testing.T) {
	total := 0
	for _, name := range nodeFixtures(t) {
		want := commentGolden(t, name)
		if want.Error != "" {
			continue
		}
		docs := mustParseFile(t, "testdata/nodes/"+name+".yml")
		for i := range docs {
			w, g := map[string]string{}, map[string]string{}
			printedSlots(want.Documents[i], fmt.Sprintf("d%d", i), w)
			printedSlots(yamlgolden.FromNode(docs[i]), fmt.Sprintf("d%d", i), g)
			for k := range g {
				if _, ok := w[k]; !ok {
					w[k] = ""
				}
			}
			for k, wv := range w {
				total++
				if g[k] != wv {
					t.Errorf("%s %s = %q, want %q", name, k, g[k], wv)
				}
			}
		}
	}
	if total < 140 {
		t.Fatalf("compared %d slots, want at least 140", total)
	}
}

func TestCommentsInEveryPrintedSlot(t *testing.T) {
	doc := mustParse(t, "# head of a\na: 1 # line of a\n# foot of a\n\nl:\n  - one # line one\n  # foot of one\n\n  # head of two\n  - two\nb: 2\n")[0].Content[0]
	a, one, two := doc.Content[0], doc.Content[3].Content[0], doc.Content[3].Content[1]
	if a.HeadComment != "# head of a" || doc.Content[1].LineComment != "# line of a" || a.FootComment != "# foot of a" {
		t.Errorf("key a slots = %q / %q / %q", a.HeadComment, doc.Content[1].LineComment, a.FootComment)
	}
	if one.LineComment != "# line one" || one.FootComment != "# foot of one" || two.HeadComment != "# head of two" {
		t.Errorf("sequence scalar slots = %q / %q / %q", one.LineComment, one.FootComment, two.HeadComment)
	}
}

// TestCommentsOnRootScalar checks the comments of a document whose root
// is a scalar against the slots yaml.v3 gives. A foot after a line
// comment on the stream's first line, or after a blank line, is the
// document's foot instead, and a head before a blank line is the
// document's head.
func TestCommentsOnRootScalar(t *testing.T) {
	type slots struct{ head, line, foot string }
	for _, c := range []struct {
		in   string
		want []slots
	}{
		{"x # c\n", []slots{{"", "# c", ""}}},
		{"x # c", []slots{{"", "# c", ""}}},
		{"--- x # c\n", []slots{{"", "# c", ""}}},
		{"--- !!str x # c\n", []slots{{"", "# c", ""}}},
		{"\"x\" # c\n", []slots{{"", "# c", ""}}},
		{"x\n# foot\n", []slots{{"", "", "# foot"}}},
		{"|\n  x\n# foot\n", []slots{{"", "", "# foot"}}},
		{"--- | # c\n  x\n", []slots{{"", "# c", ""}}},
		{"x # c\n# foot\n", []slots{{"", "# c", ""}}},
		{"x\n\n# trailing\n", []slots{{"", "", ""}}},
		{"# h\nx\n", []slots{{"# h", "", ""}}},
		{"# h\n\nx\n", []slots{{"", "", ""}}},
		{"--- # h\nx\n", []slots{{"# h", "", ""}}},
		{"# a\n---\n# b\nx\n", []slots{{"# a\n# b", "", ""}}},
		{"x # c\n---\n# h\ny # d\n# f\n", []slots{{"", "# c", ""}, {"# h", "# d", "# f"}}},
	} {
		docs := mustParse(t, c.in)
		if len(docs) != len(c.want) {
			t.Errorf("Parse(%q) = %d documents, want %d", c.in, len(docs), len(c.want))
			continue
		}
		for i, d := range docs {
			r := d.Content[0]
			if got := (slots{r.HeadComment, r.LineComment, r.FootComment}); got != c.want[i] {
				t.Errorf("Parse(%q) document %d root: %+v, want %+v", c.in, i, got, c.want[i])
			}
		}
	}
}

// TestCommentsOnDocument checks a document's head and foot comments, and
// the comments around them, against the slots yaml.v3 gives. Comments
// still pending when a document ends become its foot, and a foot right
// after the next document's "---" goes to the document before it.
func TestCommentsOnDocument(t *testing.T) {
	for _, c := range []struct {
		in   string
		want map[string]string
	}{
		{"a: 1\n\n# trailing\n", map[string]string{"d0#DF": "# trailing"}},
		{"x\n\n# f\n\n# g\n", map[string]string{"d0#DF": "# f\n\n# g"}},
		{"x\n\n# f\n\n# g\n\n", map[string]string{"d0#DF": "# f\n\n# g\n"}},
		{"- a: 1\n# f\n", map[string]string{"d0#DF": "# f"}},
		{"a: 1\n# f\n", map[string]string{"d0{0}#KF": "# f"}},
		{"# c\n\na: 1\n", map[string]string{"d0#DH": "# c"}},
		{"# a\n\n# b\nx: 1\n", map[string]string{"d0#DH": "# a", "d0{0}#KH": "# b"}},
		{"# h\n\n# h2\n\na: 1\n", map[string]string{"d0#DH": "# h\n\n# h2"}},
		{"--- # c\n# d\n\n# e\n", map[string]string{"d0#DF": "# c\n# d\n\n# e"}},
		{"a: 1\n# f\n---\nb: 2\n", map[string]string{"d0#DF": "# f"}},
		{"a: 1\n# h\n---\n# c\n\nb: 1\n", map[string]string{"d0#DF": "# c"}},
		{"a: 1 # l\n# f\n---\n# c\n\n# d\nb: 1\n", map[string]string{"d0#DF": "# c", "d0{0}#VL": "# l", "d1{0}#KH": "# d"}},
		{"- a\n---\n# c\n\n# d\n\n---\n", map[string]string{"d0#DF": "# c", "d1#DF": "# d\n"}},
		{"a: 1\n---\n# c\n---\nb: 1\n", map[string]string{"d1#DF": "# c"}},
		{"a: 1\n\n# f\n...\n", map[string]string{"d0#DF": "# f"}},
		{"a: 1\n...\n# f\n# g\n\n# h\n", map[string]string{"d0#DF": "# f\n# g"}},
		{"a: 1\n...\n\n# f\n", map[string]string{}},
		{"a: 1\n...\n---\n# c\n\nb: 1\n", map[string]string{"d1{0}#KF": "# c"}},
		{"[1, 2]\n# f\n", map[string]string{"d0#DF": "# f"}},
		{"# h\n\n[1, 2]\n\n# f\n", map[string]string{"d0#DH": "# h", "d0#DF": "# f"}},
		{"a:\n  b:\n    c: 1\n# x1\n  # z1\n# x2\n", map[string]string{"d0#DF": "# x2", "d0{0}{0}#KF": "# z1", "d0{0}{0}{0}#KF": "# x1"}},
		{"a:\n  b:\n    c: 1\n# x1\n  # z1\n# x2\n---\nd: 1\n", map[string]string{"d0#DF": "# x2", "d0{0}{0}#KF": "# z1", "d0{0}{0}{0}#KF": "# x1"}},
		{"a:\n  b: 1\n  # z1\n# x2\n", map[string]string{"d0{0}#KF": "# x2", "d0{0}{0}#KF": "# z1"}},
		{"a:\n  b:\n    c: 1\n  # z1\n# x2\n", map[string]string{"d0{0}#KF": "# x2", "d0{0}{0}{0}#KF": "# z1"}},
	} {
		got := map[string]string{}
		for i, d := range mustParse(t, c.in) {
			printedSlots(yamlgolden.FromNode(d), fmt.Sprintf("d%d", i), got)
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("Parse(%q) slots:\n got %q\nwant %q", c.in, got, c.want)
		}
	}
}

func mustParseFile(t *testing.T, path string) []*yamlnode.Node {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return mustParse(t, string(data))
}

// TestCommentsDedentedBlockDivergence pins the one comment layout where
// graft and yaml.v3 disagree, which is an accepted divergence. When a
// comment block sits between a key and its block collection and a later
// line of the block starts left of the key, yaml.v3 gives the earlier
// line to the first item as a foot comment, and graft keeps it as the
// key's foot comment. Both give the later line to the first item's head.
func TestCommentsDedentedBlockDivergence(t *testing.T) {
	y0 := mustParse(t, "a:\n  y0:\n  # c22\n# c1\n  - 1\n")[0].Content[0].Content[1]
	key, item := y0.Content[0], y0.Content[1].Content[0]
	if key.FootComment != "# c22" || item.HeadComment != "# c1" || item.FootComment != "" {
		t.Fatalf("key foot %q, item head %q, item foot %q; want %q, %q, and %q (yaml.v3 moves %q to the item's foot)",
			key.FootComment, item.HeadComment, item.FootComment, "# c22", "# c1", "", "# c22")
	}
}
