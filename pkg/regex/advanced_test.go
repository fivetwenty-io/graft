package regex

import (
	"context"
	"strings"
	"testing"
)

func TestSubjectAnchors(t *testing.T) {
	wantMatch(t, MustCompile(`\Aa\z`), "a")
	wantNoMatch(t, MustCompile(`\Aa\z`), "a\n")
	wantMatch(t, MustCompile(`\Aa\Z`), "a\n")
	wantNoMatch(t, MustCompile(`\Aa\Z`), "a\n\n")
	wantNoMatch(t, MustCompile(`\Ab`), "a\nb")
	wantEqual(t, runeStarts(t, MustCompile(`\A`), "ab", MatchOptions{Global: true}), []int{0})
	wantEqual(t, runeStarts(t, MustCompile(`\z`), "a\n", MatchOptions{Global: true}), []int{2})
	wantEqual(t, runeStarts(t, MustCompile(`\Z`), "a\n", MatchOptions{Global: true}), []int{1, 2})
	wantEqual(t, MustCompile(`\Aa`).anchored, true)
}

func TestWordBoundaries(t *testing.T) {
	wantText(t, MustCompile(`\bcat\b`), "concat cat", "cat")
	wantEqual(t, runeStarts(t, MustCompile(`\b`), "ab cd", MatchOptions{Global: true}), []int{0, 2, 3, 5})
	wantEqual(t, runeStarts(t, MustCompile(`\B`), "ab cd", MatchOptions{Global: true}), []int{1, 4})
	wantEqual(t, runeStarts(t, MustCompile(`\b`), "", MatchOptions{Global: true}), []int{})
	wantEqual(t, runeStarts(t, MustCompile(`\B`), "", MatchOptions{Global: true}), []int{0})

	// PCRE's \w is ASCII, so é is not a word character and x\b is a
	// boundary before it, while Oniguruma's \w is Unicode-aware.
	wantMatch(t, MustCompile(`x\b`), "xé")
	wantNoMatch(t, MustCompile(`x\b`, WithFlavor(Oniguruma)), "xé")
	wantEqual(t, runeStarts(t, MustCompile(`\b`, WithFlavor(Oniguruma)), "é b", MatchOptions{Global: true}), []int{0, 1, 2, 3})
}

func TestQuotedSequences(t *testing.T) {
	wantMatch(t, MustCompile(`\Qa.b\E`), "a.b")
	wantNoMatch(t, MustCompile(`\Qa.b\E`), "axb")
	wantText(t, MustCompile(`\Q(a|b)*\E`), "x(a|b)*", "(a|b)*")
	wantText(t, MustCompile(`\Qa.b`), "a.b", "a.b") // a quote may run to the end
	wantText(t, MustCompile(`\Qab\E+`), "abbbc", "abbb")
	wantText(t, MustCompile(`x\Q\Ey`), "xy", "xy")
	wantText(t, MustCompile(`a\Eb`), "ab", "ab") // a stray \E is ignored
	wantText(t, MustCompile(`\Q a #b\E`, WithFlags(Extended)), "x a #b", " a #b")
	wantText(t, MustCompile(`\Qab\E`, WithFlags(IgnoreCase)), "AB", "AB")
	wantText(t, MustCompile(`[\Q]-\E]+`), "a]-]", "]-]")
	wantText(t, MustCompile(`[a\E]+`), "aa", "aa")
	wantMatch(t, MustCompile(`[\Q.\E]`), ".")
	wantNoMatch(t, MustCompile(`[\Q.\E]`), "Q")

	// In an Oniguruma class, \Q and \E are the letters Q and E, as jq reads
	// them.
	onig := WithFlavor(Oniguruma)
	for _, s := range []string{"Q", ".", "E"} {
		wantMatch(t, MustCompile(`[\Q.\E]`, onig), s)
	}
	wantNoMatch(t, MustCompile(`[\Q.\E]`, onig), "a")
	wantText(t, MustCompile(`[a\E]+`, onig), "aEa", "aEa")
	wantNoMatch(t, MustCompile(`[\Q]-\E]+`, onig), "a]-]")

	_, err := Compile(`[\Qab`)
	var se *SyntaxError
	wantErrAs(t, err, &se)
	wantEqual(t, se.Msg, "missing terminating ] for character class")
}

