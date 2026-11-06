package interfaces

import (
	"strings"
	"testing"
)

// TestTokenizerDigitLedName pins that a run starting with a digit that
// is not purely a number is a name, the way spruce reads it. A number is
// digits with an optional fraction and an optional exponent, and an
// exponent needs a digit after the 'e' or its sign. Anything else glued
// on after the number makes the whole run one reference.
func TestTokenizerDigitLedName(t *testing.T) {
	ref := func(s string) string { return TokenReference.String() + "(" + s + ")" }
	integer := func(s string) string { return TokenInteger.String() + "(" + s + ")" }
	float := func(s string) string { return TokenFloat.String() + "(" + s + ")" }
	ident := func(s string) string { return TokenIdentifier.String() + "(" + s + ")" }
	plus := TokenPlus.String() + "(+)"
	minus := TokenMinus.String() + "(-)"

	cases := []struct {
		name string
		src  string
		want []string
	}{
		{"letters after digits", "3abc", []string{ref("3abc")}},
		{"an e with no exponent digits", "1e", []string{ref("1e")}},
		{"an e and more letters", "3em", []string{ref("3em")}},
		{"a letter after an exponent", "2e5x", []string{ref("2e5x")}},
		{"a letter after an exponent ending in a digit", "1e5a", []string{ref("1e5a")}},
		{"a letter after a signed exponent", "1e+5x", []string{ref("1e+5x")}},
		{"a letter after a negative exponent", "1e-5x", []string{ref("1e-5x")}},
		{"an e and a sign with no digit", "3e-x", []string{ref("3e-x")}},
		{"an underscore after digits", "3_x", []string{ref("3_x")}},
		{"a dash inside the name", "3abc-def", []string{ref("3abc-def")}},
		{"a hex-looking run", "0x1f", []string{ref("0x1f")}},
		{"a letter after a fraction", "3.5abc", []string{ref("3.5abc")}},
		{"a dotted path after the name", "3abc.def", []string{ref("3abc.def")}},
		{"an index after the name", "3abc[0]", []string{ref("3abc[0]")}},
		{"a slash after the name", "3abc/x", []string{ref("3abc/x")}},
		{"a name after a dot in a number", "1e5.x", []string{ref("1e5.x")}},
		{"a name after a plain number and a dot", "1.foo", []string{ref("1.foo")}},
		{"a name after a fraction and a dot", "1.5.x", []string{ref("1.5.x")}},
		{"a plus name after the name adds", "3abc+def", []string{ref("3abc"), plus, ident("def")}},
		{"a digit-led segment after a dot", "m.3x", []string{ref("m.3x")}},
		{"an exponent segment after a dot", "m.3e5", []string{ref("m.3e5")}},
		{"a signed exponent segment after a dot", "m.3e+5", []string{ref("m.3e+5")}},
		{"an e segment with no exponent digits", "m.1e", []string{ref("m.1e")}},
		{"a fraction then a name segment", "m.3.5abc", []string{ref("m.3.5abc")}},
		{"a name after an exponent segment", "m.2e5x", []string{ref("m.2e5x")}},
		{"a digit-led segment then more path", "m.3x.y[0]", []string{ref("m.3x.y[0]")}},
		{"an index segment stays an index", "m.0", []string{ref("m.0")}},
		{"index segments in a row", "m.0.1", []string{ref("m.0.1")}},
		{"a plus after an exponent segment adds", "a.2e+3+1", []string{ref("a.2e+3"), plus, integer("1")}},
		{"a minus after an index segment subtracts", "a.3-1", []string{ref("a.3"), minus, integer("1")}},
		{"an integer stays a number", "8", []string{integer("8")}},
		{"a fraction stays a number", "1.5", []string{float("1.5")}},
		{"an exponent stays a number", "1e5", []string{float("1e5")}},
		{"an upper-case exponent stays a number", "1E5", []string{float("1E5")}},
		{"a signed exponent stays a number", "1e+5", []string{float("1e+5")}},
		{"a negative exponent stays a number", "1e-5", []string{float("1e-5")}},
		{"a fraction with an exponent stays a number", "1.5e3", []string{float("1.5e3")}},
		{"unspaced subtraction of numbers", "3-1", []string{integer("3"), minus, integer("1")}},
		{"unspaced subtraction after an exponent", "2e5-1", []string{float("2e5"), minus, integer("1")}},
		{"unspaced subtraction of a name", "3-abc", []string{integer("3"), minus, ident("abc")}},
		{"unspaced subtraction of a path", "3-abc.def", []string{integer("3"), minus, ref("abc.def")}},
		{"unspaced subtraction of a path after a fraction", "1.5-x.y", []string{float("1.5"), minus, ref("x.y")}},
		{"unspaced addition after an exponent", "2e+3+1", []string{float("2e+3"), plus, integer("1")}},
		{"spaced arithmetic", "1e5 * 2", []string{float("1e5"), TokenStar.String() + "(*)", integer("2")}},
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
