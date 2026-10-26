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
	graftBuildMu   sync.Mutex
	graftBuildRefs int
	graftBuildDir  string
	graftBuildPath string
)

// buildGraftBinary builds ./cmd/graft into a temporary directory shared by
// every test that holds it, and returns its path. The first holder builds
// it and the last one to finish removes it, so the binary stays valid for
// every caller, under -count too. The package's TestMain is an ordinary
// test, so there is no process-wide teardown to hang the removal on.
func buildGraftBinary(t *testing.T) string {
	t.Helper()
	graftBuildMu.Lock()
	defer graftBuildMu.Unlock()
	if graftBuildRefs == 0 {
		dir, err := os.MkdirTemp("", "graft-alias-test-")
		if err != nil {
			t.Fatal(err)
		}
		name := "graft"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		path := filepath.Join(dir, name)
		cmd := exec.CommandContext(context.Background(), "go", "build", "-o", path, ".")
		if out, err := cmd.CombinedOutput(); err != nil {
			_ = os.RemoveAll(dir)
			t.Fatal(errors.New("go build: " + err.Error() + "\n" + string(out)))
		}
		graftBuildDir, graftBuildPath = dir, path
	}
	graftBuildRefs++
	t.Cleanup(func() {
		graftBuildMu.Lock()
		defer graftBuildMu.Unlock()
		graftBuildRefs--
		if graftBuildRefs == 0 {
			_ = os.RemoveAll(graftBuildDir)
		}
	})
	return graftBuildPath
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
	return execInEnv(t, nil, dir, bin, args, stdin)
}

// execInEnv is execIn with extra environment entries, each NAME=value,
// added after the fixed ones.
func execInEnv(t *testing.T, env []string, dir, bin string, args []string, stdin string) execResult {
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
	cmd.Env = append(cmd.Env, env...)
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
