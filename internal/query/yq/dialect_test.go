package yq

import (
	"testing"

	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

var _ runtime.Dialect = (*Dialect)(nil)

func TestYQTruthinessAndAutoCreate(t *testing.T) {
	wantYQ(t, `.a.b.c = 1`, `{}`, "a:\n  b:\n    c: 1\n")
	wantYQ(t, `.a[2] = 1`, `{}`, "a:\n  - null\n  - null\n  - 1\n")
	wantYQ(t, `.a.b`, `{}`, "null\n")
	// no and off are strings under YAML 1.2, and an empty string prints as
	// an empty line once unwrapped.
	wantYQ(t, `.[] | select(.)`, "- false\n- no\n- off\n- 0\n- \"\"\n", "no\noff\n0\n\n")
	wantYQ(t, `.[] | select(.)`, "- !!bool false\n- !!null x\n- ~\n- 1\n", "1\n")

	d := New(Options{})
	tagged := func(s, tag string) *value.Value { return value.Str(s).WithMeta(&value.Meta{Tag: tag}) }
	wantEqual(t, d.Truthy(tagged("false", "!!bool")), false)
	wantEqual(t, d.Truthy(tagged("FALSE", "!!bool")), false)
	wantEqual(t, d.Truthy(tagged("Yes", "!!bool")), true)
	wantEqual(t, d.Truthy(tagged("x", "!!null")), false)
	wantEqual(t, d.Truthy(tagged("false", "!custom")), true)
	wantEqual(t, d.Truthy(value.Str("")), true)
	wantEqual(t, d.Truthy(value.Num(0)), true)
	wantEqual(t, d.Truthy(value.Null()), false)
}

func TestYQIndex(t *testing.T) {
	wantYQ(t, `.a.b`, "a: 3\n", "")
	wantYQ(t, `.a.b = 1`, "a: 3\n", "a: 3\n")
	wantYQ(t, `.[-1]`, "[1, 2]\n", "2\n")
	wantYQ(t, `.[5]`, "[1, 2]\n", "null\n")
	wantYQ(t, `.["1"]`, "[1, 2]\n", "2\n")
	wantYQ(t, `.[]`, "3\n", "")
	wantYQ(t, `.[]`, "null\n", "")
	wantYQ(t, `.[0]`, "0: x\n", "x\n")
	wantYQError(t, `.[-3]`, "[1, 2]\n", "index [-3] out of range, array size is 2")
	wantYQError(t, `.[1.5]`, "[1, 2]\n", `cannot index array with '1.5' (strconv.ParseInt: parsing "1.5": invalid syntax)`)

	got, err := runYQ(t, Options{ReadOnly: true}, `.a.b = 1`, "{}\n", false)
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, got, "{}\n")
	got, err = runYQ(t, Options{ReadOnly: true}, `.a = 1`, "a: 0\n", false)
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, got, "a: 1\n")
}

func TestYQAssignSemantics(t *testing.T) {
	wantYQ(t, `.a = .b`, "a: 1\nb: 2\n", "a: 2\nb: 2\n")
	wantYQ(t, `.[] |= . + 1`, "- 1\n- 2\n", "- 2\n- 3\n")
	wantYQ(t, `.a |= (1, 2)`, "a: 0\n", "a: 1\n") // first result only
	wantYQ(t, `(.a, .b) = 9`, "a: 1\nb: 2\n", "a: 9\nb: 9\n")
	wantYQ(t, `.a = (1, 2)`, "a: 0\n", "a: 2\n") // last result only
	wantYQ(t, `.a += (1, 2)`, "a: 0\n", "a: 2\n")
	wantYQ(t, `.a += .b`, "a: 1\nb: 2\n", "a: 3\nb: 2\n")
	wantYQ(t, `.a = (.b | select(. > 5))`, "a: 1\nb: 2\n", "a: 1\nb: 2\n")
	wantYQ(t, `.[] |= select(. > 5)`, "- 1\n- 2\n", "- 1\n- 2\n")
	wantYQ(t, `(.. | select(tag == "!!seq")) |= (. + ["x"])`, "[[1]]\n", "[[1, x], x]\n") // children before parents
	wantYQ(t, `.a = 2`, "a: 1 # c\nb: [1, 2]\n", "a: 2 # c\nb: [1, 2]\n")
	wantYQ(t, `.a |= . + 1`, "a: 1 # c\n", "a: 2 # c\n")
}

