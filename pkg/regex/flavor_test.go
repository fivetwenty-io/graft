package regex

import (
	"strings"
	"testing"
)

// The expected matches in this file were recorded from pcre2test 10.49 and
// jq 1.8.2.

func TestFlavorDifferences(t *testing.T) {
	onig := WithFlavor(Oniguruma)
	// \h is horizontal space under PCRE and the letter h under Oniguruma,
	// as jq 1.8.2 reads it, rather than the hex digit Ruby's syntax gives.
	wantMatch(t, MustCompile(`\h`, onig), "h")
	wantNoMatch(t, MustCompile(`\h`, onig), "f")
	wantNoMatch(t, MustCompile(`\h`), "h")
	wantMatch(t, MustCompile(`\h`), " ")
	wantNoMatch(t, MustCompile(`\h`, onig), " ")
	wantMatch(t, MustCompile(`^\d$`, onig), "٣")
	wantNoMatch(t, MustCompile(`^\d$`), "٣")
	wantMatch(t, MustCompile(`^\w$`, onig), "é")
	wantNoMatch(t, MustCompile(`^\w$`), "é")
	wantMatch(t, MustCompile(`^\s$`, onig), "\u2028")
	wantNoMatch(t, MustCompile(`^\s$`), "\u2028")
	wantNoMatch(t, MustCompile(`x\b`, onig), "xé")
	wantMatch(t, MustCompile(`x\b`), "xé")

	// Both flavors let $ and \Z match before a final newline, and the
	// inline m and s mean multiline and dot-all in both.
	for _, flavor := range []Flavor{PCRE, Oniguruma} {
		on := WithFlavor(flavor)
		wantMatch(t, MustCompile(`a$`, on), "a\n")
		wantMatch(t, MustCompile(`a\Z`, on), "a\n")
		wantNoMatch(t, MustCompile(`a$`, on), "a\n\n")
		wantNoMatch(t, MustCompile(`(?m)a.b`, on), "a\nb")
		wantMatch(t, MustCompile(`(?m)^b`, on), "a\nb")
		wantMatch(t, MustCompile(`(?s)a.b`, on), "a\nb")
	}
}

// Oniguruma reads a backslash before a letter that names nothing as the
// letter, where PCRE2 refuses it.
func TestOnigurumaLetterEscapes(t *testing.T) {
	onig := WithFlavor(Oniguruma)
	cases := []struct{ pattern, subject, want string }{
		{`\U`, "xU", "U"},
		{`\C\F\I\J\L\M\T\V`, "CFIJLMTV", "CFIJLMTV"},
		{`\i\j\l\m\q`, "ijlmq", "ijlmq"},
		{`x\Ey`, "xEy", "xEy"},
		{`\kx`, "kx", "kx"},
		{`(a)\k{1}`, "ak{1}", "ak"},
		{`(a)\g1`, "ag1", "ag1"},
		{`\P`, "P", "P"},
		{`\pL`, "pL", "pL"},
		{`[\pL]+`, "xpL", "pL"},
		{`[\U\q]+`, "UqX", "Uq"},
		{`\o`, "o", "o"},
		{`a\Ob`, "a\nb", "a\nb"},
	}
	for _, c := range cases {
		wantText(t, MustCompile(c.pattern, onig), c.subject, c.want)
	}
	wantNoMatch(t, MustCompile(`(a)\k{1}`, onig), "aa")

	// The letters Oniguruma gives a meaning this engine does not implement
	// are refused by name rather than read as letters.
	for pattern, construct := range map[string]string{
		`\y`: `text segment boundary \y`,
		`\Y`: `text segment boundary \Y`,
		`\X`: `extended grapheme cluster \X`,
		`\R`: `newline sequence \R`,
		`\K`: `match reset \K`,
		`\G`: `assertion \G`,
	} {
		_, err := Compile(pattern, onig)
		var u *UnsupportedError
		wantErrAs(t, err, &u)
		wantEqual(t, u.Construct, construct)
	}

	// PCRE refuses the same letters and reads \pL as a property.
	for _, pattern := range []string{`\U`, `\q`, `\kx`, `\o`, `\O`} {
		_, err := Compile(pattern)
		var se *SyntaxError
		wantErrAs(t, err, &se)
	}
	wantText(t, MustCompile(`\pL+`), "1pL", "pL")
	wantText(t, MustCompile(`x\Ey`), "xy", "xy")
}

