package main

import (
	"strings"
	"testing"
)

// TestDiffModesFoldLongStrings pins `graft diff --unified`,
// `--side-by-side`, and `--changes` on a string that runs past column
// 80. Each mode prints its YAML through graft's merge writer, which
// folds such a string at a space past column 80, as spruce does, so the
// last word moves to an indented line of its own in every mode.
func TestDiffModesFoldLongStrings(t *testing.T) {
	const long = "the quick brown fox jumps over the lazy dog and keeps on running past column "
	from := "k: " + long + "eighty here\nz: 1\n"
	to := "k: " + long + "ninety here\nz: 1\n"
	bar := strings.Repeat("─", 39)
	for _, c := range []struct {
		mode, want string
	}{
		{"--unified", "--- from.yml\n+++ to.yml\n@@ k @@\n" +
			"-  " + long + "eighty\n" +
			"+  " + long + "ninety\n" +
			"     here\n"},
		{"--side-by-side", "from.yml                               │ to.yml\n" +
			bar + "┼" + bar + "\n" +
			"k: the quick brown fox jumps over the  │ k: the quick brown fox jumps over the \n" +
			"  here                                 │   here\n" +
			"z: 1                                   │ z: 1\n"},
		{"--changes", "Changes (1 modified, 0 added, 0 removed):\n\n  MODIFIED  k\n" +
			"            - " + long + "eighty\n" +
			"            -   here\n" +
			"            + " + long + "ninety\n" +
			"            +   here\n"},
	} {
		t.Run(c.mode, func(t *testing.T) {
			fromPath, toPath := writeDiffInputs(t, from, to)
			stdout, stderr, rc := runMainCaptured(t, "diff", c.mode, "--no-color", fromPath, toPath)
			if rc != 1 || stdout != c.want || stderr != "" {
				t.Errorf("graft diff %s: rc=%d stdout=%q stderr=%q, want rc=1 and stdout %q", c.mode, rc, stdout, stderr, c.want)
			}
		})
	}
}