func TestYQAssignKeepsPresentation(t *testing.T) {
	in := "a: [0]\nb: \"q\"\nd: []\ne:\n  - 5\nf: plain\n"
	wantYQ(t, `.a = .e`, in, "a: [5]\nb: \"q\"\nd: []\ne:\n  - 5\nf: plain\n")           // the place's style wins
	wantYQ(t, `.e = .a`, in, "a: [0]\nb: \"q\"\nd: []\ne: [0]\nf: plain\n")              // a place with none takes the value's
	wantYQ(t, `.b = .f`, in, "a: [0]\nb: \"plain\"\nd: []\ne:\n  - 5\nf: plain\n")       // quoting is a scalar's style
	wantYQ(t, `.f = .b`, in, "a: [0]\nb: \"q\"\nd: []\ne:\n  - 5\nf: \"q\"\n")           // and travels with the value
	wantYQ(t, `.b = 5`, in, "a: [0]\nb: 5\nd: []\ne:\n  - 5\nf: plain\n")                // a new tag drops it
	wantYQ(t, `.a |= {"k":1}`, in, "a:\n  k: 1\nb: \"q\"\nd: []\ne:\n  - 5\nf: plain\n") // and so does a new kind
	wantYQ(t, `.d |= [1]`, in, "a: [0]\nb: \"q\"\nd:\n  - 1\ne:\n  - 5\nf: plain\n")     // an empty collection has no style
	wantYQ(t, `.d[0] = 1`, in, "a: [0]\nb: \"q\"\nd:\n  - 1\ne:\n  - 5\nf: plain\n")
	wantYQ(t, `.x.a = 1`, "x: {}\n", "x:\n  a: 1\n")
}

func TestYQRecurse(t *testing.T) {
	wantYQ(t, `[.. | select(tag == "!!int")] | length`, "a: &x 1\nb: *x\nc: [2]\n", "2\n") // .. does not follow aliases
	wantYQ(t, `[... | select(kind == "scalar")] | length`, "a: 1\n", "2\n")                // ... includes the key
	wantYQ(t, `[... | path]`, "a: {b: 1}\n", "- []\n- - a\n- - a\n- - a\n  - b\n- - a\n  - b\n")
	wantYQ(t, `[.. | kind]`, "a: [1]\n", "- map\n- seq\n- scalar\n")
}

func TestYQCursorOperators(t *testing.T) {
	wantYQ(t, `.a | key`, "a: 1\n", "a\n")
	wantYQ(t, `.[] | key`, "a: 1\nb: 2\n", "a\nb\n")
	wantYQ(t, `.[0] | key`, "[5]\n", "0\n")
	wantYQ(t, `.[-1] | path`, "[1, 2]\n", "- 1\n")
	wantYQ(t, `key`, "a: 1\n", "")
	wantYQ(t, `parent`, "a: 1\n", "")
	wantYQ(t, `path`, "a: 1\n", "[]\n")
	wantYQ(t, `"x" | path`, "null\n", "[]\n")
	wantYQ(t, `"x" | parent`, "null\n", "")
	wantYQ(t, `.a.b | path`, "a: {b: 1}\n", "- a\n- b\n")
	wantYQ(t, `.a.b | parent`, "a: {b: 1}\n", "{b: 1}\n")
	wantYQ(t, `.a.b | parent | parent`, "a: {b: 1}\n", "a: {b: 1}\n")
	wantYQ(t, `... | key`, "a: 1\n", "a\n") // a key node has no key of its own
	wantYQ(t, `.a | key | key`, "a: 1\n", "")
	wantYQ(t, `.a | key | path`, "a: 1\n", "- a\n")
	wantYQ(t, `[.a] | .[0] | path`, "a: 1\n", "- 0\n")
	wantYQ(t, `[.a] | .[0] | parent`, "a: 1\n", "- 1\n")
	wantYQ(t, `.[1:2] | path`, "[1, 2, 3]\n", "[]\n")
	wantYQ(t, `(.a = 1) | .a | path`, "{}\n", "- a\n")

	// A missing key's parent is the container the key would be added to.
	wantYQ(t, `.a | parent`, "b: 1\n", "b: 1\na: null\n")
	wantYQ(t, `.a.b | parent`, "{}\n", "b: null\n")
	wantYQ(t, `.a.b | parent | parent`, "x: 1\n", "x: 1\na:\n  b: null\n")
	wantYQ(t, `.a.b | parent | key`, "{}\n", "a\n")
	wantYQ(t, `.[5] | parent`, "[1]\n", "[1, null, null, null, null, null]\n")
	wantYQ(t, `.a[2] | parent`, "{}\n", "- null\n- null\n- null\n")
}

