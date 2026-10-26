package yq

import (
	"strings"
	"testing"
)

// mergeCase is an expression with what yq 4.54.1 prints for it, or the
// message of the error it fails with.
type mergeCase struct{ expr, want, err string }

func runMergeCases(t *testing.T, in string, cases []mergeCase) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.expr, func(t *testing.T) {
			if tc.err != "" {
				wantYQError(t, tc.expr, in, tc.err)
				return
			}
			wantYQ(t, tc.expr, in, tc.want)
		})
	}
}

func TestYQMergeOptions(t *testing.T) {
	in := "a: {x: [1], y: 1, n: {p: 1, q: [1]}}\nb: {x: [2], z: 2, n: {q: [3, 4], r: 2}}\n"
	runMergeCases(t, in, []mergeCase{
		{".a * .b", "{x: [2], y: 1, n: {p: 1, q: [3, 4], r: 2}, z: 2}\n", ""},
		{".a *+ .b", "{x: [1, 2], y: 1, n: {p: 1, q: [1, 3, 4], r: 2}, z: 2}\n", ""},
		{".a *? .b", "{x: [2], y: 1, n: {p: 1, q: [3, 4]}}\n", ""},
		{".a *n .b", "{x: [1], y: 1, n: {p: 1, q: [1], r: 2}, z: 2}\n", ""},
		{".a *d .b", "{x: [2], y: 1, n: {p: 1, q: [3, 4], r: 2}, z: 2}\n", ""},
		{".a *+? .b", "{x: [1, 2], y: 1, n: {p: 1, q: [1, 3, 4]}}\n", ""},
		{".a *?+ .b", "{x: [1, 2], y: 1, n: {p: 1, q: [1, 3, 4]}}\n", ""},
		{".a *+n .b", "{x: [1], y: 1, n: {p: 1, q: [1], r: 2}, z: 2}\n", ""},
		{".a *dn .b", "{x: [1], y: 1, n: {p: 1, q: [1, 4], r: 2}, z: 2}\n", ""},
		{".a *+d .b", "{x: [2, 2], y: 1, n: {p: 1, q: [3, 4, 4], r: 2}, z: 2}\n", ""},
		{".a *c .b", "{x: [2], y: 1, n: {p: 1, q: [3, 4], r: 2}, z: 2}\n", ""},
		{".a * null", "{x: [1], y: 1, n: {p: 1, q: [1]}}\n", ""},
		{".a.x *d .b.x", "[2]\n", ""},
		{"[1, 2] *d [3]", "- 3\n- 2\n", ""},
	})
}

func TestYQMergeOperands(t *testing.T) {
	in := "a: {x: [1]}\n"
	runMergeCases(t, in, []mergeCase{
		{".a * 3", "", "cannot multiply !!map with !!int"},
		{"3 * .a", "", "cannot multiply !!int with !!map"},
		{`.a * "s"`, "", "cannot multiply !!map with !!str"},
		{`"s" * .a`, "", "cannot multiply !!str with !!map"},
		{".a.x * .a", "", "cannot multiply !!seq with !!map"},
		{"[] * .a", "", "cannot multiply !!seq with !!map"},
		{".a * []", "", "cannot multiply !!map with !!seq"},
		{"null * null", "null\n", ""},
		{"null * 2", "", "cannot multiply !!null with !!int"},
		{"2 * null", "2\n", ""},
		{".a.x * 2", "", "cannot multiply !!seq with !!int"},
		{"2 * .a.x", "", "cannot multiply !!int with !!seq"},
		{`"a" * 3`, "aaa\n", ""},
		{`3 * "a"`, "aaa\n", ""},
		{`"a" * 0`, "\n", ""},
		{`"a" * -1`, "", "cannot repeat string by a negative number (-1)"},
		{`"a" * 1.5`, "", "cannot multiply !!str with !!float"},
		{"true * 2", "", "cannot multiply !!bool with !!int"},
		{`"a" * "b"`, "", "cannot multiply !!str with !!str"},
		{`{"x": {"a": 1}} *+ {"x": [1]}`, "", "!!seq (x) cannot be added to a !!map (x)"},
		{`{"x": 1} *+ {"x": [1]}`, "", "!!seq (x) cannot be added to a !!int (x)"},
	})
}

