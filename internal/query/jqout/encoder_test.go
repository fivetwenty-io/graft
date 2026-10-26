package jqout

import (
	"testing"

	"github.com/fivetwenty-io/graft/pkg/query/value"
	"github.com/fivetwenty-io/graft/pkg/sortkeys"
)

func sample() *value.Value {
	return obj(
		pair("a", value.Arr(lit("1"), obj(pair("b", value.Null())))),
		pair("c", value.Str("x")),
	)
}

func TestPrettyIndentTabCompact(t *testing.T) {
	v := sample()
	wantEqual(t, encode(t, v, Options{Indent: 2}), "{\n  \"a\": [\n    1,\n    {\n      \"b\": null\n    }\n  ],\n  \"c\": \"x\"\n}\n")
	wantEqual(t, encode(t, v, Options{Compact: true}), `{"a":[1,{"b":null}],"c":"x"}`+"\n")
	wantEqual(t, encode(t, v, Options{Tab: true}), "{\n\t\"a\": [\n\t\t1,\n\t\t{\n\t\t\t\"b\": null\n\t\t}\n\t],\n\t\"c\": \"x\"\n}\n")
	wantEqual(t, encode(t, value.Arr(), Options{Indent: 2}), "[]\n")
	wantEqual(t, encode(t, obj(), Options{Indent: 2}), "{}\n")
	// An explicit indent of 0 keeps the newlines and the space after the colon, as in jq.
	wantEqual(t, encode(t, obj(pair("a", value.Arr(lit("1"), lit("2")))), Options{}), "{\n\"a\": [\n1,\n2\n]\n}\n")
	wantEqual(t, encode(t, value.Arr(), Options{}), "[]\n")
}

func TestRawJoinNULAndASCII(t *testing.T) {
	wantEqual(t, encode(t, value.Str("a\u00e9"), Options{Raw: true}), "aé\n")
	wantEqual(t, encode(t, value.Str("x"), Options{Join: true}), "x")
	wantEqual(t, encode(t, value.Str("x"), Options{RawNUL: true}), "x\x00")
	_, err := encodeErr(value.Str("a\x00b"), Options{RawNUL: true})
	wantErrContains(t, err, "Cannot dump a string containing NUL with --raw-output0 option")
	wantEqual(t, encode(t, value.Str("é\u007f"), Options{ASCII: true, Compact: true}), `"\u00e9\u007f"`+"\n")
	wantEqual(t, encode(t, value.Str("😀"), Options{ASCII: true, Compact: true}), `"\ud83d\ude00"`+"\n")
	wantEqual(t, encode(t, value.Str("/\u2028"), Options{Compact: true}), "\"/\u2028\"\n")
	wantEqual(t, encode(t, value.Arr(value.Str("s"), lit("1")), Options{Raw: true, Compact: true}), "[\"s\",1]\n") // only a top-level string prints raw
}

func TestEscapesSeqAndControlChars(t *testing.T) {
	wantEqual(t, encode(t, value.Str("\u007f\b\f\n\r\t\x01"), Options{Compact: true}), `"\u007f\b\f\n\r\t\u0001"`+"\n") // short escapes only for these five, pinned jq 1.8.2
	wantEqual(t, encode(t, value.Str("\"\\\xff"), Options{Compact: true}), `"\"\\`+"\ufffd"+`"`+"\n")
	wantEqual(t, encode(t, value.Str("x"), Options{Seq: true, Compact: true}), "\x1e\"x\"\n")
	wantEqual(t, encode(t, value.Str("x"), Options{Seq: true, Raw: true}), "x\n") // no RS before raw strings
}

func TestSortKeysRecursive(t *testing.T) {
	v := obj(pair("b", obj(pair("d", lit("1")), pair("c", lit("2")))), pair("a", lit("0")))
	wantEqual(t, encode(t, v, Options{Compact: true, SortKeys: sortkeys.Lexical}), `{"a":0,"b":{"c":2,"d":1}}`+"\n")
	wantEqual(t, encode(t, v, Options{Compact: true, SortKeys: sortkeys.None}), `{"b":{"d":1,"c":2},"a":0}`+"\n")
	w := obj(pair("b", lit("0")), pair("10", lit("0")), pair("2", lit("0")))
	wantEqual(t, encode(t, w, Options{Compact: true, SortKeys: sortkeys.Spruce}), `{"2":0,"10":0,"b":0}`+"\n")
}

