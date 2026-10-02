package yamlnode_test

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/fivetwenty-io/graft/internal/yamlgolden"
	"github.com/fivetwenty-io/graft/internal/yamlnode"
)

type nodeGolden struct {
	Documents []*yamlgolden.Node `json:"documents"`
	Error     string             `json:"error"`
}

func compareNodeFixture(t *testing.T, name string, keepComments bool) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "nodes", name+".yml"))
	if err != nil {
		t.Fatal(err)
	}
	var want nodeGolden
	yamlgolden.ReadJSON(t, filepath.Join("testdata", "golden", "nodes", name+".json"), &want)

	docs, err := yamlnode.Parse(data)
	if want.Error != "" {
		// Every erroring node fixture is one where libyaml and goccy agree
		// on the line, which is where D6 asks graft to match it.
		prefix, ok := yamlgolden.LineErrorPrefix(want.Error)
		if !ok {
			t.Fatalf("golden error %q has no line number", want.Error)
		}
		if err == nil || !strings.HasPrefix(err.Error(), prefix) {
			t.Fatalf("Parse error = %v, want the prefix of %q", err, want.Error)
		}
		return
	}
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(docs) != len(want.Documents) {
		t.Fatalf("Parse returned %d documents, want %d", len(docs), len(want.Documents))
	}
	for i := range docs {
		w := yamlgolden.Strip(want.Documents[i], keepComments, !keepComments)
		g := yamlgolden.Strip(yamlgolden.FromNode(docs[i]), keepComments, !keepComments)
		if d := yamlgolden.Diff(w, g); d != "" {
			t.Fatalf("document %d: %s", i, d)
		}
	}
}

func nodeFixtures(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob("testdata/nodes/*.yml")
	if err != nil || len(files) != 38 {
		t.Fatalf("found %d node fixtures (%v), want 38", len(files), err)
	}
	names := make([]string, len(files))
	for i, f := range files {
		names[i] = strings.TrimSuffix(filepath.Base(f), ".yml")
	}
	return names
}

func TestParseMatchesYAMLv3Nodes(t *testing.T) {
	for _, name := range nodeFixtures(t) {
		t.Run(name, func(t *testing.T) { compareNodeFixture(t, name, false) })
	}
}

// isCRLF reports whether a node fixture uses CRLF line breaks.
func isCRLF(t *testing.T, name string) bool {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "nodes", name+".yml"))
	if err != nil {
		t.Fatal(err)
	}
	return bytes.Contains(data, []byte("\r\n"))
}

// commentGolden returns the golden that holds yaml.v3's comments for a
// node fixture. yaml.v3 mangles comment text on CRLF input, which graft
// does not copy, so a CRLF fixture's comments come from the golden the
// oracle writes for its LF form.
func commentGolden(t *testing.T, name string) nodeGolden {
	t.Helper()
	file := name + ".json"
	if isCRLF(t, name) {
		file = name + ".lf.json"
	}
	var g nodeGolden
	yamlgolden.ReadJSON(t, filepath.Join("testdata", "golden", "nodes", file), &g)
	return g
}

// TestParseCRLFMatchesLF checks that every CRLF node fixture parses to
// exactly the tree, comments, and lines of its LF form. With the LF form
// matching yaml.v3 here and in TestCommentsMatchYAMLv3PrintedSlots, CRLF
// input matches yaml.v3 everywhere except the comment text yaml.v3
// mangles.
func TestParseCRLFMatchesLF(t *testing.T) {
	crlf := 0
	for _, name := range nodeFixtures(t) {
		if !isCRLF(t, name) {
			continue
		}
		crlf++
		data, err := os.ReadFile(filepath.Join("testdata", "nodes", name+".yml"))
		if err != nil {
			t.Fatal(err)
		}
		got := mustParse(t, string(data))
		want := mustParse(t, strings.ReplaceAll(string(data), "\r\n", "\n"))
		if len(got) != len(want) {
			t.Fatalf("%s: %d documents, but its LF form has %d", name, len(got), len(want))
		}
		for i := range got {
			if d := yamlgolden.Diff(yamlgolden.FromNode(want[i]), yamlgolden.FromNode(got[i])); d != "" {
				t.Errorf("%s document %d differs from its LF form: %s", name, i, d)
			}
		}
		golden := commentGolden(t, name)
		if len(want) != len(golden.Documents) {
			t.Fatalf("%s: the LF form has %d documents, yaml.v3 has %d", name, len(want), len(golden.Documents))
		}
		for i := range want {
			w := yamlgolden.Strip(golden.Documents[i], false, true)
			if d := yamlgolden.Diff(w, yamlgolden.Strip(yamlgolden.FromNode(want[i]), false, true)); d != "" {
				t.Errorf("%s document %d: the LF form differs from yaml.v3: %s", name, i, d)
			}
		}
	}
	if crlf != 3 {
		t.Fatalf("found %d CRLF node fixtures, want 3", crlf)
	}
}

