package graft

import (
	"context"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

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
	if doc == nil {
		// A blank or null document merges as {}, as the CLI merges it.
		doc = NewDocument(map[string]interface{}{})
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
		{"after a directive", "%YAML 1.1\n---\na: &a\n  b: *a\n"},
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
// either. goccy decodes that alias as null where spruce reads the inner
// definition, a known divergence the redefined cases pin.
func TestMergeAcceptsAliasesOutsideTheirAnchor(t *testing.T) {
	type m = map[string]interface{}
	for _, c := range []struct {
		name, in string
		want     m
	}{
		{"sibling", "a: &a {x: 1}\nb: *a\n", m{"a": m{"x": 1}, "b": m{"x": 1}}},
		{"redefined inside", "a: &x [&x 1, *x]\n", m{"a": []interface{}{1, nil}}},               // spruce gives [1, 1]
		{"redefined inside a mapping", "a: &x {b: &x 1, c: *x}\n", m{"a": m{"b": 1, "c": nil}}}, // spruce gives c: 1
		{"anchored key", "a: {&k key: 1}\nb: *k\n", m{"a": m{"key": 1}, "b": "key"}},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, err := guardedMerge(t, c.in)
			if err != nil {
				t.Fatalf("merge of %q = %v, want success", c.in, err)
			}
			if got := out.RawData(); !reflect.DeepEqual(got, c.want) {
				t.Errorf("merge of %q = %#v, want %#v", c.in, got, c.want)
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
		if err == nil || !strings.Contains(err.Error(), maxRecursionMessage) || strings.Contains(err.Error(), "exceeded max depth") {
			t.Errorf("merge of %d levels = %v, want the error %q", n, err, maxRecursionMessage)
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
	if err == nil || !strings.Contains(err.Error(), maxRecursionMessage) {
		t.Errorf("ParseYAML = %v, want the error %q", err, maxRecursionMessage)
	}
	if grew := after.TotalAlloc - before.TotalAlloc; grew >= limit {
		t.Errorf("ParseYAML allocated %d MB, want under %d MB", grew>>20, limit>>20)
	}
}

// TestMergeRejectsDeepNestingInAStreamItCannotCut merges a first
// document that goccy ends at "...#c", a line that is no marker line,
// followed by 10,001 unclosed "[". The merge cannot cut the stream
// there, so goccy parses every document, and the depth guard has to
// cover them all. At 9027f95 the guard checked only the first document,
// and goccy's parser took tens of gigabytes on 200,000 "[" before it
// failed with a syntax error.
func TestMergeRejectsDeepNestingInAStreamItCannotCut(t *testing.T) {
	unclosed := strings.Repeat("[", 10001) + "\n"
	for _, c := range []struct{ name, in string }{
		{"no marker line", "a: 1\n...#c\n" + unclosed},
		{"cut not proved", "a: 1\n...#c\n---\n" + unclosed},
	} {
		t.Run(c.name, func(t *testing.T) {
			var before, after runtime.MemStats
			runtime.GC()
			runtime.GC()
			runtime.ReadMemStats(&before)
			_, err := guardedMerge(t, c.in)
			runtime.ReadMemStats(&after)
			if err == nil || !strings.Contains(err.Error(), maxRecursionMessage) {
				t.Errorf("merge = %v, want the error %q", err, maxRecursionMessage)
			}
			if grew := after.TotalAlloc - before.TotalAlloc; grew >= 100<<20 {
				t.Errorf("merge allocated %d MB, want under 100 MB", grew>>20)
			}
		})
	}
}

// TestMergeIgnoresLaterDocuments merges streams whose second document
// spruce never reads, so a cycle, nesting past 10,000 levels, or a syntax
// error there leaves the merge to succeed with the first document alone.
// At d86cd74, goccy parsed every document, so the syntax error failed
// the merge and the deep document cost its parser hundreds of megabytes.
// The first document ends at a "---" or a "..." whatever comes before
// it and whatever line breaks the input uses.
func TestMergeIgnoresLaterDocuments(t *testing.T) {
	for _, c := range []struct{ name, in string }{
		{"cycle", "x: 1\n---\na: &a\n  b: *a\n"},
		{"10,001 levels", "x: 1\n---\n" + nested(10001)},
		{"syntax error", "x: 1\n---\ny: [\n"},
		{"CRLF line breaks", "x: 1\r\n---\r\ny: [\r\n"},
		{"document end marker", "x: 1\n...\n---\ny: [\n"},
		{"leading header", "---\nx: 1\n---\ny: [\n"},
		{"comment-only preamble", "# one\n\n# two\n---\nx: 1\n---\ny: [\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var before, after runtime.MemStats
			runtime.GC()
			runtime.GC()
			runtime.ReadMemStats(&before)
			out, err := guardedMerge(t, c.in)
			runtime.ReadMemStats(&after)
			if err != nil {
				t.Fatalf("merge = %v, want success", err)
			}
			if got := out.RawData(); !reflect.DeepEqual(got, map[string]interface{}{"x": 1}) {
				t.Errorf("merge = %#v, want x: 1 alone", got)
			}
			if grew := after.TotalAlloc - before.TotalAlloc; grew >= 100<<20 {
				t.Errorf("merge allocated %d MB, want under 100 MB", grew>>20)
			}
		})
	}
}

// TestMergeKeepsDirectivesWithFirstDocument merges streams that open
// with a directive. The directives before the first "---" belong to the
// document it starts, so spruce merges that document's content. goccy
// gives the directives a document of their own, and graft merged that
// empty document as {} and exited 0. spruce v1.35.17 panics on a %TAG
// for the primary handle "!", so only the named handle is its output.
func TestMergeKeepsDirectivesWithFirstDocument(t *testing.T) {
	for _, c := range []struct{ name, in string }{
		{"YAML directive", "%YAML 1.1\n---\nx: 1\n"},
		{"TAG directive", "%TAG ! tag:example.com,2000:\n---\nx: 1\n"},
		{"TAG directive for a named handle", "%TAG !e! tag:example.com,2000:\n---\nx: 1\n"},
		{"content on the header line", "%YAML 1.1\n--- {x: 1}\n"},
		{"second document", "%YAML 1.1\n---\nx: 1\n---\ny: 2\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, err := guardedMerge(t, c.in)
			if err != nil {
				t.Fatalf("merge = %v, want success", err)
			}
			if got := out.RawData(); !reflect.DeepEqual(got, map[string]interface{}{"x": 1}) {
				t.Errorf("merge = %#v, want x: 1 alone", got)
			}
		})
	}
}

// TestMergeKeepsEmptyDocumentAfterDirective merges streams whose first
// document, the one a directive opens, is empty or null. spruce merges
// that document as {} and never reads the x: 1 after it, so the merge
// must not skip the empty document the way yaml.Unmarshal does. The
// merge cuts the stream after the first document, so each case also
// hands ParseYAML11CompatAware the whole stream, which graft json does.
// A "~" on the header line followed by a key is a syntax error, and
// spruce fails it too.
func TestMergeKeepsEmptyDocumentAfterDirective(t *testing.T) {
	for _, c := range []struct{ name, in string }{
		{"empty document", "%YAML 1.1\n---\n---\nx: 1\n"},
		{"null document", "%YAML 1.1\n--- ~\n---\nx: 1\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got, err := ParseYAML11CompatAware([]byte(c.in)); got != nil || err != nil {
				t.Errorf("ParseYAML11CompatAware = %#v, %v; want nil, nil", got, err)
			}
			out, err := guardedMerge(t, c.in)
			if err != nil {
				t.Fatalf("merge = %v, want success", err)
			}
			if got := out.RawData(); !reflect.DeepEqual(got, map[string]interface{}{}) {
				t.Errorf("merge = %#v, want {}", got)
			}
		})
	}
	t.Run("null header with a key after it", func(t *testing.T) {
		if _, err := guardedMerge(t, "%YAML 1.1\n--- ~\nx: 1\n"); err == nil {
			t.Error("merge succeeded, want a syntax error")
		}
	})
}

// TestMergeStripsLeadingBOM merges streams that open with a UTF-8 byte
// order mark. spruce strips it and merges x: 1. graft kept it, so it
// became part of the first key, and before a "---" or a comment it
// failed the parse.
func TestMergeStripsLeadingBOM(t *testing.T) {
	for _, c := range []struct{ name, in string }{
		{"before a key", "\xEF\xBB\xBFx: 1\n"},
		{"before a header", "\xEF\xBB\xBF---\nx: 1\n"},
		{"before a comment", "\xEF\xBB\xBF# c\n---\nx: 1\n"},
		{"before a directive", "\xEF\xBB\xBF%YAML 1.1\n---\nx: 1\n"},
		{"before a header and a second document", "\xEF\xBB\xBF---\nx: 1\n---\ny: [\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, err := guardedMerge(t, c.in)
			if err != nil {
				t.Fatalf("merge = %v, want success", err)
			}
			if got := out.RawData(); !reflect.DeepEqual(got, map[string]interface{}{"x": 1}) {
				t.Errorf("merge = %#v, want x: 1 alone", got)
			}
		})
	}
}