func TestFloatNumbersOption(t *testing.T) {
	wantEqual(t, encode(t, value.Num(1e16), Options{Compact: true, FloatNumbers: true}), "1e+16\n")
	wantEqual(t, encode(t, value.Num(1e16), Options{Compact: true}), "10000000000000000\n")
}

func TestUnbufferedFlushesEachValue(t *testing.T) {
	var sink flushCounter
	e := New(&sink, Options{Compact: true, Unbuffered: true})
	if err := e.Write(lit("1")); err != nil {
		t.Fatal(err)
	}
	wantEqual(t, sink.s, "1\n")
}

type flushCounter struct{ s string }

func (f *flushCounter) Write(p []byte) (int, error) { f.s += string(p); return len(p), nil }

// The colored forms below were taken from jq 1.8.2 with -C.
func TestColorDefaultAndJQCOLORS(t *testing.T) {
	c := Options{Compact: true, Color: true, Palette: DefaultPalette}
	wantEqual(t, encode(t, obj(pair("a", value.Null())), c),
		"\x1b[1;39m{\x1b[0m\x1b[1;34m\"a\"\x1b[0m\x1b[1;39m:\x1b[0m\x1b[0;90mnull\x1b[0m\x1b[1;39m}\x1b[0m\n")
	arr := value.Arr(lit("1"), value.Str("x"), value.Bool(true), value.Arr(), obj())
	wantEqual(t, encode(t, arr, c),
		"\x1b[1;39m[\x1b[0m\x1b[0;39m1\x1b[0m\x1b[1;39m,\x1b[0m\x1b[0;32m\"x\"\x1b[0m\x1b[1;39m,\x1b[0m"+
			"\x1b[0;39mtrue\x1b[0m\x1b[1;39m,\x1b[0m\x1b[1;39m[]\x1b[0m\x1b[1;39m,\x1b[0m\x1b[1;39m{}\x1b[0m\x1b[1;39m]\x1b[0m\n")
	pretty := Options{Indent: 2, Color: true, Palette: DefaultPalette}
	wantEqual(t, encode(t, obj(pair("a", value.Arr(lit("1")))), pretty),
		"\x1b[1;39m{\x1b[0m\n  \x1b[1;34m\"a\"\x1b[0m\x1b[1;39m:\x1b[0m \x1b[1;39m[\x1b[0m\n    \x1b[0;39m1\x1b[0m\n  \x1b[1;39m]\x1b[0m\n\x1b[1;39m}\x1b[0m\n")
	wantEqual(t, encode(t, value.Str("raw"), Options{Raw: true, Color: true, Palette: DefaultPalette}), "raw\n")

	p, err := ParsePalette("0;31:0;32")
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, p.Null, "0;31")
	wantEqual(t, p.False, "0;32")
	wantEqual(t, p.True, DefaultPalette.True)
	p, err = ParsePalette(":1;5:::::::")
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, p.Null, "") // an empty field is an empty color in jq
	wantEqual(t, p.False, "1;5")
	_, err = ParsePalette("0;31:1;2;3m")
	wantErrContains(t, err, "invalid color")
}

func TestRawInteractions(t *testing.T) {
	// Pinned against jq 1.8.2: -r -a prints the quoted, escaped string.
	wantEqual(t, encode(t, value.Str("é"), Options{Raw: true, ASCII: true}), "\"\\u00e9\"\n")
	wantEqual(t, encode(t, value.Str("é"), Options{Join: true, ASCII: true}), "\"\\u00e9\"")
	wantEqual(t, encode(t, value.Str("é"), Options{RawNUL: true, ASCII: true}), "\"\\u00e9\"\x00")
	// One U+FFFD for each invalid byte, raw or quoted.
	wantEqual(t, encode(t, value.Str("\xff\xfe"), Options{Raw: true}), "\ufffd\ufffd\n")
	wantEqual(t, encode(t, value.Str("a\xffb"), Options{Compact: true}), "\"a\ufffdb\"\n")
	// A compound value gets the RS byte even with raw output on.
	wantEqual(t, encode(t, value.Arr(lit("1")), Options{Seq: true, Raw: true, Compact: true}), "\x1e[1]\n")
}

func TestGoFloats(t *testing.T) {
	f := value.Num(1234567.5).WithMeta(&value.Meta{Tag: "!!float"})
	wantEqual(t, encode(t, f, Options{Compact: true, GoFloats: true}), "1.2345675e+06\n")
	wantEqual(t, encode(t, f, Options{Compact: true}), "1234567.5\n")
	wantEqual(t, encode(t, value.Num(1234567.5), Options{Compact: true, GoFloats: true}), "1234567.5\n")
}
