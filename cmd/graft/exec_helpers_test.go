package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
)

// execResult is what a process wrote and how it ended.
type execResult struct {
	Stdout, Stderr string
	Code           int
}

var (
	graftBuildOnce sync.Once
	graftBuildDir  string
	graftBuildPath string
	graftBuildErr  error
)

// buildGraftBinary builds ./cmd/graft once per test binary into a shared
// temporary directory, which removeGraftBinary deletes.
func buildGraftBinary(t *testing.T) string {
	t.Helper()
	graftBuildOnce.Do(func() {
		graftBuildDir, graftBuildErr = os.MkdirTemp("", "graft-alias-test-")
		if graftBuildErr != nil {
			return
		}
		name := "graft"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		graftBuildPath = filepath.Join(graftBuildDir, name)
		cmd := exec.CommandContext(context.Background(), "go", "build", "-o", graftBuildPath, ".")
		if out, err := cmd.CombinedOutput(); err != nil {
			graftBuildErr = errors.New("go build: " + err.Error() + "\n" + string(out))
		}
	})
	if graftBuildErr != nil {
		t.Fatal(graftBuildErr)
	}
	return graftBuildPath
}

// removeGraftBinary deletes the directory buildGraftBinary made.
func removeGraftBinary() {
	if graftBuildDir != "" {
		_ = os.RemoveAll(graftBuildDir)
	}
}

// linkAs makes a symlink to graft under name in a new temporary
// directory and returns its path, so a run through it has name as argv[0].
func linkAs(t *testing.T, graft, name string) string {
	t.Helper()
	link := filepath.Join(t.TempDir(), name)
	if err := os.Symlink(graft, link); err != nil {
		t.Fatal(err)
	}
	return link
}

// execIn runs bin with args in dir, feeds it stdin, and returns what it
// wrote. The environment is fixed so that nothing on the machine, such as
// a color setting or a user config, changes the output.
func execIn(t *testing.T, dir, bin string, args []string, stdin string) execResult {
	t.Helper()
	cmd := exec.CommandContext(context.Background(), bin, args...)
	cmd.Dir = dir
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"HOME=" + t.TempDir(),
		"NO_COLOR=1",
		"TERM=dumb",
		"LC_ALL=C",
	}
	cmd.Stdin = bytes.NewBufferString(stdin)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	res := execResult{Stdout: stdout.String(), Stderr: stderr.String()}
	var ee *exec.ExitError
	switch {
	case errors.As(err, &ee):
		res.Code = ee.ExitCode()
	case err != nil:
		t.Fatal(err)
	}
	return res
}
