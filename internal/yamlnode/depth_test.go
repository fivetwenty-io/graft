package yamlnode_test

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/parser"

	"github.com/fivetwenty-io/graft/internal/yamlnode"
)

// TestParseRejectsDeepNestingCheaply feeds Parse deep nesting and checks
// that it fails with yaml.v3's depth error without tokenizing the whole
// input. At 6c0a0e9, Parse tokenized all of it before the depth check
// ran, which allocated about 298 MB for a megabyte of "[". The braces
// case also holds a placeholder, so the placeholder rewrite reaches the
// branch that tokenizes its whole input, which the depth check has to
// run ahead of. That rewrite tokenized a second time at 6c0a0e9, so the
// braces case uses half a megabyte to keep its failing run near 300 MB.
func TestParseRejectsDeepNestingCheaply(t *testing.T) {
	const limit = 64 << 20
	for _, c := range []struct {
		name string
		in   string
		want string
	}{
		{"open brackets", strings.Repeat("[", 1<<20), "yaml: exceeded max depth of 10000"},
		{"open braces after a placeholder", "a: {{x}}\nb: " + strings.Repeat("{", 1<<19), "yaml: line 2: exceeded max depth of 10000"},
		{"block dashes", strings.Repeat("- ", 1<<19) + "x\n", "yaml: exceeded max depth of 10000"},
	} {
		t.Run(c.name, func(t *testing.T) {
			src := []byte(c.in)
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			_, err := yamlnode.Parse(src)
			runtime.ReadMemStats(&after)
			var pe *yamlnode.ParseError
			if !errors.As(err, &pe) || err.Error() != c.want {
				t.Errorf("Parse = %v, want the ParseError %q", err, c.want)
			}
			if grew := after.TotalAlloc - before.TotalAlloc; grew >= limit {
				t.Errorf("Parse allocated %d MB, want under %d MB", grew>>20, limit>>20)
			}
		})
	}
}

// TestParseReusesProbeTokens parses two inputs that hold more than
// 10,000 "[" and "{", so the depth probe tokenizes each of them whole
// without tripping. They differ only in one list entry, written "- ~" in
// one and as a bare "-" in the other. The bare-dash rewrite turns the
// second into the first, so Parse tokenizes the same text for both, but
// the probe never sees that rewrite. Parse can reuse the probe's tokens
// for the first input and has to tokenize the second again, which cost
// 15 to 24 MB more in our runs, with or without the race detector.
// Without the reuse, the two came within 8 MB of each other.
//
// Comparing two parses in one process keeps the race detector's overhead
// out of the result, where an absolute bound was flaky. The flow
// sequence sits on one line because the bare-dash rewrite matches a
// regexp against every line, and under the race detector the regexp's
// pooled state is often dropped, so thousands of lines would make the
// rewrite itself cost tens of megabytes.
func TestParseReusesProbeTokens(t *testing.T) {
	const minGap = 10 << 20
	var b strings.Builder
	b.WriteString("items: [")
	for i := range 5000 {
		fmt.Fprintf(&b, "{\"name\": \"v%d\", \"list\": [\"a\", \"b\"]}, ", i)
	}
	b.WriteString("{}]\nl:\n")
	body := b.String()
	reused := parseAlloc(t, body+"- ~\nk: v\n")
	retokenized := parseAlloc(t, body+"-\nk: v\n")
	if retokenized < reused+minGap {
		t.Errorf("Parse allocated %d MB reusing the probe's tokens and %d MB without, want a gap of at least %d MB",
			reused>>20, retokenized>>20, minGap>>20)
	}
}

// parseAlloc parses in, which must hold one document, and returns the
// bytes Parse allocated. Two collections first empty the sync.Pool
// caches, so each parse starts from the same state and pays for its own
// pooled objects.
func parseAlloc(t *testing.T, in string) uint64 {
	t.Helper()
	src := []byte(in)
	var before, after runtime.MemStats
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&before)
	docs, err := yamlnode.Parse(src)
	runtime.ReadMemStats(&after)
	if err != nil || len(docs) != 1 {
		t.Fatalf("Parse = %d documents, %v; want 1 document", len(docs), err)
	}
	return after.TotalAlloc - before.TotalAlloc
}