// TestParseScalarShapesMatchYAMLv3 runs 332 generated block, quoted, and
// plain scalars through Parse. It is the differential check the engine
// design asked for, and it caught goccy dropping trailing spaces and
// skipping folding when a block scalar runs to the end of the stream, and
// rejecting an indentation indicator followed by trailing blank lines.
func TestParseScalarShapesMatchYAMLv3(t *testing.T) {
	var vectors []struct {
		Input     string             `json:"input"`
		Documents []*yamlgolden.Node `json:"documents"`
		Error     string             `json:"error"`
	}
	yamlgolden.ReadJSON(t, filepath.Join("testdata", "golden", "scalars.json"), &vectors)
	if len(vectors) != 332 {
		t.Fatalf("scalars.json holds %d vectors, want 332", len(vectors))
	}
	for _, v := range vectors {
		docs, err := yamlnode.Parse([]byte(v.Input))
		if v.Error != "" {
			if err == nil {
				t.Errorf("Parse(%q) succeeded; yaml.v3 rejects it with %q", v.Input, v.Error)
			}
			continue
		}
		if err != nil || len(docs) != len(v.Documents) {
			t.Errorf("Parse(%q) = %d documents, %v; want %d", v.Input, len(docs), err, len(v.Documents))
			continue
		}
		for i := range docs {
			w := yamlgolden.Strip(v.Documents[i], false, true)
			if d := yamlgolden.Diff(w, yamlgolden.Strip(yamlgolden.FromNode(docs[i]), false, true)); d != "" {
				t.Errorf("Parse(%q) document %d: %s", v.Input, i, d)
			}
		}
	}
}

func mustParse(t *testing.T, src string) []*yamlnode.Node {
	t.Helper()
	docs, err := yamlnode.Parse([]byte(src))
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	return docs
}

func TestParseStripChompKeepsTrailingSpace(t *testing.T) {
	doc := mustParse(t, "a: |-\n  REPLACE-ME \n\nb: >-\n  folded \n")[0].Content[0]
	if got := doc.Content[1].Value; got != "REPLACE-ME " {
		t.Errorf("|- value = %q, want %q", got, "REPLACE-ME ")
	}
	if got := doc.Content[3].Value; got != "folded " {
		t.Errorf(">- value = %q, want %q", got, "folded ")
	}
}

func TestParseBOMAndUTF16(t *testing.T) {
	encode := func(s string, bigEndian bool) []byte {
		out := []byte{0xFF, 0xFE}
		if bigEndian {
			out = []byte{0xFE, 0xFF}
		}
		var unit [2]byte
		for _, u := range utf16.Encode([]rune(s)) {
			if bigEndian {
				binary.BigEndian.PutUint16(unit[:], u)
			} else {
				binary.LittleEndian.PutUint16(unit[:], u)
			}
			out = append(out, unit[:]...)
		}
		return out
	}
	for name, in := range map[string][]byte{
		"utf-8 bom":    append([]byte{0xEF, 0xBB, 0xBF}, "é: 1\n"...),
		"utf-16le bom": encode("é: 1\n", false),
		"utf-16be bom": encode("é: 1\n", true),
	} {
		docs, err := yamlnode.Parse(in)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if key := docs[0].Content[0].Content[0]; key.Value != "é" {
			t.Errorf("%s: key = %q, want %q", name, key.Value, "é")
		}
	}
}