func TestInlineFlags(t *testing.T) {
	// Per-line anchors come only from (?m), never from a Flags value.
	wantNoMatch(t, MustCompile(`^b`, WithFlags(Flags(1)<<15)), "a\nb")
	wantMatch(t, MustCompile(`(?i)A`), "a")
	wantText(t, MustCompile(`(a(?i)b)c`), "aBc", "aBc")
	wantNoMatch(t, MustCompile(`(a(?i)b)c`), "aBC") // a flag ends with its group
	wantMatch(t, MustCompile(`(?i:a)b`), "Ab")
	wantNoMatch(t, MustCompile(`(?i:a)b`), "AB")
	wantMatch(t, MustCompile(`a(?i)b|c`), "C") // and carries into later branches
	wantText(t, MustCompile(`(?x) a b`), "ab", "ab")
	wantNoMatch(t, MustCompile(`(?i-i)a`), "A")
	wantNoMatch(t, MustCompile(`(?i)(?-i:a)`), "A")
	wantMatch(t, MustCompile(`a(?-i)B`, WithFlags(IgnoreCase)), "AB")
	wantNoMatch(t, MustCompile(`a(?-i)B`, WithFlags(IgnoreCase)), "Ab")
	wantNoMatch(t, MustCompile(`(?i:[a])[a]`), "AA") // a class is built once per case rule
	wantMatch(t, MustCompile(`(?i:[a])[a]`), "Aa")

	wantMatch(t, MustCompile(`(?s).`), "\n")
	wantNoMatch(t, MustCompile(`.`), "\n")
	wantNoMatch(t, MustCompile(`(?-s).`, WithFlags(DotAll)), "\n")

	wantMatch(t, MustCompile(`(?m)^b`), "a\nb")
	wantMatch(t, MustCompile(`(?im:^B)`), "a\nb")
	wantNoMatch(t, MustCompile(`(?m)\Ab`), "a\nb")
	wantEqual(t, runeStarts(t, MustCompile(`(?m)^`), "a\nb\n", MatchOptions{Global: true}), []int{0, 2})
	wantEqual(t, runeStarts(t, MustCompile(`(?m)$`), "a\nb\n", MatchOptions{Global: true}), []int{1, 3, 4})
	wantEqual(t, runeStarts(t, MustCompile(`$`), "a\nb\n", MatchOptions{Global: true}), []int{3, 4})
	wantEqual(t, MustCompile(`(?m)^a`).anchored, false)

	for pattern, construct := range map[string]string{
		`(?U)a`:  "inline flag group (?U)",
		`(?n:a)`: "inline flag group (?n:...)",
		`(?^i)a`: "inline flag group (?^i)",
		`(?iJ)a`: "inline flag group (?iJ)",
	} {
		_, err := Compile(pattern)
		var u *UnsupportedError
		wantErrAs(t, err, &u)
		wantEqual(t, u.Construct, construct)
	}
}

// groups returns the text of every group of the first match, with "-" for
// a group that took no part.
func groups(t *testing.T, p *Pattern, subject string) []string {
	t.Helper()
	m := find(t, p, subject)
	if m == nil {
		t.Fatalf("%q does not match %q", p.source, subject)
	}
	out := make([]string, len(m.Groups))
	for i, g := range m.Groups {
		out[i] = "-"
		if g.Matched {
			out[i] = g.Text(subject)
		}
	}
	return out
}

func wantSyntaxError(t *testing.T, pattern string, pos int, msg string, opts ...Option) {
	t.Helper()
	_, err := Compile(pattern, opts...)
	var se *SyntaxError
	wantErrAs(t, err, &se)
	if se.Pos != pos || se.Msg != msg {
		t.Fatalf("%q: got %q at %d, want %q at %d", pattern, se.Msg, se.Pos, msg, pos)
	}
}

