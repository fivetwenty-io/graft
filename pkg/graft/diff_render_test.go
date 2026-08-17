package graft

import (
	"bytes"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// updateGolden regenerates every *.golden fixture under
// pkg/graft/testdata/diff/ from the current renderer output instead of
// comparing against it. This is the first -update flag/golden-file
// convention in this package (see c2-notes.md): run
//
//	go test ./pkg/graft/... -run TestDiffRenderersGolden -update
//
// after an intentional renderer output change, inspect the diff of the
// resulting testdata/diff/*.golden files, and commit them alongside the
// change.
var updateGolden = flag.Bool("update", false, "update pkg/graft/testdata/diff golden files")

func goldenPath(name string) string {
	return filepath.Join("testdata", "diff", name+".golden")
}

// assertGolden compares got against testdata/diff/<name>.golden, or (with
// -update) overwrites the golden file with got.
func assertGolden(t *testing.T, name, got string) {
	t.Helper()

	path := goldenPath(name)
	if *updateGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
			t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(got), 0o600); err != nil {
			t.Fatalf("write golden file %s: %v", path, err)
		}
		return
	}

	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden file %s: %v (run with -update to create it)", path, err)
	}
	if got != string(want) {
		t.Errorf("golden mismatch for %s (run with -update to refresh if intentional)\n--- got ---\n%s\n--- want ---\n%s", name, got, string(want))
	}
}

// diffRenderFixture returns an (a, b) Document pair exercising every
// change kind the flattener produces: a map field modified (meta.version),
// a top-level key added/removed, a keyed list with one entry added, one
// removed, and one modified, a simple list with a positional modify plus
// a tail add, and a scalar-to-map type change (flag).
func diffRenderFixture() (Document, Document) {
	a := map[string]interface{}{
		"meta":        map[string]interface{}{"name": "orig", "version": 1},
		"removed_key": "gone",
		"servers": []interface{}{
			map[string]interface{}{"name": "web", "port": 80},
			map[string]interface{}{"name": "db", "port": 5432},
		},
		"tags": []interface{}{"a", "b", "c"},
		"flag": "yes",
	}
	b := map[string]interface{}{
		"meta":      map[string]interface{}{"name": "orig", "version": 2},
		"added_key": "new",
		"servers": []interface{}{
			map[string]interface{}{"name": "web", "port": 8080},
			map[string]interface{}{"name": "cache", "port": 6379},
		},
		"tags": []interface{}{"a", "x", "c", "d"},
		"flag": map[string]interface{}{"nested": true},
	}
	return NewDocument(a), NewDocument(b)
}

func TestDiffRenderersGolden(t *testing.T) {
	a, b := diffRenderFixture()
	result, err := DiffDocuments(a, b, nil)
	if err != nil {
		t.Fatalf("DiffDocuments: unexpected error: %v", err)
	}

	renderers := map[string]func(io.Writer, *DiffOptions) error{
		"changelist": result.WriteChangeList,
		"unified":    result.WriteUnified,
		"sidebyside": result.WriteSideBySide,
		"mergetree":  result.WriteMergeTree,
	}

	opts := DefaultDiffOptions()
	for name, render := range renderers {
		t.Run(name, func(t *testing.T) {
			var buf bytes.Buffer
			if err := render(&buf, opts); err != nil {
				t.Fatalf("render: unexpected error: %v", err)
			}
			assertGolden(t, name, buf.String())
		})
	}
}

func TestDiffRenderersGoldenColorAndShowTypes(t *testing.T) {
	a, b := diffRenderFixture()
	result, err := DiffDocuments(a, b, nil)
	if err != nil {
		t.Fatalf("DiffDocuments: unexpected error: %v", err)
	}

	opts := DefaultDiffOptions()
	opts.Color = true
	opts.ShowTypes = true

	var buf bytes.Buffer
	if err := result.WriteChangeList(&buf, opts); err != nil {
		t.Fatalf("WriteChangeList: unexpected error: %v", err)
	}
	assertGolden(t, "changelist_color_showtypes", buf.String())
}

func TestDiffRenderersOmitHeader(t *testing.T) {
	a, b := diffRenderFixture()
	result, err := DiffDocuments(a, b, nil)
	if err != nil {
		t.Fatalf("DiffDocuments: unexpected error: %v", err)
	}

	opts := DefaultDiffOptions()
	opts.OmitHeader = true

	var buf bytes.Buffer
	if err := result.WriteChangeList(&buf, opts); err != nil {
		t.Fatalf("WriteChangeList: unexpected error: %v", err)
	}
	if strings.Contains(buf.String(), "changes detected") {
		t.Errorf("OmitHeader did not suppress the header:\n%s", buf.String())
	}
}