// TestParseIgnoresBracketsOutsideStructure puts more than 10,000
// brackets where they are text, not nesting, and checks that each input
// parses. A depth check that counted brackets in the raw bytes would
// reject every one of them. The "at the limit" cases nest exactly 10,000
// levels before the text, so the 10,001st bracket in the input is the
// first one inside the text.
func TestParseIgnoresBracketsOutsideStructure(t *testing.T) {
	text := strings.Repeat("[", 20000)
	open, closing := strings.Repeat("[", 10000), strings.Repeat("]", 10000)
	for _, c := range []struct {
		name, in, want string
	}{
		{"plain scalar", "a: x" + text + "\n", "x" + text},
		{"quoted scalar", "a: \"" + text + "\"\n", text},
		{"single-quoted scalar", "a: '" + text + "'\n", text},
		{"comment", "# " + text + "\na: v\n", "v"},
		{"block literal", "a: |\n  " + text + "\n", text + "\n"},
		{"quoted scalar at the limit", "a: " + open + "\"" + text + "\"" + closing + "\n", text},
		{"comment at the limit", "a: " + open + " # " + text + "\n  v" + closing + "\n", "v"},
	} {
		t.Run(c.name, func(t *testing.T) {
			docs, err := yamlnode.Parse([]byte(c.in))
			if err != nil {
				t.Fatalf("Parse: %v, want no error", err)
			}
			if got := innermost(docs[0]).Value; got != c.want {
				t.Errorf("value = %.40q (%d bytes), want %.40q (%d bytes)", got, len(got), c.want, len(c.want))
			}
		})
	}
}

// innermost follows the first child of each node down to a leaf, past
// the document and the key of a mapping to the value.
func innermost(n *yamlnode.Node) *yamlnode.Node {
	for len(n.Content) > 0 {
		if n.Kind == yamlnode.MappingNode {
			n = n.Content[1]
			continue
		}
		n = n.Content[0]
	}
	return n
}

// TestParseDepthTailTokens checks inputs whose 10,001st candidate opener
// is a "-" that only the next byte shows to be text. Tokenizing a prefix
// that ends right after it reads a sequence entry, but the dash belongs
// to the plain scalar "-x", so the input nests 10,000 levels and parses.
func TestParseDepthTailTokens(t *testing.T) {
	for _, in := range []string{
		strings.Repeat("- ", 10000) + "-x\n",
		strings.Repeat("- ", 10000) + "-x",
	} {
		docs, err := yamlnode.Parse([]byte(in))
		if err != nil {
			t.Fatalf("Parse(%.20q...): %v, want no error", in, err)
		}
		if got := innermost(docs[0]).Value; got != "-x" {
			t.Errorf("Parse(%.20q...): innermost value %q, want %q", in, got, "-x")
		}
	}
}

// TestParseDepthCountsBracePlaceholders checks that a {{x}} placeholder
// counts as the two flow mappings yaml.v3 reads, as spruce v1.35.17
// does. Under 9,998 brackets it parses as the string "{{x}}", and under
// 9,999 brackets it fails with yaml.v3's depth error.
func TestParseDepthCountsBracePlaceholders(t *testing.T) {
	nest := func(n int) string {
		return "a: " + strings.Repeat("[", n) + "{{x}}" + strings.Repeat("]", n) + "\n"
	}
	docs, err := yamlnode.Parse([]byte(nest(9998)))
	if err != nil {
		t.Fatalf("9,998 brackets: %v, want no error", err)
	}
	if got := innermost(docs[0]); got.Tag != "!!str" || got.Value != "{{x}}" {
		t.Errorf("9,998 brackets: innermost %s %q, want !!str %q", got.Tag, got.Value, "{{x}}")
	}
	_, err = yamlnode.Parse([]byte(nest(9999)))
	if want := "yaml: exceeded max depth of 10000"; err == nil || err.Error() != want {
		t.Errorf("9,999 brackets: %v, want %q", err, want)
	}
}

// TestParseDepthLineAfterCRLF checks the depth error's line in input
// with CRLF and lone CR line breaks. goccy counts a CRLF comment line as
// two lines, so the check has to see LF line breaks, as the parse does.
func TestParseDepthLineAfterCRLF(t *testing.T) {
	deep := strings.Repeat("[", 10001)
	for _, in := range []string{
		"a: 1\r\n# c\r\nb: " + deep + "\r\n",
		"a: 1\r# c\rb: " + deep + "\r",
	} {
		_, err := yamlnode.Parse([]byte(in))
		if want := "yaml: line 3: exceeded max depth of 10000"; err == nil || err.Error() != want {
			t.Errorf("Parse(%.20q...) = %v, want %q", in, err, want)
		}
	}
}

// TestParseDepthAtInjectKeys nests a mapping 10,000 levels deep whose
// second key is graft's "<<<" inject key. goccy places an unquoted "<<<"
// key a column or more right of where it starts, which would read as an
// extra level, so the depth check has to see the key quoted, as the
// parse does.
func TestParseDepthAtInjectKeys(t *testing.T) {
	indent := strings.Repeat(" ", 2*9999)
	for _, key := range []string{"<<<", "a.<<<"} {
		in := strings.Repeat("- ", 9999) + "b: 1\n" + indent + key + ": x\n"
		docs, err := yamlnode.Parse([]byte(in))
		if err != nil {
			t.Fatalf("key %q: %v, want no error", key, err)
		}
		if m := innermost(docs[0]).Value; m != "1" {
			t.Errorf("key %q: innermost value %q, want %q", key, m, "1")
		}
	}
}

