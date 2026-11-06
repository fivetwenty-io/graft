package interfaces

import (
	"strings"
	"testing"
)

// TestTokenizerEmptySegmentStaysOneReference pins that a run of dots
// inside a path stays in the one reference token, so the parser sees the
// whole path and can refuse its empty segment by name. A '.' before a
// name with nothing ahead of it is still a dot token.
func TestTokenizerEmptySegmentStaysOneReference(t *testing.T) {
	ref := func(s string) string { return TokenReference.String() + "(" + s + ")" }
	cases := []struct {
		name string
		src  string
		want []string
	}{
		{"two dots between names", "meta..a", []string{ref("meta..a")}},
		{"three dots between names", "meta...a", []string{ref("meta...a")}},
		{"two dots after the root", "$..list", []string{ref("$..list")}},
		{"two dots later in a root path", "$.meta..a", []string{ref("$.meta..a")}},
		{"two dots at the end", "meta..", []string{ref("meta..")}},
		{"two dots before an index", "list..[0]", []string{ref("list..[0]")}},
		{"one trailing dot", "meta.", []string{ref("meta.")}},
		{"a leading dot", ".meta", []string{TokenDot.String() + "(.)", TokenIdentifier.String() + "(meta)"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := slashTokens(t, tc.src)
			if strings.Join(got, " ") != strings.Join(tc.want, " ") {
				t.Fatalf("tokens for %q:\n got  %v\n want %v", tc.src, got, tc.want)
			}
		})
	}
}
