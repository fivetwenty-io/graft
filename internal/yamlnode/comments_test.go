package yamlnode_test

import (
	"fmt"
	"os"
	"testing"

	"github.com/fivetwenty-io/graft/internal/yamlgolden"
	"github.com/fivetwenty-io/graft/internal/yamlnode"
)

// printedSlots lists the four comment slots neat prints, plus a scalar
// sequence item's head comment, keyed by mapping pair and sequence
// indexes so duplicate keys stay distinct.
func printedSlots(n *yamlgolden.Node, path string, out map[string]string) {
	put := func(k, v string) {
		if v != "" {
			out[k] = v
		}
	}
	switch n.Kind {
	case "document":
		for _, c := range n.Content {
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