func TestParseBareDashNullValueEmpty(t *testing.T) {
	list := mustParse(t, "list:\n- a\n-\nnext: value\n")[0].Content[0].Content[1]
	if len(list.Content) != 2 || list.Content[1].Tag != "!!null" || list.Content[1].Value != "" {
		t.Fatalf("bare dash item = %+v, want !!null with an empty value", list.Content[1])
	}
	explicit := mustParse(t, "list:\n- a\n- ~\nnext: value\n")[0].Content[0].Content[1]
	if explicit.Content[1].Value != "~" {
		t.Fatalf("an explicit ~ must keep its text, got %q", explicit.Content[1].Value)
	}
}

func TestParseUnknownAliasError(t *testing.T) {
	_, err := yamlnode.Parse([]byte("a: *x\n"))
	if err == nil || err.Error() != "yaml: unknown anchor 'x' referenced" {
		t.Fatalf("err = %v, want yaml.v3's unknown anchor text", err)
	}
}

// TestParseRejectsSelfReferencingAnchor checks that an alias inside the
// node its anchor names is a parse error with the text yaml.v3's decoder
// gives, because a tree that contains itself sends every walk over it
// into an endless loop. An alias to a finished node is still fine, even
// when the anchor name is reused inside the collection it first named.
func TestParseRejectsSelfReferencingAnchor(t *testing.T) {
	for _, in := range []string{
		"a: &x [*x]\n",
		"a: &x {b: *x}\n",
		"a: &x\n  b:\n    - *x\n",
		"&x [[1, *x]]\n",
	} {
		_, err := yamlnode.Parse([]byte(in))
		var pe *yamlnode.ParseError
		if !errors.As(err, &pe) || err.Error() != "yaml: anchor 'x' value contains itself" {
			t.Errorf("Parse(%q) = %v, want the ParseError yaml: anchor 'x' value contains itself", in, err)
		}
	}
	for _, in := range []string{
		"a: &x [1]\nb: [*x]\n",
		"a: &x [&x 1, *x]\n",
		"a: &x [1]\n---\nb: [*x]\n",
	} {
		if _, err := yamlnode.Parse([]byte(in)); err != nil {
			t.Errorf("Parse(%q) = %v, want no error", in, err)
		}
	}
}

// TestParseLinesInLaterDocuments checks the line numbers Parse reports
// for a document that does not start the stream: node lines, the bare
// dash rewrite's lines, comment placement, and parse error lines.
func TestParseLinesInLaterDocuments(t *testing.T) {
	src := "a: 1\n---\nb: 2\n---\n# head of c\nc:\n  -\n  - y # line of y\nd: [1,\n  2]\n"
	docs := mustParse(t, src)
	if len(docs) != 3 {
		t.Fatalf("Parse returned %d documents, want 3", len(docs))
	}
	m := docs[2].Content[0]
	c, items, d := m.Content[0], m.Content[1].Content, m.Content[2]
	if c.Line != 6 || c.HeadComment != "# head of c" {
		t.Errorf("key c: line %d, head %q; want line 6 and %q", c.Line, c.HeadComment, "# head of c")
	}
	if items[0].Line != 7 || items[0].Value != "" || items[1].Line != 8 || items[1].LineComment != "# line of y" {
		t.Errorf("items: %d %q, %d %q; want a bare dash on line 7 and y on line 8 with its line comment",
			items[0].Line, items[0].Value, items[1].Line, items[1].LineComment)
	}
	if d.Line != 9 || m.Content[3].Content[1].Line != 10 {
		t.Errorf("key d on line %d, its second item on line %d; want 9 and 10", d.Line, m.Content[3].Content[1].Line)
	}
	if _, err := yamlnode.Parse([]byte("a: 1\n---\nb: 2\n---\nc:\n\t- x\n")); err == nil || !strings.HasPrefix(err.Error(), "yaml: line 6: ") {
		t.Errorf("err = %v, want the prefix yaml: line 6: ", err)
	}
}

