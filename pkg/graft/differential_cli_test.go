// Package graft_test's differential suite proves engine.MergeFiles(...).
// Execute() (the library merge path) and `graft merge` (the CLI's own
// mergeAllDocs path) agree byte-for-byte on real fixtures, compiled from
// the actual cmd/graft sources rather than re-implemented. This is C10's
// strongest guarantee that the two paths stay in sync, and it is meant to
// stay in the suite permanently (not a throwaway regression check for the
// initial C10 landing).
package graft_test

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// moduleRoot returns the repository root (the directory containing
// go.mod and cmd/graft), derived from this test file's own compile-time
// path rather than the process's working directory, so it is correct
// regardless of where `go test` happens to be invoked from.
func moduleRoot(t *testing.T) string {
	t.Helper()
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed; cannot locate module root")
	}
	// This file lives at <root>/pkg/graft/differential_cli_test.go.
	return filepath.Join(filepath.Dir(thisFile), "..", "..")
}

var (
	graftBinaryOnce sync.Once
	graftBinaryPath string
	graftBinaryErr  error
)

// buildGraftBinary compiles cmd/graft exactly once per test binary run
// (cached across every test in this file/package) and returns the path
// to the resulting executable. Building the real binary - rather than
// invoking cmd/graft's internal functions in-process - is deliberate: it
// is the only way to compare against the exact bytes a `graft merge`
// user would see, including main()'s own stdout formatting.
func buildGraftBinary(t *testing.T) string {
	t.Helper()

	root := moduleRoot(t)
	graftBinaryOnce.Do(func() {
		dir, err := os.MkdirTemp("", "graft-differential-bin-*")
		if err != nil {
			graftBinaryErr = fmt.Errorf("creating temp dir for graft binary: %w", err)
			return
		}

		binPath := filepath.Join(dir, "graft")
		if runtime.GOOS == "windows" {
			binPath += ".exe"
		}

		// context.Background, not t.Context: the binary is built once and
		// reused by every later test in this package.
		cmd := exec.CommandContext(context.Background(), "go", "build", "-o", binPath, "./cmd/graft")
		cmd.Dir = root
		out, buildErr := cmd.CombinedOutput()
		if buildErr != nil {
			graftBinaryErr = fmt.Errorf("go build ./cmd/graft: %w\n%s", buildErr, out)
			return
		}

		graftBinaryPath = binPath
	})

	if graftBinaryErr != nil {
		t.Fatalf("building graft CLI binary: %v", graftBinaryErr)
	}
	return graftBinaryPath
}

// runGraftMerge runs the compiled graft binary's `merge` subcommand
// against paths and returns its stdout. It fails the test if the process
// exits non-zero.
func runGraftMerge(t *testing.T, bin string, goPatch bool, paths ...string) string {
	t.Helper()

	args := []string{"merge"}
	if goPatch {
		args = append(args, "--go-patch")
	}
	args = append(args, paths...)

	cmd := exec.CommandContext(t.Context(), bin, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("%s %v: %v\nstderr:\n%s", bin, args, err, stderr.String())
	}
	return stdout.String()
}

// libraryMerge runs the same merge through engine.MergeFiles(...).
// Execute() and serializes the result exactly as handleMerge
// (cmd/graft/main.go) serializes its own CLI result: via
// graft.MarshalYAML on the raw merged map, plus the trailing newline
// printStdOutf("%s\n", ...) adds.
func libraryMerge(t *testing.T, paths ...string) string {
	t.Helper()

	engine := graft.NewDefaultEngine()
	result, err := engine.MergeFiles(context.Background(), paths...).Execute()
	if err != nil {
		t.Fatalf("engine.MergeFiles(%v).Execute() error = %v", paths, err)
	}

	out, err := graft.MarshalYAML(result.RawData())
	if err != nil {
		t.Fatalf("graft.MarshalYAML() error = %v", err)
	}
	return string(out) + "\n"
}

// TestMergeFilesDifferential_CLIParity is the permanent differential
// guarantee described in the package doc comment above: for each fixture
// pair, the library's engine.MergeFiles(...).Execute() output must be
// byte-identical to `graft merge`'s own stdout.
//
// JSON-extension dispatch (ParseFile treating a ".json" path via
// ParseJSON rather than ParseYAML) is intentionally NOT exercised here:
// the CLI's own mergeAllDocs never calls ParseJSON for merge inputs (it
// always parses via YAML, which happens to accept JSON as a subset), so
// there is no CLI behavior to be byte-identical *to* for that path. See
// TestParseFile_JSONExtension (parse_file_test.go) for JSON-dispatch
// coverage on its own terms.
func TestMergeFilesDifferential_CLIParity(t *testing.T) {
	bin := buildGraftBinary(t)
	fixtures := filepath.Join(moduleRoot(t), "tests", "spruce-compat", "fixtures")

	cases := []struct {
		name    string
		files   []string
		goPatch bool
	}{
		{name: "base+override", files: []string{"base.yml", "override.yml"}},
		{
			name:    "gopatch-base+gopatch-ops",
			files:   []string{"gopatch-base.yml", "gopatch-ops.yml"},
			goPatch: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			paths := make([]string, len(tc.files))
			for i, f := range tc.files {
				paths[i] = filepath.Join(fixtures, f)
			}

			cliOutput := runGraftMerge(t, bin, tc.goPatch, paths...)
			libOutput := libraryMerge(t, paths...)

			if cliOutput != libOutput {
				t.Errorf("CLI and library merge output diverge for %v\n--- CLI (%d bytes) ---\n%s\n--- library (%d bytes) ---\n%s",
					tc.files, len(cliOutput), cliOutput, len(libOutput), libOutput)
			}
		})
	}
}