func TestYQMergeStyles(t *testing.T) {
	in := "bf: {x: 1}\nbb:\n  x: 2\nef: {}\nn: null\nbf2: {y: 3}\nsf: [1]\nsb:\n  - 2\nes: []\n"
	runMergeCases(t, in, []mergeCase{
		{".bb * .bf", "{x: 1}\n", ""},
		{".bf * .bb", "{x: 2}\n", ""},
		{".bb * {}", "x: 2\n", ""},
		{".bf * {}", "{x: 1}\n", ""},
		{".ef * .bf", "x: 1\n", ""},
		{".n * .bf", "x: 1\n", ""},
		{".n * .bb", "x: 2\n", ""},
		{"null * .bf", "x: 1\n", ""},
		{"{} * .bf", "x: 1\n", ""},
		{".ef * {}", "{}\n", ""},
		{`.bf * {"y": 1}`, "{x: 1, y: 1}\n", ""},
		{".bb * .bf2", "{x: 2, y: 3}\n", ""},
		{".ef * .bf2", "y: 3\n", ""},
		{`{"q": 1} * .bf`, "{q: 1, x: 1}\n", ""},
		{`{"a": {}} * {"a": .bf}`, "a:\n  x: 1\n", ""},
		{`{"a": .bb} * {"a": .bf}`, "a: {x: 1}\n", ""},
		{".sf * .sb", "[2]\n", ""},
		{".sb * .sf", "[1]\n", ""},
		{".es * .sf", "[1]\n", ""},
		{".n * .sf", "[1]\n", ""},
		{".sb *+ .sf", "- 2\n- 1\n", ""},
		{".sf *+ .sb", "[1, 2]\n", ""},
		{".es *+ .sf", "- 1\n", ""},
		{".sb *d .sf", "[1]\n", ""},
		{".sf *d .sb", "[2]\n", ""},
		{".sf *?d [3, 4]", "[3, 4]\n", ""},
		{".sf *+?d [3, 4]", "[3, 4, 4]\n", ""},
		{`. * {"z": 1}`, "bf: {x: 1}\nbb:\n  x: 2\nef: {}\nn: null\nbf2: {y: 3}\nsf: [1]\nsb:\n  - 2\nes: []\nz: 1\n", ""},
		{`. * {"bb": {"y": 2}}`, "bf: {x: 1}\nbb:\n  x: 2\n  y: 2\nef: {}\nn: null\nbf2: {y: 3}\nsf: [1]\nsb:\n  - 2\nes: []\n", ""},
	})
}

func TestYQMergeCommentsAndAliases(t *testing.T) {
	in := "# lead\na: # la\n  # ha\n  x: 1 # lx\n  # fa\nb: # lb\n  # hb\n  y: 2 # ly\nc: &anc {p: 1}\nd: *anc\ne: !foo {q: 1}\n"
	runMergeCases(t, in, []mergeCase{
		{".a * .b", "# ha\nx: 1 # lx\n# fa\n\n# hb\ny: 2 # ly\n", ""},
		{".b * .a", "# hb\ny: 2 # ly\n# ha\nx: 1 # lx\n# fa\n", ""},
		{`. * {"z": 1}`, "# lead\na: # la\n  # ha\n  x: 1 # lx\n  # fa\nb: # lb\n  # hb\n  y: 2 # ly\nc: &anc {p: 1}\nd: *anc\ne: !foo {q: 1}\nz: 1\n", ""},
		{`.d * {"r": 1}`, "", "cannot multiply  with !!map"},
		{".c * .d", "", "cannot multiply !!map with "},
		{`.a * {"x": .d}`, "# ha\nx: *anc # lx\n# fa\n", ""},
		{`.e * {"r": 1}`, "!foo {q: 1, r: 1}\n", ""},
		{`.e *c {"r": 1}`, "!!map {q: 1, r: 1}\n", ""},
		{`{"r": 1} * .e`, "!foo {r: 1, q: 1}\n", ""},
		{".d * null", "*anc\n", ""},
		{`.c * {"p": 2}`, "{p: 2}\n", ""},
		{`{"k": .c} * {"k": {"z": 1}}`, "k: {p: 1, z: 1}\n", ""},
		{`. * {"c": {"z": 1}} | .d`, "*anc\n", ""},
		{`.c * {"p": .d}`, "{p: *anc}\n", ""},
		{`.a *= {"w": 1}`, "# lead\na: # la\n  # ha\n  x: 1 # lx\n  # fa\n\n  w: 1\nb: # lb\n  # hb\n  y: 2 # ly\nc: &anc {p: 1}\nd: *anc\ne: !foo {q: 1}\n", ""},
	})
}

