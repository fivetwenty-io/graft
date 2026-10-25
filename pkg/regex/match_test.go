package regex

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestFindWithNamedGroup(t *testing.T) {
	p := MustCompile(`a(?<n>b+)c`)
	m, err := p.Find(context.Background(), "xxabbbc", 0)
	wantNil(t, err)
	wantEqual(t, m.Start, 2)
	wantEqual(t, m.Groups[1].Name, "n")
	wantEqual(t, m.Groups[1].Text("xxabbbc"), "bbb")
	wantEqual(t, p.GroupNames(), []string{"", "n"})
}

func TestRuneOffsets(t *testing.T) {
	m, _ := MustCompile(`é`).Find(context.Background(), "aé", 0)
	wantEqual(t, m.Start, 1)
	wantEqual(t, m.End, 3)
	wantEqual(t, m.RuneStart, 1)
	wantEqual(t, m.RuneEnd, 2)
}

func TestFindAllGlobalAndSkipEmpty(t *testing.T) {
	wantEqual(t, runeStarts(t, MustCompile(``), "ab", MatchOptions{Global: true}), []int{0, 1, 2})
	wantEqual(t, runeStarts(t, MustCompile(`a*`), "baab", MatchOptions{Global: true, SkipEmpty: true}), []int{1})
	wantEqual(t, runeStarts(t, MustCompile(`a`), "aaa", MatchOptions{}), []int{0})
}

func TestFlags(t *testing.T) {
	wantMatch(t, MustCompile(`A`, WithFlags(IgnoreCase)), "a")
	wantMatch(t, MustCompile("a b # comment\n c", WithFlags(Extended)), "abc")
	wantMatch(t, MustCompile(`a.b`, WithFlags(DotAll)), "a\nb")
	wantNoMatch(t, MustCompile(`a.b`), "a\nb")
	wantMatch(t, MustCompile(`a$`), "a\n")
	wantMatch(t, MustCompile(`a$`, WithFlags(SingleLine)), "a\n") // SingleLine turns $ into \Z, which still matches before a final newline, as jq's s flag does
	wantNoMatch(t, MustCompile(`a$`, WithFlags(SingleLine)), "a\nb")
	wantNoMatch(t, MustCompile(`ss`, WithFlavor(PCRE), WithFlags(IgnoreCase)), "ß") // PCRE2 folds one code point to one
	wantNoMatch(t, MustCompile(`^b`), "a\nb")
}

func TestClasses(t *testing.T) {
	wantMatch(t, MustCompile(`^[[:alpha:]]\d\s\w$`), "x1 _")
	wantMatch(t, MustCompile(`^\p{Greek}+$`), "αβγ")
	wantMatch(t, MustCompile(`^[^a-c]$`), "d")
	wantNoMatch(t, MustCompile(`\d`), "٣") // ASCII digits in the PCRE flavor
	wantMatch(t, MustCompile(`^\x41B\t$`), "AB\t")
}

func TestQuantifiersAndAlternation(t *testing.T) {
	wantText(t, MustCompile(`a+?`), "aaa", "a")
	wantText(t, MustCompile(`a{2,3}`), "aaaa", "aaa")
	wantText(t, MustCompile(`cat|category`), "category", "cat")
	wantText(t, MustCompile(`(ab)*c`), "ababc", "ababc")
}

func TestLongestFlag(t *testing.T) {
	wantText(t, MustCompile(`a|ab`, WithFlags(Longest)), "ab", "ab")
}

func TestReplaceAndSplit(t *testing.T) {
	out, _ := MustCompile(`(?<d>\d)`).Replace(context.Background(), "a1b2", true, func(m *Match) (string, error) { return "<" + m.Groups[1].Text("a1b2") + ">", nil })
	wantEqual(t, out, "a<1>b<2>")
	parts, _ := MustCompile(`, *`).Split(context.Background(), "a, b,c")
	wantEqual(t, parts, []string{"a", "b", "c"})
}

