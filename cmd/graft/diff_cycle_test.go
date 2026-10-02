package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"
)

// cycleChildEnv names the variable that turns this test binary into a
// plain "graft diff" run, so the parent test can kill it at a deadline.
const cycleChildEnv = "GRAFT_DIFF_CYCLE_CHILD_ARGS"

// TestDiffRejectsSelfReferencingAnchor runs "graft diff" on files whose
// anchors contain themselves. Spruce overflows its stack and exits 2 on
// them. graft must fail the load with the parse error yaml.v3's decoder
// gives and exit 2, instead of walking the cycle until memory runs out.
// Each run happens in a child process with a small stack limit and a
// deadline, so a regression fails the test rather than hanging it.
func TestDiffRejectsSelfReferencingAnchor(t *testing.T) {
	if args := os.Getenv(cycleChildEnv); args != "" {
		debug.SetMaxStack(8 << 20)
		os.Args = append([]string{"graft"}, strings.Split(args, "\n")...)
		main()
		os.Exit(0)
	}
	dir := t.TempDir()
	write := func(name, text string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	plain := write("plain.yml", "a: &x [1]\n")
	for _, c := range []struct{ name, text string }{
		{"sequence", "a: &x [*x]\n"},
		{"mapping", "a: &x {b: *x}\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			cyclic := write(c.name+".yml", c.text)
			for _, args := range [][]string{{plain, cyclic}, {cyclic, plain}, {cyclic, cyclic}} {
				rc, stderr := runDiffChild(t, append([]string{"diff", "--no-color"}, args...))
				want := "unable to parse data from " + cyclic + ": yaml: anchor 'x' value contains itself"
				if rc != 2 || !strings.Contains(stderr, want) {
					t.Errorf("graft diff %v: rc=%d stderr=%q, want rc=2 and %q", args, rc, stderr, want)
				}
			}
		})
	}
}

func runDiffChild(t *testing.T, args []string) (int, string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Other tests in this package replace os.Args, so os.Args[0] may not
	// name this binary.
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(ctx, self, "-test.run=^TestDiffRejectsSelfReferencingAnchor$")
	cmd.Env = append(os.Environ(), cycleChildEnv+"="+strings.Join(args, "\n"))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err = cmd.Run()
	if ctx.Err() != nil {
		t.Fatalf("graft diff %v did not finish within 10s", args)
	}
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) {
		t.Fatalf("graft diff %v: %v", args, err)
	}
	out := stderr.String()
	if len(out) > 400 {
		out = out[:400] + "..."
	}
	return cmd.ProcessState.ExitCode(), out
}
