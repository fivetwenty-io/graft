package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/config"
	"github.com/fivetwenty-io/graft/log"
)

// cacheCmdRun invokes a cache subcommand handler with stdout/stderr
// captured through the production hooks.
func cacheCmdRun(t *testing.T, fn func(config.CacheConfig) int, cfg config.CacheConfig) (stdout, stderr string, rc int) {
	t.Helper()
	origOut, origErr := printStdOutf, log.PrintStdErrf
	defer func() { printStdOutf, log.PrintStdErrf = origOut, origErr }()
	printStdOutf = func(format string, args ...interface{}) {
		stdout += fmt.Sprintf(format, args...)
	}
	log.PrintStdErrf = func(format string, args ...interface{}) {
		stderr += fmt.Sprintf(format, args...)
	}
	rc = fn(cfg)
	return stdout, stderr, rc
}

// populateCache runs one cacheable merge so both layers have an entry.
func populateCache(t *testing.T, cacheDir string) {
	t.Helper()
	dir := t.TempDir()
	doc := writeDoc(t, dir, "a.yml", "a: 1\n")
	_, stderr, rc := cacheTestRun(t, cacheDir, &mergeOpts{Files: []string{doc}})
	if rc != 0 {
		t.Fatalf("populate merge rc = %d, stderr: %s", rc, stderr)
	}
}

func TestCacheStatsReportsBothLayers(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "cache")
	populateCache(t, cacheDir)

	out, stderr, rc := cacheCmdRun(t, handleCacheStats, config.CacheConfig{L2Path: cacheDir})
	if rc != 0 {
		t.Fatalf("rc = %d, stderr: %s", rc, stderr)
	}
	if !strings.Contains(out, cacheDir) {
		t.Errorf("stats output missing cache directory:\n%s", out)
	}
	if !strings.Contains(out, "output: 1 ") || !strings.Contains(out, "parse: 1 ") {
		t.Errorf("stats output missing per-layer counts:\n%s", out)
	}
}

// TestCacheStatsEmptyDoesNotCreateDirectory: inspecting a cache that was
// never written must not create its directory.
func TestCacheStatsEmptyDoesNotCreateDirectory(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "cache")

	out, _, rc := cacheCmdRun(t, handleCacheStats, config.CacheConfig{L2Path: cacheDir})
	if rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	if !strings.Contains(out, "output: 0 ") || !strings.Contains(out, "parse: 0 ") {
		t.Errorf("empty stats should report zero entries:\n%s", out)
	}
	if _, err := os.Stat(cacheDir); !os.IsNotExist(err) {
		t.Fatal("stats created the cache directory")
	}
}

func TestCacheClearRemovesAllEntries(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "cache")
	populateCache(t, cacheDir)

	_, stderr, rc := cacheCmdRun(t, handleCacheClear, config.CacheConfig{L2Path: cacheDir})
	if rc != 0 {
		t.Fatalf("clear rc = %d, stderr: %s", rc, stderr)
	}

	for _, layer := range []string{"output", "parse"} {
		entries, err := os.ReadDir(filepath.Join(cacheDir, layer))
		if err != nil {
			continue // layer directory gone entirely is fine too
		}
		if len(entries) != 0 {
			t.Errorf("%s layer still has %d entries after clear", layer, len(entries))
		}
	}
}

// TestCacheClearMissingDirectorySucceeds: clearing a cache that was
// never written is a no-op, not an error.
func TestCacheClearMissingDirectorySucceeds(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "cache")
	_, _, rc := cacheCmdRun(t, handleCacheClear, config.CacheConfig{L2Path: cacheDir})
	if rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	if _, err := os.Stat(cacheDir); !os.IsNotExist(err) {
		t.Fatal("clear created the cache directory")
	}
}