func TestCapturesFollowPCRE(t *testing.T) {
	m := find(t, MustCompile(`(a?)*b`), "b")
	wantEqual(t, m.Groups[1].Matched, true) // an empty iteration ends the loop but keeps its capture, as PCRE2 does
	wantEqual(t, m.Groups[1].Start, 0)
	wantEqual(t, m.Groups[1].End, 0)

	m = find(t, MustCompile(`(?:(a)|b)*`), "ab")
	wantEqual(t, m.Groups[1].Text("ab"), "a") // a capture from an earlier iteration survives a later one that skips it

	m = find(t, MustCompile(`(a)|(b)`), "b")
	wantEqual(t, m.Groups[1], Group{Start: -1, End: -1, RuneStart: -1, RuneEnd: -1})
	wantEqual(t, m.Groups[1].Text("b"), "")
	wantEqual(t, m.Groups[2].Text("b"), "b")

	m = find(t, MustCompile(`(?P<y>\d+)-(?'m'\d+)`), "on 2026-10")
	wantEqual(t, m.Groups[1].Text("on 2026-10"), "2026")
	wantEqual(t, m.Groups[2].Name, "m")
	wantEqual(t, m.Text("on 2026-10"), "2026-10")
}

func TestEmptyLoopsTerminate(t *testing.T) {
	wantText(t, MustCompile(`(a*)*b`), "aab", "aab")
	wantText(t, MustCompile(`(a*)+b`), "b", "b")
	wantText(t, MustCompile(`(a*)*?b`), "aab", "aab")
	wantText(t, MustCompile(`(|a)+c`), "aac", "aac")
	wantText(t, MustCompile(`(?:a?){3,}x`), "aax", "aax")
}

func TestRepetitionForms(t *testing.T) {
	wantText(t, MustCompile(`a{2}`), "aaa", "aa")
	wantText(t, MustCompile(`a{2,}`), "aaaa", "aaaa")
	wantText(t, MustCompile(`a{,2}`), "aaa", "aa")
	wantText(t, MustCompile(`a{2,}?`), "aaaa", "aa")
	wantText(t, MustCompile(`a{1,3}?b`), "aaab", "aaab")
	wantText(t, MustCompile(`a??b`), "ab", "ab")
	wantText(t, MustCompile(`a{0}b`), "ab", "b")
	wantText(t, MustCompile(`x{a}`), "x{a}", "x{a}") // a brace that starts no quantifier is literal
	wantText(t, MustCompile(`a{2`), "a{2", "a{2")
}

