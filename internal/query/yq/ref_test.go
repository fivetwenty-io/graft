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
	wantYQ(t, `.names as $n | .ids[] | $n["a"]`, in, "Alice\n")
	wantYQ(t, `.names as $n | .ids[] | $n`, in, "{a: Alice, b: Bob}\n")
	wantYQ(t, `.names as $n | .ids[] | $n.a`, in, "Alice\n")
	wantYQ(t, `.names as $n | .ids[] | [$n[.]]`, in, "- Bob\n- Alice\n")
}

func TestYQEvalAsUpdatePath(t *testing.T) {
	// eval names the place an assignment changes, at the top or below a
	// pipe, as yq does.
	wantYQ(t, `eval(.where) = "set"`, "where: .t.u\n", "where: .t.u\nt:\n  u: set\n")
	wantYQ(t, `(.a | eval(.where)) = "set"`, "a: {where: .t.u}\n", "a: {where: .t.u, t: {u: set}}\n")
}

func TestYQRefVariableKeepsItsPlace(t *testing.T) {
	in := "a: {b: 1}\nl: [1, 2]\n"
	// It is read against the place it bound, from below the input too.
	wantYQ(t, `.a ref $a | [.l[] | $a.b + .]`, in, "- 2\n- 3\n")
	// A place the document lacks stays absent, whatever is done to it.
	wantYQ(t, `.nonexist ref $q | $q = 1`, in, in)
	wantYQ(t, `.a.zz ref $z | $z`, in, "")
	// Every match of a wildcard is its place.
	colors := "colors: {red: 1, rose: 2, \"r*\": 3, blue: 4}\n"
	want := "colors: {red: 9, rose: 9, \"r*\": 9, blue: 4}\n"
	wantYQ(t, `.colors."r*" ref $c | $c = 9`, colors, want+want+want)
	wantYQ(t, `.colors."r*" ref $q | del($q)`, colors, "colors: {blue: 4}\ncolors: {blue: 4}\ncolors: {blue: 4}\n")
}
