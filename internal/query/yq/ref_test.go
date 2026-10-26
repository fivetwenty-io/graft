package yq

import "testing"

func TestYQRefVariable(t *testing.T) {
	in := "cfg:\n  mode: off\nl: [1, 2]\n"
	// A ref variable stands for a place, so an assignment to it changes
	// the document, which a value variable does not.
	wantYQ(t, `.cfg.mode ref $m | $m = "on"`, in, "cfg:\n  mode: on\nl: [1, 2]\n")
	wantYQ(t, `.cfg.mode as $m | $m = "on"`, in, in)
	wantYQ(t, `.cfg ref $c | $c.mode = "on"`, in, "cfg:\n  mode: on\nl: [1, 2]\n")
	wantYQ(t, `.cfg.mode ref $m | $m += "x"`, in, "cfg:\n  mode: offx\nl: [1, 2]\n")
	wantYQ(t, `.cfg.mode ref $m | del($m)`, in, "cfg: {}\nl: [1, 2]\n")
	// It reads the place in the input it meets, so it sees later changes.
	wantYQ(t, `.cfg.mode ref $m | .cfg.mode = "z" | $m`, in, "z\n")
	wantYQ(t, `.cfg.mode as $m | .cfg.mode = "z" | $m`, in, "off\n")
	wantYQ(t, `.l[0] ref $x | .l[1] = $x`, in, "cfg:\n  mode: off\nl: [1, 1]\n")
}

func TestYQVariableLookupTakesFirstNode(t *testing.T) {
	in := "names: {a: Alice, b: Bob}\nids: [b, a]\n"
	// yq runs the right side once over every node on the left, and an
	// index by a key read from that input takes only the first node.
	wantYQ(t, `.names as $n | .ids[] | $n[.]`, in, "Bob\n")
	wantYQ(t, `.names as $n | .ids[] | $n["a"]`, in, "Alice\nAlice\n")
}

func TestYQEvalAsUpdatePath(t *testing.T) {
	// eval names the place an assignment changes, at the top or below a
	// pipe, as yq does.
	wantYQ(t, `eval(.where) = "set"`, "where: .t.u\n", "where: .t.u\nt:\n  u: set\n")
	wantYQ(t, `(.a | eval(.where)) = "set"`, "a: {where: .t.u}\n", "a: {where: .t.u, t: {u: set}}\n")
}
