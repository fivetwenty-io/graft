package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/fivetwenty-io/graft/internal/cache"
	"github.com/fivetwenty-io/graft/internal/config"
	"github.com/fivetwenty-io/graft/internal/utils/ansi"
	"github.com/fivetwenty-io/graft/log"
)

// cacheTestRun invokes handleMerge with the output cache rooted at
// cacheDir, capturing stdout/stderr through the same hooks production
// writes through.
func cacheTestRun(t *testing.T, cacheDir string, opts *mergeOpts) (stdout, stderr string, rc int) {
	t.Helper()
	ansi.Color(false)

	origOut, origErr := printStdOutf, log.PrintStdErrf
	defer func() { printStdOutf, log.PrintStdErrf = origOut, origErr }()
	printStdOutf = func(format string, args ...interface{}) {
		stdout += fmt.Sprintf(format, args...)
	}
	log.PrintStdErrf = func(format string, args ...interface{}) {
		stderr += fmt.Sprintf(format, args...)
	}

	opts.CacheCfg = config.CacheConfig{L2Enabled: true, L2Path: cacheDir}
	rc = handleMerge(opts)
	return stdout, stderr, rc
}

// outputStore opens the same store handleMerge uses for cacheDir so tests
// can count and poison entries.
func outputStore(t *testing.T, cacheDir string) *cache.FileStore {
	t.Helper()
	store, err := cache.OpenFileStore(filepath.Join(cacheDir, "output"), 0)
	if err != nil {
		t.Fatalf("OpenFileStore: %v", err)
	}
	return store
}

