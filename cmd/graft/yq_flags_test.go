package main

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestYQJSONNumbers(t *testing.T) {
	Convey("json input numbers read as yq reads them", t, func() {
		out, _, _ := runYQ(t, []string{"-p", "json", "-o", "yaml", "."}, `{"a": 1.0, "b": "x"}`)
		So(out, ShouldEqual, "a: 1\nb: x\n")
		out, _, _ = runYQ(t, []string{"-p", "json", "-o", "json", "-I0", "."}, `{"a":1.50,"b":1e3,"c":1e20,"d":12345678.5,"e":0.00001,"f":-0.0,"g":9007199254740993,"h":[1.0]}`)
		So(out, ShouldEqual, `{"a":1.5,"b":1000,"c":1e+20,"d":1.23456785e+07,"e":1e-05,"f":0,"g":9007199254740992,"h":[1]}`+"\n")
	})
}

func TestYQStringInterpolationFlag(t *testing.T) {
	Convey("string interpolation", t, func() {
		out, _, _ := runYQ(t, []string{`"x\(.a)"`}, "a: 1\n")
		So(out, ShouldEqual, "x1\n")
		out, _, _ = runYQ(t, []string{"--string-interpolation=false", `"x\(.a)"`}, "a: 1\n")
		So(out, ShouldEqual, "x\\(.a)\n")
		out, _, _ = runYQ(t, []string{"--string-interpolation=false", `"a\nb"`}, "a: 1\n")
		So(out, ShouldEqual, "a\nb\n")
		out, _, _ = runYQ(t, []string{"--string-interpolation=false", `"x\(.a) \(1+2)"`}, "a: 1\n")
		So(out, ShouldEqual, "x\\(.a) \\(1+2)\n")
		out, _, _ = runYQ(t, []string{"--string-interpolation=false", `"x\(.a"`}, "a: 1\n")
		So(out, ShouldEqual, "x\\(.a\n")
	})
}

func TestYQHeaderPreprocessFlag(t *testing.T) {
	Convey("header preprocessing", t, func() {
		// These two rows already passed before the flag reached from_yaml.
		out, _, _ := runYQ(t, []string{"."}, "# c\n---\na: 1\n")
		So(out, ShouldEqual, "# c\n---\na: 1\n")
		out, _, _ = runYQ(t, []string{"--header-preprocess=false", "."}, "# c\n---\na: 1\n")
		So(out, ShouldEqual, "# c\na: 1\n")
		out, _, _ = runYQ(t, []string{"--header-preprocess=false", ".x | from_yaml"}, "x: \"# c\\n---\\na: 1\\n\"\n")
		So(out, ShouldEqual, "# c\na: 1\n")
	})
}

const mergeReadDoc = "b: &b {p: 1, q: 2}\no: &o {r: 3, p: 4}\nd1:\n  <<: *b\n  a: 0\nd2:\n  a: 0\n  <<: *b\nd3:\n  q: 9\n  a: 0\n  <<: [*b, *o]\n  z: 5\n  r: 8\nd4:\n  <<: *b\n  q: 9\nref: *b\n"

// The expected outputs below are what yq 4.54.1 prints for each row.
func TestYQMergeKeyReads(t *testing.T) {
	rows := []struct{ expr, def, spec string }{
		{".d1.p", "1", "1"},
		{".d2.a", "0", "0"},
		{".d3.q", "2", "9"},
		{".d3.p", "4", "1"},
		{".d3.r", "8", "8"},
		{".d3.nope", "null", "null"},
		{`.d1["p"]`, "1", "1"},
		{".ref.p", "1", "1"},
		{"[.d3[]]", "[2,0,4,8,5]", "[8,1,9,0,5]"},
		{"[.d3[] | key]", `["q","a","p","r","z"]`, `["r","p","q","a","z"]`},
		{"[.d4[]]", "[1,9]", "[1,9]"},
		{`.d1 | has("p")`, "false", "false"},
	}
	Convey("merge keys read as yq reads them", t, func() {
		for _, r := range rows {
			out, _, _ := runYQ(t, []string{"-o", "json", "-I0", r.expr}, mergeReadDoc)
			So(out, ShouldEqual, r.def+"\n")
			out, _, _ = runYQ(t, []string{"--yaml-fix-merge-anchor-to-spec", "-o", "json", "-I0", r.expr}, mergeReadDoc)
			So(out, ShouldEqual, r.spec+"\n")
		}
		_, errOut, _ := runYQ(t, []string{".d1.p"}, mergeReadDoc)
		So(errOut, ShouldContainSubstring, "--yaml-fix-merge-anchor-to-spec is false")
		_, errOut, _ = runYQ(t, []string{"--yaml-fix-merge-anchor-to-spec", ".d1.p"}, mergeReadDoc)
		So(errOut, ShouldBeEmpty)
	})

	Convey("the brief's merge rows", t, func() {
		doc := "base: &b {x: 1}\nd:\n  <<: *b\n  y: 2\n"
		out, _, _ := runYQ(t, []string{".d.x"}, doc)
		So(out, ShouldEqual, "1\n")
		out, _, _ = runYQ(t, []string{"--yaml-fix-merge-anchor-to-spec", ".d | keys"}, doc)
		So(out, ShouldEqual, "- <<\n- y\n")
	})
}
