package yamlout

import (
	"bytes"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/query/load"
	"github.com/fivetwenty-io/graft/pkg/decimal"
	"github.com/fivetwenty-io/graft/pkg/graft"
	"github.com/fivetwenty-io/graft/pkg/query/value"
	"github.com/fivetwenty-io/graft/pkg/sortkeys"
)

func TestBlockStyleInInputOrder(t *testing.T) {
	v := sample()
	wantEqual(t, emitOne(t, v, Options{Indent: 2}), "b: 1\na:\n  \"10\": 1\n  \"2\": 2\n  x:\n    - 1\n    - y: null\ne: []\no: {}\n")
	wantEqual(t, emitOne(t, v, Options{Indent: 4}), "b: 1\na:\n    \"10\": 1\n    \"2\": 2\n    x:\n        - 1\n        - y: null\ne: []\no: {}\n")
	wantEqual(t, emitOne(t, v, Options{Indent: 1}), emitOne(t, v, Options{Indent: 2}))
	wantEqual(t, emitOne(t, v, Options{Indent: 12}), emitOne(t, v, Options{Indent: 9}))
	wantEqual(t, emitOne(t, obj(pair("a", obj(pair("b", lit("1"))))), Options{Indent: 9}), "a:\n         b: 1\n")
	wantEqual(t, emitOne(t, v, Options{}), emitOne(t, v, Options{Indent: 2}))
}

func TestNestedCollections(t *testing.T) {
	v := obj(
		pair("a", arr(lit("1"), arr(lit("2"), lit("3")), obj(pair("y", value.Null()), pair("z", arr(lit("4")))))),
		pair("s", value.Str("line\nbreak")),
		pair("t", arr(value.Str("a: b"), obj(pair("k", value.Str("  x\n"))))),
	)
	want := "a:\n  - 1\n  - - 2\n    - 3\n  - y: null\n    z:\n      - 4\ns: |-\n  line\n  break\nt:\n  - 'a: b'\n  - k: |2\n        x\n"
	wantEqual(t, emitOne(t, v, Options{Indent: 2}), want)
	wantEqual(t, emitOne(t, arr(value.Str("a\nb"), arr(value.Str("x\ny"))), Options{Indent: 4}), "- |-\n  a\n  b\n- - |-\n    x\n    y\n")
	wantEqual(t, emitOne(t, arr(), Options{}), "[]\n")
	wantEqual(t, emitOne(t, obj(), Options{}), "{}\n")
}

func TestScalarStyleMatchesMarshalYAML(t *testing.T) {
	corpus := []string{"1", "yes", "x: y", "plain", "", "null", "~", "1.0", "0x10", "- a", "a #b", "#c", "@x", "`x`", "%p", "!t", "&a", "*a", "'q'", "\"d\"", "line\nbreak", " lead", "trail ", "true", "True", "0o7", "1_000", ".inf", "2001-12-14", "a: ", ": b", "[x]", "{y}", "?", "|", ">", "\t", "é", " ", "._1", "0x_1", "1_0", "_1", "-1_000", "+1_0", "1__0", "0b1_0", "1.5_0", ".5_0", "1e1_0"}
	for _, s := range corpus {
		m := map[string]interface{}{"k": s}
		want, err := graft.MarshalYAML(m)
		if err != nil {
			t.Fatal(err)
		}
		got := emitOne(t, obj(pair("k", value.Str(s))), Options{Indent: 2})
		if got != string(want) {
			t.Errorf("%q: got %q, want %q", s, got, want)
		}
	}
	wantEqual(t, scalarStyle("x: y", scalarContext{}), StyleSingleQuoted)
	wantEqual(t, scalarStyle("yes", scalarContext{}), StyleDoubleQuoted)
	wantEqual(t, scalarStyle("plain", scalarContext{}), StylePlain)
	wantEqual(t, scalarStyle("a, b", scalarContext{Flow: true}), StyleDoubleQuoted)
	wantEqual(t, scalarStyle("a, b", scalarContext{}), StylePlain)
	wantEqual(t, emitOne(t, arr(lit("1.0"), lit("100000000000000000000"), value.Bool(true), value.Null()), Options{Indent: 2}), "- 1.0\n- 100000000000000000000\n- true\n- null\n")
}