func TestYQEvalAll(t *testing.T) {
	two := "a: 1\n---\na: 2\n"
	wantYQAll(t, `[.a]`, two, "- 1\n- 2\n")
	wantYQAll(t, `.a = 3`, two, "a: 3\n---\na: 3\n")
	wantYQAll(t, `.a | parent`, two, "a: 1\n---\na: 2\n")
	wantYQ(t, `.a = 3`, two, "a: 3\n---\na: 3\n")
}

// Under eval-all a definition, a literal, a variable, an if, and an
// object runs once for each document, while [...], the source of "as",
// and an operator's operands see them all at once.
func TestYQEvalAllRunsEachCandidate(t *testing.T) {
	two := "a: 1\n---\na: 2\n"
	wantYQAll(t, `select(.a == 2)`, two, "a: 2\n")
	wantYQAll(t, `select(.a == 2) | .a`, two, "2\n")
	wantYQAll(t, `select(has("b"))`, "a: 1\n---\nb: 2\n", "b: 2\n")
	wantYQAll(t, `to_entries`, two, "- key: a\n  value: 1\n- key: a\n  value: 2\n")
	wantYQAll(t, `with_entries(.value += 1)`, two, "a: 2\na: 3\n")
	wantYQAll(t, `del(.a)`, two, "{}\n---\n{}\n")
	wantYQAll(t, `map_values(. + 1)`, two, "a: 2\n---\na: 3\n")
	wantYQAll(t, `1`, two, "1\n1\n")
	wantYQAll(t, `"s"`, two, "s\ns\n")
	wantYQAll(t, `"a\(.a)"`, two, "a1\na2\n")
	wantYQAll(t, `if .a == 1 then "x" else "y" end`, two, "x\ny\n")
	wantYQAll(t, `{"x": .a}`, two, "x: 1\n---\nx: 2\n")
	wantYQAll(t, `{}`, two, "{}\n")
	wantYQAll(t, `.a as $x | $x`, two, "1\n---\n2\n")
	wantYQAll(t, `.a as $x | [$x]`, two, "- 1\n- 1\n- 2\n- 2\n")
	wantYQAll(t, `. as $d | $d.a`, two, "1\n---\n2\n")
	wantYQAll(t, `. as $d | $d[]`, two, "1\n---\n2\n")
	wantYQAll(t, `.a, .a`, two, "1\n---\n2\n---\n1\n---\n2\n")
	wantYQAll(t, `. as $i ireduce (0; . + $i.a)`, two, "3\n3\n")
	wantYQAll(t, `. as $i ireduce ([]; . + [$i.a])`, two, "- 1\n- 2\n")
}