func TestYQAssignKeepsTagsAndAnchors(t *testing.T) {
	in := "a: !foo x\nb: &an 1\nc: y # lc\nd: !!str 5\ne: {p: 1}\nf: [1]\ng: !bar {q: 1}\n"
	rest := func(a, b, c, d, e, f, g string) string {
		return "a: " + a + "\nb: " + b + "\nc: " + c + "\nd: " + d + "\ne: " + e + "\nf: " + f + "\ng: " + g + "\n"
	}
	same := func(field, v string) string {
		vals := map[string]string{"a": "!foo x", "b": "&an 1", "c": "y # lc", "d": "!!str 5", "e": "{p: 1}", "f": "[1]", "g": "!bar {q: 1}"}
		vals[field] = v
		return rest(vals["a"], vals["b"], vals["c"], vals["d"], vals["e"], vals["f"], vals["g"])
	}
	runMergeCases(t, in, []mergeCase{
		{`.a = "z"`, same("a", "!foo z"), ""},
		{`.a |= "z"`, same("a", "!foo z"), ""},
		{`.a =c "z"`, same("a", "!!str z"), ""},
		{`.a |=c "z"`, same("a", "!!str z"), ""},
		{`.a=cc "x"`, same("a", "!!str x"), ""},
		{`.a |=c"x"`, same("a", "!!str x"), ""},
		{".b = 2", same("b", "&an 2"), ""},
		{".b =c 2", same("b", "&an 2"), ""},
		{".b |= 3", same("b", "&an 3"), ""},
		{`.d = "s"`, same("d", "!!str s"), ""},
		{`.g = {"r": 2}`, same("g", "!bar\n  r: 2"), ""},
		{`.g =c {"r": 2}`, strings.TrimSuffix(same("g", ""), " \n") + "\n  r: 2\n", ""},
		{".a = .g", same("a", "!foo {q: 1}"), ""},
		{".a = .b", same("a", "!foo 1"), ""},
		{".a = 5", same("a", "!foo 5"), ""},
		{`.c = "q"`, same("c", "q # lc"), ""},
		{".d = 6", same("d", "6"), ""},
		{".e = .f", same("e", "[1]"), ""},
		{`.e |= {"w": 1}`, same("e", "{w: 1}"), ""},
		{".c = .b", same("c", "1 # lc"), ""},
		{".b = .c", same("b", "&an y # lc"), ""},
		{`.a style=c "double"`, same("a", `!foo "x"`), ""},
		{`.a style |=c "double"`, same("a", `!foo "x"`), ""},
		{".a =c .b | .a", "1\n", ""},
	})
}

func TestYQMergeAssign(t *testing.T) {
	in := "a: !foo x\nb: &an 1\nc: y # lc\nd: !!str 5\ne: {p: 1}\nf: [1]\ng: !bar {q: 1}\n"
	head := "a: !foo x\nb: &an 1\nc: y # lc\nd: !!str 5\n"
	runMergeCases(t, in, []mergeCase{
		{`.e *= {"w": 1}`, head + "e: {p: 1, w: 1}\nf: [1]\ng: !bar {q: 1}\n", ""},
		{`.g *= {"w": 1}`, head + "e: {p: 1}\nf: [1]\ng: !bar {q: 1, w: 1}\n", ""},
		{`.g *=c {"w": 1}`, head + "e: {p: 1}\nf: [1]\ng: !!map {q: 1, w: 1}\n", ""},
		{".f *=+ [2]", head + "e: {p: 1}\nf: [1, 2]\ng: !bar {q: 1}\n", ""},
		{".f *= [2]", head + "e: {p: 1}\nf: [2]\ng: !bar {q: 1}\n", ""},
		{`.e *=n {"p": 2, "w": 3}`, head + "e: {p: 1, w: 3}\nf: [1]\ng: !bar {q: 1}\n", ""},
	})
}