func TestEscapes(t *testing.T) {
	wantText(t, MustCompile(`\x{1F600}`), "a😀", "😀")
	wantText(t, MustCompile(`é`), "é", "é")
	wantText(t, MustCompile(`\o{101}\0\cA`), "A\x00\x01", "A\x00\x01")
	wantText(t, MustCompile(`\x4g`), "\x04g", "\x04g")
	wantText(t, MustCompile(`\n\r\t\f\e\a`), "\n\r\t\f\x1b\x07", "\n\r\t\f\x1b\x07")
	wantText(t, MustCompile(`\.\*\\`), `.*\`, `.*\`)
	wantText(t, MustCompile(`[\b]`), "\b", "\b")
	wantText(t, MustCompile(`a\Eb`), "ab", "ab") // a stray \E is ignored, as PCRE2 does
	wantText(t, MustCompile(`(?#note)a`), "a", "a")
	wantText(t, MustCompile(`\N+`), "ab\nc", "ab")
	wantText(t, MustCompile(`\h+`), "a \t\u00a0b", " \t\u00a0")
	wantText(t, MustCompile(`\v+`), "a\n\r\u2028b", "\n\r\u2028")
	wantText(t, MustCompile(`\H\V`), " ab", "ab")
}

func TestClassSyntax(t *testing.T) {
	wantText(t, MustCompile(`[]a]+`), "x]a]", "]a]")
	wantText(t, MustCompile(`[^]a]+`), "]bc", "bc")
	wantText(t, MustCompile(`[a-]+`), "x-a-", "-a-")
	wantText(t, MustCompile(`[\d.]+`), "v1.25x", "1.25")
	wantText(t, MustCompile(`[[:^digit:]]+`), "12ab3", "ab")
	wantText(t, MustCompile(`[\x{3b1}-\x{3b3}]+`), "αβγδ", "αβγ")
	wantText(t, MustCompile(`[\p{Lu}\d]+`), "aB2c", "B2")
	wantText(t, MustCompile(`\P{L}+`), "ab12c", "12")
	wantText(t, MustCompile(`\p{^L}+`), "ab12c", "12")
	wantText(t, MustCompile(`\pN+`), "ab12c", "12")
	wantText(t, MustCompile(`\p{greek}+`), "aαβ", "αβ") // property names match loosely, as PCRE2 does
	wantText(t, MustCompile(`\p{L&}+`), "1aB", "aB")
	wantText(t, MustCompile(`\p{White_Space}+`), "a \u2003b", " \u2003")
	wantText(t, MustCompile(`[\W\d]+`), "ab-1c", "-1")
	wantText(t, MustCompile(`[ab&&b]+`), "&&", "&&") // && is literal under PCRE
}

func TestIgnoreCase(t *testing.T) {
	ci := WithFlags(IgnoreCase)
	wantText(t, MustCompile(`k`, ci), "\u212a", "\u212a") // the Kelvin sign folds to k
	wantText(t, MustCompile(`[a-z]+`, ci), "ABſ", "ABſ")
	wantNoMatch(t, MustCompile(`[^k]`, ci), "\u212a")
	wantNoMatch(t, MustCompile(`\w`, ci), "ſ")                     // \w is not folded
	wantText(t, MustCompile(`[[:upper:]]+`, ci), "aB", "aB")       // [:upper:] and [:lower:] match every ASCII letter
	wantNoMatch(t, MustCompile(`[[:upper:]]`, ci), "ſ")            // but nothing outside ASCII
	wantText(t, MustCompile(`\p{Lu}+`, ci), "aBǅ", "aBǅ")          // as do \p{Lu}, \p{Ll}, and \p{Lt}
	wantText(t, MustCompile(`ÉCOLE`, ci), "école", "école")        // non-ASCII literals fold too
	wantText(t, MustCompile(`σ+`, ci), "ΣσςX", "Σσς")              // so does every member of a fold orbit
	wantNoMatch(t, MustCompile(`ß`, ci), "ss")                     // PCRE folds one code point to one
	wantText(t, MustCompile(`ß`, ci), "ẞ", "ẞ")                    // capital sharp s is in the same orbit
	wantText(t, MustCompile(`[^a]+`, ci), "bAac", "b")             // negation applies after folding
	wantText(t, MustCompile(`\p{Greek}+`, ci), "αΑx", "αΑ")        // other properties are unaffected
	wantNoMatch(t, MustCompile(`\p{Ll}`, ci), "1")                 // and the folded ones still need a letter
	wantNoMatch(t, MustCompile(`[[:digit:]]`, ci), "A")            // and so are the other POSIX classes
	wantText(t, MustCompile(`[\d-]+`, ci), "x-1", "-1")            // escapes inside classes are kept as written
	wantText(t, MustCompile(`a`, WithFlags(IgnoreCase)), "A", "A") // single runes without orbits stay runes
}

func TestOnigurumaClassRules(t *testing.T) {
	onig := WithFlavor(Oniguruma)
	wantMatch(t, MustCompile(`\d`, onig), "٣")
	wantMatch(t, MustCompile(`^\w+$`, onig), "héllo_1")
	wantNoMatch(t, MustCompile(`^\w+$`), "héllo_1")
	wantMatch(t, MustCompile(`\s`, onig), "\u2003")
	wantNoMatch(t, MustCompile(`\s`), "\u2003")
	wantText(t, MustCompile(`\h+`, onig), "xa0F9z", "a0F9") // \h is a hex digit under Oniguruma
	wantMatch(t, MustCompile(`^[[:alpha:]]+$`, onig), "éa")
	wantNoMatch(t, MustCompile(`^[[:alpha:]]+$`), "éa")
}

// The expected memberships were recorded from jq 1.8.2.
func TestOnigurumaPosixClasses(t *testing.T) {
	subjects := []string{"a", "Z", "é", "中", "٣", "_", "$", "+", "!", " ", "\u00a0", "\t", "\n", "\u0001", "\u200b", "\u0300", "ǅ", "ª", "Ⅷ"}
	cases := map[string][]string{
		"alpha":  {"a", "Z", "é", "中", "ǅ", "ª", "Ⅷ"},
		"digit":  {"٣"},
		"alnum":  {"a", "Z", "é", "中", "٣", "ǅ", "ª", "Ⅷ"},
		"upper":  {"Z", "Ⅷ"},
		"lower":  {"a", "é", "ª"},
		"space":  {" ", "\u00a0", "\t", "\n"},
		"blank":  {" ", "\u00a0", "\t"},
		"cntrl":  {"\t", "\n", "\u0001"},
		"graph":  {"a", "Z", "é", "中", "٣", "_", "$", "+", "!", "\u200b", "\u0300", "ǅ", "ª", "Ⅷ"},
		"print":  {"a", "Z", "é", "中", "٣", "_", "$", "+", "!", " ", "\u00a0", "\u200b", "\u0300", "ǅ", "ª", "Ⅷ"},
		"punct":  {"_", "$", "+", "!"},
		"word":   {"a", "Z", "é", "中", "٣", "_", "\u0300", "ǅ", "ª", "Ⅷ"},
		"xdigit": {"a"},
		"ascii":  {"a", "Z", "_", "$", "+", "!", " ", "\t", "\n", "\u0001"},
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			wantEqual(t, matching(t, MustCompile(`^[[:`+name+`:]]$`, WithFlavor(Oniguruma)), subjects), want)
		})
	}
}

