package yamlnode_test

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/humanreport"
	"github.com/fivetwenty-io/graft/internal/yamldiff"
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
		{"a\n...\n--- # c3\n\nk: 1\n", map[string]string{"d1{0}#KH": "# c3\n"}},
		{"a\n...\n# c1\n--- # c3\n\nk: 1\n", map[string]string{"d1{0}#KH": "# c1\n# c3\n"}},
		{"--- x\n...\n--- # c1\n\n--- null\n", map[string]string{"d1#DF": "# c1\n"}},
		{"a\n... # c0\n--- # c3\n\nk: 1\n", map[string]string{"d1{0}#KH": "# c0\n# c3\n"}},
		{"a\n... # c0\n---\nk: 1\n", map[string]string{"d1{0}#KH": "# c0"}},
		{"a\n... # c0\n# c1\n---\nk: 1\n", map[string]string{"d1{0}#KH": "# c0\n# c1"}},
		{"a\n... # c0\n\n---\nk: 1\n", map[string]string{"d1{0}#KH": "# c0\n"}},
		{"a\n... # c0\n# c1\n\n# c2\n--- # c3\nk: 1\n", map[string]string{"d1{0}#KH": "# c0\n# c1\n\n# c2\n# c3"}},
		{"a\n... # c0\n---\n", map[string]string{"d1#DF": "# c0"}},
		{"a: 1\n... # c0\n---\nb: 1\n... # c9\n---\nc: 1\n", map[string]string{"d1{0}#KH": "# c0", "d2{0}#KH": "# c9"}},
		{"--- !!map # c3\nk: 1\n", map[string]string{}},
		{"a\n--- !!map # c3\nk: 1\n", map[string]string{}},
		{"--- !!map # c3\n\n# c4\nk: 1\n", map[string]string{"d0{0}#KH": "# c4"}},
		{"--- !!str # c\nx\n", map[string]string{"d0#VL": "# c"}},
		{"--- &a # c\nx\n", map[string]string{"d0#VL": "# c"}},
		{"--- !!seq # c\n- a\n", map[string]string{"d0[0]#VL": "# c"}},
		{"--- !!seq # c3\n\n# c4\n- a\n", map[string]string{"d0[0]#VL": "# c3", "d0[0]#VH": "# c4"}},
		{"a\n--- !!seq # c\n- a\n", map[string]string{"d1[0]#VL": "# c"}},
		{"--- !!map # c3\r\nk: 1\r\n", map[string]string{}},
		{"--- # c3\r\nk: 1\r\n", map[string]string{"d0{0}#KH": "# c3"}},
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

// flowSlots records the comments on every node inside a flow collection,
// keyed by its path from the collection, so the same flow can be compared
// wherever it sits.
func flowSlots(n *yamlnode.Node, path string, out map[string]string) {
	for i, c := range n.Content {
		p := fmt.Sprintf("%s/%d", path, i)
		for slot, v := range map[string]string{"H": c.HeadComment, "L": c.LineComment, "F": c.FootComment} {
			if v != "" {
				out[p+"#"+slot] = v
			}
		}
		flowSlots(c, p, out)
	}
}