// TestFirstDocumentDepthLimit checks the depth guard FirstDocument runs
// on raw input. It fails nesting past 10,000 levels in the first
// document with yaml.v3's message, passes nesting at the limit, and
// ignores nesting in a later document, which neither spruce nor graft's
// merge reads. A key at column 1 that starts with "..." and a non-blank
// character does not end the first document, so nesting in its value
// counts.
func TestFirstDocumentDepthLimit(t *testing.T) {
	deep := func(n int) string { return "a: " + strings.Repeat("[", n) + strings.Repeat("]", n) + "\n" }
	for _, c := range []struct {
		name string
		in   string
		want string
	}{
		{"at the limit", deep(10000), ""},
		{"past the limit", deep(10001), "yaml: exceeded max depth of 10000"},
		{"unclosed brackets", "a: " + strings.Repeat("[", 20000) + "\n", "yaml: exceeded max depth of 10000"},
		{"past the limit in the second document", "x: 1\n---\n" + deep(10001), ""},
		{"past the limit in a key that starts with three dots", "a: 1\n..." + deep(10001)[1:], "yaml: line 2: exceeded max depth of 10000"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := ""
			if _, err := yamlnode.FirstDocument([]byte(c.in)); err != nil {
				got = err.Error()
			}
			if got != c.want {
				t.Errorf("FirstDocument = %q, want %q", got, c.want)
			}
		})
	}
}

// TestFirstDocument checks where FirstDocument cuts a stream. It cuts at
// the start of the line holding the "---" that ends the first document,
// or after the line holding the "..." that ends it, whatever line breaks
// the input uses, and it leaves the input whole
// when no marker ends the first document or the cut could not be proved.
// Each cut must give goccy the same first document the whole input
// gives it.
func TestFirstDocument(t *testing.T) {
	for _, c := range []struct {
		name, in, want string
	}{
		{"one document", "x: 1\ny: 2\n", "x: 1\ny: 2\n"},
		{"two documents", "x: 1\n---\ny: 2\n", "x: 1\n"},
		{"CRLF line breaks", "x: 1\r\nz: 3\r\n---\r\ny: 2\r\n", "x: 1\nz: 3\n"},
		{"CR line breaks", "x: 1\rz: 3\r---\ry: 2\r", "x: 1\nz: 3\n"},
		{"ended by a document end marker", "x: 1\n... # end\r\n---\r\ny: 2\n", "x: 1\n... # end\n"},
		{"document end marker then a header", "x: 1\n...\n---\ny: 2\n", "x: 1\n...\n"},
		{"document end marker at the end of the input", "x: 1\n...", "x: 1\n..."},
		{"header with a comment", "x: 1\n--- # two\ny: 2\n", "x: 1\n"},
		{"header at the end of the input", "x: 1\n---", "x: 1\n"},
		{"leading header alone", "--- # one\nx: 1\n", "--- # one\nx: 1\n"},
		{"leading header", "---\nx: 1\n---\ny: 2\n", "---\nx: 1\n"},
		{"comment-only preamble", "# one\n\n# two\n---\nx: 1\n---\ny: 2\n", "# one\n\n# two\n---\nx: 1\n"},
		{"directive", "%YAML 1.1\n---\nx: 1\n---\ny: 2\n", "%YAML 1.1\n---\nx: 1\n"},
		{"tag directive and a header with a comment", "%TAG !e! tag:e.com,2000:\n--- # h\nx: 1\n---\ny: 2\n", "%TAG !e! tag:e.com,2000:\n--- # h\nx: 1\n"},
		{"directive after a comment", "# c\n%YAML 1.1\n---\nx: 1\n...\n---\ny: 2\n", "# c\n%YAML 1.1\n---\nx: 1\n...\n"},
		{"empty first document", "---\n---\ny: 2\n", "---\n"},
		{"marker in a literal block scalar", "a: |\n  ---\n  b\nc: 3\n---\nd: 4\n", "a: |\n  ---\n  b\nc: 3\n"},
		{"marker in a quoted multi-line string", "a: \"b\n  ---\n  c\"\nd: 4\n---\ne: 5\n", "a: \"b\n  ---\n  c\"\nd: 4\n"},
		{"marker in a single-quoted multi-line string", "a: 'b\n  ---\n  c'\nd: 4\n---\ne: 5\n", "a: 'b\n  ---\n  c'\nd: 4\n"},
		{"marker in a folded block scalar", "a: >\n  b\n  ---\n  c\nx: 1\n---\ny: 2\n", "a: >\n  b\n  ---\n  c\nx: 1\n"},
		{"marker in a plain multi-line scalar", "a: b\n  ---\n  c\nx: 1\n---\ny: 2\n", "a: b\n  ---\n  c\nx: 1\n"},
		{"header followed by a tab", "x: 1\n---\t\ny: 2\n", "x: 1\n"},
		{"dashes followed by a comment sign", "x: 1\n---#c\ny: 2\n", "x: 1\n---#c\ny: 2\n"},
		{"marker that ends a literal block scalar", "a: |\n  b\n---\nc: 3\n", "a: |\n  b\n"},
		{"marker at column 1 in a quoted string", "a: \"b\n---\nc\"\nd: 4\n", "a: \"b\n---\nc\"\nd: 4\n"},
		{"marker inside a flow collection", "a: [1,\n---\n2]\n", "a: [1,\n---\n2]\n"},
		{"dashes that are text", "a: 1\n---b\nc: 3\n", "a: 1\n---b\nc: 3\n"},
		{"dots that start a key", "x: 1\n...x: 2\n---\ny: [\n", "x: 1\n...x: 2\n"},
		{"inject key", "a:\n  <<<: (( grab b ))\n---\nc: [\n", "a:\n  <<<: (( grab b ))\n"},
		{"multibyte text", "é: ü\r\n---\r\ny: 2\r\n", "é: ü\n"},
		{"next line character", "x: a\u0085b\n---\ny: 2\n", "x: a\u0085b\n"},
		{"byte order mark", "\xEF\xBB\xBFx: 1\n---\ny: 2\n", "\xEF\xBB\xBFx: 1\n"},
		{"syntax error in the second document", "x: 1\n---\ny: [\n", "x: 1\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := yamlnode.FirstDocument([]byte(c.in))
			if err != nil || string(got) != c.want {
				t.Fatalf("FirstDocument = %q, %v; want %q", got, err, c.want)
			}
			if c.want == c.in {
				return
			}
			whole, wholeErr := parser.ParseBytes([]byte(c.in), 0)
			cut, cutErr := parser.ParseBytes(got, 0)
			if cutErr != nil {
				t.Fatalf("goccy fails the first document: %v", cutErr)
			}
			if wholeErr == nil && contentDocument(whole) != contentDocument(cut) {
				t.Errorf("goccy reads the first document as %q, want %q", contentDocument(cut), contentDocument(whole))
			}
		})
	}
}