func TestDocumentsSortingAndUnwrap(t *testing.T) {
	two := []*value.Value{obj(pair("a", lit("1"))), obj(pair("b", lit("2")))}
	wantEqual(t, emitAll(t, two, Options{Indent: 2}), "a: 1\n---\nb: 2\n")
	wantEqual(t, emitAll(t, two, Options{Indent: 2, NoDoc: true}), "a: 1\nb: 2\n")
	wantEqual(t, emitOne(t, obj(pair("b", lit("1")), pair("a", lit("2"))), Options{Indent: 2, SortKeys: sortkeys.Lexical}), "a: 2\nb: 1\n")
	wantEqual(t, emitOne(t, value.Str("x"), Options{UnwrapScalar: true}), "x\n")
	wantEqual(t, emitOne(t, value.Str("x"), Options{}), "\"x\"\n")
}

func TestSpecialNumbers(t *testing.T) {
	v := arr(value.NumDec(decimal.NaN()), value.NumDec(decimal.Inf(1)), value.NumDec(decimal.Inf(-1)), dec("-0"), value.NumText("1e1000000000"), dec("0.5"))
	wantEqual(t, emitOne(t, v, Options{}), "- .nan\n- .inf\n- -.inf\n- -0\n- 1e1000000000\n- 0.5\n")
}

func TestKeysThatNeedQuotes(t *testing.T) {
	v := obj(pair("", lit("1")), pair("a: b", lit("2")), pair("true", lit("3")), pair("k\nl", lit("4")))
	wantEqual(t, emitOne(t, v, Options{}), "\"\": 1\n'a: b': 2\n\"true\": 3\n\"k\\nl\": 4\n")
}

// "<<" departs from MarshalYAML on purpose: a plain << key reads back as a
// YAML merge key in graft's own loader, so the emitter quotes it.
func TestMergeKeyIsQuoted(t *testing.T) {
	wantEqual(t, emitOne(t, obj(pair("<<", lit("1"))), Options{}), "\"<<\": 1\n")
	wantEqual(t, scalarStyle("<<", scalarContext{}), StyleDoubleQuoted)
}

// These three depart from MarshalYAML on purpose, which writes bytes that
// are not valid YAML for them. The emitter writes escapes that read back
// as the same string.
func TestDeliberateEscapes(t *testing.T) {
	wantEqual(t, emitOne(t, obj(pair("k", value.Str("a\xffb"))), Options{}), "k: \"a\\xFFb\"\n")
	wantEqual(t, emitOne(t, obj(pair("k", value.Str("a\u2028b"))), Options{}), "k: \"a\\Lb\"\n")
	wantEqual(t, emitOne(t, obj(pair("k", value.Str("\ufeffa"))), Options{}), "k: \"\\uFEFFa\"\n")
}

func TestRoundTripPreservesEverything(t *testing.T) {
	for _, f := range globTestdata(t, "roundtrip/*.yml") {
		docs := mustLoadFile(t, f, load.LoadOptions{KeepMeta: true})
		got := emitDocs(t, docs, Options{Indent: 2})
		if want := mustReadFile(t, f); got != want {
			t.Errorf("%s: got\n%s\nwant\n%s", f, got, want)
		}
	}
}

// A new string takes the Task 11 rules, which quote what reads back as
// another type and single-quote what plain style cannot carry.
func TestNewScalarsUseLibyamlRules(t *testing.T) {
	v := obj(pair("a", value.Str("1")), pair("b", value.Str("yes")), pair("c", value.Str("x: y")))
	wantEqual(t, emitOne(t, v, Options{Indent: 2}), "a: \"1\"\nb: \"yes\"\nc: 'x: y'\n")
}

func TestIndentClampNoDocAndUnwrap(t *testing.T) {
	v := mustLoadOne(t, "a:\n  - 1\n", load.LoadOptions{KeepMeta: true})
	wantEqual(t, emitOne(t, v, Options{Indent: 4}), "a:\n    - 1\n")
	wantEqual(t, emitOne(t, v, Options{Indent: 1}), "a:\n  - 1\n")
	wantEqual(t, emitOne(t, value.Str("x"), Options{UnwrapScalar: true}), "x\n")
	wantEqual(t, emitOne(t, value.Str("x"), Options{}), "\"x\"\n")
}