func TestLookahead(t *testing.T) {
	wantMatch(t, MustCompile(`a(?=b)`), "ab")
	wantText(t, MustCompile(`a(?=b)`), "ab", "a")
	wantNoMatch(t, MustCompile(`a(?!b)`), "ab")
	wantEqual(t, runeStarts(t, MustCompile(`a(?!b)`), "abac", MatchOptions{Global: true}), []int{2})
	wantEqual(t, groups(t, MustCompile(`(?=(a))a`), "a"), []string{"a", "a"})
	wantEqual(t, groups(t, MustCompile(`(?!(b))a`), "a"), []string{"a", "-"})

	// An assertion is atomic, and backtracking past it restores the
	// groups its body set.
	wantEqual(t, groups(t, MustCompile(`(?:(?=(a))ax|ab)`), "ab"), []string{"ab", "-"})
}

func TestLookbehind(t *testing.T) {
	wantMatch(t, MustCompile(`(?<=a)b`), "ab")
	wantNoMatch(t, MustCompile(`(?<!a)b`), "ab")
	wantMatch(t, MustCompile(`(?<!a)b`), "cb")
	wantMatch(t, MustCompile(`(?<!a)b`), "b")
	wantEqual(t, runeStarts(t, MustCompile(`(?<=a)b`), "abab", MatchOptions{Global: true}), []int{1, 3})
	wantEqual(t, runeStarts(t, MustCompile(`(?<=é)b`), "ébb", MatchOptions{Global: true}), []int{1})
	wantEqual(t, runeStarts(t, MustCompile(`(?<=..)b`), "éxb", MatchOptions{Global: true}), []int{2})
	wantMatch(t, MustCompile(`(?<=a(?<=a))b`), "ab")
	wantMatch(t, MustCompile(`(?<=(?=a)a)b`), "ab")
	wantMatch(t, MustCompile(`(?<=\b)a`), "a")
	wantText(t, MustCompile(`(?<=^|,)a`), ",a", "a")

	// Branches are tried in order, each at its own lengths, and a
	// variable branch tries its longest length first, as in PCRE2.
	wantEqual(t, groups(t, MustCompile(`(?<=(a)|(aa))b`), "xaab"), []string{"b", "a", "-"})
	wantEqual(t, groups(t, MustCompile(`(?<=(aa)|(a))b`), "xaab"), []string{"b", "aa", "-"})
	wantEqual(t, groups(t, MustCompile(`(?<=(a{1,3}))b`), "aaab"), []string{"b", "aaa"})
	wantEqual(t, groups(t, MustCompile(`(?<=(a|aa))b`), "aaab"), []string{"b", "aa"})
	wantNoMatch(t, MustCompile(`(?<!a|bc)d`), "bcd")
	wantMatch(t, MustCompile(`(?<!a|bc)d`), "cd")

	// PCRE refuses a branch with no length limit or one that varies past
	// 255 runes. Oniguruma accepts an unbounded lookbehind.
	wantSyntaxError(t, `(?<=a+)b`, 0, "length of lookbehind assertion is not limited")
	wantSyntaxError(t, `x(?<!a|b*)b`, 1, "length of lookbehind assertion is not limited")
	wantSyntaxError(t, `(?<=a{1,256})b`, 0, "branch too long in variable-length lookbehind assertion")
	wantMatch(t, MustCompile(`(?<=a{1,255})b`), "ab")
	wantNoMatch(t, MustCompile(`(?<=a{300})b`), "ab")
	wantText(t, MustCompile(`(?<=xa+)b`, WithFlavor(Oniguruma)), "xaab", "b")
	wantNoMatch(t, MustCompile(`(?<=xa+)b`, WithFlavor(Oniguruma)), "aab")
	wantNoMatch(t, MustCompile(`(?<!a*)b`, WithFlavor(Oniguruma)), "b")

	// Oniguruma, as jq builds it, refuses a lookbehind holding a choice
	// whose branches differ in length, unless the lookbehind can match
	// nothing at all and so always holds.
	onig := WithFlavor(Oniguruma)
	for pattern, pos := range map[string]int{
		`(?<=a|cd)`: 0, `x(?<=x(?:ab|c))`: 1, `(?<=(?:a+|bc))`: 0,
		`(?<=a|\b)`: 0, `(?<!a|bc)d`: 0, `(?<=(?:a|bc)+)`: 0,
	} {
		wantSyntaxError(t, pattern, pos, "invalid pattern in look-behind", onig)
	}
	wantEqual(t, runeStarts(t, MustCompile(`(?<=ab|cd)`, onig), "abcd", MatchOptions{Global: true}), []int{2, 4})
	wantEqual(t, runeStarts(t, MustCompile(`(?<=(?:a{1,2}|b{1,2}))`, onig), "abcd", MatchOptions{Global: true}), []int{1, 2})
	for _, pattern := range []string{`(?<=a*|b)`, `(?<=a|)`, `(?<=(?:a|bc)?)`} {
		wantEqual(t, runeStarts(t, MustCompile(pattern, onig), "ab", MatchOptions{Global: true}), []int{0, 1, 2})
	}

	// Under full case folding a lookbehind's length follows its lowering.
	wantMatch(t, MustCompile(`(?<=ß)x`, WithFlavor(Oniguruma), WithFlags(IgnoreCase)), "ssx")
}