// TestFirstDocumentGivesLFLineBreaks checks that FirstDocument returns
// its result with LF line breaks, on every path that returns one: a cut
// at a marker, a stream with no marker, and a stream whose marker line
// cannot be proved a cut. goccy folds a quoted scalar across LF breaks
// only, so a CRLF or a lone CR that reached it split the scalar into the
// wrong value.
func TestFirstDocumentGivesLFLineBreaks(t *testing.T) {
	for _, c := range []struct {
		name, in, want string
	}{
		{"CRLF without a marker", "k: \"q\r\n  r s\"\r\n", "k: \"q\n  r s\"\n"},
		{"lone CR without a marker", "a: 1\rb: 2\r", "a: 1\nb: 2\n"},
		{"CRLF cut at a header", "x: 1\r\nz: 3\r\n---\r\ny: 2\r\n", "x: 1\nz: 3\n"},
		{"lone CR cut at a header", "x: 1\rz: 3\r---\ry: 2\r", "x: 1\nz: 3\n"},
		{"CRLF cut after a document end", "x: 1\r\n... # end\r\n---\r\ny: 2\r\n", "x: 1\n... # end\n"},
		{"CRLF in a stream returned whole", "a: 1\r\n---b\r\nc: 3\r\n", "a: 1\n---b\nc: 3\n"},
		{"CRLF in a quoted scalar before a header", "k: \"a\r\n\r\n  b\"\r\n---\r\nz: 1\r\n", "k: \"a\n\n  b\"\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := yamlnode.FirstDocument([]byte(c.in))
			if err != nil || string(got) != c.want {
				t.Fatalf("FirstDocument = %q, %v; want %q", got, err, c.want)
			}
		})
	}
}

// TestFirstDocumentKeepsInputWithoutCR checks that input with no CR
// comes back as the same slice, whole or cut, so ordinary input pays no
// copy for the line break pass.
func TestFirstDocumentKeepsInputWithoutCR(t *testing.T) {
	for _, c := range []struct {
		name, in string
		wantLen  int
	}{
		{"no marker", "x: 1\ny: 2\n", 10},
		{"cut at a header", "x: 1\n---\ny: 2\n", 5},
		{"stream returned whole", "a: 1\n---b\nc: 3\n", 15},
	} {
		t.Run(c.name, func(t *testing.T) {
			in := []byte(c.in)
			got, err := yamlnode.FirstDocument(in)
			if err != nil || len(got) != c.wantLen {
				t.Fatalf("FirstDocument = %q, %v; want %d bytes", got, err, c.wantLen)
			}
			if &got[0] != &in[0] {
				t.Errorf("FirstDocument copied input that holds no CR, want the same slice")
			}
		})
	}
}

