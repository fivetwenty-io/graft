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
