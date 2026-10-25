package yamlout

import (
	"testing"

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
	wantEqual(t, emitOne(t, v, Options{Indent: 12}), emitOne(t, v, Options{Indent: 7}))
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