// contentDocument returns the text of file's first document that is not
// a directive. goccy gives the directives before a "---" a document of
// their own, so the document they belong to comes after it.
func contentDocument(file *ast.File) string {
	for _, doc := range file.Docs {
		if _, ok := doc.Body.(*ast.DirectiveNode); !ok {
			return doc.String()
		}
	}
	return ""
}

// TestFirstDocumentDepth checks that FirstDocument fails a first
// document nested past 10,000 levels, and that it cuts nesting that deep
// from a later document without tokenizing all of it. At d86cd74, the
// merge parsed every document, so the second document cost goccy's
// parser hundreds of megabytes.
func TestFirstDocumentDepth(t *testing.T) {
	deep := func(n int) string { return "a: " + strings.Repeat("[", n) + strings.Repeat("]", n) + "\n" }
	if _, err := yamlnode.FirstDocument([]byte(deep(10001))); err == nil || err.Error() != "yaml: exceeded max depth of 10000" {
		t.Errorf("FirstDocument = %v, want the depth error", err)
	}
	src := []byte("x: 1\n---\n" + deep(10001))
	var before, after runtime.MemStats
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&before)
	got, err := yamlnode.FirstDocument(src)
	runtime.ReadMemStats(&after)
	if err != nil || string(got) != "x: 1\n" {
		t.Errorf("FirstDocument = %.20q, %v; want \"x: 1\\n\"", got, err)
	}
	if grew := after.TotalAlloc - before.TotalAlloc; grew >= 20<<20 {
		t.Errorf("FirstDocument allocated %d MB, want under 20 MB", grew>>20)
	}
}

// TestFirstDocumentDepthOfAStreamItReturnsWhole feeds FirstDocument a
// first document followed by 10,001 unclosed "[". In the first two, a
// "---" inside an open flow collection is no cut FirstDocument can
// prove, so it returns the stream whole, and goccy then parses every
// document, so the depth check has to cover them all. At 9027f95 it
// checked only the first document, and goccy's parser took tens of
// gigabytes on 200,000 "[" before it failed. In the last two, goccy ends
// the first document at "...#c", a line yaml.v3 reads as a scalar that
// cannot follow a mapping, and FirstDocument fails it before it reads
// the brackets.
func TestFirstDocumentDepthOfAStreamItReturnsWhole(t *testing.T) {
	unclosed := strings.Repeat("[", 10001) + "\n"
	for _, c := range []struct{ name, in, want string }{
		{"open flow", "a: [1,\n---\n" + unclosed, "yaml: line 3: exceeded max depth of 10000"},
		{"open flow before an end marker", "a: [1,\n...\n" + unclosed, "yaml: line 3: exceeded max depth of 10000"},
		{"scalar read as an end marker", "a: 1\n...#c\n" + unclosed, "yaml: line 2: could not find expected ':'"},
		{"scalar read as an end marker before a header", "a: 1\n...#c\n---\n" + unclosed, "yaml: line 2: could not find expected ':'"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var before, after runtime.MemStats
			runtime.GC()
			runtime.GC()
			runtime.ReadMemStats(&before)
			_, err := yamlnode.FirstDocument([]byte(c.in))
			runtime.ReadMemStats(&after)
			if err == nil || err.Error() != c.want {
				t.Errorf("FirstDocument = %v, want %q", err, c.want)
			}
			if grew := after.TotalAlloc - before.TotalAlloc; grew >= 20<<20 {
				t.Errorf("FirstDocument allocated %d MB, want under 20 MB", grew>>20)
			}
		})
	}
}

