package yq

import "testing"

func TestYQFirstChild(t *testing.T) {
	wantYQ(t, `first(. > 1)`, "[1, 2, 3]\n", "2\n")
	wantYQ(t, `first(.n > 9)`, "[{n: 1}]\n", "")
	wantYQ(t, `first(.n)`, "[{n: false}, {n: 0}]\n", "{n: 0}\n")
	wantYQ(t, `first(.[])`, "[[false, 1], [5]]\n", "[false, 1]\n")
	wantYQ(t, `first(. == 2) = 9`, "[1, 2, 3]\n", "[1, 9, 3]\n")
	wantYQ(t, `first(. == "b")`, "{k1: a, k2: b}\n", "b\n")
}

func TestYQFilter(t *testing.T) {
	wantYQ(t, `filter(. > 1)`, "[1, 2, 3]\n", "[2, 3]\n")
	wantYQ(t, `filter(. == "keep")`, "a: keep\nb: drop\n", "- keep\n")
}

func TestYQPivot(t *testing.T) {
	wantYQ(t, `pivot`, "[{x: 1, y: 2}, {x: 3}]\n", "x:\n  - 1\n  - 3\ny:\n  - 2\n  -\n")
	wantYQ(t, `pivot`, "[[a, b], [1, 2]]\n", "- - a\n  - 1\n- - b\n  - 2\n")
	wantYQ(t, `pivot`, "[[1], []]\n", "- - 1\n  -\n")
	wantYQError(t, `pivot`, "[1, 2]\n", "can only pivot elements of !!seq or !!map types, received !!int")
	wantYQError(t, `pivot`, "[[1], {a: 1}]\n", "sequence contains elements of !!seq and !!map types")
	wantYQError(t, `pivot`, "a: 1\n", "cannot pivot node of type !!map")
}

func TestYQArrayToMap(t *testing.T) {
	wantYQ(t, `array_to_map`, "[a, null, c]\n", "0: a\n2: c\n")
	wantYQ(t, `array_to_map`, "[]\n", "{}\n")
	wantYQ(t, `array_to_map`, "a: b\n", "a: b\n")
	wantYQ(t, `array_to_map`, "5\n", "{}\n")
}
