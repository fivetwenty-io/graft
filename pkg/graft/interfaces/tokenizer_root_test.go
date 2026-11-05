package interfaces

import (
	"strings"
	"testing"
)

// TestTokenizerRootReference pins how a reference rooted at "$" is
// lexed. A '$' followed by a '.' or a '[' names the document root, as
// spruce reads it, so the whole path is one reference token. A '$'
// followed by a name is still an environment variable.
func TestTokenizerRootReference(t *testing.T) {
	ref := func(s string) string { return TokenReference.String() + "(" + s + ")" }
	ident := func(s string) string { return TokenIdentifier.String() + "(" + s + ")" }
	env := func(s string) string { return TokenEnvironment.String() + "(" + s + ")" }
	tk := func(tt TokenType, lit string) string { return tt.String() + "(" + lit + ")" }
	start := tk(TokenOperatorStart, "((")
	end := tk(TokenOperatorEnd, "))")

	cases := []struct {
		name string
		src  string
		want []string
	}{
		{"dotted path", "$.meta.a", []string{ref("$.meta.a")}},
		{"single segment", "$.a", []string{ref("$.a")}},
		{"after an operator name", "(( grab $.meta.a ))", []string{start, ident("grab"), ref("$.meta.a"), end}},
		{"bracket index", "$.list[0]", []string{ref("$.list[0]")}},
		{"dotted index", "$.list.0", []string{ref("$.list.0")}},
		{"bracket right after the dot", "$.[0]", []string{ref("$.[0]")}},
		{"bracket right after the dollar", "$[0]", []string{ref("$[0]")}},
		{"slash path", "$.jobs.postgresql/0.ips[0]", []string{ref("$.jobs.postgresql/0.ips[0]")}},
		{"slash key at the root", "$./x", []string{ref("$./x")}},
		{"environment variable segment", "$.$HOME", []string{ref("$.$HOME")}},
		{"plus name joins the segment", "$.a+b", []string{ref("$.a+b")}},
		{"plus digit adds", "$.a+1", []string{ref("$.a"), tk(TokenPlus, "+"), tk(TokenInteger, "1")}},
		{"spaced slash divides", "$.a / 2", []string{ref("$.a"), tk(TokenSlash, "/"), tk(TokenInteger, "2")}},
		{"both sides of ||", "$.a || $.b", []string{ref("$.a"), tk(TokenOr, "||"), ref("$.b")}},
		{"ternary branches", "t ? $.a : $.b", []string{
			ident("t"), tk(TokenQuestion, "?"), ref("$.a"), tk(TokenColon, ":"), ref("$.b"),
		}},
		{"dollar dot alone", "$.", []string{ref("$.")}},
		{"environment variable", "$X", []string{env("$X")}},
		{"environment variable with a slash", "$HOME/x", []string{ref("$HOME/x")}},
		{"environment variable after a slash", "a/$X", []string{ref("a/$X")}},
		{"braced environment variable", "${X}", []string{env("${X}")}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := slashTokensWithOperators(t, tc.src)
			if strings.Join(got, " ") != strings.Join(tc.want, " ") {
				t.Fatalf("tokens for %q:\n got  %v\n want %v", tc.src, got, tc.want)
			}
		})
	}
}
