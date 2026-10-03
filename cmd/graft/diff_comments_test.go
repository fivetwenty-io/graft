package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDiffPrintsRootScalarComments checks that a root scalar keeps its
// line and foot comments in the report, and that its head comment stays
// out of it. The expected output is what spruce v1.35.17 prints for
// each pair.
func TestDiffPrintsRootScalarComments(t *testing.T) {
	withStdoutTerminal(t, false, 80)
	dir := t.TempDir()
	base := filepath.Join(dir, "base.yml")
	if err := os.WriteFile(base, []byte("a: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	const header = "\n(root level)\n± type change from map to string\n- a: 1\n"
	for _, c := range []struct{ to, want string }{
		{"x # c\n", header + "+ x # c\n\n\n"},
		{"--- x # c\n", header + "+ x # c\n\n\n"},
		{"x\n# foot\n", header + "+ x\n  # foot\n\n\n"},
		{"x # c\n# foot\n", header + "+ x # c\n\n\n"},
		{"# h\nx # c\n", header + "+ x # c\n\n\n"},
	} {
		to := filepath.Join(dir, "to.yml")
		if err := os.WriteFile(to, []byte(c.to), 0o600); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, rc := runMainCaptured(t, "diff", "--no-color", base, to)
		if rc != 1 || stderr != "" || stdout != c.want {
			t.Errorf("to.yml %q: rc=%d stderr=%q\nstdout=%q\nwant  %q", c.to, rc, stderr, stdout, c.want)
		}
	}
}

// TestDiffLeavesTrailingCommentsWithTheDocument checks that a comment the
// stream ends on, which yaml.v3 leaves as the document's foot comment,
// stays out of the report instead of landing on the outermost key. The
// expected output is what spruce v1.35.17 prints.
func TestDiffLeavesTrailingCommentsWithTheDocument(t *testing.T) {
	withStdoutTerminal(t, false, 80)
	dir := t.TempDir()
	base, to := filepath.Join(dir, "base.yml"), filepath.Join(dir, "to.yml")
	if err := os.WriteFile(base, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(to, []byte("a:\n  b:\n    c: 1\n# x1\n  # z1\n# x2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	want := "\n(root level)\n± type change from string to map\n- x\n+ a:\n    b:\n      c: 1\n  # x1\n  # z1\n\n\n"
	stdout, stderr, rc := runMainCaptured(t, "diff", "--no-color", base, to)
	if rc != 1 || stderr != "" || stdout != want {
		t.Errorf("rc=%d stderr=%q\nstdout=%q\nwant  %q", rc, stderr, stdout, want)
	}
}

// diffCase is one pair of files and the report spruce v1.35.17 prints
// for it, which exits 1.
type diffCase struct{ from, to, want string }

func runDiffCases(t *testing.T, cases []diffCase) {
	t.Helper()
	withStdoutTerminal(t, false, 80)
	dir := t.TempDir()
	base, to := filepath.Join(dir, "base.yml"), filepath.Join(dir, "to.yml")
	for _, c := range cases {
		if err := os.WriteFile(base, []byte(c.from), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(to, []byte(c.to), 0o600); err != nil {
			t.Fatal(err)
		}
		stdout, stderr, rc := runMainCaptured(t, "diff", "--no-color", base, to)
		if rc != 1 || stderr != "" || stdout != c.want {
			t.Errorf("%q to %q: rc=%d stderr=%q\nstdout=%q\nwant  %q", c.from, c.to, rc, stderr, stdout, c.want)
		}
	}
}

// TestDiffPrintsFlowCommentAfterCollectionEntry checks that a comment
// after a flow entry whose value is a collection is printed under the
// entry, as spruce v1.35.17 does.
func TestDiffPrintsFlowCommentAfterCollectionEntry(t *testing.T) {
	runDiffCases(t, []diffCase{
		{"r: 0\n", "r: {\n  k0: [],\n  # c1\n}\n", "\nr\n± type change from int to map\n- 0\n+ k0: []\n  # c1\n\n\n"},
		{"r: 0\n", "r: {\n  k0: {a: 1},\n  # c1\n\n}\n", "\nr\n± type change from int to map\n- 0\n+ k0:\n    a: 1\n  # c1\n\n\n"},
		{"r: 0\n", "r: [\n  k0: [],\n  # c1\n]\n", "\nr\n± type change from int to list\n- 0\n+ - k0: []\n  # c1\n\n\n"},
	})
}

// TestDiffPrintsCommentAfterTagOrAnchor checks that a comment after a
// tag or an anchor is the line comment of the first scalar after it,
// ahead of that scalar's own comment, as spruce v1.35.17 prints it.
func TestDiffPrintsCommentAfterTagOrAnchor(t *testing.T) {
	runDiffCases(t, []diffCase{
		{"0\n", "--- &a # c1\nx # c2\n", "\n(root level)\n± type change from int to string\n- 0\n+ x # c1\n  # c2\n\n\n"},
		{"0\n", "--- &a !!seq # c1\n- x # c2\n", "\n(root level)\n± type change from int to list\n- 0\n+ - x # c1\n  # c2\n\n\n"},
		{"0\n", "!!str # c1\nx # c2\n", "\n(root level)\n± type change from int to string\n- 0\n+ x # c1\n  # c2\n\n\n"},
		{"0\n", "!!str # c1\nx\n", "\n(root level)\n± type change from int to string\n- 0\n+ x # c1\n\n\n"},
		{"k: 0\n", "k: !!str # c1\n  x # c2\n", "\nk\n± type change from int to string\n- 0\n+ x # c1\n  # c2\n\n\n"},
		{"k: 0\n", "k: &a # c1\n  - x\n", "\nk\n± type change from int to list\n- 0\n+ - x # c1\n\n\n"},
	})
}

// TestDiffPrintsCommentAfterTaggedSequenceRoot checks that a comment
// after an anchor on a "---" line reaches the first scalar of a
// sequence whose first item is nested or on the next line, as spruce
// v1.35.17 does.
func TestDiffPrintsCommentAfterTaggedSequenceRoot(t *testing.T) {
	runDiffCases(t, []diffCase{
		{"zzz\n", "--- &a # c\n-\n  - x\n", "\n(root level)\n± type change from string to list\n- zzz\n+ - - x # c\n\n\n"},
		{"zzz\n", "--- &a # c\n-\n  x\n", "\n(root level)\n± type change from string to list\n- zzz\n+ - x # c\n\n\n"},
		{"x: 1\n", "--- !!map # c1\na: 1 # c2\n", "\n(root level)\n- one map entry removed:   + one map entry added:\nx: 1                       a: 1 # c2\n\n\n"},
	})
}

// TestDiffPlacesCommentUnderBareDash checks a comment that follows a
// sequence dash with no value. libyaml holds no comment on such a dash,
// so the comment goes to the next node that takes one, or to the
// document, as spruce v1.35.17 prints it.
func TestDiffPlacesCommentUnderBareDash(t *testing.T) {
	runDiffCases(t, []diffCase{
		{"zzz\n", "-\n# c9\n", "\n(root level)\n± type change from string to list\n- zzz\n+ -\n\n\n"},
		{"zzz\n", "-\n# c8\n\n-\n", "\n(root level)\n± type change from string to list\n- zzz\n+ -\n  -\n\n\n"},
		{"m: 0\n", "m:\n  -\n    # c4\n\n    k0: 1\n", "\nm\n± type change from int to list\n- 0\n+ - k0: 1\n  # c4\n\n\n"},
		{"zzz\n", "-\n  -\n      # c7\n\n    - null\n", "\n(root level)\n± type change from string to list\n- zzz\n+ - - - null\n  # c7\n\n\n"},
	})
}
