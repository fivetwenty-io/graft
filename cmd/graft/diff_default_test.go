package main

import (
	"os"
	"strings"
	"testing"
)

const casesDir = "../../tests/diff-parity/cases/"

func withStdoutTerminal(t *testing.T, capable bool, width int) {
	t.Helper()
	prevCapable, prevWidth := isStdoutColorCapable, stdoutWidth
	isStdoutColorCapable = func() bool { return capable }
	stdoutWidth = func() int { return width }
	t.Cleanup(func() { isStdoutColorCapable, stdoutWidth = prevCapable, prevWidth })
}

func golden(t *testing.T, name, mode string) string {
	t.Helper()
	data, err := os.ReadFile(casesDir + name + "/want/" + mode)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestDiffAutoColorFollowsResolveColor(t *testing.T) {
	from, to := casesDir+"map-and-scalars/from.yml", casesDir+"map-and-scalars/to.yml"
	t.Setenv("COLORTERM", "truecolor")
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("NO_COLOR", "")
	withStdoutTerminal(t, true, 80)

	stdout, _, rc := runMainCaptured(t, "diff", from, to)
	if rc != 1 || stdout != golden(t, "map-and-scalars", "truecolor-80") {
		t.Fatalf("auto mode on a color-capable stdout: rc=%d, stdout differs from truecolor-80", rc)
	}

	stdout, _, _ = runMainCaptured(t, "diff", "--no-color", from, to)
	if stdout != golden(t, "map-and-scalars", "plain-80") {
		t.Fatal("--no-color must print the plain report")
	}

	t.Setenv("NO_COLOR", "1")
	stdout, _, _ = runMainCaptured(t, "diff", from, to)
	if stdout != golden(t, "map-and-scalars", "plain-80") {
		t.Fatal("NO_COLOR must disable color in auto mode")
	}

	t.Setenv("NO_COLOR", "")
	t.Setenv("COLORTERM", "")
	stdout, _, _ = runMainCaptured(t, "diff", from, to)
	if stdout != golden(t, "map-and-scalars", "ansi16-80") {
		t.Fatal("without COLORTERM the report must use the 16-color palette")
	}

	t.Setenv("COLORTERM", "truecolor")
	t.Setenv("TERM", "dumb")
	stdout, _, _ = runMainCaptured(t, "diff", from, to)
	if stdout != golden(t, "map-and-scalars", "plain-80") {
		t.Fatal("TERM=dumb must disable color in auto mode even with COLORTERM=truecolor")
	}
}

func TestDiffForcedColorBeatsNoColorInPipe(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	t.Setenv("COLORTERM", "truecolor")
	withStdoutTerminal(t, false, 80)
	stdout, stderr, rc := runMainCaptured(t, "diff", "--color=on", casesDir+"map-and-scalars/from.yml", casesDir+"map-and-scalars/to.yml")
	if rc != 1 || stderr != "" || stdout != golden(t, "map-and-scalars", "truecolor-80") {
		t.Fatalf("--color=on into a pipe: rc=%d stderr=%q, stdout differs from truecolor-80", rc, stderr)
	}
}

func TestDiffWidthZeroStacksBlocks(t *testing.T) {
	withStdoutTerminal(t, false, 0)
	stdout, _, _ := runMainCaptured(t, "diff", casesDir+"map-and-scalars/from.yml", casesDir+"map-and-scalars/to.yml")
	if stdout != golden(t, "map-and-scalars", "plain-0") {
		t.Fatal("width 0 must stack blocks exactly as spruce does under fake_tty")
	}
}

func TestDiffPartialReportExitsOne(t *testing.T) {
	withStdoutTerminal(t, false, 80)
	stdout, stderr, rc := runMainCaptured(t, "diff", casesDir+"binary-invalid/from.yml", casesDir+"binary-invalid/to.yml")
	if rc != 1 || stderr != "" || stdout != golden(t, "binary-invalid", "plain-80") {
		t.Fatalf("rc=%d stderr=%q stdout=%q, want the partial report, exit 1, and no stderr", rc, stderr, stdout)
	}
}

func TestDiffReadsStdinAndDirectories(t *testing.T) {
	withStdoutTerminal(t, false, 80)
	restore := setStdinFromFile(t, "../../assets/merge/first.yml")
	defer restore()
	stdout, stderr, rc := runMainCaptured(t, "diff", "-", "../../assets/merge/first.yml")
	if rc != 0 || stderr != "" || stdout != "\n\n" {
		t.Fatalf("stdin against the same file: rc=%d stderr=%q stdout=%q", rc, stderr, stdout)
	}
	dir := "../../internal/yamldiff/testdata/load/inputs/dirA"
	stdout, _, rc = runMainCaptured(t, "diff", dir, dir)
	if rc != 0 || stdout != "\n\n" {
		t.Fatalf("a directory against itself: rc=%d stdout=%q", rc, stdout)
	}
}

func TestDiffLoadErrorStylesLocation(t *testing.T) {
	t.Setenv("COLORTERM", "truecolor")
	withStdoutTerminal(t, false, 80)
	_, stderr, rc := runMainCaptured(t, "diff", "--color=on", "/nonexistent/graft/nosuch.yml", "../../assets/merge/first.yml")
	if rc != 2 || !strings.HasPrefix(stderr, "unable to load data from \x1b[4;38;2;100;149;237m/nonexistent/graft/nosuch.yml\x1b[0m: ") {
		t.Fatalf("rc=%d stderr=%q, want a styled URI location", rc, stderr)
	}
	_, stderr, _ = runMainCaptured(t, "diff", "/nonexistent/graft/nosuch.yml", "../../assets/merge/first.yml")
	if !strings.HasPrefix(stderr, "unable to load data from /nonexistent/graft/nosuch.yml: Get \"/nonexistent/graft/nosuch.yml\"") {
		t.Fatalf("stderr=%q, want ytbx's plain message", stderr)
	}
}

func TestDiffQuietPrintsNothing(t *testing.T) {
	withStdoutTerminal(t, true, 80)
	stdout, stderr, rc := runMainCaptured(t, "diff", "--quiet", casesDir+"map-and-scalars/from.yml", casesDir+"map-and-scalars/to.yml")
	if rc != 1 || stdout != "" || stderr != "" {
		t.Fatalf("--quiet: rc=%d stdout=%q stderr=%q", rc, stdout, stderr)
	}
}
