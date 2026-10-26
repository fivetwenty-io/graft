package yq

import (
	"strings"
	"testing"
)

func TestYQSortKeysRecursive(t *testing.T) {
	wantYQ(t, `sort_keys(..)`, "b:\n  d: 1\n  c: 2\na: 0\n", "a: 0\nb:\n  c: 2\n  d: 1\n")
	wantYQ(t, `sort_keys(..)`, "b: [{z: 1, y: 2}]\na: 1\n", "a: 1\nb: [{y: 2, z: 1}]\n")
	wantYQ(t, `sort_keys(.[])`, "- {b: 1, a: 2}\n- {d: 1, c: 2}\n", "- {a: 2, b: 1}\n- {c: 2, d: 1}\n")
	wantYQ(t, `sort_keys(..)`, "B: 1\na: 2\n10: 3\n9: 4\n_: 5\n", "10: 3\n9: 4\nB: 1\n_: 5\na: 2\n")
	wantYQ(t, `sort_keys(.a)`, "a: 3\n", "a: 3\n")
}

func TestYQExplodeNested(t *testing.T) {
	wantYQ(t, `explode(.)`, "a: &x\n  b: &y 1\nc: *x\nd: *y\n", "a:\n  b: 1\nc:\n  b: 1\nd: 1\n")
	// A collection written in flow style stays in flow style.
	wantYQ(t, `explode(.)`, "a: &x {b: &y 1}\nc: *x\nd: *y\n", "a: {b: 1}\nc: {b: 1}\nd: 1\n")
}

func TestYQShuffle(t *testing.T) {
	wantYQ(t, `shuffle | length`, "- 1\n- 2\n- 3\n", "3\n")
	wantYQ(t, `shuffle | sort`, "[3, 1, 2]\n", "[1, 2, 3]\n")
	wantYQ(t, `shuffle | length`, "[]\n", "0\n")
	wantYQ(t, `shuffle`, "[1] # c\n", "[1] # c\n")
	wantYQ(t, `shuffle | tag`, "[1, 2]\n", "!!seq\n")
	wantYQ(t, `shuffle | style`, "[1, 2]\n", "flow\n")
	wantYQ(t, `[.a[] | shuffle | length]`, "a: [[1, 2], [3, 4, 5]]\n", "- 2\n- 3\n")
	wantYQError(t, `shuffle`, "a: 1\n", "node at path [] is not an array (it's a !!map)")
	wantYQError(t, `.a | shuffle`, "a: {b: 1}\n", "node at path [a] is not an array (it's a !!map)")
	wantYQError(t, `.a | shuffle`, "a: 3\n", "node at path [a] is not an array (it's a !!int)")
	wantYQError(t, `.a | shuffle`, "a:\n", "node at path [a] is not an array (it's a !!null)")
}

// A shuffle gives every order, so a few runs see more than one.
func TestYQShuffleVaries(t *testing.T) {
	seen := map[string]bool{}
	for range 40 {
		got, err := runYQ(t, testOpts, `shuffle`, "[1, 2, 3, 4]\n", false)
		if err != nil {
			t.Fatal(err)
		}
		seen[strings.TrimSpace(got)] = true
	}
	if len(seen) < 2 {
		t.Fatalf("shuffle gave one order in 40 runs: %v", seen)
	}
}