func TestQuantifiedLookaround(t *testing.T) {
	// PCRE2 obeys a quantified assertion once, or may skip it when the
	// quantifier allows zero, and never obeys it under {0}.
	wantText(t, MustCompile(`a(?=b)+`), "ab", "a")
	wantMatch(t, MustCompile(`(?=x)?b`), "b")
	wantEqual(t, groups(t, MustCompile(`(?=(b))?b`), "b"), []string{"b", "b"})
	wantMatch(t, MustCompile(`(?=x){0}b`), "b")
	wantMatch(t, MustCompile(`(?<!b)*b`), "bb")

	// jq's Oniguruma refuses them.
	wantSyntaxError(t, `a(?=b)+`, 6, "target of repeat operator is invalid", WithFlavor(Oniguruma))
}

func TestAtomicGroupsAndPossessives(t *testing.T) {
	wantNoMatch(t, MustCompile(`(?>a+)b`), "aaac")
	wantText(t, MustCompile(`(?>a+)b`), "aab", "aab")
	wantNoMatch(t, MustCompile(`(?>a|ab)c`), "abc")
	wantMatch(t, MustCompile(`(?:a|ab)c`), "abc")
	wantNoMatch(t, MustCompile(`a*+a`), "aaa")
	wantText(t, MustCompile(`a++b`), "aab", "aab")
	wantNoMatch(t, MustCompile(`a?+a`), "a")
	wantText(t, MustCompile(`a{2}+`), "aaa", "aa")
	wantText(t, MustCompile(`(?:ab)*+c`), "ababc", "ababc")
	wantText(t, MustCompile(`(?>a)*b`), "aab", "aab")
	wantEqual(t, groups(t, MustCompile(`(?>(a))b`), "ab"), []string{"ab", "a"})
	wantEqual(t, groups(t, MustCompile(`(?:(?>(a))x|ab)`), "ab"), []string{"ab", "-"})
	for _, flavor := range []Flavor{PCRE, Oniguruma} {
		wantNoMatch(t, MustCompile(`a*+a`, WithFlavor(flavor)), "aaa")
	}

	// An atomic group keeps none of its branches, so the stack stays
	// flat however many it passes.
	p := MustCompile(`(?>a?){500}`, WithBudget(Budget{MaxStack: 100}))
	wantText(t, p, strings.Repeat("a", 500), strings.Repeat("a", 500))
}

// \R matches any one line break, taking a CR LF pair whole and never
// giving its LF back to the rest of the pattern.
func TestLineBreakEscape(t *testing.T) {
	for _, flavor := range []Flavor{PCRE, Oniguruma} {
		p := MustCompile(`\R`, WithFlavor(flavor))
		wantEqual(t, texts(t, p, "a\r\nb\nc\rd\ve\ff\u0085g\u2028h\u2029i", MatchOptions{Global: true}),
			[]string{"\r\n", "\n", "\r", "\v", "\f", "\u0085", "\u2028", "\u2029"})
		wantNoMatch(t, p, "a b\tc")
		wantNoMatch(t, MustCompile(`\R\n`, WithFlavor(flavor)), "\r\n")
		wantText(t, MustCompile(`a\R+b`, WithFlavor(flavor)), "a\r\n\nb", "a\r\n\nb")
		wantText(t, MustCompile(`(?i)\R`, WithFlavor(flavor)), "x\r\ny", "\r\n")
	}
}