// A value an operator derives from its input takes the input's place, so
// it has the input's path, key, parent, and document. A literal and an
// object are new, and so is a value derived from a document's root unless
// it is of the root's kind.
func TestYQDerivedValuesKeepTheirPlace(t *testing.T) {
	wantYQ(t, `.a | . + 1 | path`, "a: 1\n", "- a\n")
	wantYQ(t, `.a | tostring | path`, "a: 1\n", "- a\n")
	wantYQ(t, `.a | path | path`, "a: 1\n", "- a\n")
	wantYQ(t, `.a | length | path`, "a: [1]\n", "- a\n")
	wantYQ(t, `.a | (. + 1) | key`, "a: 1\n", "a\n")
	wantYQ(t, `.a | . + 1 | parent`, "a: 1\n", "a: 1\n")
	wantYQ(t, `.a == 1 | path`, "a: 1\n", "- a\n")
	wantYQ(t, `.a | to_entries | .[0] | path`, "a: {b: 1}\n", "- a\n- 0\n")
	wantYQ(t, `.a | [.] | .[0] | path`, "a: 1\n", "- a\n- 0\n")
	wantYQ(t, `.a as $x | $x | path`, "a: 1\n", "- a\n")
	wantYQ(t, `.a as $x | $x | parent`, "a: 1\nb: 2\n", "a: 1\nb: 2\n")
	wantYQ(t, `.a | 1 | path`, "a: 1\n", "[]\n")
	wantYQ(t, `.a | .[0:1] | path`, "a: [1, 2]\n", "- a\n")
	wantYQ(t, `.a | {"x": .} | .x | path`, "a: 1\n", "- x\n")

	two := "a: 1\n---\na: 2\n"
	wantYQAll(t, `.a | tostring`, two, "1\n---\n2\n")
	wantYQAll(t, `[.a] | .[]`, two, "1\n2\n")
	wantYQAll(t, `keys`, two, "- a\n- a\n")
	wantYQAll(t, `del(.a) | path`, two, "[]\n[]\n")
}

// The right side of "|=" runs at each target, so it sees the target's
// path, key, and parent.
func TestYQUpdateRunsAtItsTarget(t *testing.T) {
	in := "a: {b: 1, c: 2}\n"
	wantYQ(t, `.a[] |= key`, in, "a: {b: b, c: c}\n")
	wantYQ(t, `.a[] |= (key + "x")`, in, "a: {b: bx, c: cx}\n")
	wantYQ(t, `.a.b |= (parent | length)`, in, "a: {b: 2, c: 2}\n")
	wantYQ(t, `(.. | select(tag == "!!int")) |= (path | join("."))`, in, "a: {b: a.b, c: a.c}\n")
}

// The right side of "=" and of the arithmetic forms reads without
// creating anything, so a key the input lacks gives no output there.
func TestYQAssignSourceCreatesNothing(t *testing.T) {
	in := "a: 1\nc: 2\n"
	wantYQ(t, `.a = .x`, in, in)
	wantYQ(t, `.a = .x.y`, in, in)
	wantYQ(t, `.a = .[5]`, in, in)
	wantYQ(t, `.a = (.x | length)`, in, in)
	wantYQ(t, `.a += .x`, in, in)
	wantYQ(t, `.a = (2, .x)`, in, "a: 2\nc: 2\n")
	wantYQ(t, `.a = (.x // 3)`, in, "a: 3\nc: 2\n")
	wantYQ(t, `.a = ([.x])`, in, "a: []\nc: 2\n")
	wantYQ(t, `.a |= .x`, in, in)
	// A key under null is missing too, while a list index past the end
	// reads as null.
	wantYQ(t, `.a = ([.n.y] | length)`, "a: 5\nn: null\n", "a: 0\nn: null\n")
	wantYQ(t, `.a = ([.n.y, .s.x] | length)`, "a: 5\nn: null\ns: str\n", "a: 0\nn: null\ns: str\n")
	wantYQ(t, `.a = ([.d[5]] | length) | .a`, "a: 5\nd: [1]\n", "1\n")
}

// An assignment skips a left side that is not a path, and an undefined
// variable has no value, as in yq.
func TestYQLenience(t *testing.T) {
	in := "a: 1\nc: 2\n"
	wantYQ(t, `(.b + .a) += 2`, in, in)
	wantYQ(t, `(.b + .a) |= 3`, in, in)
	wantYQ(t, `(1) = 2`, in, in)
	wantYQ(t, `(.a, 1) = 3`, in, "a: 3\nc: 2\n")
	wantYQ(t, `$__loc__`, in, "")
	wantYQ(t, `$nope | length`, in, "")
	wantYQ(t, `[$nope]`, in, "[]\n")
	wantYQ(t, `$ENV`, in, "")
}

func TestYQErrorShape(t *testing.T) {
	wantYQError(t, `.a +`, "{}", "'+' expects 2 args but there is 1")
	wantYQError(t, `.a`, "[1]\n", `cannot index array with 'a' (strconv.ParseInt: parsing "a": invalid syntax)`)
}
