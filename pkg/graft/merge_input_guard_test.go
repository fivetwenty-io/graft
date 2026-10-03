package graft

import (
	"context"
	"fmt"
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
		{"beside a nested anchor", "a: &a\n  p: &p v\n  q: *p\n  r: *a\n"},
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
// either. It reads the inner definition, as spruce does. goccy decoded
// it as null, so graft printed a null in its place.
func TestMergeAcceptsAliasesOutsideTheirAnchor(t *testing.T) {
	type m = map[string]interface{}
	for _, c := range []struct {
		name, in string
		want     m
	}{
		{"sibling", "a: &a {x: 1}\nb: *a\n", m{"a": m{"x": 1}, "b": m{"x": 1}}},
		{"redefined inside", "a: &x [&x 1, *x]\n", m{"a": []interface{}{1, 1}}},
		{"redefined inside a mapping", "a: &x {b: &x 1, c: *x}\n", m{"a": m{"b": 1, "c": 1}}},
		{"anchored key", "a: {&k key: 1}\nb: *k\n", m{"a": m{"key": 1}, "b": "key"}},
		// A known divergence: spruce fails a list used as a mapping key
		// with "invalid map key". goccy reads it as the list's text, as
		// graft did before aliases were expanded, and this pins that.
		{"aliased list as a key", "a: &k [1]\n*k : 2\n", m{"a": []interface{}{1}, "[1]": 2}},
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

// TestMergeExpandsAliasesInsideAnchoredValues merges aliases to anchors
// defined inside an anchored collection. spruce reads each alias as the
// value its anchor names. goccy decoded every alias to a name first
// defined inside an enclosing anchored value as null, so graft printed
// a null in its place and exited 0.
func TestMergeExpandsAliasesInsideAnchoredValues(t *testing.T) {
	type m = map[string]interface{}
	type l = []interface{}
	for _, c := range []struct {
		name, in string
		want     m
	}{
		{"nested anchor", "x: &o\n  p: &p v\n  q: *p\nr: *o\n",
			m{"x": m{"p": "v", "q": "v"}, "r": m{"p": "v", "q": "v"}}},
		{"aliased list", "a: &x [&y 1, *y]\nb: *x\n",
			m{"a": l{1, 1}, "b": l{1, 1}}},
		{"redefined, then aliased", "a: &x [&x 1, *x]\nb: *x\n",
			m{"a": l{1, 1}, "b": 1}},
		{"merge key", "a: &x {p: &p v, q: *p}\nb: {<<: *x, r: *p}\n",
			m{"a": m{"p": "v", "q": "v"}, "b": m{"p": "v", "q": "v", "r": "v"}}},
		{"grab inside an aliased map", "meta: {v: hello}\nbase: &b\n  g: (( grab meta.v ))\n  k: &k 1\n  j: *k\nuse: *b\n",
			m{"meta": m{"v": "hello"}, "base": m{"g": "hello", "k": 1, "j": 1}, "use": m{"g": "hello", "k": 1, "j": 1}}},
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

// aliasBomb returns a document of levels anchored lists. The first
// holds ten scalars and each later one holds ten aliases to the one
// before it, so the last expands to 10^(levels) scalars.
func aliasBomb(levels int) string {
	var b strings.Builder
	b.WriteString("a0: &a0 [x, x, x, x, x, x, x, x, x, x]\n")
	for i := 1; i < levels; i++ {
		alias := fmt.Sprintf("*a%d", i-1)
		fmt.Fprintf(&b, "a%d: &a%d [%s]\n", i, i, strings.TrimSuffix(strings.Repeat(alias+", ", 10), ", "))
	}
	return b.String()
}

// TestParseYAMLRejectsAliasBomb parses documents whose aliases expand to
// ten million nodes and to 10^30, past what an int can count. Each alias
// decodes as a copy of the value its anchor names, so ParseYAML has to
// fail such a document with the recursion text, before it builds any
// copy, instead of filling memory.
func TestParseYAMLRejectsAliasBomb(t *testing.T) {
	const limit = 100 << 20
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	for _, levels := range []int{7, 30} {
		src := []byte(aliasBomb(levels))
		var before, after runtime.MemStats
		runtime.GC()
		runtime.GC()
		runtime.ReadMemStats(&before)
		_, err = engine.ParseYAML(src)
		runtime.ReadMemStats(&after)
		if err == nil || !strings.Contains(err.Error(), maxRecursionMessage) {
			t.Errorf("ParseYAML of a %d-level alias bomb = %v, want the error %q", levels, err, maxRecursionMessage)
		}
		if grew := after.TotalAlloc - before.TotalAlloc; grew >= limit {
			t.Errorf("ParseYAML of a %d-level alias bomb allocated %d MB, want under %d MB", levels, grew>>20, limit>>20)
		}
	}
}

// TestParseYAMLAliasCapBoundary pins the cap on expanded nodes at
// 400,000. The document's a0 and a1 hold 7 + 999 + 398 * 1,000 =
// 399,006 nodes once a1's aliases are expanded, and b's list of n
// scalars adds 3 + n more, so n = 991 reaches the cap exactly.
func TestParseYAMLAliasCapBoundary(t *testing.T) {
	doc := func(n int) []byte {
		x := strings.TrimSuffix(strings.Repeat("x, ", 999), ", ")
		a := strings.TrimSuffix(strings.Repeat("*a0, ", 398), ", ")
		b := strings.TrimSuffix(strings.Repeat("x, ", n), ", ")
		return []byte("a0: &a0 [" + x + "]\na1: [" + a + "]\nb: [" + b + "]\n")
	}
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := engine.ParseYAML(doc(991)); err != nil {
		t.Errorf("ParseYAML of 400,000 expanded nodes = %v, want success", err)
	}
	if _, err := engine.ParseYAML(doc(992)); err == nil || !strings.Contains(err.Error(), maxRecursionMessage) {
		t.Errorf("ParseYAML of 400,001 expanded nodes = %v, want the error %q", err, maxRecursionMessage)
	}
}

// TestParseYAMLRejectsAliasesNestedTooDeep parses four lists nested
// 3,000 levels deep, each holding an alias to the one before it, so the
// last expands to 12,000 levels. ParseYAML fails it with the recursion
// text, which every over-deep merge gives, and not with goccy's own.
func TestParseYAMLRejectsAliasesNestedTooDeep(t *testing.T) {
	const n = 3000
	src := "a0: &a0 " + strings.Repeat("[", n) + strings.Repeat("]", n) + "\n"
	for i := 1; i < 4; i++ {
		src += fmt.Sprintf("a%d: &a%d %s*a%d%s\n", i, i, strings.Repeat("[", n), i-1, strings.Repeat("]", n))
	}
	engine, err := NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	_, err = engine.ParseYAML([]byte(src))
	if err == nil || !strings.Contains(err.Error(), maxRecursionMessage) || strings.Contains(err.Error(), "exceeded max depth") {
		t.Errorf("ParseYAML = %v, want the error %q", err, maxRecursionMessage)
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
// document followed by 10,001 unclosed "[". In the first two, a "---" or
// a "..." inside an open flow collection is no cut the merge can prove,
// so goccy parses every document, and the depth guard has to cover them
// all. At 9027f95 the guard checked only the first document, and goccy's
// parser took tens of gigabytes on 200,000 "[" before it failed with a
// syntax error. In the last two, goccy ends the first document at
// "...#c", which yaml.v3 reads as a scalar that cannot follow a mapping,
// and the merge fails that line, as spruce does, before it reads the
// brackets.
func TestMergeRejectsDeepNestingInAStreamItCannotCut(t *testing.T) {
	unclosed := strings.Repeat("[", 10001) + "\n"
	for _, c := range []struct{ name, in, want string }{
		{"open flow", "a: [1,\n---\n" + unclosed, maxRecursionMessage},
		{"open flow before an end marker", "a: [1,\n...\n" + unclosed, maxRecursionMessage},
		{"scalar read as an end marker", "a: 1\n...#c\n" + unclosed, "yaml: line 2: could not find expected ':'"},
		{"scalar read as an end marker before a header", "a: 1\n...#c\n---\n" + unclosed, "yaml: line 2: could not find expected ':'"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var before, after runtime.MemStats
			runtime.GC()
			runtime.GC()
			runtime.ReadMemStats(&before)
			_, err := guardedMerge(t, c.in)
			runtime.ReadMemStats(&after)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("merge = %v, want the error %q", err, c.want)
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

// TestMergeRejectsContentAfterADocumentEnd merges streams with content
// after the "..." that ends the first document and no "---" before it.
// yaml.v3 needs a "---" to start a document after a "...", so spruce
// fails each one with "did not find expected <document start>" on the
// line before that content, and exits 2. Comments, blank lines, more
// "..." lines, and directives may come between. graft merged the first
// document, dropped the content, and exited 0. The byte order mark case
// failed before e8f2c1e, which stripped the mark. A "---" after the
// "..." starts a document that the merge never reads.
func TestMergeRejectsContentAfterADocumentEnd(t *testing.T) {
	for _, c := range []struct {
		name, in string
		line     int
	}{
		{"key", "x: 1\n...\nq: 1\n", 2},
		{"byte order mark and a key that starts with three dots", "\xEF\xBB\xBF---\nk: 2\n...x: 3\n...\nq: 1\n", 4},
		{"comments on the markers", "# c\n--- # d\nx: 1\n... # e\nz: 3\n", 4},
		{"second end marker", "x: 1\n...\n...\nq: 1\n", 3},
		{"blank and comment lines", "x: 1\n...\n\n# c\n   \nq: 1\n", 5},
		{"CRLF line breaks", "x: 1\r\n...\r\nq: 1\r\n", 2},
		{"after a literal block scalar", "x: |\n  a\n...\nq: 1\n", 3},
		{"directive", "x: 1\n...\n%TAG !e! tag:e.com,2000:\nq: 1\n", 3},
		{"indented dashes", "x: 1\n...\n  ---\n", 2},
		{"dashes that are text", "x: 1\n...\n---x\n", 2},
		{"directive at the end of the input", "x: 1\n...\n%TAG !e! tag:e.com,2000:\n", 3},
		{"directive before an end marker", "x: 1\n...\n%YAML 1.1\n...\n---\nq: 1\n", 3},
	} {
		t.Run(c.name, func(t *testing.T) {
			want := fmt.Sprintf("yaml: line %d: did not find expected <document start>", c.line)
			if _, err := guardedMerge(t, c.in); err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("merge = %v, want an error naming %q", err, want)
			}
		})
	}
	for _, c := range []struct{ name, in string }{
		{"header", "x: 1\n...\n---\ny: 2\n"},
		{"comment, end marker, and a header with a trailing space", "x: 1\n... # e\n...\n--- \ny: 2\n"},
		{"directive and a header", "x: 1\n...\n%TAG !e! tag:e.com,2000:\n---\nq: 1\n"},
		{"comment", "x: 1\n...\n# c\n"},
		{"second end marker", "x: 1\n...\n...\n"},
	} {
		t.Run("accepts "+c.name, func(t *testing.T) {
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
// ends at a "..." that a blank or the end of the line follows. The merge
// reads only that first document, as it did before keys that start with
// "..." were kept. Content after the "..." needs a "---" first, which
// TestMergeRejectsContentAfterADocumentEnd covers.
func TestMergeKeepsDocumentEndMarkers(t *testing.T) {
	for _, in := range []string{"a: 1\n...\n---\nb: 2\n", "a: 1\n... # c\n", "a: 1\n...\t\n---\nb: 2\n", "a: 1\n..."} {
		out, err := guardedMerge(t, in)
		if err != nil {
			t.Errorf("merge of %q = %v, want success", in, err)
			continue
		}
		if got, want := out.RawData(), map[string]interface{}{"a": 1}; !reflect.DeepEqual(got, want) {
			t.Errorf("merge of %q = %#v, want %#v", in, got, want)
		}
	}
}

// TestMergeRejectsAScalarGoccyReadsAsADocumentEnd merges documents with
// a column-1 "..." that a character other than a blank or a line break
// follows. goccy ends the document there, so the merge dropped what
// came after it, or the whole scalar, and exited 0, where yaml.v3 reads
// a plain scalar. Each one now fails as spruce fails it, with yaml.v3's
// message and line, or, where yaml.v3 reads a key or a scalar document
// that goccy would lose, with graft's own message on the "..." line.
func TestMergeRejectsAScalarGoccyReadsAsADocumentEnd(t *testing.T) {
	for in, want := range map[string]string{
		"a: 1\n...#c\nq: 1\n":      "yaml: line 2: could not find expected ':'",
		"a: 1\n...#c\n":            "yaml: line 2: could not find expected ':'",
		"a: 1\n...x\n":             "yaml: line 2: could not find expected ':'",
		"a:\n...x\n":               "yaml: line 2: could not find expected ':'",
		"a:\n...x\n...\n":          "yaml: line 2: could not find expected ':'",
		"a: 1\n...#c\n---\nq: 1\n": "yaml: line 2: could not find expected ':'",
		"...#c\na: 1\n":            "yaml: line 1: mapping values are not allowed in this context",
		"...#c\n# d\na: 1\n":       "yaml: line 2: did not find expected <document start>",
		"a: 1\n...\n...#c\n":       "yaml: line 2: did not find expected <document start>",
		"...#c\n---\na: 1\n":       `yaml: line 1: cannot read a plain scalar that starts a line with "..."; quote it`,
		"a: 1\n...#c: 2\n":         `yaml: line 2: cannot read a plain scalar that starts a line with "..."; quote it`,
	} {
		if _, err := guardedMerge(t, in); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("merge of %q = %v, want the error %q", in, err, want)
		}
	}
}

// TestMergeKeepsLinesThatStartWithThreeDots merges documents with lines
// that start with "..." which goccy reads as yaml.v3 does, so the merge
// keeps them, as spruce does.
func TestMergeKeepsLinesThatStartWithThreeDots(t *testing.T) {
	for _, c := range []struct {
		in   string
		want map[string]interface{}
	}{
		{"a: 1\n  ...#c\nq: 1\n", map[string]interface{}{"a": "1 ...#c", "q": 1}},
		{"a:\n  ...#c\nq: 1\n", map[string]interface{}{"a": "...#c", "q": 1}},
		{"a: \"q\n...#c\"\n", map[string]interface{}{"a": "q ...#c"}},
		{"a: 'q\n...#c'\n", map[string]interface{}{"a": "q ...#c"}},
		{"a: 1\n... #c\n", map[string]interface{}{"a": 1}},
	} {
		out, err := guardedMerge(t, c.in)
		if err != nil {
			t.Errorf("merge of %q = %v, want success", c.in, err)
			continue
		}
		if got := out.RawData(); !reflect.DeepEqual(got, c.want) {
			t.Errorf("merge of %q = %#v, want %#v", c.in, got, c.want)
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

// TestJSONReadsOnlyTheFirstDocumentOfAPart feeds `graft json` parts
// that hold a second document. JSONifyFiles splits a file only at a
// "---" line with nothing else on it, so a header with a trailing space
// or a comment, or one between CRLF line breaks, leaves the next
// document in the same part. spruce's json reads only the first document
// of a part and prints {"x":1}. At 9027f95, goccy parsed the whole part,
// so a syntax error in the second document failed it, and nesting there
// skipped the depth guard and cost goccy's parser tens of gigabytes on
// 200,000 unclosed "[". spruce also strips a leading byte order mark.
func TestJSONReadsOnlyTheFirstDocumentOfAPart(t *testing.T) {
	for _, c := range []struct{ name, in string }{
		{"syntax error after a header with a trailing space", "x: 1\n--- \n[[[\n"},
		{"10,001 unclosed brackets after a header with a trailing space", "x: 1\n--- \n" + strings.Repeat("[", 10001) + "\n"},
		{"syntax error after a header with a comment", "x: 1\n--- # c\n[[[\n"},
		{"CRLF line breaks", "x: 1\r\n---\r\n[[[\r\n"},
		{"byte order mark", "\xEF\xBB\xBFx: 1\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var before, after runtime.MemStats
			runtime.GC()
			runtime.GC()
			runtime.ReadMemStats(&before)
			got, err := jsonifyData([]byte(c.in), false)
			runtime.ReadMemStats(&after)
			if err != nil || got != `{"x":1}` {
				t.Errorf("jsonifyData = %.80q, %.200v; want {\"x\":1}", got, err)
			}
			if grew := after.TotalAlloc - before.TotalAlloc; grew >= 100<<20 {
				t.Errorf("jsonifyData allocated %d MB, want under 100 MB", grew>>20)
			}
		})
	}
}
