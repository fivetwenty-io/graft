package main

import (
	"testing"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
)

// TestYQKeepsMergeKeyTags pins `yq .` on tagged merge keys, plain and
// with -C. The !!merge and !!str tags print back as the input wrote
// them, with an anchor in front of the tag, in a block or flow mapping,
// and after "?", which prints as a simple key.
func TestYQKeepsMergeKeyTags(t *testing.T) {
	cases := []struct{ src, want string }{
		{"b: &b {a: 1}\nm:\n  !!merge <<: *b\n  c: 2\n", "b: &b {a: 1}\nm:\n  !!merge <<: *b\n  c: 2\n"},
		{"b: &b {a: 1}\nm: {!!merge <<: *b, c: 2}\n", "b: &b {a: 1}\nm: {!!merge <<: *b, c: 2}\n"},
		{"b: &b {a: 1}\nm:\n  &x !!merge <<: *b\n  c: 2\n", "b: &b {a: 1}\nm:\n  &x !!merge <<: *b\n  c: 2\n"},
		{"b: &b {a: 1}\nm:\n  ? !!merge <<\n  : *b\n  c: 2\n", "b: &b {a: 1}\nm:\n  !!merge <<: *b\n  c: 2\n"},
		{"m:\n  !!str <<: {a: 1}\n", "m:\n  !!str <<: {a: 1}\n"},
	}
	for _, c := range cases {
		out, errOut, st := runYQ(t, []string{"."}, c.src)
		if st.Code != 0 || out != c.want {
			t.Errorf("%q: yq . = %q (%q, status %d), want %q", c.src, out, errOut, st.Code, c.want)
		}
		colored, _, st := runYQ(t, []string{"-C", "."}, c.src)
		if st.Code != 0 || colored == c.want || ansi.StripEscapes(colored) != c.want {
			t.Errorf("%q: yq -C . = %q, want %q in color", c.src, colored, c.want)
		}
	}
}