// \K moves the start of the match to where it stands, so the text before
// it must match but is left out of the match and out of a replacement.
func TestMatchReset(t *testing.T) {
	ctx := context.Background()
	for _, flavor := range []Flavor{PCRE, Oniguruma} {
		with := WithFlavor(flavor)
		wantText(t, MustCompile(`foo\Kbar`, with), "foobar", "bar")
		wantNoMatch(t, MustCompile(`foo\Kbar`, with), "fobar")
		wantEqual(t, groups(t, MustCompile(`(a)\K(b)`, with), "ab"), []string{"b", "a", "b"})
		wantEqual(t, find(t, MustCompile(`a\K`, with), "xab").Start, 2)

		// A branch that fails after \K takes its reset back with it.
		wantText(t, MustCompile(`x\Ky|xz`, with), "xz", "xz")
		wantText(t, MustCompile(`(?:a\K)+b`, with), "aab", "b")

		got, err := MustCompile(`foo\Kbar`, with).Replace(ctx, "foobar foobar", true, func(*Match) (string, error) { return "X", nil })
		wantNil(t, err)
		wantEqual(t, got, "fooX fooX")
		wantEqual(t, texts(t, MustCompile(`a\K`, with), "aaa", MatchOptions{Global: true}), []string{"", ""})
	}

	// A reset inside a lookaround is refused, by PCRE2's rule under PCRE
	// and by name under Oniguruma.
	for _, pattern := range []string{`a(?=b\Kc)`, `(?<=a\Kb)c`, `(?!a\K)b`} {
		var se *SyntaxError
		_, err := Compile(pattern)
		wantErrAs(t, err, &se)
		var u *UnsupportedError
		_, err = Compile(pattern, WithFlavor(Oniguruma))
		wantErrAs(t, err, &u)
	}
}