func writeDoc(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// TestMergeCacheHitReplaysStoredBytes proves the second run is served
// from the cache: after the first run the stored entry is poisoned with
// sentinel bytes, and the second run must emit exactly those bytes.
func TestMergeCacheHitReplaysStoredBytes(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	doc := writeDoc(t, dir, "a.yml", "meta:\n  name: thing\nout: (( grab meta.name ))\n")

	out1, err1, rc := cacheTestRun(t, cacheDir, &mergeOpts{Files: []string{doc}})
	if rc != 0 {
		t.Fatalf("first run rc = %d, stderr: %s", rc, err1)
	}
	if out1 == "" {
		t.Fatal("first run produced no output")
	}

	store := outputStore(t, cacheDir)
	count, _, _ := store.Stats()
	if count != 1 {
		t.Fatalf("expected 1 cached entry after a pure merge, found %d", count)
	}

	// Poison the stored entry under the exact key the CLI derives.
	inputs := docs("meta:\n  name: thing\nout: (( grab meta.name ))\n")
	key := mergeOutputCacheKey(&mergeOpts{Files: []string{doc}}, inputs, false)
	poisoned, encErr := encodeCachedOutput(cachedMergeOutput{Stdout: []byte("poison: true\n")})
	if encErr != nil {
		t.Fatalf("encodeCachedOutput: %v", encErr)
	}
	if err := store.Put(key, poisoned); err != nil {
		t.Fatalf("Put: %v", err)
	}

	out2, _, rc2 := cacheTestRun(t, cacheDir, &mergeOpts{Files: []string{doc}})
	if rc2 != 0 {
		t.Fatalf("second run rc = %d", rc2)
	}
	if out2 != "poison: true\n" {
		t.Fatalf("second run was not served from the cache: %q", out2)
	}
}

// TestMergeCacheNeverReplaysPreDashDashDashPrefixEntry is the
// cache-replay-safety regression for the "---\n" merge-output-prefix fix
// (renderMergedTree): an entry stored under the key a pre-fix (v1
// schema) graft binary would have derived for these exact opts/inputs -
// bare YAML, no leading "---\n" - must never be served as a hit by the
// current binary. mergeOutputCacheKeySchemaVersionBumped (merge_cache_
// test.go) proves the two keys differ in isolation; this proves that
// difference actually prevents the stale bytes from reaching stdout
// end-to-end, through the real store/handleMerge path.
func TestMergeCacheNeverReplaysPreDashDashDashPrefixEntry(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	docContent := "meta:\n  name: thing\nout: (( grab meta.name ))\n"
	doc := writeDoc(t, dir, "a.yml", docContent)

	// Simulate a v1-binary-populated cache: store the bare-YAML (no
	// "---\n") bytes a pre-fix graft would have produced, under the key
	// a pre-fix graft would have derived for it.
	store := outputStore(t, cacheDir)
	legacyKey := legacyV1MergeOutputCacheKey(&mergeOpts{Files: []string{doc}}, docs(docContent), false)
	legacyEntry, encErr := encodeCachedOutput(cachedMergeOutput{Stdout: []byte("meta:\n  name: thing\nout: thing\n")})
	if encErr != nil {
		t.Fatalf("encodeCachedOutput: %v", encErr)
	}
	if err := store.Put(legacyKey, legacyEntry); err != nil {
		t.Fatalf("Put: %v", err)
	}

	out, stderr, rc := cacheTestRun(t, cacheDir, &mergeOpts{Files: []string{doc}})
	if rc != 0 {
		t.Fatalf("rc = %d, stderr: %s", rc, stderr)
	}
	if out == "meta:\n  name: thing\nout: thing\n" {
		t.Fatal("replayed a pre-\"---\\n\"-prefix (v1) cache entry instead of running a fresh merge")
	}
	want := "---\nmeta:\n  name: thing\nout: thing\n\n"
	if out != want {
		t.Fatalf("stdout = %q, want %q (fresh merge, v1 entry ignored)", out, want)
	}
}

// TestMergeCacheMissAndHitAreByteIdentical is the drop-in guarantee for
// the cache itself: cold and warm runs must produce identical bytes.
func TestMergeCacheMissAndHitAreByteIdentical(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	base := writeDoc(t, dir, "base.yml", "meta:\n  a: 1\nlist:\n- name: x\n  v: 1\n")
	over := writeDoc(t, dir, "over.yml", "list:\n- name: x\n  v: 2\nout: (( grab meta.a ))\n")

	mk := func() *mergeOpts { return &mergeOpts{Files: []string{base, over}} }
	out1, err1, rc1 := cacheTestRun(t, cacheDir, mk())
	out2, err2, rc2 := cacheTestRun(t, cacheDir, mk())

	if rc1 != 0 || rc2 != 0 {
		t.Fatalf("rc = %d, %d; stderr: %q, %q", rc1, rc2, err1, err2)
	}
	if out1 != out2 {
		t.Fatalf("stdout differs between miss and hit:\nmiss: %q\nhit:  %q", out1, out2)
	}
	if err1 != err2 {
		t.Fatalf("stderr differs between miss and hit:\nmiss: %q\nhit:  %q", err1, err2)
	}
}

// TestMergeCacheReplaysWarnings: deterministic merge warnings land on
// stderr on the cold run and must replay identically on the warm run.
func TestMergeCacheReplaysWarnings(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	// A hash-valued "name" key defeats key-merge and produces the
	// deterministic fallback warning.
	base := writeDoc(t, dir, "base.yml", "jobs:\n- name:\n    nested: true\n  v: 1\n")
	over := writeDoc(t, dir, "over.yml", "jobs:\n- name:\n    nested: true\n  v: 2\n")

	mk := func() *mergeOpts { return &mergeOpts{Files: []string{base, over}} }
	out1, err1, rc1 := cacheTestRun(t, cacheDir, mk())
	if rc1 != 0 {
		t.Fatalf("first run rc = %d, stderr: %s", rc1, err1)
	}
	if err1 == "" {
		t.Fatal("expected a fallback warning on stderr; test premise broken")
	}

	out2, err2, rc2 := cacheTestRun(t, cacheDir, mk())
	if rc2 != 0 {
		t.Fatalf("second run rc = %d", rc2)
	}
	if out2 != out1 || err2 != err1 {
		t.Fatalf("warm run diverged:\nstdout %q vs %q\nstderr %q vs %q", out2, out1, err2, err1)
	}
}

// TestMergeCacheImpureInvocationNotStored: an invocation containing an
// impure operator must never be written to the cache, even on success.
func TestMergeCacheImpureInvocationNotStored(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	doc := writeDoc(t, dir, "a.yml", "meta:\n  list:\n  - only\nout: (( shuffle meta.list ))\n")

	_, stderr, rc := cacheTestRun(t, cacheDir, &mergeOpts{Files: []string{doc}})
	if rc != 0 {
		t.Fatalf("rc = %d, stderr: %s", rc, stderr)
	}

	count, _, _ := outputStore(t, cacheDir).Stats()
	if count != 0 {
		t.Fatalf("impure invocation was stored: %d entries", count)
	}
}

// TestMergeCacheSkipEvalStoresInertOperators: with --skip-eval a vault
// call is an inert string, so the invocation is cacheable.
func TestMergeCacheSkipEvalStoresInertOperators(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	doc := writeDoc(t, dir, "a.yml", "secret: (( vault \"a/b:c\" ))\n")

	out1, stderr, rc := cacheTestRun(t, cacheDir, &mergeOpts{Files: []string{doc}, SkipEval: true})
	if rc != 0 {
		t.Fatalf("rc = %d, stderr: %s", rc, stderr)
	}
	count, _, _ := outputStore(t, cacheDir).Stats()
	if count != 1 {
		t.Fatalf("skip-eval invocation not stored: %d entries", count)
	}

	out2, _, rc2 := cacheTestRun(t, cacheDir, &mergeOpts{Files: []string{doc}, SkipEval: true})
	if rc2 != 0 || out2 != out1 {
		t.Fatalf("warm skip-eval run diverged: rc %d, %q vs %q", rc2, out2, out1)
	}
}

// TestMergeCacheFailureNotStored: a failed merge must not be cached.
func TestMergeCacheFailureNotStored(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	doc := writeDoc(t, dir, "a.yml", "out: (( grab missing.path ))\n")

	_, _, rc := cacheTestRun(t, cacheDir, &mergeOpts{Files: []string{doc}})
	if rc == 0 {
		t.Fatal("expected the merge to fail; test premise broken")
	}
	count, _, _ := outputStore(t, cacheDir).Stats()
	if count != 0 {
		t.Fatalf("failed merge was stored: %d entries", count)
	}
}

// TestMergeCacheEditInvalidates: changing one input's bytes must miss.
func TestMergeCacheEditInvalidates(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	doc := writeDoc(t, dir, "a.yml", "a: 1\n")

	out1, _, rc1 := cacheTestRun(t, cacheDir, &mergeOpts{Files: []string{doc}})
	if rc1 != 0 {
		t.Fatalf("rc1 = %d", rc1)
	}
	writeDoc(t, dir, "a.yml", "a: 2\n")
	out2, _, rc2 := cacheTestRun(t, cacheDir, &mergeOpts{Files: []string{doc}})
	if rc2 != 0 {
		t.Fatalf("rc2 = %d", rc2)
	}
	if out1 == out2 {
		t.Fatal("edited input served stale cached output")
	}
}

// TestMergeCacheDebugBypassesCache: -D runs must never read or write the
// cache - their diagnostics have to come from a real merge.
func TestMergeCacheDebugBypassesCache(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	doc := writeDoc(t, dir, "a.yml", "a: 1\n")

	log.DebugOn = true
	defer func() { log.DebugOn = false }()

	_, _, rc := cacheTestRun(t, cacheDir, &mergeOpts{Files: []string{doc}})
	if rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, "output")); !os.IsNotExist(err) {
		t.Fatal("debug run touched the cache directory")
	}
}

