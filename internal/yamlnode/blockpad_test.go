package yamlnode

import "testing"

func TestPadBlockScalarLines(t *testing.T) {
	cases := []struct {
		name, in, want string
		changed        bool
	}{
		{"mapping value", "k: |2-\n\n  a\n\n", "k: |2-\n  \n  a\n  \n", true},
		{"keep block before a key", "a: |2+\n\nb: 1\n", "a: |2+\n  \nb: 1\n", true},
		{"nested mapping", "a:\n  k: |2+\n\n  z: 1\n", "a:\n  k: |2+\n    \n  z: 1\n", true},
		{"sequence item", "l:\n- |2+\n\n- x\n", "l:\n- |2+\n  \n- x\n", true},
		{"mapping in a sequence", "l:\n- k: |1-\n\n   x\n", "l:\n- k: |1-\n   \n   x\n", true},
		{"nested sequence", "- - |2+\n\n", "- - |2+\n    \n", true},
		{"explicit key", "? |2+\n\n: 1\n", "? |2+\n  \n: 1\n", true},
		{"explicit value", "? a\n: |2+\n\n", "? a\n: |2+\n  \n", true},
		{"tag, anchor, and comment", "k: !!str &a |+2 # c\n\n", "k: !!str &a |+2 # c\n  \n", true},
		{"quoted key", "\"q: r\": >2+\n\n", "\"q: r\": >2+\n  \n", true},
		{"space line longer than the indentation", "k: |1+\n   \n", "k: |1+\n   \n", false},
		{"content ends the block", "k: |2-\n  a\nz: |\n\n  b\n", "k: |2-\n  a\nz: |\n\n  b\n", false},
		{"no indentation indicator", "k: |-\n\n  a\n", "k: |-\n\n  a\n", false},
		{"top level of a document", "--- |2+\n\n", "--- |2+\n\n", false},
		{"last line with no line break", "k: |2+\n\n ", "k: |2+\n  \n ", true},
		{"header inside a block", "k: |2\n  x: |2+\n\n", "k: |2\n  x: |2+\n  \n", true},
		{"plain scalar ending in a bar", "k: a |2\n\n", "k: a |2\n\n", false},
		{"block without an indicator holds a header", "script: |\n  x: |4\n\n  y\nk: |2+\n\nb: 1\n", "script: |\n  x: |4\n\n  y\nk: |2+\n  \nb: 1\n", true},
		{"folded block holds a header", "s: >\n  k: |2-\n\n    a\nz: |2-\n\n  last\n\n", "s: >\n  k: |2-\n\n    a\nz: |2-\n  \n  last\n  \n", true},
		{"sequence item block holds a header", "l:\n- |\n  x: |2\n\n  y\n- |2+\n\n", "l:\n- |\n  x: |2\n\n  y\n- |2+\n  \n", true},
		{"indicator block holds a header", "a: |2\n  k: |4\n\n  y\n", "a: |2\n  k: |4\n  \n  y\n", true},
		{"document block holds a header", "--- |\n x: |2\n\n y\n", "--- |\n x: |2\n\n y\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, changed := padBlockScalarLines([]byte(c.in))
			if string(got) != c.want || changed != c.changed {
				t.Errorf("padBlockScalarLines(%q) = %q, %v; want %q, %v", c.in, got, changed, c.want, c.changed)
			}
		})
	}
}