// TestFirstDocumentRejectsContentAfterADocumentEnd feeds FirstDocument
// streams whose first document a "..." ends, with content after it and
// no "---" before that content. yaml.v3 fails each one with "did not
// find expected <document start>" on the line before the content, as
// Parse does, and FirstDocument has to fail it too, since the content
// is no document the merge may drop. The wide cases hold more than
// 10,000 brackets, so FirstDocument reads them in prefixes, and in the
// padded ones the content lies past the prefix that ends the first
// document. A "---" before the content, or nothing at all, is fine.
func TestFirstDocumentRejectsContentAfterADocumentEnd(t *testing.T) {
	wide := "a: [" + strings.Repeat("[], ", 12000) + "1]\n"
	pad := strings.Repeat("# padding\n", 20000)
	for _, c := range []struct {
		name, in, want string
	}{
		{"key", "x: 1\n...\nq: 1\n", "yaml: line 2: did not find expected <document start>"},
		{"content on the marker line", "x: 1\n... q\n", "yaml: line 1: did not find expected <document start>"},
		{"directive", "x: 1\n...\n%YAML 1.1\nq: 1\n", "yaml: line 3: did not find expected <document start>"},
		{"wide", wide + "...\nq: 1\n", "yaml: line 2: did not find expected <document start>"},
		{"directive at the end of the input", "x: 1\n...\n%TAG !e! tag:e.com,2000:\n", "yaml: line 3: did not find expected <document start>"},
		{"directive at the end of an unbroken last line", "x: 1\n...\n%TAG !e! tag:e.com,2000:", "yaml: line 3: did not find expected <document start>"},
		{"directive and a comment at the end of the input", "x: 1\n...\n%TAG !e! tag:e.com,2000:\n# c\n\n", "yaml: line 5: did not find expected <document start>"},
		{"directive before an end marker", "x: 1\n...\n%YAML 1.1\n...\n---\nq: 1\n", "yaml: line 3: did not find expected <document start>"},
		{"wide and padded directive at the end", wide + "...\n%YAML 1.1\n" + pad, "yaml: line 20003: did not find expected <document start>"},
		{"wide and padded", wide + "...\n" + pad + "q: 1\n", "yaml: line 20002: did not find expected <document start>"},
		{"wide and padded with more after", wide + "...\n" + pad + "q: 1\n" + strings.Repeat(pad, 3), "yaml: line 20002: did not find expected <document start>"},
		{"wide and padded before a header", wide + "...\n" + pad + "---\nq: 1\n", ""},
		{"wide and padded before a header with more after", wide + "...\n" + pad + "---\nq: 1\n" + strings.Repeat(pad, 3), ""},
		{"wide and padded to the end", wide + "...\n" + pad, ""},
		{"header", "x: 1\n...\n---\nq: 1\n", ""},
		{"end of the input", "x: 1\n...\n# c\n", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			got, err := yamlnode.FirstDocument([]byte(c.in))
			if c.want != "" {
				if err == nil || err.Error() != c.want {
					t.Errorf("FirstDocument = %.40q, %v; want the error %q", got, err, c.want)
				}
				return
			}
			if err != nil || !strings.HasSuffix(string(got), "...\n") {
				t.Errorf("FirstDocument = %.40q, %v; want the first document and its \"...\"", got, err)
			}
		})
	}
}

// TestFirstDocumentRejectsALeadingDocumentEnd feeds FirstDocument
// streams whose first line that is not blank or a comment is a "..."
// marker. spruce's YAML library reads the start of the first document
// as implicit there, finds the "..." where a node has to start, and
// fails with "did not find expected node content" on the marker's line,
// counted from 0 and left out when it is 0. goccy read an empty first
// document, so the merge went on to the content after it or merged {}.
// A "..." that is indented, or that a non-blank follows, is no marker.
func TestFirstDocumentRejectsALeadingDocumentEnd(t *testing.T) {
	const msg = "did not find expected node content"
	for in, want := range map[string]string{
		"...\na: 1\n":                 "yaml: " + msg,
		"...\n":                       "yaml: " + msg,
		"...":                         "yaml: " + msg,
		"...\n---\na: 1\n":            "yaml: " + msg,
		"...\n%YAML 1.1\n---\na: 1\n": "yaml: " + msg,
		"... # c\na: 1\n":             "yaml: " + msg,
		"...\t\na: 1\n":               "yaml: " + msg,
		"...\n# c\n":                  "yaml: " + msg,
		"...\n...x\n":                 "yaml: " + msg,
		"# c\n...\na: 1\n":            "yaml: line 1: " + msg,
		" # c\n...\n":                 "yaml: line 1: " + msg,
		"\n\n...\na: 1\n":             "yaml: line 2: " + msg,
		"...\r\na: 1\r\n":             "yaml: " + msg,
		"# c\r\n\r\n...\r\na: 1\r\n":  "yaml: line 2: " + msg,
	} {
		if got, err := yamlnode.FirstDocument([]byte(in)); err == nil || err.Error() != want {
			t.Errorf("FirstDocument(%q) = %q, %v; want the error %q", in, got, err, want)
		}
	}
	for _, in := range []string{"---\n...\n", "a: 1\n...\n", "....\n", "...x: 1\n", "# c\n---\na: 1\n"} {
		if _, err := yamlnode.FirstDocument([]byte(in)); err != nil {
			t.Errorf("FirstDocument(%q) = %v, want no error", in, err)
		}
	}
}

// TestParseRejectsContentAfterADocumentEnd checks that Parse, which the
// diff reads with, fails the streams FirstDocument fails, with the same
// error. Content on the "..." line is left out, because Parse reports
// goccy's error for the document that line ends first.
func TestParseRejectsContentAfterADocumentEnd(t *testing.T) {
	for _, in := range []string{"x: 1\n...\nq: 1\n", "x: 1\n...\n%YAML 1.1\nq: 1\n", "x: 1\n...\n...\n\n# c\nq: 1\n"} {
		_, cutErr := yamlnode.FirstDocument([]byte(in))
		_, parseErr := yamlnode.Parse([]byte(in))
		if cutErr == nil || parseErr == nil || cutErr.Error() != parseErr.Error() {
			t.Errorf("%q: FirstDocument = %v, Parse = %v; want one error", in, cutErr, parseErr)
		}
	}
}