func TestConditional(t *testing.T) {
	all := MatchOptions{Global: true}
	for _, flavor := range []Flavor{PCRE, Oniguruma} {
		with := WithFlavor(flavor)
		wantEqual(t, texts(t, MustCompile(`(x)?(?(1)y|z)`, with), "xy z xz", all), []string{"xy", "z", "z"})
		wantEqual(t, texts(t, MustCompile(`(?<q>x)?(?(<q>)y|z)`, with), "xy z", all), []string{"xy", "z"})
		wantEqual(t, texts(t, MustCompile(`(?'q'x)?(?('q')y|z)`, with), "xy z", all), []string{"xy", "z"})
		wantEqual(t, texts(t, MustCompile(`(x)?(?(01)y|z)`, with), "xy z", all), []string{"xy", "z"})
		wantEqual(t, texts(t, MustCompile(`(x)?(?(-1)y|z)`, with), "xy z", all), []string{"xy", "z"})

		// A condition may name a group that comes after it, which has not
		// captured yet the first time the condition is tested.
		wantText(t, MustCompile(`(?:(?(1)y|z)(x))+`, with), "zxyx", "zxyx")

		// A yes branch that is a group of its own is not split into two.
		wantEqual(t, texts(t, MustCompile(`(x)?(?(1)(?:y|v)|z)w`, with), "xvw xyw zw", all), []string{"xvw", "xyw", "zw"})
		wantEqual(t, groups(t, MustCompile(`(x)?(?(1)(y)|(z))`, with), "z"), []string{"z", "-", "-", "z"})
		wantEqual(t, texts(t, MustCompile(`(x)?(?(1)y|z)+`, with), "xyy zz", all), []string{"xyy", "zz"})

		for _, bad := range []string{`(?(1)y|z)`, `(x)(?(2)y|z)`, `(x)(?(0)y|z)`, `(x)(?(-2)y|z)`, `(?(<q>)y)`} {
			var se *SyntaxError
			_, err := Compile(bad, with)
			wantErrAs(t, err, &se)
		}
		for _, other := range []string{`(?(R)a|b)`, `(?(DEFINE)a)b`, `(?(?=a)ab|c)`, `(x)(?(q)y)`} {
			var u *UnsupportedError
			_, err := Compile(other, with)
			wantErrAs(t, err, &u)
		}
	}

	// Under PCRE a condition inside its group sees the group's last
	// complete capture, and under Oniguruma the group has not captured
	// while it is open.
	wantEqual(t, texts(t, MustCompile(`(x(?(1)y|z))+`), "xzxy", all), []string{"xzxy"})
	wantEqual(t, texts(t, MustCompile(`(x(?(1)y|z))+`, WithFlavor(Oniguruma)), "xzxy", all), []string{"xz"})

	// Oniguruma takes every branch after the first as the no branch, where
	// PCRE2 refuses a third branch, and its conditional of one empty
	// branch fails when the group has not captured.
	onig := WithFlavor(Oniguruma)
	wantEqual(t, texts(t, MustCompile(`(x)?(?(1)y|z|w)`, onig), "w xy z", all), []string{"w", "xy", "z"})
	wantSyntaxError(t, `(x)?(?(1)y|z|w)`, 4, "conditional subpattern contains more than two branches")
	wantEqual(t, texts(t, MustCompile(`(x)?(?(1))`, onig), "x w x", all), []string{"x", "x"})
	wantEqual(t, len(texts(t, MustCompile(`(x)?(?(1))`), "x w", all)), 4)

	// Under PCRE a missing no branch matches the empty string. Oniguruma
	// refuses a conditional of one branch that is not empty, because jq's
	// search plans as if the branch matched nothing and misses matches.
	wantEqual(t, texts(t, MustCompile(`(x)?(?(1)y)w`), "xyw w", all), []string{"xyw", "w"})
	for _, yesOnly := range []string{`(x)?(?(1)y)w`, `(x)?(?(1)(?:))`, `(x)?(?(1)y*)`} {
		var u *UnsupportedError
		_, err := Compile(yesOnly, onig)
		wantErrAs(t, err, &u)
	}

	// Oniguruma reads a number in brackets as a group number and lets
	// groups share the name a condition tests, where PCRE2 wants a name.
	wantEqual(t, texts(t, MustCompile(`(x)?(?(<1>)y|z)`, onig), "xy z", all), []string{"xy", "z"})
	wantEqual(t, texts(t, MustCompile(`(?<q>x)?(?<q>v)?(?(<q>)y|z)`, onig), "xy vy z", all), []string{"xy", "vy", "z"})
	wantSyntaxError(t, `(x)?(?(<1>)y|z)`, 4, "subpattern name must start with a non-digit")
	wantSyntaxError(t, `(x)?(?(<>)y|z)`, 4, "subpattern name expected")
	wantSyntaxError(t, `(x)?(?(<>)y|z)`, 4, "group name is empty", onig)
	wantSyntaxError(t, `(x)?(?(2)y|z)`, 4, "invalid backref number/name", onig)
	wantSyntaxError(t, `(x)?(?(<q>)y|z)`, 4, "undefined name <q> reference", onig)
	wantSyntaxError(t, `(x)?(?(1)y|z`, 4, "missing closing parenthesis")
}

// The step budget is one count for a match, the bodies of its assertions
// and atomic groups, and every start of a search, so nesting cannot
// multiply it.
func TestNestedBodiesShareTheBudget(t *testing.T) {
	subject := strings.Repeat("a", 40) + "b"
	for _, pattern := range []string{`(?=(a+)+$)`, `(?!(a+)+$)`, `$(?<=^(a+)+)`, `(?>(a+)+$)`, `(?=(?=(?=(a+)+$)))`} {
		p := MustCompile(pattern, WithFlavor(Oniguruma), WithBudget(Budget{Steps: 100000}))
		_, err := p.Find(context.Background(), subject, 0)
		wantErrIs(t, err, ErrBudget)
	}
}

