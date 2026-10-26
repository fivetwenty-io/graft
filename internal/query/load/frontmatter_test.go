package load

import (
	"strings"
	"testing"
)

func TestSplitFrontMatter(t *testing.T) {
	cases := []struct {
		name, in, yaml, rest string
		found                bool
	}{
		{"markdown", "---\ntitle: x\n---\n# body\n", "---\ntitle: x\n", "---\n# body\n", true},
		{"no body", "---\ntitle: x\n---\n", "---\ntitle: x\n", "---\n", true},
		{"unterminated", "---\ntitle: x\n", "---\ntitle: x\n", "", true},
		{"no leading line", "title: x\n---\nbody\n", "title: x\n---\nbody\n", "", false},
		{"blank line first", "\n---\nt: 1\n---\nbody\n", "\n---\nt: 1\n---\nbody\n", "", false},
		{"opening with a comment", "--- # c\nt: 1\n---\nb\n", "--- # c\nt: 1\n---\nb\n", "", false},
		{"opening with trailing space", "--- \nt: 1\n---\nb\n", "--- \nt: 1\n", "---\nb\n", true},
		{"closing line with text after", "---\nt: 1\n--- # c\nb\n", "---\nt: 1\n", "--- # c\nb\n", true},
		{"four dashes close it", "---\nt: 1\n----\nb\n", "---\nt: 1\n", "----\nb\n", true},
		{"indented dashes do not", "---\nt: 1\n  ---\nb\n", "---\nt: 1\n  ---\nb\n", "", true},
		{"carriage returns", "---\r\nt: 1\r\n---\r\nb\r\n", "---\r\nt: 1\r\n", "---\r\nb\r\n", true},
		{"empty", "", "", "", false},
		{"only the second line closes", "---\n---\nbody\n", "---\n", "---\nbody\n", true},
		{"no final newline", "---\nt: 1\n---\nbody", "---\nt: 1\n", "---\nbody", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			y, r, found, err := SplitFrontMatter(strings.NewReader(c.in))
			if err != nil {
				t.Fatal(err)
			}
			if string(y) != c.yaml || string(r) != c.rest || found != c.found {
				t.Errorf("got (%q, %q, %v), want (%q, %q, %v)", y, r, found, c.yaml, c.rest, c.found)
			}
		})
	}
}
