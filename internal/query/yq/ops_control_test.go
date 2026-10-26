package yq

import (
	"testing"

	"github.com/fivetwenty-io/graft/internal/query/runtime"
)

func TestYQControl(t *testing.T) {
	wantYQ(t, `with(.a; . = 2)`, "a: 1\n", "a: 2\n")
	wantYQ(t, `.[] as $i ireduce (0; . + $i)`, "- 1\n- 2\n", "3\n")
	wantYQ(t, `eval(".a")`, "a: 1\n", "1\n")
	wantYQ(t, `.[] | split_doc`, "[1, 2]\n", "1\n---\n2\n")
	wantYQError(t, `error("boom")`, "{}", "boom")
}

func TestYQLoad(t *testing.T) {
	f := writeTemp(t, "b: 2\n")
	fs := writeTemp(t, "text")
	wantYQ(t, `.a = load("`+f+`") | .c = load_str("`+fs+`")`, "{}", "a:\n  b: 2\nc: text\n")
	wantYQErrorOpts(t, Options{Caps: runtime.Capabilities{Env: true}}, `load("`+f+`")`, "{}", "file operations have been disabled")
}

func TestYQErrorGivesText(t *testing.T) {
	wantYQError(t, `error(5)`, "{}", "5")
	wantYQError(t, `error(null)`, "{}", "null")
	wantYQError(t, `error({"a": 1})`, "{}", "")
	wantYQError(t, `error(1, 2)`, "{}", "1")
	wantYQError(t, `error`, "{}", "'error' expects 1 arg but received none")
	wantYQError(t, `error | 1`, "{}", "'error' expects 1 arg but received none")
	wantYQError(t, `.[] | error`, "[]", "'|' expects 2 args but there is 1")
	wantYQError(t, `1 + error`, "{}", "'+' expects 2 args but there is 1")
	wantYQError(t, `.a = error`, "{}", "'=' expects 2 args but there is 1")
}

func TestYQSiblingAssignmentsShareTheDocument(t *testing.T) {
	// An assignment below a document's root builds on the changes its
	// siblings made to that document before it, so the last output holds
	// them all, as yq's shared nodes do. An earlier output cannot see a
	// later sibling's change, because graft prints it first.
	wantYQ(t, `.[] | .x = 1 | parent`, "[{x: 0}, {x: 0}]\n", "[{x: 1}, {x: 0}]\n[{x: 1}, {x: 1}]\n")
	wantYQ(t, `(.[0] | .x = 1 | parent), (.[1] | .x = 2 | parent)`, "[{x: 0}, {x: 0}]\n", "[{x: 1}, {x: 0}]\n[{x: 1}, {x: 2}]\n")
	wantYQ(t, `.a | .b = 1 | parent`, "a: {b: 0}\n", "a: {b: 1}\n")
}

func TestYQWithChangesTheWholeDocument(t *testing.T) {
	// with's body may change the document outside its node, and with gives
	// its own input with every change made.
	doc := "x: {a: {b: 0}}\na: {b: 0}\n"
	wantYQ(t, `with(.a; .b = 2 | parent.d = 5)`, doc, "x: {a: {b: 0}}\na: {b: 2}\nd: 5\n")
	wantYQ(t, `.x | with(.a; .b = 2 | parent.d = 5)`, doc, "{a: {b: 2}, d: 5}\n")
	wantYQ(t, `with(.a; .b = 2, .c = 3)`, doc, "x: {a: {b: 0}}\na: {b: 2, c: 3}\n")
	wantYQ(t, `with(.[]; .q = 1)`, doc, "x: {a: {b: 0}, q: 1}\na: {b: 0, q: 1}\n")
	wantYQ(t, `with(.a; .b = (1, 2))`, doc, "x: {a: {b: 0}}\na: {b: 2}\n")
	wantYQ(t, `.a | (parent.d = 5) | parent`, "a: {b: 0}\n", "a: {b: 0}\nd: 5\n")
	wantYQ(t, `.a | .b = .c | parent`, "a: {b: 0, c: 3}\n", "a: {b: 3, c: 3}\n")
	wantYQ(t, `.a | key = "z"`, "a: {b: 0}\n", "{b: 0}\n")
	wantYQ(t, `with(.a; 5)`, "a: 1\n", "a: 1\n")
	wantYQ(t, `with(1; . + 1)`, "a: 1\n", "a: 1\n")
}

func TestYQLoadKeepsComments(t *testing.T) {
	// load keeps the comment above a document's first node, and reads a
	// file with no node as an empty null. from_yaml keeps comments below
	// the first node only, gives an empty null for text with no node, and
	// fails on no text at all.
	head := writeTemp(t, "# head\nb: 2 # line\n")
	gap := writeTemp(t, "# doc\n\n# key\nb: 2\n")
	empty := writeTemp(t, "")
	only := writeTemp(t, "# only\n")
	wantYQ(t, `.m = load("`+head+`")`, "a: 1\n", "a: 1\nm:\n  # head\n  b: 2 # line\n")
	wantYQ(t, `.m = load("`+gap+`")`, "a: 1\n", "a: 1\nm:\n  # key\n  b: 2\n")
	wantYQ(t, `.m = load("`+empty+`") | .n = load("`+only+`")`, "a: 1\n", "a: 1\nm:\nn:\n")
	wantYQ(t, `.m = ("# head\nb: 2\n# c\nd: 3" | from_yaml)`, "a: 1\n", "a: 1\nm:\n  b: 2\n  # c\n  d: 3\n")
	wantYQ(t, `.m = ("# only" | from_yaml) | .n = ("\n" | from_yaml)`, "a: 1\n", "a: 1\nm:\nn:\n")
	wantYQError(t, `"" | from_yaml`, "a: 1\n", "EOF")
}