func TestParseBracePlaceholdersAreStrings(t *testing.T) {
	m := mustParse(t, "a: {{x}}\nb: {{x}}-v1\n")[0].Content[0]
	for i, want := range []string{"{{x}}", "{{x}}-v1"} {
		if v := m.Content[2*i+1]; v.Tag != "!!str" || v.Value != want {
			t.Errorf("value %d = %s %q, want !!str %q", i, v.Tag, v.Value, want)
		}
	}
	if _, err := yamlnode.Parse([]byte("{{{{\n")); err == nil {
		t.Fatal("unbalanced braces must stay a parse error")
	}
}

func TestParseErrorKeepsTheLibyamlLine(t *testing.T) {
	_, err := yamlnode.Parse([]byte("a:\n\t- x\n"))
	if err == nil || !strings.HasPrefix(err.Error(), "yaml: line 2: ") {
		t.Fatalf("err = %v, want the prefix yaml: line 2: ", err)
	}
}

// TestParseErrorLineDivergences pins the lines goccy reports where libyaml
// reports a different line or none. D6 accepts goccy's line on these
// inputs, and docs/spruce/genesis-compat-contract.md lists them.
func TestParseErrorLineDivergences(t *testing.T) {
	for _, c := range []struct {
		in        string
		libyaml   string
		goccyLine string
	}{
		{"a: [1, 2\nb: 3\n", "yaml: line 1: did not find expected ',' or ']'", "yaml: line 2: "},
		{"a:\n  b: 1\n c: 2\n", "yaml: line 2: did not find expected key", "yaml: line 3: "},
		{"a: 'x\nb: 1\n", "yaml: line 3: found unexpected end of stream", "yaml: line 1: "},
		{"- a\nb: 1\n", "yaml: line 1: did not find expected '-' indicator", "yaml: line 2: "},
		{"a: b: c\n", "yaml: mapping values are not allowed in this context", "yaml: line 1: "},
		{"a: @x\n", "yaml: found character that cannot start any token", "yaml: line 1: "},
	} {
		_, err := yamlnode.Parse([]byte(c.in))
		if err == nil || !strings.HasPrefix(err.Error(), c.goccyLine) {
			t.Errorf("Parse(%q) = %v, want the prefix %q (libyaml says %q)", c.in, err, c.goccyLine, c.libyaml)
		}
	}
}

func TestParseKeepsDuplicateKeys(t *testing.T) {
	m := mustParse(t, "a: 1\na: 2\n")[0].Content[0]
	if len(m.Content) != 4 || m.Content[2].Value != "a" {
		t.Fatalf("duplicate keys were not both kept: %+v", m.Content)
	}
}

// TestParseRejectsInvalidUTF8 checks that bytes that are not UTF-8 fail
// with the message libyaml's reader gives, which has no line number.
func TestParseRejectsInvalidUTF8(t *testing.T) {
	for in, want := range map[string]string{
		"a: \xff\n":             "yaml: invalid leading UTF-8 octet",
		"# \xff\na: 1\n":        "yaml: invalid leading UTF-8 octet",
		"a: |\n  \xff\n":        "yaml: invalid leading UTF-8 octet",
		"a: [1, 2\n\xff":        "yaml: invalid leading UTF-8 octet",
		"a: \xc3\n":             "yaml: invalid trailing UTF-8 octet",
		"a: \xc3":               "yaml: incomplete UTF-8 octet sequence",
		"a: \xc0\x80\n":         "yaml: invalid length of a UTF-8 sequence",
		"a: \xed\xa0\x80\n":     "yaml: invalid Unicode character",
		"a: \xf4\x90\x80\x80\n": "yaml: invalid Unicode character",
	} {
		_, err := yamlnode.Parse([]byte(in))
		if err == nil || err.Error() != want {
			t.Errorf("Parse(%q) = %v, want %q", in, err, want)
		}
	}
	if docs := mustParse(t, "a: \xef\xbf\xbd\n"); docs[0].Content[0].Content[1].Value != "\ufffd" {
		t.Error("an encoded U+FFFD is valid UTF-8 and must parse")
	}
}

