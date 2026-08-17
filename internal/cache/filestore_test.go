package cache

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestFileStorePutGet(t *testing.T) {
	store, err := OpenFileStore(filepath.Join(t.TempDir(), "cache"), 0)
	if err != nil {
		t.Fatalf("OpenFileStore: %v", err)
	}

	if _, ok := store.Get("absent"); ok {
		t.Fatal("Get on an empty store must miss")
	}

	want := []byte("merged: output\n")
	if err := store.Put("some key", want); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, ok := store.Get("some key")
	if !ok {
		t.Fatal("Get after Put must hit")
	}
	if string(got) != string(want) {
		t.Fatalf("Get returned %q, want %q", got, want)
	}
}

// TestFileStoreKeyIsNotAPath: keys are canonical strings, not filenames -
// separators, dots, and other filesystem-hostile characters must be safe
// and must never escape the store directory.
func TestFileStoreKeyIsNotAPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	store, err := OpenFileStore(dir, 0)
	if err != nil {
		t.Fatalf("OpenFileStore: %v", err)
	}

	key := "../../etc/passwd\x00weird/key\nwith newlines"
	if err := store.Put(key, []byte("v")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if got, ok := store.Get(key); !ok || string(got) != "v" {
		t.Fatalf("Get = %q, %v; want \"v\", true", got, ok)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 entry inside the store dir, found %d", len(entries))
	}
}

func TestFileStoreDistinctKeys(t *testing.T) {
	store, err := OpenFileStore(filepath.Join(t.TempDir(), "cache"), 0)
	if err != nil {
		t.Fatalf("OpenFileStore: %v", err)
	}
	if err := store.Put("a", []byte("va")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Put("b", []byte("vb")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if got, _ := store.Get("a"); string(got) != "va" {
		t.Fatalf("key a returned %q", got)
	}
	if got, _ := store.Get("b"); string(got) != "vb" {
		t.Fatalf("key b returned %q", got)
	}
}

// TestFileStoreTTLExpiry: an entry older than the store TTL is a miss and
// is removed from disk on lookup.
func TestFileStoreTTLExpiry(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	store, err := OpenFileStore(dir, time.Hour)
	if err != nil {
		t.Fatalf("OpenFileStore: %v", err)
	}
	if err := store.Put("k", []byte("v")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	// Fresh entry hits.
	if _, ok := store.Get("k"); !ok {
		t.Fatal("fresh entry must hit")
	}

	// Backdate the entry beyond the TTL.
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("ReadDir: %v (%d entries)", err, len(entries))
	}
	old := time.Now().Add(-2 * time.Hour)
	path := filepath.Join(dir, entries[0].Name())
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	if _, ok := store.Get("k"); ok {
		t.Fatal("expired entry must miss")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("expired entry must be removed on lookup")
	}
}

func TestFileStoreZeroTTLNeverExpires(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache")
	store, err := OpenFileStore(dir, 0)
	if err != nil {
		t.Fatalf("OpenFileStore: %v", err)
	}
	if err := store.Put("k", []byte("v")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	entries, _ := os.ReadDir(dir)
	old := time.Now().Add(-1000 * time.Hour)
	path := filepath.Join(dir, entries[0].Name())
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}
	if _, ok := store.Get("k"); !ok {
		t.Fatal("zero-TTL entry must never expire")
	}
}

func TestFileStoreOverwrite(t *testing.T) {
	store, err := OpenFileStore(filepath.Join(t.TempDir(), "cache"), 0)
	if err != nil {
		t.Fatalf("OpenFileStore: %v", err)
	}
	if err := store.Put("k", []byte("first")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Put("k", []byte("second")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if got, _ := store.Get("k"); string(got) != "second" {
		t.Fatalf("Get after overwrite = %q, want \"second\"", got)
	}
}

// TestFileStoreConcurrentPutGet: concurrent writers and readers on the
// same key must not error, corrupt data, or race (run under -race).
func TestFileStoreConcurrentPutGet(t *testing.T) {
	store, err := OpenFileStore(filepath.Join(t.TempDir(), "cache"), 0)
	if err != nil {
		t.Fatalf("OpenFileStore: %v", err)
	}

	var wg sync.WaitGroup
	for w := 0; w < 4; w++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				_ = store.Put("shared", []byte("same content every time"))
			}
		}()
		go func() {
			defer wg.Done()
			for i := 0; i < 50; i++ {
				if data, ok := store.Get("shared"); ok && string(data) != "same content every time" {
					t.Error("reader observed torn write")
					return
				}
			}
		}()
	}
	wg.Wait()
}

func TestFileStoreClearAndStats(t *testing.T) {
	store, err := OpenFileStore(filepath.Join(t.TempDir(), "cache"), 0)
	if err != nil {
		t.Fatalf("OpenFileStore: %v", err)
	}
	if err := store.Put("a", []byte("1234")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if err := store.Put("b", []byte("12345678")); err != nil {
		t.Fatalf("Put: %v", err)
	}

	count, bytes, err := store.Stats()
	if err != nil {
		t.Fatalf("Stats: %v", err)
	}
	if count != 2 {
		t.Fatalf("Stats count = %d, want 2", count)
	}
	if bytes != 12 {
		t.Fatalf("Stats bytes = %d, want 12", bytes)
	}

	if err := store.Clear(); err != nil {
		t.Fatalf("Clear: %v", err)
	}
	count, bytes, err = store.Stats()
	if err != nil {
		t.Fatalf("Stats after Clear: %v", err)
	}
	if count != 0 || bytes != 0 {
		t.Fatalf("Stats after Clear = %d entries, %d bytes; want 0, 0", count, bytes)
	}
	if _, ok := store.Get("a"); ok {
		t.Fatal("Get after Clear must miss")
	}
}