// TestMergeKeepsMarkersInsideScalars merges a first document holding a
// "---" line inside a literal block scalar and inside a quoted
// multi-line string. Neither line ends the document, so the keys after
// it survive, and only the "---" at column 1 cuts the stream.
func TestMergeKeepsMarkersInsideScalars(t *testing.T) {
	for _, c := range []struct {
		name, in string
		want     interface{}
	}{
		{"literal block scalar", "a: |\n  ---\n  b\nx: 1\n---\ny: [\n", "---\nb\n"},
		{"quoted multi-line string", "a: \"b\n  ---\n  c\"\nx: 1\n---\ny: [\n", "b --- c"},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, err := guardedMerge(t, c.in)
			if err != nil {
				t.Fatalf("merge = %v, want success", err)
			}
			want := map[string]interface{}{"a": c.want, "x": 1}
			if got := out.RawData(); !reflect.DeepEqual(got, want) {
				t.Errorf("merge = %#v, want %#v", got, want)
			}
		})
	}
}

// TestMergeKeepsKeysThatStartWithThreeDots merges documents with a key
// at column 1 that starts with "..." and a character other than a blank.
// spruce v1.35.17 keeps each key. goccy reads the "..." as the end of
// the document, so graft dropped the key and exited 0, or renamed a key
// on the first line, or failed "...: 2" with "found an invalid key".
func TestMergeKeepsKeysThatStartWithThreeDots(t *testing.T) {
	for _, c := range []struct {
		name, in string
		want     map[string]interface{}
	}{
		{"after a key", "a: 1\n...x: 2\n", map[string]interface{}{"a": 1, "...x": 2}},
		{"after a literal block scalar", "a: |\n  t\n...x: 2\n", map[string]interface{}{"a": "t\n", "...x": 2}},
		{"three dots as the key", "a:\n- 1\n...: 2\n", map[string]interface{}{"a": []interface{}{1}, "...": 2}},
		{"on the first line", "...x: 2\na: 1\n", map[string]interface{}{"a": 1, "...x": 2}},
		{"with a quote, a space, and a comment", "a: 1\n...x 'y: 2 # c\n", map[string]interface{}{"a": 1, "...x 'y": 2}},
		{"with a nested value", "a: 1\n...x:\n  b: 2\n", map[string]interface{}{"a": 1, "...x": map[string]interface{}{"b": 2}}},
		{"CRLF line breaks", "a: 1\r\n...x: 2\r\n", map[string]interface{}{"a": 1, "...x": 2}},
		{"tab before the colon", "a: 1\n...x\t: 2\n", map[string]interface{}{"a": 1, "...x": 2}},
		{"before a second document", "a: 1\n...x: 2\n---\nb: [\n", map[string]interface{}{"a": 1, "...x": 2}},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, err := guardedMerge(t, c.in)
			if err != nil {
				t.Fatalf("merge = %v, want success", err)
			}
			if got := out.RawData(); !reflect.DeepEqual(got, c.want) {
				t.Errorf("merge = %#v, want %#v", got, c.want)
			}
		})
	}
}