func TestDiffRenderersEmptyResult(t *testing.T) {
	a := NewDocument(map[string]interface{}{"a": 1})
	result, err := DiffDocuments(a, a, nil)
	if err != nil {
		t.Fatalf("DiffDocuments: unexpected error: %v", err)
	}

	renderers := []func(io.Writer, *DiffOptions) error{
		result.WriteChangeList, result.WriteUnified, result.WriteSideBySide, result.WriteMergeTree,
	}
	for _, render := range renderers {
		var buf bytes.Buffer
		if err := render(&buf, nil); err != nil {
			t.Fatalf("render: unexpected error: %v", err)
		}
		if !strings.Contains(buf.String(), "no changes detected") {
			t.Errorf("expected \"no changes detected\" header for an empty result, got:\n%s", buf.String())
		}
	}
}

func TestWriteUnifiedContextTruncates(t *testing.T) {
	a := NewDocument(map[string]interface{}{
		"config": map[string]interface{}{"a": 1, "b": 2, "c": 3, "d": 4, "e": 5},
	})
	b := NewDocument(map[string]interface{}{})

	result, err := DiffDocuments(a, b, nil)
	if err != nil {
		t.Fatalf("DiffDocuments: unexpected error: %v", err)
	}

	opts := DefaultDiffOptions()
	opts.Context = 1

	var buf bytes.Buffer
	if err := result.WriteUnified(&buf, opts); err != nil {
		t.Fatalf("WriteUnified: unexpected error: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "more lines") {
		t.Errorf("expected a truncation marker with Context=1 against a 5-line value, got:\n%s", out)
	}

	// Context == 0 (DefaultDiffOptions()'s own value) must show every line
	// rather than truncating.
	var full bytes.Buffer
	if err := result.WriteUnified(&full, DefaultDiffOptions()); err != nil {
		t.Fatalf("WriteUnified: unexpected error: %v", err)
	}
	if strings.Contains(full.String(), "more lines") {
		t.Errorf("Context=0 should not truncate, got:\n%s", full.String())
	}
}

func TestWriteSideBySideRespectsWidth(t *testing.T) {
	a := NewDocument(map[string]interface{}{"name": "a-very-long-original-value-here"})
	b := NewDocument(map[string]interface{}{"name": "a-very-long-replacement-value-here"})

	result, err := DiffDocuments(a, b, nil)
	if err != nil {
		t.Fatalf("DiffDocuments: unexpected error: %v", err)
	}

	opts := DefaultDiffOptions()
	opts.Width = 20

	var buf bytes.Buffer
	if err := result.WriteSideBySide(&buf, opts); err != nil {
		t.Fatalf("WriteSideBySide: unexpected error: %v", err)
	}

	for _, line := range strings.Split(buf.String(), "\n") {
		if !strings.Contains(line, " | ") {
			continue // header/path lines aren't column rows
		}
		if len(line) > 40 {
			t.Errorf("row exceeds a sane bound for Width=20: %q (len=%d)", line, len(line))
		}
	}
}

func TestColorizeOnlyAppliesWhenRequested(t *testing.T) {
	a := NewDocument(map[string]interface{}{"a": 1})
	b := NewDocument(map[string]interface{}{"a": 2})

	result, err := DiffDocuments(a, b, nil)
	if err != nil {
		t.Fatalf("DiffDocuments: unexpected error: %v", err)
	}

	var plain bytes.Buffer
	if err := result.WriteChangeList(&plain, DefaultDiffOptions()); err != nil {
		t.Fatalf("WriteChangeList: unexpected error: %v", err)
	}
	if strings.Contains(plain.String(), "\033[") {
		t.Errorf("Color: false must not emit ANSI escapes, got:\n%q", plain.String())
	}

	opts := DefaultDiffOptions()
	opts.Color = true
	var colored bytes.Buffer
	if err := result.WriteChangeList(&colored, opts); err != nil {
		t.Fatalf("WriteChangeList: unexpected error: %v", err)
	}
	if !strings.Contains(colored.String(), "\033[") {
		t.Errorf("Color: true should emit ANSI escapes, got:\n%q", colored.String())
	}
}
