package main

import (
	"strings"
	"testing"
)

// mergeOnce runs `graft merge` on text and returns its stdout.
func mergeOnce(t *testing.T, text string) string {
	t.Helper()
	path := writeCRLFFile(t, "in.yml", text)
	stdout, stderr, rc := runGraftCommand(t, []string{"merge", path})
	if rc != 0 || stderr != "" {
		t.Fatalf("graft merge: rc=%d stderr=%q stdout=%q", rc, stderr, stdout)
	}
	return stdout
}

// TestMergeKeepsQuotesOnLiteralMergeKeyName merges documents with a
// quoted "<<" key, which is a literal string key. The output keeps the
// quotes, so merging the output again gives the same document.
func TestMergeKeepsQuotesOnLiteralMergeKeyName(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"map value", "a:\n  \"<<\": {x: 1}\n  b: 1\n", "---\na:\n  \"<<\":\n    x: 1\n  b: 1\n\n"},
		{"scalar value", "a:\n  \"<<\": literal\n  b: 1\n", "---\na:\n  \"<<\": literal\n  b: 1\n\n"},
		{"top level", "\"<<\": s\nk: 1\n", "---\n\"<<\": s\nk: 1\n\n"},
		{"single quoted", "'<<': s\n", "---\n\"<<\": s\n\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			first := mergeOnce(t, c.in)
			if first != c.want {
				t.Errorf("first merge = %q, want %q", first, c.want)
			}
			if second := mergeOnce(t, first); second != first {
				t.Errorf("second merge = %q, want %q", second, first)
			}
		})
	}
}

// TestMergeStillResolvesRealMergeKey checks that an unquoted "<<" key
// with an anchor still merges, and leaves no "<<" in the output.
func TestMergeStillResolvesRealMergeKey(t *testing.T) {
	in := "base: &b {x: 1, p: 2}\nuse:\n  <<: *b\n  p: 3\n"
	want := "---\nbase:\n  p: 2\n  x: 1\nuse:\n  p: 3\n  x: 1\n\n"
	got := mergeOnce(t, in)
	if got != want || strings.Contains(got, "<<") {
		t.Errorf("merge = %q, want %q", got, want)
	}
}
