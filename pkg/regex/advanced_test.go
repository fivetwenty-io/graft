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