// Oniguruma reads \xhh and octal escapes as UTF-8 bytes, so a run of them
// spells one character, where PCRE reads each as a code point.
func TestOnigurumaByteEscapes(t *testing.T) {
	onig := WithFlavor(Oniguruma)
	cases := []struct{ pattern, subject, want string }{
		{`\xc3\xa9`, "café", "é"},
		{`\303\251`, "café", "é"},
		{`[\xc3\xa9]`, "café", "é"},
		{`\xc3\xa9+`, "aééb", "éé"},
		{`\xf0\x9f\x98\x80`, "a😀", "😀"},
		{`(?x)\xc3 \xa9`, "é", "é"},
		{`\xc3(?#c)\xa9`, "é", "é"},
		{`\400`, "a\x00", "\x00"},
		{`\500`, "a@", "@"},
		{`[a-\xff]+`, "azÿ", "az"},
		{`[\xa0-\xc3\xa9]+`, "a\u00a0é", "\u00a0é"},
		{`[\xc0\x80-\xc3\xa9]+`, "aé", "é"},
		{`a|\xff`, "ÿa", "a"},
	}
	for _, c := range cases {
		wantText(t, MustCompile(c.pattern, onig), c.subject, c.want)
	}
	wantText(t, MustCompile(`\xc3\x89`, onig, WithFlags(IgnoreCase)), "café", "é")
	for _, pattern := range []string{`\777`, `\xc0\x80`, `\xed\xa0\x80`, `\xf8`, `[\xa0-\xff]`, `[\xa9]`} {
		wantNoMatch(t, MustCompile(pattern, onig), "aÿǿ\x00\u00a0©")
	}
	wantSyntaxError(t, `a\xe9`, 1, "too short multibyte code string", onig)
	wantSyntaxError(t, `\xc3\x{a9}`, 0, "too short multibyte code string", onig)
	wantSyntaxError(t, `[\351]`, 1, "too short multibyte code string", onig)
	wantSyntaxError(t, `\200`, 0, "invalid code point value", onig)
	wantSyntaxError(t, `\xc3\x41`, 0, "invalid code point value", onig)
	wantSyntaxError(t, `[\xc3\x41]`, 1, "invalid code point value", onig)
	wantSyntaxError(t, `[\500]`, 1, "too big number", onig)

	// PCRE reads each escape as a code point.
	wantText(t, MustCompile(`\xe9`), "café", "é")
	wantText(t, MustCompile(`\xc3\xa9`), "Ã©", "Ã©")
	wantText(t, MustCompile(`[\400]`), "Ā", "Ā")
}

func TestOnigurumaQuantifierRules(t *testing.T) {
	onig := WithFlavor(Oniguruma)
	wantText(t, MustCompile(`a{,2}`, onig), "aa{,2}", "a{,2}")
	wantNoMatch(t, MustCompile(`a{,2}`, onig), "aa")
	wantText(t, MustCompile(`a{,2}`), "aaa", "aa")

	// A quantifier on a quantifier repeats the repetition.
	wantText(t, MustCompile(`^a{2}{3}$`, onig), "aaaaaa", "aaaaaa")
	wantNoMatch(t, MustCompile(`^a{2}{3}$`, onig), "aaaaa")
	wantText(t, MustCompile(`a{1}{2}`, onig), "aaaa", "aa")
	wantText(t, MustCompile(`a**`, onig), "aaa", "aaa")
	wantText(t, MustCompile(`a+*b`, onig), "aab", "aab")
	wantText(t, MustCompile(`(?:ab){1,2}{2}`, onig), "ababababab", "abababab")
	wantText(t, MustCompile(`a?+`, onig), "aaa", "a")
	wantText(t, MustCompile(`a{1,2}+`, onig), "aaa", "aa")
	wantText(t, MustCompile(`a+?`, onig), "aaa", "a")
	for _, pattern := range []string{`a**`, `a{2}{3}`} {
		_, err := Compile(pattern)
		var se *SyntaxError
		wantErrAs(t, err, &se)
	}

	// Oniguruma allows counts up to 100,000, where PCRE2 stops at 65,535.
	roomy := WithBudget(Budget{MaxInstructions: 1 << 20})
	wantMatch(t, MustCompile(`^a{100000}$`, onig, roomy), strings.Repeat("a", 100000))
	wantNoMatch(t, MustCompile(`^a{100000}$`, onig, roomy), strings.Repeat("a", 99999))
	wantSyntaxError(t, `a{100001}`, 1, "too big number for repeat range", onig)
	wantSyntaxError(t, `a{65536}`, 1, "number too big in {} quantifier")
}

// A run of stacked quantifiers as long as the pattern cap allows compiles,
// and one walk of the tree answers nullable for every level of the stack,
// so the compiler's question at each level costs nothing more. Before
// nullable remembered its answers the 64 KiB pattern took tens of seconds.
func TestStackedQuantifiersCompileInOneWalk(t *testing.T) {
	onig := WithFlavor(Oniguruma)
	for _, q := range []string{"+", "+?", "*"} {
		_, err := Compile("a"+strings.Repeat(q, (64<<10-1)/len(q)), onig)
		wantNil(t, err)
	}
	// A counted stack doubles the program at each level, so the
	// instruction cap refuses it.
	_, err := Compile("a"+strings.Repeat("{1,2}+", (64<<10-1)/6), onig)
	wantErrIs(t, err, ErrBudget)

	leaf := &node{kind: kindLiteral, runes: []rune("a")}
	root := leaf
	for range 1 << 16 {
		root = &node{kind: kindRepeat, subs: []*node{root}, min: 1, max: unbounded, greedy: true}
	}
	wantEqual(t, nullable(root), false)
	for n := root; ; n = n.subs[0] {
		if !n.nullKnown {
			t.Fatal("a node below the root was not answered by the walk from the root")
		}
		if n == leaf {
			break
		}
	}
}

