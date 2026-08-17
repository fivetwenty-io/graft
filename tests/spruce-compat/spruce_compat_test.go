// Package sprucecompat wraps the spruce/graft golden-output parity
// harness (run.sh) so it can be invoked via `go test ./tests/spruce-compat/...`
// alongside the rest of the suite, in addition to standalone bash
// invocation. All comparison logic lives in run.sh and lib/harness.sh —
// this file only shells out and surfaces the harness's own PASS/FAIL/SKIP
// report as the test's failure output.
package sprucecompat

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// TestSpruceCompatParity runs the full 16-genesis-pattern parity harness
// against whatever spruce/graft binaries it resolves (see run.sh's own
// resolution rules and env overrides: GRAFT_BIN, SPRUCE_BIN, SPRUCE_REPO).
// It fails if the harness reports any FAIL. If no spruce binary can be
// found or built, the harness itself exits 0 with a SKIP message, and
// this test passes (matching the CI graceful-skip requirement) while
// echoing that message via t.Log so it is visible in test output.
func TestSpruceCompatParity(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not found on PATH; the parity harness requires bash (genesis's own shell), not sh")
	}

	cmd := exec.CommandContext(t.Context(), "bash", "run.sh")
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	output := out.String()

	if strings.HasPrefix(output, "SKIP: spruce binary not found") {
		t.Log(output)
		t.Skip("spruce binary unavailable; harness skipped gracefully (see output above)")
		return
	}

	if err != nil {
		t.Fatalf("spruce/graft parity harness failed:\n%s", output)
	}

	t.Log(output)
}
