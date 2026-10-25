package load

import (
	"strings"
	"testing"
)

// drain reads a source to its end, and fails when the reader yields more
// documents than the input has bytes to hold.
func drain(t *testing.T, s string, opts LoadOptions) {
	t.Helper()
	r := Open(Source{Reader: strings.NewReader(s)}, 0, opts)
	for n := 0; ; n++ {
		if _, err := r.Next(); err != nil {
			return
		}
		if n > len(s)+1 {
			t.Fatalf("%d documents from %d bytes", n, len(s))
		}
	}
}

func FuzzJSONReader(f *testing.F) {
	for _, s := range []string{`{"a":[1,2]}`, `1 2 3`, `"a\ud800"`, "\"a\xffb\"", `[1e999999999, 123456789012345678901234567890]`, `{"a":`, "\x1e1\n\x1e2"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		drain(t, s, LoadOptions{Format: FormatJSON})
		drain(t, s, LoadOptions{Format: FormatJSON, Seq: true})
		drain(t, s, LoadOptions{Format: FormatJSON, Stream: true})
	})
}

func FuzzYAMLReader(f *testing.F) {
	for _, s := range []string{"a: &x [1, *x]\n", "---\na: 1\n---\nb: 2\n", "a: !!binary aGk=\n", "? [a]\n: b\n", "a: &a [*a]\n", "- 0x10\n- 012\n- .5\n- .inf\n"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		drain(t, s, LoadOptions{Format: FormatYAML, AliasBudget: 1 << 16})
		drain(t, s, LoadOptions{Format: FormatAuto, AliasBudget: 1 << 16, DepthLimit: 64})
	})
}