// TestParseTagHandlesLastOneDocument checks libyaml's %TAG scope. A
// handle applies only to the document that declares it, a named handle
// that nobody declared is an error reported one line above the node, and
// "%TAG !" changes the primary handle.
func TestParseTagHandlesLastOneDocument(t *testing.T) {
	for in, want := range map[string]string{
		"%TAG !e! tag:example.com,2000:\n---\na: !e!x 1\n---\nb: !e!y 2\n": "yaml: line 4: found undefined tag handle",
		"a: !e!x 1\n":         "yaml: found undefined tag handle",
		"b: 1\na: !e!x 1\n":   "yaml: line 1: found undefined tag handle",
		"a:\n  - &k !e!x 1\n": "yaml: line 1: found undefined tag handle",
	} {
		_, err := yamlnode.Parse([]byte(in))
		if err == nil || err.Error() != want {
			t.Errorf("Parse(%q) = %v, want %q", in, err, want)
		}
	}
	for in, want := range map[string][]string{
		"%TAG !e! tag:a,2000:\n---\na: !e!x 1\n...\n%TAG !e! tag:b,2000:\n---\nb: !e!y 2\n": {"tag:a,2000:x", "tag:b,2000:y"},
		"%TAG ! tag:example.com,2000:\n---\na: !foo 1\n":                                    {"tag:example.com,2000:foo"},
		"a: !foo 1\n---\nb: !!str 2\n":                                                      {"!foo", "!!str"},
	} {
		docs := mustParse(t, in)
		for i, tag := range want {
			if got := docs[i].Content[0].Content[1].Tag; got != tag {
				t.Errorf("Parse(%q) document %d tag = %q, want %q", in, i, got, tag)
			}
		}
	}
}

// TestParseAcceptanceDivergences pins the inputs where goccy and yaml.v3
// disagree about whether the input is YAML at all. They are accepted
// divergences, listed in docs/spruce/genesis-compat-contract.md.
func TestParseAcceptanceDivergences(t *testing.T) {
	for _, c := range []struct {
		in       string
		yamlv3   string // yaml.v3's outcome, for the record
		goccyErr string // the error Parse returns, or "" when it accepts the input
	}{
		{"%YAML 1.2\n---\na: 1\n", "yaml: found incompatible YAML document", ""},
		{"a: !!str\nb: 1\n", `accepted: a is !!str "", b is !!int 1`, "yaml: line 2: "},
		{"a: !!merge <<\n", `accepted: a is !!merge "<<"`, "yaml: line 1: "},
		{"%TAG !! tag:example.com,2000:\n---\na: !!foo 1\n", "accepted: a is tag:example.com,2000:foo", "yaml: line 3: "},
	} {
		_, err := yamlnode.Parse([]byte(c.in))
		switch {
		case c.goccyErr == "" && err != nil:
			t.Errorf("Parse(%q) = %v, want it accepted (yaml.v3: %s)", c.in, err, c.yamlv3)
		case c.goccyErr != "" && (err == nil || !strings.HasPrefix(err.Error(), c.goccyErr)):
			t.Errorf("Parse(%q) = %v, want the prefix %q (yaml.v3: %s)", c.in, err, c.goccyErr, c.yamlv3)
		}
	}
}

