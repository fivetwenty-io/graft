package tree

import (
	"errors"
	"sort"
	"testing"
)

// cursorStrings extracts string representations from a slice of cursors.
func cursorStrings(cursors []*Cursor) []string {
	result := make([]string, len(cursors))
	for i, c := range cursors {
		result[i] = c.String()
	}
	sort.Strings(result)
	return result
}

// --- Glob wildcard on maps ---

func TestGlobWildcardOnMap(t *testing.T) {
	data := map[string]interface{}{
		"a": "val_a",
		"b": "val_b",
		"c": "val_c",
	}
	c, _ := ParseCursor("*")
	cursors, err := c.Glob(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	paths := cursorStrings(cursors)
	if len(paths) != 3 {
		t.Errorf("got %d paths, want 3: %v", len(paths), paths)
	}
	// All of a, b, c should be present
	found := map[string]bool{}
	for _, p := range paths {
		found[p] = true
	}
	for _, key := range []string{"a", "b", "c"} {
		if !found[key] {
			t.Errorf("expected path %q in results %v", key, paths)
		}
	}
}

// --- Glob wildcard on list ---

func TestGlobWildcardOnList(t *testing.T) {
	data := map[string]interface{}{
		"items": []interface{}{"x", "y", "z"},
	}
	c, _ := ParseCursor("items.*")
	cursors, err := c.Glob(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	paths := cursorStrings(cursors)
	if len(paths) != 3 {
		t.Errorf("got %d paths, want 3: %v", len(paths), paths)
	}
	expected := []string{"items.0", "items.1", "items.2"}
	sort.Strings(expected)
	for i, p := range paths {
		if p != expected[i] {
			t.Errorf("paths[%d] = %q, want %q", i, p, expected[i])
		}
	}
}

// --- Glob wildcard on nested map ---

func TestGlobWildcardNestedMap(t *testing.T) {
	data := map[string]interface{}{
		"jobs": map[string]interface{}{
			"web": map[string]interface{}{
				"instances": 2,
			},
			"worker": map[string]interface{}{
				"instances": 4,
			},
		},
	}
	c, _ := ParseCursor("jobs.*.instances")
	cursors, err := c.Glob(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	paths := cursorStrings(cursors)
	if len(paths) != 2 {
		t.Errorf("got %d paths, want 2: %v", len(paths), paths)
	}
	found := map[string]bool{}
	for _, p := range paths {
		found[p] = true
	}
	for _, key := range []string{"jobs.web.instances", "jobs.worker.instances"} {
		if !found[key] {
			t.Errorf("expected path %q in results %v", key, paths)
		}
	}
}

// --- Glob non-wildcard (specific path) ---

func TestGlobNonWildcardSimplePath(t *testing.T) {
	data := map[string]interface{}{
		"a": map[string]interface{}{
			"b": "hello",
		},
	}
	c, _ := ParseCursor("a.b")
	cursors, err := c.Glob(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cursors) != 1 {
		t.Fatalf("got %d cursors, want 1", len(cursors))
	}
	if cursors[0].String() != "a.b" {
		t.Errorf("got %q, want a.b", cursors[0].String())
	}
}

func TestGlobNonWildcardListByIndex(t *testing.T) {
	data := map[string]interface{}{
		"list": []interface{}{"first", "second"},
	}
	c, _ := ParseCursor("list.0")
	cursors, err := c.Glob(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cursors) != 1 {
		t.Fatalf("got %d cursors, want 1", len(cursors))
	}
	if cursors[0].String() != "list.0" {
		t.Errorf("got %q, want list.0", cursors[0].String())
	}
}

func TestGlobNonWildcardListByNameField(t *testing.T) {
	data := map[string]interface{}{
		"jobs": []interface{}{
			map[string]interface{}{"name": "web", "instances": 3},
			map[string]interface{}{"name": "worker", "instances": 1},
		},
	}
	c, _ := ParseCursor("jobs.web")
	cursors, err := c.Glob(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cursors) != 1 {
		t.Fatalf("got %d cursors, want 1", len(cursors))
	}
	if cursors[0].String() != "jobs.web" {
		t.Errorf("got %q, want jobs.web", cursors[0].String())
	}
}

// --- Glob error cases ---

func TestGlobNotFound(t *testing.T) {
	data := map[string]interface{}{
		"a": "val",
	}
	c, _ := ParseCursor("nonexistent")
	_, err := c.Glob(data)
	if err == nil {
		t.Fatal("expected NotFoundError, got nil")
	}
	var notFound NotFoundError
	if !errors.As(err, &notFound) {
		t.Errorf("expected NotFoundError, got %T: %v", err, err)
	}
}

func TestGlobTypeMismatchOnScalar(t *testing.T) {
	data := map[string]interface{}{
		"scalar": "value",
	}
	c, _ := ParseCursor("scalar.*")
	_, err := c.Glob(data)
	if err == nil {
		t.Fatal("expected TypeMismatchError, got nil")
	}
	var typeMismatch TypeMismatchError
	if !errors.As(err, &typeMismatch) {
		t.Errorf("expected TypeMismatchError, got %T: %v", err, err)
	}
}

func TestGlobListIndexOutOfRange(t *testing.T) {
	data := map[string]interface{}{
		"list": []interface{}{"a", "b"},
	}
	c, _ := ParseCursor("list.99")
	_, err := c.Glob(data)
	if err == nil {
		t.Fatal("expected NotFoundError, got nil")
	}
	var notFound NotFoundError
	if !errors.As(err, &notFound) {
		t.Errorf("expected NotFoundError, got %T: %v", err, err)
	}
}

func TestGlobMapKeyNotFound(t *testing.T) {
	data := map[string]interface{}{
		"a": map[string]interface{}{
			"b": "val",
		},
	}
	c, _ := ParseCursor("a.nosuchkey")
	_, err := c.Glob(data)
	if err == nil {
		t.Fatal("expected NotFoundError, got nil")
	}
	var notFound NotFoundError
	if !errors.As(err, &notFound) {
		t.Errorf("expected NotFoundError, got %T: %v", err, err)
	}
}

// --- Glob wildcard skips NotFound sub-paths ---

func TestGlobWildcardSkipsMissingSubPaths(t *testing.T) {
	// When globbing *.value, some entries may not have "value" — those are skipped
	data := map[string]interface{}{
		"jobs": map[string]interface{}{
			"web":    map[string]interface{}{"instances": 2},
			"worker": map[string]interface{}{"value": "found"},
		},
	}
	c, _ := ParseCursor("jobs.*.value")
	cursors, err := c.Glob(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cursors) != 1 {
		t.Errorf("got %d cursors, want 1: %v", len(cursors), cursorStrings(cursors))
	}
	if len(cursors) > 0 && cursors[0].String() != "jobs.worker.value" {
		t.Errorf("got %q, want jobs.worker.value", cursors[0].String())
	}
}
