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
