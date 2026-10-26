package regex

import (
	"context"
	"strings"
	"testing"
)

// The expected matches in this file were recorded from jq 1.8.2.

// TestGraphemeTableIsUnicode16 pins the table to the Unicode version of the
// grapheme tables in Oniguruma 6.9.10, which jq 1.8.2 vendors, rather than
// to the Go toolchain's, so \X splits text where jq splits it.
func TestGraphemeTableIsUnicode16(t *testing.T) {
	wantEqual(t, graphemeUnicodeVersion, "16.0.0")
	for i, r := range graphemeRanges {
		if r.lo > r.hi || i > 0 && r.lo <= graphemeRanges[i-1].hi {
			t.Fatalf("range %d, %04X..%04X, is out of order", i, r.lo, r.hi)
		}
	}
	for c, want := range map[rune]graphemeProp{
		'a':      gbOther,
		'\r':     gbCR,
		'\n':     gbLF,
		0x0000:   gbControl,
		0x0301:   gbExtend,
		0x200D:   gbZWJ,
		0x1F1E6:  gbRegionalIndicator,
		0x0600:   gbPrepend,
		0x0903:   gbSpacingMark,
		0x1100:   gbL,
		0x1160:   gbV,
		0x11A8:   gbT,
		0xAC00:   gbLV,
		0xAC01:   gbLVT,
		0x1F600:  gbPictographic,
		0x00A9:   gbPictographic,
		0x10FFFF: gbOther,
	} {
		if got := graphemeProperty(c); got != want {
			t.Errorf("%U: got property %d, want %d", c, got, want)
		}
	}
}

// clusters returns the rune length of each \X match in subject.
func clusters(t *testing.T, subject string) []int {
	t.Helper()
	var got []int
	p := MustCompile(`\X`, WithFlavor(Oniguruma))
	for m, err := range p.FindAll(context.Background(), subject, MatchOptions{Global: true}) {
		wantNil(t, err)
		got = append(got, m.RuneEnd-m.RuneStart)
	}
	return got
}

func TestExtendedGraphemeClusters(t *testing.T) {
	cases := []struct {
		name, subject string
		want          []int
	}{
		{"combining mark", "e\u0301*x", []int{2, 1, 1}},
		{"empty", "", nil},
		{"crlf", "a\r\nb", []int{1, 2, 1}},
		{"cr then mark", "\r\u0301", []int{1, 1}},
		{"lf alone", "\n\n", []int{1, 1}},
		{"control then mark", "\x01\u0301", []int{1, 1}},
		{"hangul syllables", "\u1100\u1161\u11A8\uAC00\u11A8", []int{3, 2}},
		{"hangul L before LVT", "\u1100\uAC01", []int{2}},
		{"hangul T after V", "\u1161\u11A8\u1100", []int{2, 1}},
		{"zwj joins pictographs", "\U0001F468\u200D\U0001F469", []int{3}},
		{"zwj after extend", "\U0001F468\u0301\u200D\U0001F469", []int{4}},
		{"zwj without pictograph", "a\u200D\U0001F469", []int{2, 1}},
		{"regional pairs", "\U0001F1FA\U0001F1F8\U0001F1EB\U0001F1F7\U0001F1E9", []int{2, 2, 1}},
		{"regional after letter", "a\U0001F1FA\U0001F1F8\U0001F1EB", []int{1, 2, 1}},
		{"prepend", "\u0600a", []int{2}},
		{"spacing mark", "\u0915\u0903", []int{2}},
		// Oniguruma 6.9.10 has no rule GB9c, so a virama does not join
		// two consonants.
		{"virama", "\u0915\u094D\u0937", []int{2, 1}},
		{"virama and zwj", "\u0915\u094D\u200D\u0937", []int{3, 1}},
		{"newline is a cluster", "a\nb", []int{1, 1, 1}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			wantEqual(t, clusters(t, c.subject), c.want)
		})
	}
}

func TestTextSegmentBoundaries(t *testing.T) {
	onig := WithFlavor(Oniguruma)
	wantEqual(t, runeStarts(t, MustCompile(`\y`, onig), "ab\r\nc", MatchOptions{Global: true}), []int{0, 1, 2, 4, 5})
	wantEqual(t, runeStarts(t, MustCompile(`\Y`, onig), "ab\r\ne\u0301", MatchOptions{Global: true}), []int{3, 5})
	wantEqual(t, runeStarts(t, MustCompile(`\y`, onig), "", MatchOptions{Global: true}), []int{0})
	wantEqual(t, runeStarts(t, MustCompile(`\Y`, onig), "", MatchOptions{Global: true}), []int{})
	wantText(t, MustCompile(`e\Y.`, onig), "ee\u0301", "e\u0301")
	wantText(t, MustCompile(`(?i)\X\X`, onig), "AE\u0301", "AE\u0301")

	wantText(t, MustCompile(`\Xe`, onig), "e\u0301e", "e\u0301e")

	// \X is atomic, so it never gives back part of a cluster.
	wantNoMatch(t, MustCompile("^\\X\u0301", onig), "e\u0301")

	// An anchor takes no quantifier in jq.
	for _, pattern := range []string{`\y*`, `\Y+`} {
		_, err := Compile(pattern, onig)
		var se *SyntaxError
		wantErrAs(t, err, &se)
	}
}

// PCRE2's \X follows a newer Unicode version with rules Oniguruma 6.9.10
// lacks, so the PCRE flavor refuses it rather than answer as jq would.
func TestPCRERefusesGraphemeClusters(t *testing.T) {
	_, err := Compile(`\X`)
	var u *UnsupportedError
	wantErrAs(t, err, &u)
	wantEqual(t, u.Construct, `extended grapheme cluster \X`)
	for _, pattern := range []string{`\y`, `\Y`} {
		_, err := Compile(pattern)
		var se *SyntaxError
		wantErrAs(t, err, &se)
	}
}

// A run of regional indicators makes each boundary test look back over the
// run, and that walk draws on the step budget.
func TestGraphemeBreakWalkIsCharged(t *testing.T) {
	flags := strings.Repeat("\U0001F1FA", 20000)
	p := MustCompile(`\X`, WithFlavor(Oniguruma), WithBudget(Budget{Steps: 1000000}))
	for _, err := range p.FindAll(context.Background(), flags, MatchOptions{Global: true}) {
		if err != nil {
			wantErrIs(t, err, ErrBudget)
			return
		}
	}
	t.Fatal("matching every cluster in 20,000 regional indicators stayed within 1,000,000 steps")
}
