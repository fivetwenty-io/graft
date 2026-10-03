package yamlnode

import (
	"errors"
	"testing"

	"github.com/goccy/go-yaml/parser"
)

// TestNormalizeLineBreaksSharesInputWithoutCR checks that Parse makes no
// copy of a stream that has no CR or U+0085 to turn into LF, including
// one that holds U+2028, U+2029, or other multibyte characters.
func TestNormalizeLineBreaksSharesInputWithoutCR(t *testing.T) {
	in := []byte("a: 1\nb: 2\n")
	if out := normalizeLineBreaks(in); len(out) != len(in) || &out[0] != &in[0] {
		t.Error("input without CR must come back as the original slice")
	}
	other := []byte("a: \u2028\u2029\u00e9\u0084\u0086 \u0185\n")
	if out := normalizeLineBreaks(other); len(out) != len(other) || &out[0] != &other[0] {
		t.Error("input with no CR or U+0085 must come back as the original slice")
	}
	if got := string(normalizeLineBreaks([]byte("a\r\nb\rc\n"))); got != "a\nb\nc\n" {
		t.Errorf("normalizeLineBreaks = %q, want %q", got, "a\nb\nc\n")
	}
}

// TestNormalizeLineBreaksTurnsNELIntoLF checks that U+0085 becomes one
// LF, alone or beside CR and LF, and that U+2028 and U+2029 stay as they
// are. A byte 0x85 inside another character is not a next-line.
func TestNormalizeLineBreaksTurnsNELIntoLF(t *testing.T) {
	for in, want := range map[string]string{
		"a\u0085b\n":            "a\nb\n",
		"a\u0085\u0085b":        "a\n\nb",
		"a\r\u0085b\r\nc\u0085": "a\n\nb\nc\n",
		"a\u2028b\u2029c":       "a\u2028b\u2029c",
		"a\u0145b":              "a\u0145b",
	} {
		if got := string(normalizeLineBreaks([]byte(in))); got != want {
			t.Errorf("normalizeLineBreaks(%q) = %q, want %q", in, got, want)
		}
	}
}

// streamTextSink keeps streamText's result escaping, so the allocation
// count cannot drop to zero when the compiler inlines the call.
var streamTextSink string

// TestStreamTextCopiesOnce checks that turning the prepared bytes into
// the text Parse tokenizes copies them once, with or without the final
// line break Parse adds.
func TestStreamTextCopiesOnce(t *testing.T) {
	for _, c := range []struct {
		in, want string
		added    bool
	}{
		{"a: 1\n", "a: 1\n", false},
		{"a: 1", "a: 1\n", true},
		{"", "", false},
	} {
		in := []byte(c.in)
		if text, added := streamText(in); text != c.want || added != c.added {
			t.Errorf("streamText(%q) = %q, %v; want %q, %v", c.in, text, added, c.want, c.added)
		}
		if c.in == "" {
			continue
		}
		if n := testing.AllocsPerRun(10, func() { streamTextSink, _ = streamText(in) }); n != 1 {
			t.Errorf("streamText(%q) allocated %v times, want 1", c.in, n)
		}
	}
}

// TestKindTagRetryNeedsAKindMismatch checks that Parse retries a chunk
// only when goccy refused it for a core tag over a node of another kind,
// and that the retry blanks that tag and nothing else.
func TestKindTagRetryNeedsAKindMismatch(t *testing.T) {
	for _, in := range []string{
		"a: b: c\n",
		"k: !!str : v\n",
		"k: !!str\n  |\n    x\n",
		"k: !!str\n  !!map\n  - x\n",
		"a: [\n",
	} {
		_, err := parser.ParseBytes([]byte(in), 0, parser.AllowDuplicateMapKey())
		if err == nil {
			t.Fatalf("goccy accepted %q", in)
		}
		if _, _, ok := neutralizeKindTags(in, err); ok {
			t.Errorf("neutralizeKindTags(%q, %v) asked for a retry", in, err)
		}
	}
	if _, _, ok := neutralizeKindTags("k: !!map\n  - a\n", errors.New("could not find map")); ok {
		t.Error("an error that is not goccy's syntax error asked for a retry")
	}
	in := "a: !!str x\nk: !!map # c\n  - a\n"
	_, err := parser.ParseBytes([]byte(in), 0, parser.AllowDuplicateMapKey())
	text, tags, ok := neutralizeKindTags(in, err)
	if want := "a: !!str x\nk: !xxxx # c\n  - a\n"; !ok || text != want {
		t.Fatalf("neutralizeKindTags(%q) = %q, %v, want %q", in, text, ok, want)
	}
	if len(tags) != 1 || tags[[2]int{2, 4}] != "!!map" {
		t.Errorf("the original tags = %v, want !!map at 2:4", tags)
	}
}
