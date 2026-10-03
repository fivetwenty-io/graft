package yamlnode_test

import (
	"errors"
	"fmt"
	"runtime"
	"strings"
	"testing"

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
