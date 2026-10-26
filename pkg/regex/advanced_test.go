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
	for _, flavor := range []Flavor{PCRE, Oniguruma} {
		wantMatch(t, MustCompile(`[\Q.\E]`, WithFlavor(flavor)), ".")
		wantNoMatch(t, MustCompile(`[\Q.\E]`, WithFlavor(flavor)), "a")
	}

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