func TestAnchors(t *testing.T) {
	wantNoMatch(t, MustCompile(`a$`), "a\n\n")
	wantMatch(t, MustCompile(`^$`), "")
	wantMatch(t, MustCompile(`^$`), "\n")
	wantText(t, MustCompile(`^a|b`), "cab", "b")
	p := MustCompile(`^a`)
	m, err := p.Find(context.Background(), "aa", 1)
	wantNil(t, err)
	if m != nil {
		t.Fatalf("^ matched at %d, after the start of the subject", m.Start)
	}
}

func TestFindFromOffset(t *testing.T) {
	m, err := MustCompile(`\w+`).Find(context.Background(), "αβ ab cd", 6)
	wantNil(t, err)
	wantEqual(t, m.Text("αβ ab cd"), "b")
	wantEqual(t, m.RuneStart, 4)
	m, err = MustCompile(`x`).Find(context.Background(), "x", 2)
	wantNil(t, err)
	if m != nil {
		t.Fatal("a start past the end matched")
	}
}

func TestFindAllOffsetsAndEarlyStop(t *testing.T) {
	subject := "é1 ü22 ö333"
	var starts, ends, groupStarts []int
	for m, err := range MustCompile(`(\d)\d*`).FindAll(context.Background(), subject, MatchOptions{Global: true}) {
		wantNil(t, err)
		starts = append(starts, m.RuneStart)
		ends = append(ends, m.RuneEnd)
		groupStarts = append(groupStarts, m.Groups[1].RuneStart)
	}
	wantEqual(t, starts, []int{1, 4, 8})
	wantEqual(t, ends, []int{2, 6, 11})
	wantEqual(t, groupStarts, []int{1, 4, 8})

	n := 0
	for _, err := range MustCompile(`\d`).FindAll(context.Background(), subject, MatchOptions{Global: true}) {
		wantNil(t, err)
		n++
		if n == 2 {
			break
		}
	}
	wantEqual(t, n, 2)

	wantEqual(t, texts(t, MustCompile(`a*`), "ab", MatchOptions{Global: true}), []string{"a", "", ""})
	wantEqual(t, texts(t, MustCompile(`a*?`), "aa", MatchOptions{Global: true}), []string{"", "", ""})
	wantEqual(t, texts(t, MustCompile(`a*?`), "aa", MatchOptions{Global: true, SkipEmpty: true}), []string{"a", "a"}) // SkipEmpty rejects empty matches inside the matcher
	wantEqual(t, runeStarts(t, MustCompile(``), "éa", MatchOptions{Global: true}), []int{0, 1, 2})
}