// renderFlowDiff renders what graft diff prints for a change from
// fromText to toText, so a root flow and a nested one can be compared by
// what they add.
func renderFlowDiff(t *testing.T, fromText, toText string) string {
	t.Helper()
	dir := t.TempDir()
	from, to := filepath.Join(dir, "from.yml"), filepath.Join(dir, "to.yml")
	for path, text := range map[string]string{from: fromText, to: toText} {
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	f, g, err := yamldiff.LoadFiles(from, to)
	if err != nil {
		t.Fatalf("LoadFiles: %v", err)
	}
	report, err := yamldiff.CompareInputFiles(f, g)
	if err != nil {
		t.Fatalf("CompareInputFiles: %v", err)
	}
	var buf bytes.Buffer
	if err := humanreport.Write(&buf, report, humanreport.Options{Width: 80}); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// The first line names the location, which differs between a root and
	// a nested flow. Everything after it has to agree.
	_, body, _ := strings.Cut(strings.TrimLeft(buf.String(), "\n"), "\n")
	return body
}

// TestCommentsRootFlowMatchesNestedFlow keeps a root flow collection and
// the same flow nested under a key on one comment model, in the slots and
// in the report graft diff prints. Both differ from yaml.v3 in the same
// places, so they have to move together when that model changes. The
// root flow starts either on its own line after "---" or on the "---"
// line itself. A foot comment depends on the line the flow starts on, as
// yaml.v3 only has one past the first line of the stream, so the nested
// flow starts on the same line as the root flow does.
func TestCommentsRootFlowMatchesNestedFlow(t *testing.T) {
	for _, flow := range []string{
		"{\n# c2\n\nk0: 1\n}",
		"[\n# c1\na, # c2\nb\n# c3\n]",
		"{\nk0: 1, # c1\n# c2\nk1: [x, # c3\n  y],\nk2: {a: 1} # c4\n}",
		"{\nk0: 1\n# c5\n}",
		"[\n{a: 1, # c1\n  b: 2},\n# c2\n[x, y] # c3\n]",
		"[1, # c1\n 2]",
		"{a: 1 # c1\n }",
		"!!seq [1, # c1\n 2]",
		"&a {a: 1 # c1\n }",
		"{a: 1,\n# c1\n\nb: 2}",
	} {
		lines := strings.Split(flow, "\n")
		for i := 1; i < len(lines); i++ {
			lines[i] = "  " + lines[i]
		}
		body := strings.Join(lines, "\n")
		for _, form := range []struct{ root, above string }{{"---\n", "z: 0\n"}, {"--- ", ""}} {
			root, nested := form.root+body+"\n", form.above+"r: "+body+"\n"
			gotRoot, gotNested := map[string]string{}, map[string]string{}
			flowSlots(mustParse(t, root)[0].Content[0], "", gotRoot)
			doc := mustParse(t, nested)[0].Content[0]
			flowSlots(doc.Content[len(doc.Content)-1], "", gotNested)
			// A block of comments right after the opening bracket of a flow that
			// starts past the first line is a foot of the bracket, which holds
			// no comments, so only the "--- " form has to keep every comment.
			if form.above == "" && len(gotRoot) == 0 {
				t.Errorf("Parse(%q) placed no comments inside the flow", root)
			}
			if !reflect.DeepEqual(gotRoot, gotNested) {
				t.Errorf("root flow and nested flow place comments differently:\n%q\n got %q\n%q\n got %q", root, gotRoot, nested, gotNested)
			}
			got, want := renderFlowDiff(t, "x\n", root), renderFlowDiff(t, form.above+"r: x\n", nested)
			if got != want {
				t.Errorf("root flow and nested flow render differently:\n%q\n got %q\n%q\n got %q", root, got, nested, want)
			}
		}
	}
}

// TestCommentsFlowEntryFootAfterCollection checks the slots yaml.v3
// gives a comment that follows a flow-map entry whose value is a flow
// collection. The comment is the foot of the entry's key.
func TestCommentsFlowEntryFootAfterCollection(t *testing.T) {
	for _, c := range []struct {
		in   string
		want map[string]string
	}{
		{"r: {\n  k0: [],\n  # c1\n}\n", map[string]string{"d0{0}{0}#KF": "# c1"}},
		{"r: {\n  k0: {a: 1},\n  # c1\n\n}\n", map[string]string{"d0{0}{0}#KF": "# c1"}},
		{"r: [\n  k0: [],\n  # c1\n]\n", map[string]string{"d0{0}[0]{0}#KF": "# c1"}},
	} {
		got := map[string]string{}
		printedSlots(yamlgolden.FromNode(mustParse(t, c.in)[0]), "d0", got)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("Parse(%q) slots = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestCommentsAfterTagOrAnchor checks the slots yaml.v3 gives a comment
// that follows a tag or an anchor. It is the line comment of the first
// scalar after it, ahead of that scalar's own line comment, wherever
// the scalar sits. A tag or an anchor on a mapping gives the comment to
// the key, whose line comment is not printed.
func TestCommentsAfterTagOrAnchor(t *testing.T) {
	for _, c := range []struct {
		in   string
		want map[string]string
	}{
		{"--- &a # c1\nx # c2\n", map[string]string{"d0#VL": "# c1\n# c2"}},
		{"--- &a !!seq # c1\n- x # c2\n", map[string]string{"d0[0]#VL": "# c1\n# c2"}},
		{"!!str # c1\nx # c2\n", map[string]string{"d0#VL": "# c1\n# c2"}},
		{"!!str # c1\nx\n", map[string]string{"d0#VL": "# c1"}},
		{"k: !!str # c1\n  x # c2\n", map[string]string{"d0{0}#VL": "# c1\n# c2"}},
		{"k: &a # c1\n  - x\n", map[string]string{"d0{0}[0]#VL": "# c1"}},
		{"--- &a # c\n-\n  - x\n", map[string]string{"d0[0][0]#VL": "# c"}},
		{"--- &a # c\n-\n  x\n", map[string]string{"d0[0]#VL": "# c"}},
		{"--- !!map # c1\na: 1 # c2\n", map[string]string{"d0{0}#VL": "# c2"}},
	} {
		got := map[string]string{}
		printedSlots(yamlgolden.FromNode(mustParse(t, c.in)[0]), "d0", got)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("Parse(%q) slots = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestCommentsUnderBareDash checks the slots yaml.v3 gives a comment
// that follows a sequence dash with no value. The dash's null holds no
// foot comment, so the comment reaches the next node that takes one.
// When no node after it takes the comment, yaml.v3 leaves it on the
// document, which the report never prints, and graft drops it.
func TestCommentsUnderBareDash(t *testing.T) {
	for _, c := range []struct {
		in   string
		want map[string]string
	}{
		{"-\n# c9\n", map[string]string{}},
		{"-\n# c8\n\n-\n", map[string]string{}},
		{"m:\n  -\n    # c4\n\n    k0: 1\n", map[string]string{"d0{0}[0]{0}#KF": "# c4"}},
		{"-\n  -\n      # c7\n\n    - null\n", map[string]string{"d0[0][0][0]#VF": "# c7"}},
	} {
		got := map[string]string{}
		printedSlots(yamlgolden.FromNode(mustParse(t, c.in)[0]), "d0", got)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("Parse(%q) slots = %q, want %q", c.in, got, c.want)
		}
	}
}