// TestParseAcceptsInputsYamlV3Rejects pins two kinds of input that goccy accepts
// and yaml.v3 rejects, which are accepted divergences listed in
// docs/user-guide/diffing.md. yaml.v3 rejects both, and Parse reads the
// \/ escape as a slash and keeps the control character in the value.
func TestParseAcceptsInputsYamlV3Rejects(t *testing.T) {
	for _, c := range []struct {
		name string
		in   string
		want string // the value Parse reads for key a
	}{
		{"slash escape", "a: \"x\\/y\"\n", "x/y"},
		{"control character in a quoted scalar", "a: \"x\x01y\"\n", "x\x01y"},
		{"control character in a plain scalar", "a: x\x01y\n", "x\x01y"},
	} {
		docs, err := yamlnode.Parse([]byte(c.in))
		if err != nil {
			t.Errorf("%s: Parse(%q) = %v, want it accepted", c.name, c.in, err)
			continue
		}
		if len(docs) != 1 || len(docs[0].Content) != 1 || len(docs[0].Content[0].Content) != 2 {
			t.Errorf("%s: Parse(%q) did not give one document holding one mapping entry", c.name, c.in)
			continue
		}
		if got := docs[0].Content[0].Content[1].Value; got != c.want {
			t.Errorf("%s: Parse(%q) read a as %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

// TestParseAcceptsContentAfterDocumentEnd pins another input yaml.v3
// rejects. Content after a "..." line without a new "---" starts a second
// document in goccy, and Parse keeps it.
func TestParseAcceptsContentAfterDocumentEnd(t *testing.T) {
	docs, err := yamlnode.Parse([]byte("a: 1\n...\nb: 2\n"))
	if err != nil {
		t.Fatalf("Parse: %v, want it accepted", err)
	}
	if len(docs) != 2 {
		t.Fatalf("Parse returned %d documents, want 2", len(docs))
	}
	if got, want := outline(docs[0]), `{!!str "a"@1: !!int "1"}`; got != want {
		t.Errorf("first document = %s, want %s", got, want)
	}
	if got, want := outline(docs[1]), `{!!str "b"@3: !!int "2"}`; got != want {
		t.Errorf("second document = %s, want %s", got, want)
	}
}

// outline renders a parsed document compactly: each scalar as its tag and
// quoted value, and each mapping key with its line, which is the line
// yaml.v3 parity covers.
func outline(n *yamlnode.Node) string {
	switch n.Kind {
	case yamlnode.DocumentNode:
		return outline(n.Content[0])
	case yamlnode.MappingNode:
		parts := make([]string, 0, len(n.Content)/2)
		for i := 0; i+1 < len(n.Content); i += 2 {
			parts = append(parts, fmt.Sprintf("%s@%d: %s", outline(n.Content[i]), n.Content[i].Line, outline(n.Content[i+1])))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case yamlnode.SequenceNode:
		parts := make([]string, 0, len(n.Content))
		for _, c := range n.Content {
			parts = append(parts, outline(c))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	default:
		return fmt.Sprintf("%s %q", n.Tag, n.Value)
	}
}

// checkOutlines parses each input and compares every document's outline
// with the one yaml.v3 v3.0.5 gives for the same input.
func checkOutlines(t *testing.T, cases []struct {
	in   string
	want []string
}) {
	t.Helper()
	for _, c := range cases {
		docs, err := yamlnode.Parse([]byte(c.in))
		if err != nil {
			t.Errorf("Parse(%q): %v", c.in, err)
			continue
		}
		got := make([]string, len(docs))
		for i, d := range docs {
			got[i] = outline(d)
		}
		if strings.Join(got, "\n") != strings.Join(c.want, "\n") {
			t.Errorf("Parse(%q) =\n  %q\nwant (yaml.v3)\n  %q", c.in, got, c.want)
		}
	}
}

// TestParseLoneCRLineBreaks checks that a lone CR is a line break, as it is
// in libyaml, so documents split and lines count the way yaml.v3 does.
// The expected outlines come from yaml.v3 v3.0.5.
func TestParseLoneCRLineBreaks(t *testing.T) {
	checkOutlines(t, []struct {
		in   string
		want []string
	}{
		{"a: 1\r\r\r---\r\rb: 2\n", []string{`{!!str "a"@1: !!int "1"}`, `{!!str "b"@6: !!int "2"}`}},
		{"a: 1\r---\rb: 2\r", []string{`{!!str "a"@1: !!int "1"}`, `{!!str "b"@3: !!int "2"}`}},
		{"a: 1\r\r---\rb: 2\n", []string{`{!!str "a"@1: !!int "1"}`, `{!!str "b"@4: !!int "2"}`}},
		{"a: |\r  x\r---\rb: 1\r", []string{`{!!str "a"@1: !!str "x\n"}`, `{!!str "b"@4: !!int "1"}`}},
		{"# c\r# d\r---\ra: 1\n", []string{`{!!str "a"@4: !!int "1"}`}},
		{"0\r\r...", []string{`!!int "0"`}},
		{"a: |\r  x\r\r  y\r", []string{`{!!str "a"@1: !!str "x\n\ny\n"}`}},
		{"a: \"x\r\r  y\"\rb: 2\r\n", []string{`{!!str "a"@1: !!str "x\ny", !!str "b"@4: !!int "2"}`}},
	})
}

// TestParseBlockScalarTrailingWhitespaceLines checks block scalars that
// end in whitespace-only lines. Whatever lies past the content
// indentation is content, so strip chomping keeps it, while a line no
// longer than the indentation is only a line break. The expected outlines
// come from yaml.v3 v3.0.5.
func TestParseBlockScalarTrailingWhitespaceLines(t *testing.T) {
	checkOutlines(t, []struct {
		in   string
		want []string
	}{
		// Strip chomping, literal.
		{"a: |-\n  x\n    \nb: 1", []string{`{!!str "a"@1: !!str "x\n  ", !!str "b"@4: !!int "1"}`}},
		{"a: |-\n  x\n   \n", []string{`{!!str "a"@1: !!str "x\n "}`}},
		{"a: |-\n  x  \n   \n", []string{`{!!str "a"@1: !!str "x  \n "}`}},
		{"a: |-\n  x\t \n", []string{`{!!str "a"@1: !!str "x\t "}`}},
		{"a: |-\n  x\n  \t \n", []string{`{!!str "a"@1: !!str "x\n\t "}`}},
		{"a: |-\n  x\n   \n    \n", []string{`{!!str "a"@1: !!str "x\n \n  "}`}},
		{"a: |-\n  x\n    \n  \n   \n", []string{`{!!str "a"@1: !!str "x\n  \n\n "}`}},
		{"a: |-\n  x\n\n   \nb: 1\n", []string{`{!!str "a"@1: !!str "x\n\n ", !!str "b"@5: !!int "1"}`}},
		{"a: |-\n  x\n   \n# c\nb: 1\n", []string{`{!!str "a"@1: !!str "x\n ", !!str "b"@5: !!int "1"}`}},
		{"a: |-\n  x\n   ", []string{`{!!str "a"@1: !!str "x\n "}`}},
		{"a: |2-\n  x\n   \n", []string{`{!!str "a"@1: !!str "x\n "}`}},
		{"a: |2-\n  x\n   \n\n", []string{`{!!str "a"@1: !!str "x\n "}`}},
		{"a: |2-\n  x\n   \n---\nb: 1\n", []string{`{!!str "a"@1: !!str "x\n "}`, `{!!str "b"@5: !!int "1"}`}},
		{"a: |1-\n   x\n  \nb: 1\n", []string{`{!!str "a"@1: !!str "  x\n ", !!str "b"@4: !!int "1"}`}},
		{"- |-\n  x\n   \n- y\n", []string{`[!!str "x\n ", !!str "y"]`}},
		{"- |1-\n   x\n  \n- 1\n", []string{`[!!str "  x\n ", !!int "1"]`}},
		{"a:\n  b: |-\n    x\n      \n  c: 1\n", []string{`{!!str "a"@1: {!!str "b"@2: !!str "x\n  ", !!str "c"@5: !!int "1"}}`}},
		{"- a: |2-\n      x\n       \n  c: 1\n", []string{`[{!!str "a"@1: !!str "  x\n   ", !!str "c"@4: !!int "1"}]`}},
		{"--- |-\n  x\n   \n", []string{`!!str "x\n "`}},
		{"a: |2-\n\n   \n", []string{`{!!str "a"@1: !!str "\n "}`}},
		{"a: |2-\n   \n   \n", []string{`{!!str "a"@1: !!str " \n "}`}},
		// Strip chomping, folded.
		{"a: >-\n  x\n    \nb: 1\n", []string{`{!!str "a"@1: !!str "x\n  ", !!str "b"@4: !!int "1"}`}},
		{"a: >-\n  x\n   y\n   \nb: 1\n", []string{`{!!str "a"@1: !!str "x\n y\n ", !!str "b"@5: !!int "1"}`}},
		{"a: >2-\n    x\n   y\n   \n", []string{`{!!str "a"@1: !!str "  x\n y\n "}`}},
		// Clip chomping.
		{"a: |\n  x\n    \nb: 1", []string{`{!!str "a"@1: !!str "x\n  \n", !!str "b"@4: !!int "1"}`}},
		{"a: |\n  x\n   \n", []string{`{!!str "a"@1: !!str "x\n \n"}`}},
		{"a: |\n  x\n   ", []string{`{!!str "a"@1: !!str "x\n "}`}},
		{"a: |2\n  x\n   \n", []string{`{!!str "a"@1: !!str "x\n \n"}`}},
		{"a: |2\n  x\n   \n \n", []string{`{!!str "a"@1: !!str "x\n \n"}`}},
		{"a: |2\n    x\n   \nb: 1\n", []string{`{!!str "a"@1: !!str "  x\n \n", !!str "b"@4: !!int "1"}`}},
		{"a: >\n  x\n    \nb: 1", []string{`{!!str "a"@1: !!str "x\n  \n", !!str "b"@4: !!int "1"}`}},
		{"a: >2\n  x\n   \n", []string{`{!!str "a"@1: !!str "x\n \n"}`}},
		// Keep chomping.
		{"a: |+\n  x\n    \nb: 1", []string{`{!!str "a"@1: !!str "x\n  \n", !!str "b"@4: !!int "1"}`}},
		{"a: |2+\n  x\n   \n", []string{`{!!str "a"@1: !!str "x\n \n"}`}},
		{"a: >+\n  x\n   \n", []string{`{!!str "a"@1: !!str "x\n \n"}`}},
	})
}

// TestParseRecoversParserPanic checks that a panic inside goccy becomes a
// parse error. goccy v1.19.2 dereferences nil on this input.
func TestParseRecoversParserPanic(t *testing.T) {
	docs, err := yamlnode.Parse([]byte("%TAG !! 0\n--- !\n"))
	if err == nil || !strings.HasPrefix(err.Error(), "yaml: ") || docs != nil {
		t.Fatalf("Parse = %v, %v; want no documents and a yaml: error", docs, err)
	}
}

// FuzzParse checks that no input makes Parse panic.
func FuzzParse(f *testing.F) {
	for _, seed := range []string{
		"a: 1\r\r\r---\r\rb: 2\n",
		"0\r\r...",
		"%TAG !! 0\n--- !\n",
		"a: |-\n  x\n    \nb: 1",
		"a: |2-\n  x\n   \n---\nb: 1\n",
		"- a: |2-\n      x\n       \n  c: 1\n",
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = yamlnode.Parse(data)
	})
}

// BenchmarkParseManyDocuments parses a stream of 5,000 small documents.
// Parse time must grow linearly with the number of documents. It grew
// with the square of it while each document was parsed behind padding
// for every line before it.
func BenchmarkParseManyDocuments(b *testing.B) {
	var sb strings.Builder
	for i := 0; i < 5000; i++ {
		fmt.Fprintf(&sb, "---\n# document %d\nkind: ConfigMap\nmetadata:\n  name: cm-%d # name\ndata:\n  key: |\n    value\n", i, i)
	}
	src := []byte(sb.String())
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		docs, err := yamlnode.Parse(src)
		if err != nil || len(docs) != 5000 {
			b.Fatalf("Parse = %d documents, %v; want 5000", len(docs), err)
		}
	}
}
