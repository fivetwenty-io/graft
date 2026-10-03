package yamlnode

import "testing"

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