// TestMergeKeepsDocumentEndMarkers merges documents whose first document
// ends at a "..." that a blank, a "#", or the end of the line follows.
// The merge reads only that first document, as it did before keys that
// start with "..." were kept. A scalar "...x" after a key, or as the
// value of an empty key, still fails on line 2, as it does in spruce.
func TestMergeKeepsDocumentEndMarkers(t *testing.T) {
	for _, in := range []string{"a: 1\n...\nb: 2\n", "a: 1\n... # c\n", "a: 1\n...\t\nb: 2\n", "a: 1\n...#c\n", "a: 1\n..."} {
		out, err := guardedMerge(t, in)
		if err != nil {
			t.Errorf("merge of %q = %v, want success", in, err)
			continue
		}
		if got, want := out.RawData(), map[string]interface{}{"a": 1}; !reflect.DeepEqual(got, want) {
			t.Errorf("merge of %q = %#v, want %#v", in, got, want)
		}
	}
	for _, in := range []string{"a: 1\n...x\n", "a:\n...x\n", "a:\n...x\n...\n"} {
		if _, err := guardedMerge(t, in); err == nil || !strings.Contains(err.Error(), "[2:4] unexpected end content") {
			t.Errorf("merge of %q = %v, want goccy's error at [2:4]", in, err)
		}
	}
}