// A null the run builds, put in place of a node read from a document,
// prints as null, in block and in flow style. A null read from a
// document keeps the text it was read with, which is nothing for "b:".
func TestYQAssignNull(t *testing.T) {
	block, flow := "a:\n  x: 1\n  y: 2\n", "a: {x: 1, y: 2}\n"
	for _, c := range []struct{ expr, in, want string }{
		{".a = null", "a: 1\n", "a: null\n"},
		{".a = null", "{a: 1}\n", "{a: null}\n"},
		{".a.x = null", flow, "a: {x: null, y: 2}\n"},
		{".a.x = null", block, "a:\n  x: null\n  y: 2\n"},
		{".a.x |= null", flow, "a: {x: null, y: 2}\n"},
		{".a.x |= null", block, "a:\n  x: null\n  y: 2\n"},
		{".a.x = null", "a:\n  x: 1\n", "a:\n  x: null\n"},
		{".a.x = null", "a: {x: 1}\n", "a: {x: null}\n"},
		{`. * {"a": {"x": null}}`, flow, "a: {x: null, y: 2}\n"},
		{`. * {"a": {"x": null}}`, block, "a:\n  x: null\n  y: 2\n"},
		{".a.x = .b", flow + "b:\n", "a: {x: '', y: 2}\nb:\n"},
		{".a.x = .b", block + "b:\n", "a:\n  x:\n  y: 2\nb:\n"},
		{".a[0] = null", "a: [1]\n", "a: [null]\n"},
		{".a[0] = null", "a:\n  - 1\n", "a:\n  - null\n"},
		{".a[0] = .b", "a: [1]\nb:\n", "a: ['']\nb:\n"},
	} {
		wantYQ(t, c.expr, c.in, c.want)
	}
}

// A scalar with a custom tag whose text reads as a number multiplies as
// that number. With the custom tag on the left, the result is the left
// node with the product's text, keeping its tag, style, anchor, and
// comment, and otherwise it is a plain number.
func TestYQMultiplyCustomTaggedNumbers(t *testing.T) {
	in := "a: !foo \"5\"\nb: !foo \"6\"\nc: !foo 5\nd: !foo 1.5\n"
	runMergeCases(t, in, []mergeCase{
		{".a * .b", "30\n", ""},
		{".a * 2", "10\n", ""},
		{`{"x": .a * .b}`, "x: !foo \"30\"\n", ""},
		{`{"x": .a * 2}`, "x: !foo \"10\"\n", ""},
		{`{"x": 2 * .a}`, "x: 10\n", ""},
		{`{"x": .c * .c}`, "x: !foo 25\n", ""},
		{`{"x": .c * 2}`, "x: !foo 10\n", ""},
		{`{"x": .d * 2}`, "x: !foo 3\n", ""},
		{`{"x": .a * .d}`, "x: !foo \"7.5\"\n", ""},
		{`{"x": .a *+ .b}`, "x: !foo \"30\"\n", ""},
		{"(.a * 2) | tag", "!foo\n", ""},
		{"(2 * .a) | tag", "!!int\n", ""},
		{"(1.5 * .c) | tag", "!!float\n", ""},
		{`.a * "x"`, "xxxxx\n", ""},
	})
	wantYQ(t, `{"x": .a * 2}`, "a: !foo 0x10\n", "x: !foo 0x20\n")
	wantYQ(t, `{"x": .a * 2}`, "a: &an !foo 5 # c\n", "x: &an !foo 10 # c\n")
	wantYQ(t, ".a *= 3", "a: !foo \"5\"\n", "a: !foo \"15\"\n")
	wantYQError(t, ".a * 2", "a: !foo .inf\n", "cannot multiply !foo with !!int")
}

func TestYQRepeatedStringKeepsItsSpelling(t *testing.T) {
	wantYQ(t, `.c = .a * 3`, "a: !!str 2\n", "a: !!str 2\nc: !!str 222\n")
	wantYQ(t, `.c = .a * 3`, "a: \"2\"\n", "a: \"2\"\nc: \"222\"\n")
	wantYQ(t, `.c = .a * .b`, "a: !!str 2\nb: !foo 3\n", "a: !!str 2\nb: !foo 3\nc: !!str 222\n")
	wantYQ(t, `.c = .b * .a`, "a: !!str 2\nb: 3\n", "a: !!str 2\nb: 3\nc: !!str 222\n")
	wantYQ(t, `.c = "2" * 3`, "a: 1\n", "a: 1\nc: \"222\"\n")
}