// TestMergeCacheDisabledConfigTouchesNothing: with L2Enabled false no
// cache directory may be created at all.
func TestMergeCacheDisabledConfigTouchesNothing(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	doc := writeDoc(t, dir, "a.yml", "a: 1\n")

	ansi.Color(false)
	origOut, origErr := printStdOutf, log.PrintStdErrf
	defer func() { printStdOutf, log.PrintStdErrf = origOut, origErr }()
	printStdOutf = func(string, ...interface{}) {}
	log.PrintStdErrf = func(string, ...interface{}) {}

	opts := &mergeOpts{Files: []string{doc}, CacheCfg: config.CacheConfig{L2Enabled: false, L2Path: cacheDir}}
	if rc := handleMerge(opts); rc != 0 {
		t.Fatalf("rc = %d", rc)
	}
	if _, err := os.Stat(cacheDir); !os.IsNotExist(err) {
		t.Fatal("disabled cache created its directory")
	}
}

// TestMergeCacheCorruptEntryFallsBack: a corrupt cache entry must be
// treated as a miss, not an error.
func TestMergeCacheCorruptEntryFallsBack(t *testing.T) {
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "cache")
	docContent := "a: 1\n"
	doc := writeDoc(t, dir, "a.yml", docContent)

	out1, _, rc1 := cacheTestRun(t, cacheDir, &mergeOpts{Files: []string{doc}})
	if rc1 != 0 {
		t.Fatalf("rc1 = %d", rc1)
	}

	key := mergeOutputCacheKey(&mergeOpts{Files: []string{doc}}, docs(docContent), false)
	if err := outputStore(t, cacheDir).Put(key, []byte("not gob at all")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	out2, _, rc2 := cacheTestRun(t, cacheDir, &mergeOpts{Files: []string{doc}})
	if rc2 != 0 {
		t.Fatalf("rc2 = %d", rc2)
	}
	if out2 != out1 {
		t.Fatalf("corrupt entry did not fall back to a real merge: %q vs %q", out2, out1)
	}
}