// TestGoPatchParsersReadOnlyTheFirstDocument feeds DetectArrayRoot and
// ParseGoPatch an operation list followed by a document with a syntax
// error. spruce reads only the first document, and applies the
// operation.
func TestGoPatchParsersReadOnlyTheFirstDocument(t *testing.T) {
	src := []byte("- type: replace\n  path: /x\n  value: 2\n---\n- [\n")
	if err := DetectArrayRoot(src); !IsArrayError(err) {
		t.Errorf("DetectArrayRoot = %v, want the array-root signal", err)
	}
	ops, err := ParseGoPatch(src)
	if err != nil || len(ops) != 1 {
		t.Errorf("ParseGoPatch = %d operations, %v; want 1 operation", len(ops), err)
	}
}

// TestGoPatchParsersKeepDirectivesWithFirstDocument feeds DetectArrayRoot
// and ParseGoPatch an operation list after a directive and a "---".
// spruce's --go-patch applies the operation.
func TestGoPatchParsersKeepDirectivesWithFirstDocument(t *testing.T) {
	src := []byte("%YAML 1.1\n---\n- type: replace\n  path: /x\n  value: 2\n")
	if err := DetectArrayRoot(src); !IsArrayError(err) {
		t.Errorf("DetectArrayRoot = %v, want the array-root signal", err)
	}
	ops, err := ParseGoPatch(src)
	if err != nil || len(ops) != 1 {
		t.Errorf("ParseGoPatch = %d operations, %v; want 1 operation", len(ops), err)
	}
}

// TestGoPatchParsersStripLeadingBOM feeds DetectArrayRoot and
// ParseGoPatch an operation list after a UTF-8 byte order mark. spruce's
// --go-patch applies the operation, and graft failed the parse.
func TestGoPatchParsersStripLeadingBOM(t *testing.T) {
	src := []byte("\xEF\xBB\xBF- type: replace\n  path: /x\n  value: 2\n")
	if err := DetectArrayRoot(src); !IsArrayError(err) {
		t.Errorf("DetectArrayRoot = %v, want the array-root signal", err)
	}
	ops, err := ParseGoPatch(src)
	if err != nil || len(ops) != 1 {
		t.Errorf("ParseGoPatch = %d operations, %v; want 1 operation", len(ops), err)
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
			if err == nil || IsArrayError(err) || !strings.Contains(err.Error(), maxRecursionMessage) {
				t.Errorf("%s = %v, want the error %q", c.name, err, maxRecursionMessage)
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
