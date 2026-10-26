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

func TestYQObjectKeysKeepTheirNode(t *testing.T) {
	// A key is the node itself, with its tag and type, and a collection
	// stays a complex key.
	in := "n: {m: deep}\nnum: 4\nl: [1, 2]\ntg: !foo 5\nb: !bar true\n"
	wantYQ(t, `{.missing: 1}`, in, "null: 1\n")
	wantYQ(t, `{.num: 1}`, in, "4: 1\n")
	wantYQ(t, `{(.num): 1}`, in, "4: 1\n")
	wantYQ(t, `{.tg: 1}`, in, "!foo 5: 1\n")
	wantYQ(t, `{.b: 1}`, in, "!bar true: 1\n")
	wantYQ(t, `{.l: 1}`, in, "? [1, 2]\n: 1\n")
	wantYQ(t, `{.n: 1}`, in, "? {m: deep}\n: 1\n")
}
