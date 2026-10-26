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
	for pattern, construct := range map[string]string{
		`(a)\g1`:         `backreference \g1`,
		`(?<n>a)\k{n}`:   `backreference \k{n}`,
		`(?P<n>a)(?P=n)`: "backreference (?P=n)",
	} {
		_, err := Compile(pattern, onig)
		var u *UnsupportedError
		wantErrAs(t, err, &u)
		wantEqual(t, u.Construct, construct)
	}

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
