package regex

import (
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
