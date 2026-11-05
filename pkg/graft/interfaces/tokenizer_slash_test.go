package interfaces

import (
	"strings"
	"testing"
)

// slashTokens tokenizes src the way the expression parser does and
// renders the stream as "TYPE(literal)" entries, dropping the final EOF.
func slashTokens(t *testing.T, src string) []string {
	t.Helper()
	tz := NewAdvancedTokenizer(src, TokenizerOptions{
		RecognizeReferencePaths: true,
		AllowEnvironmentVars:    true,
		TrackPositions:          true,
		AllowUnicode:            true,
	})
	var out []string
	for i := 0; i < 64; i++ {
		tok := tz.NextToken()
		if tok.Type == TokenEOF {
			return out
		}
		out = append(out, tok.Type.String()+"("+tok.Literal+")")
	}
	t.Fatalf("tokenizer did not reach EOF for %q", src)
	return nil
}

// TestTokenizerSlashContinuesReference pins how a '/' is lexed. A '/'
// that directly follows a reference character is part of the reference,
// as in BOSH's name/index instance notation and as spruce reads it. A
// '/' with whitespace before it is the division operator.
func TestTokenizerSlashContinuesReference(t *testing.T) {
	ref := func(s string) string { return TokenReference.String() + "(" + s + ")" }
	ident := func(s string) string { return TokenIdentifier.String() + "(" + s + ")" }
	slash := TokenSlash.String() + "(/)"

	cases := []struct {
		name string
		src  string
		want []string
	}{
		{"instance index inside a dotted path", "jobs.postgresql/0.ips[0]", []string{ref("jobs.postgresql/0.ips[0]")}},
		{"slash in the last segment", "jobs.n/2", []string{ref("jobs.n/2")}},
		{"several slashes in one segment", "meta.a/b/c", []string{ref("meta.a/b/c")}},
		{"slash in the first segment", "postgresql/0.ips[0]", []string{ref("postgresql/0.ips[0]")}},
		{"single segment with a slash", "a/b", []string{ref("a/b")}},
		{"numeric segment with a slash", "8/2", []string{ref("8/2")}},
		{"float-looking segments with a slash", "1.5/2", []string{ref("1.5/2")}},
		{"trailing slash", "a/", []string{ref("a/")}},
		{"trailing slash before a space", "a/ b", []string{ref("a/"), ident("b")}},
		{"slash before a bracket", "arr/[0]", []string{ref("arr/[0]")}},
		{"slash after a bracket", "arr[0]/x", []string{ref("arr[0]/x")}},
		{"environment variable followed by a slash", "$X/2", []string{ref("$X/2")}},
		{"environment variable after a slash continues the path", "a/$X", []string{ref("a/$X")}},
		{"environment variable after a slash mid-path", "m.x/$X.y", []string{ref("m.x/$X.y")}},
		{"slash directly after a dot continues the path", "m./x", []string{ref("m./x")}},
		{"slash after a dotted index continues the path", "arr.0./x", []string{ref("arr.0./x")}},
		{"plus name after a slash joins the segment", "m.x/b+c", []string{ref("m.x/b+c")}},
		{"plus digit after a slash adds", "m.x/b+1", []string{ref("m.x/b"), TokenPlus.String() + "(+)", TokenInteger.String() + "(1)"}},
		{"spaced slash divides", "a / b", []string{ident("a"), slash, ident("b")}},
		{"slash with a space before it divides", "a /b", []string{ident("a"), slash, ident("b")}},
		{"spaced slash after a dotted path divides", "jobs.n / 2", []string{ref("jobs.n"), slash, TokenInteger.String() + "(2)"}},
		{"leading slash stays an operator", "/b", []string{slash, ident("b")}},
		{"slash after a closing paren divides", "(a)/2", []string{
			TokenLeftParen.String() + "(()", ident("a"), TokenRightParen.String() + "())", slash, TokenInteger.String() + "(2)",
		}},
		{"slash after a string divides", `"s"/2`, []string{TokenString.String() + `("s")`, slash, TokenInteger.String() + "(2)"}},
		{"plain number is still a number", "8", []string{TokenInteger.String() + "(8)"}},
		{"plain float is still a float", "1.5", []string{TokenFloat.String() + "(1.5)"}},
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

// TestTokenizerExponentNumberBeforeSlash pins that a number in exponent
// form followed directly by a '/' is a path, the way "8/2" is, as spruce
// reads it, while the same number with no '/' stays a number.
func TestTokenizerExponentNumberBeforeSlash(t *testing.T) {
	ref := func(s string) string { return TokenReference.String() + "(" + s + ")" }
	float := func(s string) string { return TokenFloat.String() + "(" + s + ")" }

	cases := []struct {
		name string
		src  string
		want []string
	}{
		{"exponent with a plus sign", "1e+5/2", []string{ref("1e+5/2")}},
		{"upper-case exponent", "1E5/2", []string{ref("1E5/2")}},
		{"upper-case exponent with a plus sign", "1E+5/2", []string{ref("1E+5/2")}},
		{"exponent with a minus sign", "1e-5/2", []string{ref("1e-5/2")}},
		{"fraction with an exponent", "1.5e3/2", []string{ref("1.5e3/2")}},
		{"fraction with a signed exponent", "1.5e+3/2", []string{ref("1.5e+3/2")}},
		{"exponent after a slash", "x/1e+5", []string{ref("x/1e+5")}},
		{"plus name after a digit segment", "z.pg/0+meta.name", []string{ref("z.pg/0+meta.name")}},
		{"exponent alone is a number", "1e+5", []string{float("1e+5")}},
		{"fraction with an exponent alone is a number", "1.5e+3", []string{float("1.5e+3")}},
		{"spaced division of an exponent", "1e+5 / 2", []string{
			float("1e+5"), TokenSlash.String() + "(/)", TokenInteger.String() + "(2)",
		}},
		{"exponent then a plus digit adds", "2e+3+1", []string{
			float("2e+3"), TokenPlus.String() + "(+)", TokenInteger.String() + "(1)",
		}},
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

// TestTokenizerDollarSegmentGluesPlusName pins that a '+' followed by a
// name continues a "$name" segment after a '/', the same way it does
// after a plain name, so "a/$X+y" is one reference, as spruce reads it.
func TestTokenizerDollarSegmentGluesPlusName(t *testing.T) {
	ref := func(s string) string { return TokenReference.String() + "(" + s + ")" }

	cases := []struct {
		name string
		src  string
		want []string
	}{
		{"plus name after a dollar segment", "a/$X+y", []string{ref("a/$X+y")}},
		{"plus name after a dollar segment mid-path", "m.x/$X+y.k", []string{ref("m.x/$X+y.k")}},
		{"plus digit after a dollar segment adds", "a/$X+1", []string{
			ref("a/$X"), TokenPlus.String() + "(+)", TokenInteger.String() + "(1)",
		}},
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