// TestFirstDocumentRejectsAScalarItReadsAsADocumentEnd feeds
// FirstDocument a column-1 "..." that a character other than a blank or
// a line break follows, such as "...#c" or "...x", where goccy ends the
// document and yaml.v3 reads a plain scalar. goccy's merge would drop
// what follows, so FirstDocument fails each line yaml.v3 cannot read as
// part of the document, with yaml.v3's message and line, as spruce
// reports them. A key, or a scalar that is the whole document, is one
// yaml.v3 reads, and FirstDocument fails it with a message of its own on
// the "..." line, since goccy's reading would lose it.
func TestFirstDocumentRejectsAScalarItReadsAsADocumentEnd(t *testing.T) {
	const notExpected = "could not find expected ':'"
	const mapping = "mapping values are not allowed in this context"
	const docStart = "did not find expected <document start>"
	const tab = "found a tab character that violate indentation"
	const quoteIt = `cannot read a plain scalar that starts a line with "..."; quote it`
	wide := "a: [" + strings.Repeat("[], ", 12000) + "1]\n"
	for _, c := range []struct {
		in   string
		line int
		msg  string
	}{
		{"a: 1\n...#c\nq: 1", 2, notExpected},
		{"a: 1\n...#c", 2, notExpected},
		{"a: 1\n...#c\n", 2, notExpected},
		{"a: 1\n...x\nq: 1", 2, notExpected},
		{"a: 1\n...x", 2, notExpected},
		{"a: |\n  x\n...#c", 3, notExpected},
		{"a:\n  b: 1\n...#c\nq: 1", 3, notExpected},
		{"a: b\n...#c", 2, notExpected},
		{"- a\n...#c", 2, notExpected},
		{"a: 1\n...x y\nq: 1", 2, notExpected},
		{"a: 1\n...#c\n---\nq: 1", 2, notExpected},
		{"a: 1\n...x\n---\nq: 1", 2, notExpected},
		{"a: 1\n...#c\n...", 2, notExpected},
		{"a: 1\n...#c\n# d", 2, notExpected},
		{"a:\n...#c", 2, notExpected},
		{"a:\n...x\n", 2, notExpected},
		{"a:\n...x\n...\n", 2, notExpected},
		{"a: 1\n...x\n...\nq: 1", 2, notExpected},
		{"a: 1\n...#c\n\n\nq: 1", 4, notExpected},
		{"a:\n  b:\n    c: 1\n...#c", 4, notExpected},
		{"a: 1\n...x\n\n# c", 3, notExpected},
		{"a: 1\r\n...#c\r\nq: 1\r", 2, notExpected},
		{"a: 1\n...#c # d\nq: 1", 2, notExpected},
		{"- a\n- b\n...#c", 3, notExpected},
		{"a: 1\n...x y z", 2, notExpected},
		{"---\na: 1\n...#c\nq: 1", 3, notExpected},
		{"%YAML 1.1\n---\na: 1\n...#c", 4, notExpected},
		{"a: >\n  x\n...#c", 3, notExpected},
		{"a: \"x\"\n...#c", 2, notExpected},
		{"a: 1\n...#c\n---\nb: [", 2, notExpected},
		{"a: 1\n...#", 2, notExpected},
		{"a: 1\n...x:y", 2, notExpected},
		{"a: 1\n...#c\n  more\nq: 1", 3, notExpected},
		{"a:\n  b: 1\n...#c\n  c: 2", 3, notExpected},
		{"- a\n...#c\n- b", 2, notExpected},
		{"a: 1\n...#c\n  # d\nq: 1", 2, notExpected},
		{"a: 1\n...#c\n  more: 2", 2, notExpected},
		{"a: 1\n...#c\n \t \nq: 1\n", 3, notExpected},
		{"a:\n  - x\n...#c\nq: 1\n", 3, notExpected},
		{"a: 1\n...#c\n  more\n  # x\nq: 1\n", 3, notExpected},
		{"a: 1\n...#c\n  more: 2\nq: 1\n", 2, notExpected},
		{"a: 1\n...#c\n  more # x\nq: 1\n", 2, notExpected},
		{"a: 1\n...#c\n\n", 3, notExpected},
		{"a: 1\n...#c\n  \n  ", 3, notExpected},
		{"a: 1\n...#c\n  \n  \n", 4, notExpected},
		{"a: 1\n...#c\n  more", 2, notExpected},
		{"a: 1\n...#c\n  more\n", 3, notExpected},
		{"a: 1\n...#c ", 2, notExpected},
		{"a: 1\n...#c\n ", 2, notExpected},
		{"a: 1\n...#c\n\n ", 3, notExpected},
		{"a: 1\n...#c # d", 2, notExpected},
		{"a: 1\n...#c\r\n  \r\n  ", 3, notExpected},
		{"a: 1\n...x\n  y\n\n  ", 4, notExpected},
		{"a: 1\r\n...#c\r\n\r\n", 3, notExpected},
		{"a: 1\n...#c\n  x:y\nq: 1\n", 3, notExpected},
		{"a: 1\n...#c\n  \"k\": 1\n", 2, notExpected},
		{"a: 1\n...#c\n\t\nq: 1", 2, tab},
		{"a: 1\n...#c\n\tq: 1\n", 2, tab},
		{wide + "...#c\n" + strings.Repeat("\n", 20000) + "q: 1\n", 20002, notExpected},
		{"...#c\na: 1", 1, mapping},
		{"# c\n...#c\na: 1", 2, mapping},
		{"...#c\n\na: 1", 2, mapping},
		{"...#c\nfoo\na: 1", 2, mapping},
		{"...#c\n  b: 1\n", 1, mapping},
		{"...#c\n\t\na: 1\n", 2, mapping},
		{"...#c\n# d\na: 1", 2, docStart},
		{"...#c # d\na: 1", 1, docStart},
		{"...x # c\na: 1\n", 1, docStart},
		{"...#c\nfoo # x\na: 1\n", 2, docStart},
		{"...#c\n  # d\n  a: 1\n", 2, docStart},
		{"a: 1\n...\n...#c", 2, docStart},
		{"a: 1\n...\n...#c\nq: 1", 2, docStart},
		{"...#c\n  b\n", 1, quoteIt},
		{"...x\nfoo\n", 1, quoteIt},
		{"...#c\n  ---\n", 1, quoteIt},
		{"a: 1\n...#c: 2\n...\nq: 1", 3, docStart},
	} {
		want := fmt.Sprintf("yaml: line %d: %s", c.line, c.msg)
		got, err := yamlnode.FirstDocument([]byte(c.in))
		if err == nil || err.Error() != want {
			t.Errorf("FirstDocument(%.40q) = %.20q, %v; want the error %q", c.in, got, err, want)
		}
	}
}