func TestLookaroundBackrefsAndAtomic(t *testing.T) {
	wantMatch(t, MustCompile(`a(?=b)`), "ab")
	wantNoMatch(t, MustCompile(`a(?!b)`), "ab")
	wantMatch(t, MustCompile(`(?<=a)b`), "ab")
	wantNoMatch(t, MustCompile(`(?<!a)b`), "ab")
	wantText(t, MustCompile(`(a)\1`), "xaab", "aa")
	wantText(t, MustCompile(`(?<q>["']).*?\k<q>`), `say "hi" now`, `"hi"`)
	wantNoMatch(t, MustCompile(`(?>a+)b`), "aaac")
	wantNoMatch(t, MustCompile(`a*+a`), "aaa")
	wantMatch(t, MustCompile(`(?i)A`), "a")
	wantMatch(t, MustCompile(`\Qa.b\E`), "a.b")
	wantNoMatch(t, MustCompile(`\Qa.b\E`), "axb")
	wantMatch(t, MustCompile(`\Aa\z`), "a")
	wantNoMatch(t, MustCompile(`\Aa\z`), "a\n")
	wantMatch(t, MustCompile(`\Aa\Z`), "a\n")
	wantText(t, MustCompile(`\bcat\b`), "concat cat", "cat")
	_, err := Compile(`(?<=a+)b`)
	var se *SyntaxError
	wantErrAs(t, err, &se) // lookbehind must have bounded length
}