func TestRoundTripInline(t *testing.T) {
	for _, src := range []string{
		"a: |-\n\n  x\n",
		"a: |2\n   y\n  z\n",
		"a: [1, {b: c}, []]\n",
		"a: !!binary aGk=\n",
		"a: !<tag:example.com,2000:x> 1\n",
		"- 'two\n\n  lines'\n",
		"a: |\n  x\nb: 1\n",
		"h: 2001-12-14\nn: yes\n",
	} {
		docs := mustLoad(t, "inline", src, load.LoadOptions{KeepMeta: true})
		wantEqual(t, emitDocs(t, docs, Options{Indent: 2}), src)
	}
	aliasKey := value.ObjEntries(value.Entry{Key: meta(value.Null(), value.Meta{AliasOf: "x"}), Value: lit("1")})
	wantEqual(t, emitOne(t, aliasKey, Options{}), "*x : 1\n")
	seqKey := value.ObjEntries(value.Entry{Key: meta(arr(value.Str("a"), value.Str("b")), value.Meta{Style: value.Flow}), Value: lit("1")})
	wantEqual(t, emitOne(t, seqKey, Options{}), "? [a, b]\n: 1\n")
}

// The separator logic copies yq's printer: a result from another document
// gets "---" unless its leading content opens with one, the file index it
// compares against is the first result's, whatever came after, and the
// first result keeps a "---" of its leading content only in a file's first
// document.
func TestDocumentSeparators(t *testing.T) {
	src := "# lead\n---\na: 1\n---\n# two\na: 2\n"
	docs := mustLoad(t, "inline", src, load.LoadOptions{KeepMeta: true})
	wantEqual(t, emitDocs(t, docs, Options{}), src)
	wantEqual(t, emitDocs(t, docs, Options{NoDoc: true}), "# lead\na: 1\n# two\na: 2\n")
	// A later document printed first drops the "---" that parts it from
	// the documents before it.
	wantEqual(t, emitDocs(t, docs[1:], Options{}), "# two\na: 2\n")

	at := func(file, doc int, v *value.Value) load.Document {
		return load.Document{FileIndex: file, DocIndex: doc, Value: v}
	}
	results := []load.Document{
		at(0, 0, obj(pair("x", lit("1")))),
		at(0, 0, obj(pair("y", lit("2")))),
		at(1, 0, obj(pair("x", lit("3")))),
		at(1, 0, obj(pair("y", lit("4")))),
	}
	wantEqual(t, emitDocs(t, results, Options{}), "x: 1\ny: 2\n---\nx: 3\n---\ny: 4\n")
	wantEqual(t, emitDocs(t, results, Options{NoDoc: true}), "x: 1\ny: 2\nx: 3\ny: 4\n")
	wantEqual(t, emitDocs(t, []load.Document{at(0, 0, lit("1")), at(0, 1, lit("2"))}, Options{NUL: true}), "1\x00---\n2\x00")
	wantEqual(t, emitAll(t, []*value.Value{lit("1"), lit("2")}, Options{NUL: true}), "1\x00---\n2\x00")
}

func TestNULInsideAValueIsAnError(t *testing.T) {
	var buf bytes.Buffer
	e := New(&buf, Options{NUL: true, UnwrapScalar: true})
	err := e.Write(value.Str("a\x00b"))
	if err == nil || !strings.Contains(err.Error(), "contains NUL char") {
		t.Fatalf("got %v, want the NUL error", err)
	}
}

func entry(k string, v *value.Value) value.Entry { return value.Entry{Key: value.Str(k), Value: v} }

func meta(v *value.Value, m value.Meta) *value.Value { return v.WithMeta(&m) }

