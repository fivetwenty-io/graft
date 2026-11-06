package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMergeHistoryViewsHideLoopBindings runs each history view over a
// document with nested loops. No view lists the loop binding tree, and
// each one shows a loop body as the user wrote it, or counts and lists
// the loop body paths alone.
func TestMergeHistoryViewsHideLoopBindings(t *testing.T) {
	src := filepath.Join(t.TempDir(), "loops.yml")
	doc := `outer: [{n: 1}]
inner: [1, 2]
out:
(( for svc in outer ))
(( for i in inner ))
  - (( grab svc.n ))
  - big-(( grab i ))
(( done ))
(( done ))
`
	if err := os.WriteFile(src, []byte(doc), 0o600); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		flag string
		want string
	}{
		{"--history", "(( grab svc.n ))"},
		{"--show-changes", "(2 changed, 0 added, 0 removed)"},
		{"--changes-only", "out[0]"},
		{"--trace-path=out", "(( grab svc.n ))"},
	}
	for _, tc := range cases {
		t.Run(tc.flag, func(t *testing.T) {
			stdout, stderr, rc := runGraftCommand(t, []string{"merge", tc.flag, src})
			if rc != 0 {
				t.Fatalf("rc = %d, stderr = %q", rc, stderr)
			}
			if strings.Contains(stdout, "__graft_loop") {
				t.Errorf("%s names the loop binding tree:\n%s", tc.flag, stdout)
			}
			if !strings.Contains(stdout, tc.want) {
				t.Errorf("%s does not show %q:\n%s", tc.flag, tc.want, stdout)
			}
		})
	}
}
