package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/cache"
	"github.com/fivetwenty-io/graft/log"
)

// parseStore opens the same per-document parse store handleMerge uses
// for cacheDir so tests can count and poison entries.
func parseStore(t *testing.T, cacheDir string) *cache.FileStore {
	t.Helper()
	store, err := cache.OpenFileStore(filepath.Join(cacheDir, "parse"), 0)
	if err != nil {
		t.Fatalf("OpenFileStore: %v", err)
	}
	return store
}

// TestParseCacheHitServesStoredTree proves the second run's documents
// come from the parse cache. The invocation contains shuffle, so Layer 1
// (the output cache) never participates and cannot mask Layer 2: after
// the first run the stored tree for the document is poisoned, and the
// second run's output must reflect the poisoned tree.
func TestParseCacheHitServesStoredTree(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	content := "name: original\nmeta:\n  list:\n  - only\nout: (( shuffle meta.list ))\n"
	doc := writeDoc(t, dir, "a.yml", content)

	out1, stderr, rc := cacheTestRun(t, cacheDir, &mergeOpts{Files: []string{doc}})
	if rc != 0 {
		t.Fatalf("first run rc = %d, stderr: %s", rc, stderr)
	}
	if !strings.Contains(out1, "name: original") {
		t.Fatalf("first run output missing expected value: %q", out1)
	}

	store := parseStore(t, cacheDir)
	count, _, _ := store.Stats()
	if count != 1 {
		t.Fatalf("expected 1 parse cache entry, found %d", count)
	}

	poisonTree := map[string]interface{}{
		"name": "poisoned",
		"meta": map[string]interface{}{
			"list": []interface{}{"only"},
		},
		"out": "(( shuffle meta.list ))",
	}
	encoded, err := encodeCachedTree(poisonTree)
	if err != nil {
		t.Fatalf("encodeCachedTree: %v", err)
	}
	if err := store.Put(parseCacheKey([]byte(content)), encoded); err != nil {
		t.Fatalf("Put: %v", err)
	}

	out2, _, rc2 := cacheTestRun(t, cacheDir, &mergeOpts{Files: []string{doc}})
	if rc2 != 0 {
		t.Fatalf("second run rc = %d", rc2)
	}
	if !strings.Contains(out2, "name: poisoned") {
		t.Fatalf("second run did not use the parse cache: %q", out2)
	}
}

// TestParseCacheControlFlowNeverCached: a document with control-flow
// markers evaluates operators during parse, so it must be neither stored
// nor served from the parse cache.
func TestParseCacheControlFlowNeverCached(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	doc := writeDoc(t, dir, "a.yml",
		"meta:\n  on: true\n(( if meta.on ))\nname: x\n(( fi ))\n")

	_, stderr, rc := cacheTestRun(t, cacheDir, &mergeOpts{Files: []string{doc}})
	if rc != 0 {
		t.Fatalf("rc = %d, stderr: %s", rc, stderr)
	}

	count, _, _ := parseStore(t, cacheDir).Stats()
	if count != 0 {
		t.Fatalf("control-flow document was parse-cached: %d entries", count)
	}
}

// TestParseCacheCorruptEntryFallsBack: a corrupt parse entry is a miss;
// the real parse runs and the merge output is unaffected.
func TestParseCacheCorruptEntryFallsBack(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	content := "name: original\nout: (( shuffle name ))\n"
	doc := writeDoc(t, dir, "a.yml", content)

	out1, stderr, rc := cacheTestRun(t, cacheDir, &mergeOpts{Files: []string{doc}})
	if rc != 0 {
		t.Fatalf("first run rc = %d, stderr: %s", rc, stderr)
	}

	store := parseStore(t, cacheDir)
	if err := store.Put(parseCacheKey([]byte(content)), []byte("not gob at all")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	out2, _, rc2 := cacheTestRun(t, cacheDir, &mergeOpts{Files: []string{doc}})
	if rc2 != 0 {
		t.Fatalf("second run rc = %d", rc2)
	}
	if out2 != out1 {
		t.Fatalf("corrupt parse entry changed output:\nfirst:  %q\nsecond: %q", out2, out1)
	}
}

// TestParseCacheDebugBypasses: -D runs must not create any cache
// directory, parse cache included.
func TestParseCacheDebugBypasses(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	doc := writeDoc(t, dir, "a.yml", "a: 1\n")

	log.DebugOn = true
	defer func() { log.DebugOn = false }()

	_, _, rc := cacheTestRun(t, cacheDir, &mergeOpts{Files: []string{doc}})
	if rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	if _, err := os.Stat(cacheDir); !os.IsNotExist(err) {
		t.Fatal("debug run created a cache directory")
	}
}