// TestFirstDocumentKeepsLinesThatStartWithThreeDots checks the lines
// that start with "..." which FirstDocument leaves as they stand. goccy
// reads a quoted key, an indented "...#c", and a "...#c" inside a quoted
// scalar as yaml.v3 does. In a flow collection goccy fails the stream
// itself, so FirstDocument has nothing to cut and returns it whole. A
// "..." that ends the first document still cuts it, and a "...#c" in a
// later document is past the cut.
func TestFirstDocumentKeepsLinesThatStartWithThreeDots(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"a: 1\n...x: 2\n", "a: 1\n...x: 2\n"},
		{"...x: 2\n", "...x: 2\n"},
		{"---\n...x: 1\n", "---\n...x: 1\n"},
		{"a: 1\n...x:\n", "a: 1\n...x:\n"},
		{"...x\n", "...x\n"},
		{"a: 1\n  ...#c\nq: 1\n", "a: 1\n  ...#c\nq: 1\n"},
		{"a:\n  ...#c\nq: 1\n", "a:\n  ...#c\nq: 1\n"},
		{"a: \"q\n...#c\"\n", "a: \"q\n...#c\"\n"},
		{"a: 'q\n...#c'\n", "a: 'q\n...#c'\n"},
		{"a: [1,\n...x]\n", "a: [1,\n...x]\n"},
		{"a: [1,\n...#c]\n", "a: [1,\n...#c]\n"},
		{"a: {b: 1,\n...#c: 2}\n", "a: {b: 1,\n...#c: 2}\n"},
		{"a: [1,\n...#c\n]\n", "a: [1,\n...#c\n]\n"},
		{"a: 1\n....\n", "a: 1\n....\n"},
		{"a: 1\n... #c\n", "a: 1\n... #c\n"},
		{"a: 1\n...\t#c\n", "a: 1\n...\t#c\n"},
		{"a: 1\n...#c: 2\n", "a: 1\n...#c: 2\n"},
		{"...#c: 2\nq: 1", "...#c: 2\nq: 1"},
		{"a: 1\n...#c: 2\nq: 1", "a: 1\n...#c: 2\nq: 1"},
		{"- a\n...#c: 2\n", "- a\n...#c: 2\n"},
		{"...#c", "...#c"},
		{"--- !!map\n...#c", "--- !!map\n...#c"},
		{"...#c\n...\n", "...#c\n...\n"},
		{"...#c # d\n\n---\na: 1\n", "...#c # d\n\n"},
		{"...#c\n#d\n---\n", "...#c\n#d\n"},
		{"...#c\n---\na: 1\n", "...#c\n"},
		{"a: 1\n...\n---\n...#c\n", "a: 1\n...\n"},
		{"a: 1\n---\n...#c\n", "a: 1\n"},
	} {
		got, err := yamlnode.FirstDocument([]byte(c.in))
		if err != nil || string(got) != c.want {
			t.Errorf("FirstDocument(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
}
