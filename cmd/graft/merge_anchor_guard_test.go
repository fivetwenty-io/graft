package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMergeAndJSONRejectSelfContainingAnchor runs `graft merge` and
// `graft json` on a file whose alias sits inside the mapping its anchor
// names. spruce fails both with "anchor 'a' value contains itself" and
// exits 2. goccy decodes the alias as null, so graft used to print
// "b: null" and exit 0.
func TestMergeAndJSONRejectSelfContainingAnchor(t *testing.T) {
	cyclic := filepath.Join(t.TempDir(), "cyclic.yml")
	if err := os.WriteFile(cyclic, []byte("a: &a\n  b: *a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{"merge", "json"} {
		t.Run(cmd, func(t *testing.T) {
			stderr, rc := runGraftCapturingOutput(t, []string{cmd, cyclic})
			if rc != 2 || !strings.Contains(stderr, "anchor 'a' value contains itself") {
				t.Errorf("graft %s: rc=%d stderr=%q, want rc=2 and anchor 'a' value contains itself", cmd, rc, stderr)
			}
		})
	}
}

// TestMergeOverDeepPrintsOneMessage merges files nested 4,096 and 10,001
// levels deep. spruce prints the same bare recursion sentence for both
// and exits 2. graft printed it bare at 4,096 levels but prefixed the
// file name and "parse_error:" from 10,001 levels on, where the depth
// guard rather than the cycle check rejects the input.
func TestMergeOverDeepPrintsOneMessage(t *testing.T) {
	const want = "Hit max recursion depth. You seem to have a self-referencing dataset\n"
	dir := t.TempDir()
	for _, n := range []int{4096, 10001} {
		path := filepath.Join(dir, "deep.yml")
		text := "a: " + strings.Repeat("[", n) + strings.Repeat("]", n) + "\n"
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		stderr, rc := runGraftCapturingOutput(t, []string{"merge", path})
		if rc != 2 || stderr != want {
			t.Errorf("graft merge of %d levels: rc=%d stderr=%q, want rc=2 and %q", n, rc, stderr, want)
		}
	}
}
