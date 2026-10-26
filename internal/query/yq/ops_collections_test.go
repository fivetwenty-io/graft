package yq

import "testing"

func TestYQCollections(t *testing.T) {
	wantYQ(t, `sort_by(.a) | map(.a)`, "- a: 2\n- a: 1\n", "- 1\n- 2\n")
	wantYQ(t, `unique_by(.a) | length`, "- a: 1\n- a: 1\n", "1\n")
	wantYQ(t, `any_c(. > 1), all_c(. > 0)`, "- 1\n- 2\n", "true\ntrue\n")
	wantYQ(t, `unique, reverse, sort, min, max`, "[b, a, b]\n", "[b, a]\n[b, a, b]\n[a, b, b]\na\nb\n")
	wantYQ(t, `group_by(.k) | map(length)`, "[{k: x, v: 1}, {k: y, v: 2}, {k: x, v: 3}]\n", "- 2\n- 1\n")
	wantYQ(t, `flatten`, "[[1, [2]], 3]\n", "[1, 2, 3]\n")
	wantYQ(t, `sort_by(.)`, "{a: 1}\n", "{a: 1}\n")
	wantYQ(t, `sort_keys(..)`, "{b: 1, a: {d: 1, c: 2}}\n", "{a: {c: 2, d: 1}, b: 1}\n")
}

func TestYQCollectionErrors(t *testing.T) {
	wantYQError(t, `.a | sort`, "a: 1\n", `node at path [a] is not an array or map (it's a !!int)`)
	wantYQError(t, `reverse`, "a: 1\n", `node at path [] is not an array (it's a !!map)`)
	wantYQError(t, `flatten`, "a: 1\n", "only arrays are supported for flatten")
	wantYQError(t, `any_c(.)`, "a: 1\n", "any only supports arrays, was !!map")
}

func TestYQUnionOfTheSameNodes(t *testing.T) {
	// yq's comma gives a list once when both its sides give the very list
	// they were handed, as . and one variable do.
	wantYQ(t, `map(., .)`, "[1, 2]\n", "[1, 2]\n")
	wantYQ(t, `map((., .) | . + 1)`, "[1, 2]\n", "[2, 3]\n")
	wantYQ(t, `[., .], [(. | .), .], (.a as $x | [$x, $x])`, "{a: 1}\n", "- {a: 1}\n- {a: 1}\n- 1\n")
	wantYQ(t, `[., ., .] | length, ([., .a, .] | length), ([.a, ., .] | length), ([.a, .a] | length)`, "{a: 1}\n", "2\n3\n2\n2\n")
}