func TestOnigurumaHyphenAndBarePosix(t *testing.T) {
	onig := WithFlavor(Oniguruma)
	// A hyphen after a class escape or a POSIX name is a literal hyphen.
	wantText(t, MustCompile(`[\d-z]+`, onig), "d-z", "-z")
	wantText(t, MustCompile(`[\w-\d]+`, onig), "a-1", "a-1")
	wantText(t, MustCompile(`[[:alpha:]-z]+`, onig), "1a-z", "a-z")
	wantSyntaxError(t, `[\d-z]`, 1, "invalid range in character class")
	_, err := Compile(`[a-\d]`, onig)
	var se *SyntaxError
	wantErrAs(t, err, &se)

	// [:alpha:] outside a class is the class of its runes.
	wantText(t, MustCompile(`[:alpha:]+`, onig), "a:x", "a:")
	wantSyntaxError(t, `[:alpha:]`, 0, "POSIX named classes are supported only within a class")
}

func TestOnigurumaPropertyNames(t *testing.T) {
	onig := WithFlavor(Oniguruma)
	cases := []struct{ pattern, subject, want string }{
		{`\p{Alpha}+`, "1éa!", "éa"},
		{`\p{Alnum}+`, "!é1", "é1"},
		{`\p{Word}+`, "!_a", "_a"},
		{`\p{Punct}+`, "€!", "!"},
		{`\p{XDigit}+`, "xA9", "A9"},
		{`\p{Digit}+`, "a٣1", "٣1"},
		{`\p{Space}+`, "a\u2028 ", "\u2028 "},
		{`\p{Upper}+`, "aÉB", "ÉB"},
		{`\p{Lower}+`, "Aéb", "éb"},
		{`\p{ASCII}+`, "éa1", "a1"},
		{`\p{Lowercase_Letter}+`, "Aé", "é"},
		{`\p{lowercaseletter}+`, "Aé", "é"},
		{`\p{Decimal_Number}+`, "a٣", "٣"},
		{`[\p{Alpha}]+`, "1é", "é"},
		{`\P{Alpha}+`, "é12", "12"},
	}
	for _, c := range cases {
		wantText(t, MustCompile(c.pattern, onig), c.subject, c.want)
	}
	// A property outside a class keeps its case under IgnoreCase.
	wantNoMatch(t, MustCompile(`\p{Lower}`, onig, WithFlags(IgnoreCase)), "É")
	wantSyntaxError(t, `\p{L&}`, 0, "invalid character property name {L&}", onig)
	wantSyntaxError(t, `\p{Foo}`, 0, "invalid character property name {Foo}", onig)
	wantMatch(t, MustCompile(`\p{L&}`), "a")
}

func TestOnigurumaExtendedSkipsASCIISpaceOnly(t *testing.T) {
	onig := WithFlavor(Oniguruma)
	ext := WithFlags(Extended)
	for _, space := range []string{"\u0085", "\u00a0", "\u2028", "\v"} {
		wantNoMatch(t, MustCompile("a"+space+"b", onig, ext), "ab")
	}
	for _, space := range []string{" ", "\t", "\n", "\r", "\f"} {
		wantMatch(t, MustCompile("a"+space+"b", onig, ext), "ab")
	}
	for _, space := range []string{"\u0085", "\u2028", "\v"} {
		wantMatch(t, MustCompile("a"+space+"b", ext), "ab")
	}
}

func TestOnigurumaGroupSyntax(t *testing.T) {
	onig := WithFlavor(Oniguruma)
	wantSyntaxError(t, `(?P<n>a)`, 2, "undefined group option", onig)
	wantSyntaxError(t, `(?P=n)`, 2, "undefined group option", onig)
	wantText(t, MustCompile(`(?P<n>a)(?P=n)`), "aa", "aa")

	// A number past the group count that starts with 8 or 9 is that digit,
	// and the rest of the number is read again as text.
	wantText(t, MustCompile(`(a)\81`, onig), "a81", "a81")
	wantText(t, MustCompile(`(a)\91`, onig), "a91", "a91")
	wantSyntaxError(t, `(a)\8`, 3, "invalid backref number/name", onig)

	// A \k before < or ' starts a reference, so an unclosed one is an
	// error rather than letters.
	wantSyntaxError(t, `\k<`, 0, "group name is empty", onig)
	wantSyntaxError(t, `\k''`, 0, "group name is empty", onig)
	wantSyntaxError(t, `(?<a>x)\k<a`, 7, "invalid group name <a>", onig)
	wantSyntaxError(t, `(a)\81`, 3, "reference to non-existent subpattern")
}

// Every flavor has rules, and a flavor the package does not define reads
// patterns as PCRE does.
func TestFlavorRulesTable(t *testing.T) {
	wantEqual(t, PCRE.rules().maxRepeat, 65535)
	wantEqual(t, Oniguruma.rules().maxRepeat, 100000)
	wantEqual(t, Flavor(9).rules(), PCRE.rules())
}
