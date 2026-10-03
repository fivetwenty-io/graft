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
