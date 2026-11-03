package controlflow

import "testing"

// TestRewriteVarRefs_PathContinuations pins which names rewriteVarRefs
// treats as a loop variable. A name is the variable only when it starts
// a reference path. A name that continues a path after a '/', a '-', or
// a '.' is part of a document key, as the expression lexer reads it, so
// it stays as written.
func TestRewriteVarRefs_PathContinuations(t *testing.T) {
	const target = "__graft_loop.l1.0.svc"
	targets := map[string]string{"svc": target}
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"bare variable", "grab svc", "grab " + target},
		{"variable with a dotted tail", "grab svc.ip", "grab " + target + ".ip"},
		{"variable with a slash in its tail", "grab svc.ip/0", "grab " + target + ".ip/0"},
		{"name after a slash in a path", "grab jobs.web/svc.ip", "grab jobs.web/svc.ip"},
		{"name after a hyphen in a path", "grab jobs.web-svc.ip", "grab jobs.web-svc.ip"},
		{"name after a slash after a bracket", "grab arr[0]/svc", "grab arr[0]/svc"},
		{"name after a dot after a bracket", "grab arr[0].svc", "grab arr[0].svc"},
		{"name after a slash after a dot", "grab arr.0./svc", "grab arr.0./svc"},
		{"name after a slash after a digit", "grab 0/svc", "grab 0/svc"},
		{"name after a dollar name after a slash", "grab a/$X.svc", "grab a/$X.svc"},
		{"name glued by a plus in a dotted segment", "grab a.b+svc", "grab a.b+svc"},
		{"slash segment that starts with the name", "grab svc/0", "grab svc/0"},
		{"hyphenated name that starts with the name", "grab svc-name", "grab svc-name"},
		{"spaced division by the variable", "a / svc", "a / " + target},
		{"division with a space before the slash", "a /svc", "a /" + target},
		{"division after a closing paren", "(a)/svc", "(a)/" + target},
		{"subtraction after a bracket", "arr[0]-svc", "arr[0]-" + target},
		{"subtraction after a number", "5-svc", "5-" + target},
		{"variable inside an index", "grab list[svc]", "grab list[" + target + "]"},
		{"variable in a quoted string is left alone", `concat "svc/" svc`, `concat "svc/" ` + target},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := rewriteVarRefs(tc.in, targets); got != tc.want {
				t.Fatalf("rewriteVarRefs(%q)\n got  %q\n want %q", tc.in, got, tc.want)
			}
		})
	}
}
