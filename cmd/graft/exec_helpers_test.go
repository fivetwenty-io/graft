package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
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

// TestMain pins the zone the diff goldens were rendered in, points HOME at
// an empty directory and clears the Vault and safe variables so no test
// reaches the developer's own Vault, runs the package's tests, and then
// removes the binary buildGraftBinary built for
// them, if any test asked for one. Earlier tests leak cleanup tickers
// whose goroutines call time.Now, which reads time.Local, so a write made
// from inside a test (as yamlgolden.PinLocal does) races with them. Only a
// write made before any test starts is race-free.
func TestMain(m *testing.M) {
	time.Local = time.FixedZone("UTC-4", -4*60*60)
	_, cleanup := isolateVaultEnv()
	code := m.Run()
	cleanup()
	if graftBuildDir != "" {
		_ = os.RemoveAll(graftBuildDir)
	}
	os.Exit(code)
}

// isolateVaultEnv gives the test binary an empty HOME, so ~/.saferc,
// ~/.svtoken and ~/.vault-token cannot be found, and unsets the variables
// that name a Vault target or token. The Go caches are pinned to their
// real locations first, because go build in a test would otherwise fall
// back to the empty HOME and rebuild the world.
func isolateVaultEnv() (string, func()) {
	for _, key := range []string{"GOCACHE", "GOMODCACHE"} {
		if os.Getenv(key) != "" {
			continue
		}
		if out, err := exec.CommandContext(context.Background(), "go", "env", key).Output(); err == nil {
			_ = os.Setenv(key, strings.TrimSpace(string(out)))
		}
	}
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "VAULT_") || strings.HasPrefix(name, "SAFE_") {
			_ = os.Unsetenv(name)
		}
	}
	home, err := os.MkdirTemp("", "graft-home-")
	if err != nil {
		return "", func() {}
	}
	_ = os.Setenv("HOME", home)
	return home, func() { _ = os.RemoveAll(home) }
}

// buildGraftBinary builds ./cmd/graft once per test run, into a temporary
// directory that TestMain removes at the end, and returns its path. A run
// that never calls it pays nothing.
func buildGraftBinary(t *testing.T) string {
	t.Helper()
	graftBuildOnce.Do(func() {
		dir, err := os.MkdirTemp("", "graft-alias-test-")
		if err != nil {
			graftBuildErr = err
			return
		}
		graftBuildDir = dir
		name := "graft"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		path := filepath.Join(dir, name)
		cmd := exec.CommandContext(context.Background(), "go", "build", "-o", path, ".")
		if out, err := cmd.CombinedOutput(); err != nil {
			graftBuildErr = errors.New("go build: " + err.Error() + "\n" + string(out))
			return
		}
		graftBuildPath = path
	})
	if graftBuildErr != nil {
		t.Fatal(graftBuildErr)
	}
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
