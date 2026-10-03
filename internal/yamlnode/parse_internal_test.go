package yamlnode

import "testing"

// TestNormalizeLineBreaksSharesInputWithoutCR checks that Parse makes no
// copy of a stream that has no CR to turn into LF.
func TestNormalizeLineBreaksSharesInputWithoutCR(t *testing.T) {
	in := []byte("a: 1\nb: 2\n")
	if out := normalizeLineBreaks(in); len(out) != len(in) || &out[0] != &in[0] {
		t.Error("input without CR must come back as the original slice")
	}
	if got := string(normalizeLineBreaks([]byte("a\r\nb\rc\n"))); got != "a\nb\nc\n" {
		t.Errorf("normalizeLineBreaks = %q, want %q", got, "a\nb\nc\n")
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
