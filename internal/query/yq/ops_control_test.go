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