// The expected text in these comes from yq 4.54.1 after setting the same
// comments with its comment operators.
func TestCommentPlacement(t *testing.T) {
	cases := []struct {
		v    *value.Value
		want string
	}{
		{value.ObjEntries(entry("a", withComments(obj(pair("b", lit("1"))), "", "L", "")), entry("c", lit("2"))), "a:\n  b: 1\nc: 2 # L\n"},
		{value.ObjEntries(entry("a", withComments(arr(lit("1")), "", "", "F")), entry("b", lit("2"))), "a:\n  - 1\nb: 2\n# F\n"},
		{value.ObjEntries(entry("x", withComments(lit("3"), "H", "", "")), entry("y", lit("4"))), "x: 3\n# H\ny: 4\n"},
		{value.ObjEntries(value.Entry{Key: withComments(value.Str("x"), "K", "", ""), Value: lit("3")}, entry("y", withComments(lit("4"), "", "", "F2"))), "# K\nx: 3\ny: 4\n# F2\n"},
		{value.ObjEntries(entry("a", arr(withComments(lit("1"), "", "", "F"), withComments(lit("2"), "H", "L", ""))), entry("b", lit("2"))), "a:\n  - 1\n  # F\n\n  # H\n  - 2 # L\nb: 2\n"},
		{withComments(obj(pair("a", lit("1"))), "top", "", "bottom"), "# top\na: 1\n# bottom\n"},
		{obj(pair("a", meta(arr(withComments(lit("1"), "", "L", ""), lit("2")), value.Meta{Style: value.Flow}))), "a: [1, # L\n  2]\n"},
		{value.ObjEntries(value.Entry{Key: withComments(value.Str("a"), "", "KL", ""), Value: lit("1")}, entry("b", lit("2"))), "a: 1 # KL\nb: 2\n"},
		{value.ObjEntries(value.Entry{Key: withComments(value.Str("a"), "", "KL", ""), Value: obj(pair("x", lit("1")))}, entry("b", lit("2"))), "a: # KL\n  x: 1\nb: 2\n"},
		{value.ObjEntries(value.Entry{Key: withComments(value.Str("a"), "", "", "KF"), Value: lit("1")}, entry("b", lit("2"))), "a: 1\n# KF\n\nb: 2\n"},
		{obj(pair("a", obj(pair("b", lit("1")))), pair("c", withComments(value.Str("x\n"), "", "LC", ""))), "a:\n  b: 1\nc: | # LC\n  x\n"},
	}
	for _, c := range cases {
		wantEqual(t, emitOne(t, c.v, Options{}), c.want)
	}
}

func TestCompactSequenceIndent(t *testing.T) {
	v := mustLoadOne(t, "a:\n  - 1\n  - b:\n      - 2\n    c: [x]\n  - - 3\n", load.LoadOptions{KeepMeta: true})
	wantEqual(t, emitOne(t, v, Options{CompactSeqIndent: true}), "a:\n- 1\n- b:\n  - 2\n  c: [x]\n- - 3\n")
	wantEqual(t, emitOne(t, v, Options{CompactSeqIndent: true, Indent: 4}), "a:\n  - 1\n  - b:\n      - 2\n    c: [x]\n  - - 3\n")
	root := mustLoadOne(t, "- a:\n    - 1\n", load.LoadOptions{KeepMeta: true})
	wantEqual(t, emitOne(t, root, Options{CompactSeqIndent: true}), "- a:\n  - 1\n")
	wantEqual(t, emitOne(t, sample(), Options{CompactSeqIndent: true}), "b: 1\na:\n  \"10\": 1\n  \"2\": 2\n  x:\n  - 1\n  - y: null\ne: []\no: {}\n")
}

// Nested levels align to multiples of the indent, the way yq lays them
// out, rather than adding the indent to the column a sequence item opened.
func TestIndentAlignsToMultiples(t *testing.T) {
	v := mustLoadOne(t, "a:\n  b:\n    c: 1\n  d:\n    - e: |\n        lit\n", load.LoadOptions{KeepMeta: true})
	wantEqual(t, emitOne(t, v, Options{Indent: 3}), "a:\n   b:\n      c: 1\n   d:\n      - e: |\n         lit\n")
	wantEqual(t, emitOne(t, arr(obj(pair("a", obj(pair("b", lit("1")))))), Options{Indent: 4}), "- a:\n    b: 1\n")
}

func TestUnwrapAndRootScalarsWithMeta(t *testing.T) {
	v := mustLoadOne(t, "a: 0x10\nb: ~\nc: \"q\" # lc\nd: &x 5\ne: !!str 7\nf: hello # L\n", load.LoadOptions{KeepMeta: true})
	field := func(k string) *value.Value {
		x, ok := v.Get(k)
		if !ok {
			t.Fatalf("no %s", k)
		}
		return x
	}
	unwrapped := []*value.Value{field("a"), field("b"), field("c"), field("d")}
	wantEqual(t, emitAll(t, unwrapped, Options{UnwrapScalar: true, NoDoc: true}), "0x10\n~\nq\n5\n")
	wrapped := []*value.Value{field("c"), field("d"), field("e"), field("f")}
	wantEqual(t, emitAll(t, wrapped, Options{NoDoc: true}), "\"q\" # lc\n&x 5\n!!str 7\nhello # L\n")
	crlf := load.Document{LeadingContent: "# c\r\n", Value: field("a")}
	wantEqual(t, emitDocs(t, []load.Document{crlf}, Options{UnwrapScalar: true}), "# c\r\n0x10\r\n")
}