func TestLongestSearchesEveryStart(t *testing.T) {
	p := MustCompile(`ab|abc`, WithFlags(Longest))
	m := find(t, p, "xab abc")
	wantEqual(t, m.Text("xab abc"), "abc")
	wantEqual(t, m.RuneStart, 4)
	wantEqual(t, texts(t, p, "xab abc", MatchOptions{Global: true}), []string{"abc"})
	wantText(t, MustCompile(`a|b`, WithFlags(Longest)), "ba", "b") // the earliest of equal lengths wins
	wantText(t, MustCompile(`(a|ab)(c|bcd)`, WithFlags(Longest)), "abcd", "abcd")
	wantText(t, MustCompile(`a*?`, WithFlags(Longest)), "aa", "aa")
}

func TestReplaceForms(t *testing.T) {
	ctx := context.Background()
	upper := func(m *Match) (string, error) { return "-", nil }
	out, err := MustCompile(`x*`).Replace(ctx, "abc", true, upper)
	wantNil(t, err)
	wantEqual(t, out, "-a-b-c-")
	out, err = MustCompile(`\d`).Replace(ctx, "a1b2", false, upper)
	wantNil(t, err)
	wantEqual(t, out, "a-b2")
	out, err = MustCompile(`z`).Replace(ctx, "abc", true, upper)
	wantNil(t, err)
	wantEqual(t, out, "abc")
	boom := errors.New("boom")
	_, err = MustCompile(`b`).Replace(ctx, "abc", true, func(*Match) (string, error) { return "", boom })
	wantErrIs(t, err, boom)
}

func TestSplitForms(t *testing.T) {
	ctx := context.Background()
	parts, err := MustCompile(``).Split(ctx, "ab")
	wantNil(t, err)
	wantEqual(t, parts, []string{"", "a", "b", ""})
	parts, err = MustCompile(`,`).Split(ctx, ",a,")
	wantNil(t, err)
	wantEqual(t, parts, []string{"", "a", ""})
	parts, err = MustCompile(`,`).Split(ctx, "abc")
	wantNil(t, err)
	wantEqual(t, parts, []string{"abc"})
}

func TestInvalidUTF8Subject(t *testing.T) {
	wantText(t, MustCompile(`a.b`), "a\xffb", "a\xffb")
	m := find(t, MustCompile(`b`), "\xff\xfeb")
	wantEqual(t, m.RuneStart, 2)
}

func TestExtendedMode(t *testing.T) {
	x := WithFlags(Extended)
	wantText(t, MustCompile(`a +  # many a
		b`, x), "aaab", "aaab")
	wantText(t, MustCompile(`a\ b`, x), "a b", "a b")
	wantText(t, MustCompile(`[ ]`, x), "a b", " ") // whitespace inside a class is kept
	wantText(t, MustCompile(`a\#`, x), "a#", "a#")
}

func TestPatternIsSafeForConcurrentUse(t *testing.T) {
	p := MustCompile(`(\w+)@(\w+)`)
	done := make(chan string)
	for i := range 8 {
		go func() {
			subject := strings.Repeat("x", i) + "user@host"
			m, err := p.Find(context.Background(), subject, 0)
			if err != nil || m == nil {
				done <- "no match"
				return
			}
			done <- m.Groups[2].Text(subject)
		}()
	}
	for range 8 {
		wantEqual(t, <-done, "host")
	}
}