func TestBackreferences(t *testing.T) {
	both := []Flavor{PCRE, Oniguruma}
	for _, flavor := range both {
		on := WithFlavor(flavor)
		wantText(t, MustCompile(`(a)\1`, on), "xaab", "aa")
		wantText(t, MustCompile(`(?<n>a)\k<n>`, on), "aa", "aa")
		wantText(t, MustCompile(`(?<n>a)\k'n'`, on), "aa", "aa")
		wantText(t, MustCompile(`(\w)(\w)\2\1`, on), "xabba", "abba")
		wantNoMatch(t, MustCompile(`(a)?x\1`, on), "x") // a group that took no part fails
		wantNoMatch(t, MustCompile(`\1(a)`, on), "aa")  // and so does one that has not yet
		wantNoMatch(t, MustCompile(`(a)\1`, on), "aA")  // case counts
		wantText(t, MustCompile(`(a)\1`, on, WithFlags(IgnoreCase)), "aA", "aA")
		wantText(t, MustCompile(`(a)(?i)\1`, on), "aA", "aA") // the case rule at the reference applies
		wantNoMatch(t, MustCompile(`(?i:(a))\1`, on), "aA")
		wantNoMatch(t, MustCompile(`(ß)\1`, on, WithFlags(IgnoreCase)), "ßss")
		wantText(t, MustCompile(`(a)|\1`, on), "a", "a")
		wantNoMatch(t, MustCompile(`(a)(?:(b)|c)\2`, on), "acb")
		wantText(t, MustCompile(`(a)(?<=\1)b`, on), "ab", "ab")
		wantText(t, MustCompile(`(a+)(?=\1)`, on), "aaaa", "aa")
		wantText(t, MustCompile(`(a)(b)(c)(d)(e)(f)(g)(h)(i)(j)(k)(l)\12`, on), "abcdefghijkll", "abcdefghijkll")
	}

	// PCRE spells references in more ways and resolves forward names.
	wantText(t, MustCompile(`(a)\g1`), "aa", "aa")
	wantText(t, MustCompile(`(a)\g{1}`), "aa", "aa")
	wantText(t, MustCompile(`(a)(b)\g{-2}`), "aba", "aba")
	wantText(t, MustCompile(`(a)(b)\g-1`), "abb", "abb")
	wantNoMatch(t, MustCompile(`(a)\g{+1}(b)`), "aab")
	wantText(t, MustCompile(`(?<n>a)\g{n}`), "aa", "aa")
	wantText(t, MustCompile(`(?<n>a)\k{n}`), "aa", "aa")
	wantText(t, MustCompile(`(?P<n>a)(?P=n)`), "aa", "aa")
	wantNoMatch(t, MustCompile(`\k<n>(?<n>a)`), "aa")
	wantSyntaxError(t, `(a)\2`, 3, "reference to non-existent subpattern")
	wantSyntaxError(t, `\k<nope>`, 0, "reference to non-existent subpattern")
	wantSyntaxError(t, `(a)\g0`, 3, "reference to non-existent subpattern")
	wantSyntaxError(t, `(a)\g{-2}`, 3, "reference to non-existent subpattern")
	wantSyntaxError(t, `(a)\81`, 3, "reference to non-existent subpattern")
	wantSyntaxError(t, `(a)\k`, 3, `\k is not followed by a braced, angle-bracketed, or quoted name`)
	wantSyntaxError(t, `(a)\k<1>`, 3, "subpattern name must start with a non-digit")

	// Oniguruma numbers references inside \k and wants names defined first.
	onig := WithFlavor(Oniguruma)
	wantText(t, MustCompile(`(a)\k<1>`, onig), "aa", "aa")
	wantText(t, MustCompile(`(a)(b)\k<-2>`, onig), "aba", "aba")
	wantNoMatch(t, MustCompile(`(a)\k<+1>(b)`, onig), "abb")
	wantSyntaxError(t, `\k<n>(?<n>a)`, 0, "undefined name <n> reference", onig)
	wantSyntaxError(t, `(a)\k<0>`, 3, "invalid backref number/name", onig)
	wantSyntaxError(t, `(a)\k<2>`, 3, "invalid backref number/name", onig)
	wantSyntaxError(t, `(a)\2`, 3, "invalid backref number/name", onig)
	// It has no \g reference, no \k{name}, and no (?P=name), so \g and \k
	// there are letters and (?P is a syntax error.
	wantText(t, MustCompile(`(a)\g1`, onig), "ag1", "ag1")
	wantText(t, MustCompile(`(?<n>a)\k{n}`, onig), "ak{n}", "ak{n}")
	wantSyntaxError(t, `(?P<n>a)(?P=n)`, 2, "undefined group option", onig)

	// Oniguruma lets groups share a name, and a reference to the name
	// tries the last of them first and then each earlier one.
	shared := MustCompile(`(?<a>x)|(?<a>y)`, onig)
	wantEqual(t, shared.GroupNames(), []string{"", "a", "a"})
	wantEqual(t, groups(t, shared, "y"), []string{"y", "-", "y"})
	wantText(t, MustCompile(`(?<a>x)(?<a>y)\k<a>`, onig), "xyy", "xyy")
	wantText(t, MustCompile(`(?<a>x)(?<a>y)\k<a>`, onig), "xyx", "xyx")
	wantNoMatch(t, MustCompile(`(?<a>x)(?<a>y)\k<a>`, onig), "xyz")
	wantText(t, MustCompile(`(?<a>a)(?<a>\k<a>)`, onig), "aa", "aa")
	wantText(t, MustCompile(`(?<a>x)(?<a>y)(?i)\k<a>`, onig), "xyY", "xyY")
	wantSyntaxError(t, `(?<n>a)(?<n>b)`, 10, "two named subpatterns have the same name")

	// A reference inside the group it names sees the group's last
	// complete capture under PCRE, and fails under Oniguruma, where the
	// group is still open.
	wantEqual(t, groups(t, MustCompile(`^(a|b\1)+$`), "aba"), []string{"aba", "ba"})
	wantNoMatch(t, MustCompile(`^(a|b\1)+$`, onig), "aba")
	wantEqual(t, groups(t, MustCompile(`^(?:(a)|b\1)+$`, onig), "aba"), []string{"aba", "a"})
	wantNoMatch(t, MustCompile(`(a\1)`), "aa")

	// PCRE needs a reference in a lookbehind to have a bounded length.
	wantSyntaxError(t, `(a(?<=\1))`, 2, "length of lookbehind assertion is not limited")
	wantSyntaxError(t, `(a+)(?<=\1)`, 4, "length of lookbehind assertion is not limited")

	// Comparing a reference draws on the step budget. This search takes
	// a few thousand steps but compares over a million bytes.
	pattern := `^(.*)` + strings.Repeat(`\1`, 21)
	wantText(t, MustCompile(pattern), strings.Repeat("a", 1000), strings.Repeat("a", 990))
	_, err := MustCompile(pattern, WithBudget(Budget{Steps: 100000})).Find(context.Background(), strings.Repeat("a", 1000), 0)
	wantErrIs(t, err, ErrBudget)
}
