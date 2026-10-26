package yq

import "testing"

func TestYQObjectExpressionKeys(t *testing.T) {
	in := "name: box\ntags: [red, blue]\nn: {m: deep}\n"
	wantYQ(t, `{.name: 1}`, in, "box: 1\n")
	wantYQ(t, `{.n.m: 1}`, in, "deep: 1\n")
	wantYQ(t, `{.name | upcase: 1}`, in, "BOX: 1\n")
	wantYQ(t, `{.name: .tags[]}`, in, "box: red\nbox: blue\n")
	wantYQ(t, `{.tags[]: 1}`, in, "red: 1\nblue: 1\n")
	wantYQ(t, `{.name: 1, .tags[0]: 2}`, in, "box: 1\nred: 2\n")
}

func TestYQObjectKeysAreNodeText(t *testing.T) {
	in := "n: {m: deep}\nnum: 4\n"
	wantYQ(t, `{.missing: 1}`, in, "\"null\": 1\n")
	wantYQ(t, `{.n: 1}`, in, "\"\": 1\n")
	wantYQ(t, `{.num: 1}`, in, "\"4\": 1\n")
	wantYQ(t, `{(.missing): 1}`, in, "\"null\": 1\n")
}
