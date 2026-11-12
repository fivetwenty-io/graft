package yaml

import (
	"errors"
	"strings"
	"testing"
)

func mustParse(t *testing.T, src string) *Document {
	t.Helper()
	d, err := ParseDocument([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return d
}

const editableSrc = "plain: a\nquoted: \"b\"\nsingle: 'c'\nlit: |\n  x\nfold: >\n  y\nmap: {k: v}\nlist: [1, 2]\nanch: &x v\nali: *x\ntagged: !!str 5\nnested:\n  deep: z\nmulti: \"one\n  two\"\nempty:\n"

func TestDocumentEditable(t *testing.T) {
	d := mustParse(t, editableSrc)
	for _, p := range []string{"plain", "quoted", "single", "lit", "fold", "map.k", "list.1", "nested.deep"} {
		if err := d.Editable(p); err != nil {
			t.Errorf("Editable(%q) = %v, want nil", p, err)
		}
	}
	for p, reason := range map[string]string{
		"map":    "the value is a collection",
		"anch":   "the value is anchored",
		"ali":    "the value is an alias",
		"tagged": "the value has a tag",
		"multi":  "the value is a multi-line flow scalar",
		"empty":  "the value has no text to replace",
	} {
		err := d.Editable(p)
		var de *DocumentError
		if !errors.Is(err, ErrUnsupportedEdit) || !errors.As(err, &de) || de.Reason != reason || de.Line == 0 {
			t.Errorf("Editable(%q) = %v, want %q with a line", p, err, reason)
		}
	}
	if err := d.Editable("missing"); !errors.Is(err, ErrPathNotFound) {
		t.Errorf("Editable(missing) = %v", err)
	}
	if err := mustParse(t, "a: 1\na: 2\n").Editable("a"); !errors.Is(err, ErrAmbiguousPath) {
		t.Errorf("Editable of a duplicate key = %v", err)
	}
}

func TestDocumentEditableWholeInputRefusals(t *testing.T) {
	for src, reason := range map[string]string{
		"a: 1\r\n":              "the input holds a carriage return",
		"\ufeffa: 1\n":          "the input starts with a byte order mark",
		"a:\t1\n":               "a tab sits on a line the edit touches",
		"b: &r {a: 1}\nc: *r\n": "the value is anchored",
	} {
		path := "a"
		if strings.HasPrefix(src, "b:") {
			path = "b.a"
		}
		var de *DocumentError
		if err := mustParse(t, src).Editable(path); !errors.As(err, &de) || de.Reason != reason {
			t.Errorf("%q: Editable(%s) = %v, want %q", src, path, err, reason)
		}
	}
	if err := mustParse(t, "b:\t2\na: 1\n").Editable("a"); err != nil {
		t.Errorf("a tab on another line: %v", err)
	}
}

func TestDocumentEditableRefusesAPathThroughAnAlias(t *testing.T) {
	d := mustParse(t, "base: &b\n  k: v\nuse: *b\n")
	var de *DocumentError
	if err := d.Editable("use.k"); !errors.Is(err, ErrUnsupportedEdit) || !errors.As(err, &de) ||
		de.Reason != "the path passes through an alias" || de.Line != 2 {
		t.Errorf("Editable(use.k) = %v, want the alias refusal at the value's line", err)
	}
	if err := d.Editable("base.k"); !errors.As(err, &de) || de.Reason != "the value is anchored" {
		t.Errorf("Editable(base.k) = %v, want the anchor refusal", err)
	}
}

func TestDocumentEditableRefusesOtherTags(t *testing.T) {
	for _, src := range []string{"a: ! 5\n", "a: !local x\n"} {
		var de *DocumentError
		if err := mustParse(t, src).Editable("a"); !errors.As(err, &de) || de.Reason != "the value has a tag" {
			t.Errorf("%q: Editable(a) = %v, want the tag refusal", src, err)
		}
	}
}

func TestDocumentEditableTakesQuotedEmptyAndGraftOperators(t *testing.T) {
	d := mustParse(t, "a: ''\nb: \"\"\nc: (( grab a ))\nd: ~\n")
	for _, p := range []string{"a", "b", "c", "d"} {
		if err := d.Editable(p); err != nil {
			t.Errorf("Editable(%q) = %v, want nil", p, err)
		}
	}
}

func TestDocumentEditableRefusesAUTF16Input(t *testing.T) {
	src := []byte{0xFF, 0xFE, 'a', 0, ':', 0, ' ', 0, '1', 0, '\n', 0}
	d, err := ParseDocument(src)
	if err != nil {
		t.Fatal(err)
	}
	var de *DocumentError
	if err := d.Editable("a"); !errors.As(err, &de) || de.Reason != "the input starts with a byte order mark" {
		t.Errorf("Editable(a) = %v, want the byte order mark refusal", err)
	}
}

func TestScalarSpan(t *testing.T) {
	src := "a: web   # c\nb: \"q x\"\nc: 'it''s'\nl: [1, two]\nné: ü\n" +
		"e: \"x\\u0001y\\\"\" # \"\nf: '' # 'g'\n"
	d := mustParse(t, src)
	for path, want := range map[string]string{
		"a": "web", "b": `"q x"`, "c": `'it''s'`, "l.1": "two", "né": "ü",
		"e": `"x\u0001y\""`, "f": `''`,
	} {
		loc, err := d.resolve("edit", path)
		if err != nil {
			t.Fatal(err)
		}
		sp, err := d.scalarSpan(loc.node)
		if err != nil || src[sp.start:sp.end] != want {
			t.Errorf("span of %s = %q, %v; want %q", path, src[sp.start:sp.end], err, want)
		}
	}
}

// TestScalarSpanDistrustsTabsAndAByteOrderMark covers the lines where
// goccy's column is not the column in the input: after a tab between
// tokens, and on the first line of an input that starts with a byte
// order mark.
func TestScalarSpanDistrustsTabsAndAByteOrderMark(t *testing.T) {
	for _, c := range []struct{ src, path string }{
		{"x: [y,\ty, y]\n", "x.1"},
		{"x: [y,\ty, y]\n", "x.2"},
		{"a:\t1\n", "a"},
		{"\ufeffa: 1\n", "a"},
	} {
		src, path := c.src, c.path
		d := mustParse(t, src)
		loc, err := d.resolve("edit", path)
		if err != nil {
			t.Fatal(err)
		}
		if sp, err := d.scalarSpan(loc.node); err == nil {
			t.Errorf("%q: span of %s = %q, want a refusal", src, path, src[sp.start:sp.end])
		}
	}
	d := mustParse(t, "\ufeffa: 1\nb: 2\n")
	loc, err := d.resolve("edit", "b")
	if err != nil {
		t.Fatal(err)
	}
	if sp, err := d.scalarSpan(loc.node); err != nil || string(d.src[sp.start:sp.end]) != "2" {
		t.Errorf("span of b after a byte order mark = %v, %v; want the 2 on line 2", sp, err)
	}
}

func TestScalarSpanRefusesWhatItCannotPlace(t *testing.T) {
	d := mustParse(t, "a: x\n")
	loc, err := d.resolve("edit", "a")
	if err != nil {
		t.Fatal(err)
	}
	moved := *loc.node
	moved.TokenColumn = 1
	if _, err := d.scalarSpan(&moved); err == nil {
		t.Error("a node placed on its key's token got a span")
	}
	other := *loc.node
	other.Value = "y"
	if _, err := d.scalarSpan(&other); err == nil {
		t.Error("a node whose text reads as another value got a span")
	}
}

// TestScalarSpanCountsLinesAsTheParserDoes checks a scalar after a U+0085
// line break, which goccy's lexer reads as an ordinary character but the
// parser reads as a line break.
func TestScalarSpanCountsLinesAsTheParserDoes(t *testing.T) {
	src := "a: \"x y\u0085z\"\nb: web\n"
	d := mustParse(t, src)
	loc, err := d.resolve("edit", "b")
	if err != nil {
		t.Fatal(err)
	}
	if sp, err := d.scalarSpan(loc.node); err != nil || src[sp.start:sp.end] != "web" {
		t.Errorf("span of b = %v, %v; want web", sp, err)
	}
}

// TestScalarSpanRefusesARewrittenLine checks lines the parser reads only
// after yamlprep rewrote them. Quoting {{a}} moves every later column on
// its line by two, which here would land on the other b.
func TestScalarSpanRefusesARewrittenLine(t *testing.T) {
	d := mustParse(t, "l: [{{a}}, b,b]\nm: {{c}}\n")
	for _, path := range []string{"l.0", "l.1", "l.2", "m"} {
		loc, err := d.resolve("edit", path)
		if err != nil {
			t.Fatal(err)
		}
		if sp, err := d.scalarSpan(loc.node); !errors.Is(err, errInexact) {
			t.Errorf("span of %s = %v, %v; want a refusal", path, sp, err)
		}
	}
	var de *DocumentError
	if err := d.Editable("l.1"); !errors.As(err, &de) || de.Reason != "the value's source position is not exact" {
		t.Errorf("Editable(l.1) = %v", err)
	}
}

func TestScalarSpanRefusesAQuotedScalarThatRunsOn(t *testing.T) {
	for _, src := range []string{"a: 'one\n  two'\n", "a: \"one\\\n  two\"\n", "a: \"one \\\"\n  two\"\n"} {
		d := mustParse(t, src)
		loc, err := d.resolve("edit", "a")
		if err != nil {
			t.Fatal(err)
		}
		if sp, err := d.scalarSpan(loc.node); !errors.Is(err, errMultiLine) {
			t.Errorf("%q: span = %v, %v; want the multi-line refusal", src, sp, err)
		}
	}
}

func TestParseBlockHeader(t *testing.T) {
	for text, want := range map[string]blockHeader{
		"|":        {},
		">":        {folded: true},
		"|-":       {chomp: '-'},
		">+":       {folded: true, chomp: '+'},
		"|2":       {explicit: true},
		"|-2":      {chomp: '-', explicit: true},
		">1+":      {folded: true, chomp: '+', explicit: true},
		"| # note": {},
		"|-\t# c":  {chomp: '-'},
	} {
		if got, ok := parseBlockHeader(text); !ok || got != want {
			t.Errorf("parseBlockHeader(%q) = %+v, %v, want %+v", text, got, ok, want)
		}
	}
	for _, text := range []string{"", "x", "|x", "|#", "|--", "|22", "|0", "|+-", "| x"} {
		if _, ok := parseBlockHeader(text); ok {
			t.Errorf("parseBlockHeader(%q) accepted", text)
		}
	}
}
