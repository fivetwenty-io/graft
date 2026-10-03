package graft

import (
	"context"
	"runtime"
	"strings"
	"testing"
)

// maxRecursionText is the text CheckForCycles reports for a merged tree
// deeper than 4,096 levels, the same text spruce prints for any merge
// deeper than that.
const maxRecursionText = "Hit max recursion depth. You seem to have a self-referencing dataset"

// nested returns a document whose key a holds n balanced flow lists.
func nested(n int) string {
	return "a: " + strings.Repeat("[", n) + strings.Repeat("]", n) + "\n"
}

// guardedMerge parses src, merges it, and checks the merged tree for
// cycles at 4,096 levels the way `graft merge` does, returning the first
// error any step gives.
func guardedMerge(t *testing.T, src string) (Document, error) {
	t.Helper()
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	doc, err := engine.ParseYAML([]byte(src))
	if err != nil {
		return nil, err
	}
	out, err := engine.Merge(context.Background(), doc).Execute()
	if err != nil {
		return nil, err
	}
	if err := CheckForCycles(out.RawData(), 4096); err != nil {
		return nil, err
	}
	return out, nil
}

// TestMergeRejectsSelfContainingAnchor merges documents whose alias sits
// inside the collection its anchor names. spruce fails them with
// "anchor 'a' value contains itself". goccy decodes the alias as null,
// so graft used to print a null in its place and exit 0.
func TestMergeRejectsSelfContainingAnchor(t *testing.T) {
	for _, c := range []struct{ name, in string }{
		{"mapping", "a: &a\n  b: *a\n"},
		{"sequence", "a: &a [*a]\n"},
		{"nested", "a: &a\n  b:\n    c: [1, {d: *a}]\n"},
		{"merge key", "a: &a\n  <<: *a\n  b: 1\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			_, err := guardedMerge(t, c.in)
			if err == nil || !strings.Contains(err.Error(), "anchor 'a' value contains itself") {
				t.Errorf("merge of %q = %v, want an error naming anchor 'a' value contains itself", c.in, err)
			}
		})
	}
}

// TestMergeAcceptsAliasesOutsideTheirAnchor checks that the guard leaves
// ordinary aliases alone. An alias after its anchor's collection closes
// is no cycle, and an anchor name binds to its latest definition, so an
// alias to a redefined name inside the first definition is no cycle
// either.
func TestMergeAcceptsAliasesOutsideTheirAnchor(t *testing.T) {
	for _, c := range []struct{ name, in string }{
		{"sibling", "a: &a {x: 1}\nb: *a\n"},
		{"redefined inside", "a: &x [&x 1, *x]\n"},
		{"anchored key", "a: {&k key: 1}\nb: *k\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := guardedMerge(t, c.in); err != nil {
				t.Errorf("merge of %q = %v, want success", c.in, err)
			}
		})
	}
}

// TestMergeDepthBoundaries pins the depth at which a merge fails. spruce
// and graft both merge 4,095 levels and fail from 4,096 with the
// recursion text. From 9,998 levels graft used to fail with goccy's
// "exceeded max depth" instead, so the depth guard reports the
// recursion text, and every over-deep merge fails with one message.
func TestMergeDepthBoundaries(t *testing.T) {
	if _, err := guardedMerge(t, nested(4095)); err != nil {
		t.Errorf("merge of 4,095 levels = %v, want success", err)
	}
	for _, n := range []int{4096, 9997, 10001} {
		_, err := guardedMerge(t, nested(n))
		if err == nil || !strings.Contains(err.Error(), maxRecursionText) || strings.Contains(err.Error(), "exceeded max depth") {
			t.Errorf("merge of %d levels = %v, want the error %q", n, err, maxRecursionText)
		}
	}
}

// TestParseYAMLRejectsUnclosedBracketsCheaply feeds ParseYAML 20,000
// unclosed "[". goccy's parser took 0.23 seconds and a 724 MB peak on
// that before failing, so the depth guard has to fail it before goccy
// sees it.
func TestParseYAMLRejectsUnclosedBracketsCheaply(t *testing.T) {
	const limit = 100 << 20
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	src := []byte("a: " + strings.Repeat("[", 20000) + "\n")
	var before, after runtime.MemStats
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&before)
	_, err = engine.ParseYAML(src)
	runtime.ReadMemStats(&after)
	if err == nil || !strings.Contains(err.Error(), maxRecursionText) {
		t.Errorf("ParseYAML = %v, want the error %q", err, maxRecursionText)
	}
	if grew := after.TotalAlloc - before.TotalAlloc; grew >= limit {
		t.Errorf("ParseYAML allocated %d MB, want under %d MB", grew>>20, limit>>20)
	}
}

// TestMergeIgnoresLaterDocuments checks that both guards read only the
// first document. spruce and graft's merge never read a later document,
// so a cycle or deep nesting there still merges.
func TestMergeIgnoresLaterDocuments(t *testing.T) {
	for _, c := range []struct{ name, second string }{
		{"cycle", "a: &a\n  b: *a\n"},
		{"10,001 levels", nested(10001)},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, err := guardedMerge(t, "x: 1\n---\n"+c.second)
			if err != nil {
				t.Fatalf("merge = %v, want success", err)
			}
			if got, _ := out.GetInt("x"); got != 1 {
				t.Errorf("x = %v, want 1", got)
			}
		})
	}
}

// TestGoPatchParsersRejectDeepNesting feeds DetectArrayRoot and
// ParseGoPatch an array root nested 10,001 levels deep. Neither goes
// through ParseYAML, so each runs the depth guard on the bytes it
// receives, and fails with the text an over-deep merge gives.
func TestGoPatchParsersRejectDeepNesting(t *testing.T) {
	src := []byte(strings.Repeat("[", 10001) + strings.Repeat("]", 10001) + "\n")
	for _, c := range []struct {
		name  string
		parse func([]byte) error
	}{
		{"DetectArrayRoot", DetectArrayRoot},
		{"ParseGoPatch", func(b []byte) error { _, err := ParseGoPatch(b); return err }},
	} {
		t.Run(c.name, func(t *testing.T) {
			err := c.parse(src)
			if err == nil || IsArrayError(err) || !strings.Contains(err.Error(), maxRecursionText) {
				t.Errorf("%s = %v, want the error %q", c.name, err, maxRecursionText)
			}
		})
	}
}

// TestJSONRejectsDeepNesting checks that `graft json` runs the depth
// guard before goccy parses its input. It keeps yaml.v3's depth error
// rather than the merge's recursion text, because spruce's json reports
// a depth error too.
func TestJSONRejectsDeepNesting(t *testing.T) {
	_, err := jsonifyData([]byte(nested(10001)), false)
	if err == nil || !strings.Contains(err.Error(), "yaml: exceeded max depth of 10000") {
		t.Errorf("jsonifyData = %v, want yaml: exceeded max depth of 10000", err)
	}
}
