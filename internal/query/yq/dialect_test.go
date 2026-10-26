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

func TestYQErrorShape(t *testing.T) {
	wantYQError(t, `.a +`, "{}", "'+' expects 2 args but there is 1")
	wantYQError(t, `.a`, "[1]\n", `cannot index array with 'a' (strconv.ParseInt: parsing "a": invalid syntax)`)
}
