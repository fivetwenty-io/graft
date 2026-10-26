package regex

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// The expected matches in this file were recorded from jq 1.8.2.

// spans returns each match FindAll yields as its rune start and its text.
func spans(t *testing.T, p *Pattern, subject string) []string {
	t.Helper()
	out := []string{}
	for m, err := range p.FindAll(context.Background(), subject, MatchOptions{Global: true}) {
		wantNil(t, err)
		out = append(out, fmt.Sprintf("%d:%s", m.RuneStart, m.Text(subject)))
	}
	return out
}

// TestAbsentOperator checks (?~absent), which takes the runes up to the
// first place at or after the position where absent matches, or up to the
// end of the subject, and gives them back one at a time.
func TestAbsentOperator(t *testing.T) {
	cases := []struct {
		pattern, subject string
		want             []string
	}{
		{`(?~ab)`, "aab", []string{"0:a", "1:", "2:b", "3:"}},
		{`(?~a.*z)`, "xaybz", []string{"0:x", "1:", "2:ybz", "5:"}},
		{`(?~)`, "abc", []string{"0:", "1:", "2:", "3:"}},
		{`(?~b)b`, "ab", []string{"0:ab"}},
		{`(?~(?<=a)b)`, "abab", []string{"0:a", "1:", "2:a", "3:", "4:"}},
		{`(?i)(?~x)`, "aXb", []string{"0:a", "1:", "2:b", "3:"}},
		{`(?~c)+`, "abc", []string{"0:ab", "2:", "3:"}},
		{`(?~c)*c`, "abcabc", []string{"0:abc", "3:abc"}},
		{`(?~bc)$`, "abc", []string{"2:c", "3:"}},
		{`(?~\b)`, "abc", []string{"0:", "1:bc", "3:"}},
		{`(?~b)(?~a)`, "abab", []string{"0:ab", "2:ab", "4:"}},
		{`(?~c|bc)`, "abcd", []string{"0:a", "1:", "2:", "3:d", "4:"}},
		{`(?~(?=c))`, "abcd", []string{"0:ab", "2:", "3:d", "4:"}},
		{`(?~$)`, "ab", []string{"0:ab", "2:"}},
		{`(?~a)*?`, "xab", []string{"0:", "1:", "2:", "3:"}},
		{`(a)(?~\1)`, "xaab", []string{"1:a", "2:ab"}},
		{`(?~a)++`, "ab", []string{"0:", "1:b", "2:"}},
		{`(?~b)?b`, "xab", []string{"0:xab"}},
		{`(?<=(?~a)|q)b`, "abc", []string{"1:b"}},
		{`(?<=(?~a))b`, "bab", []string{"0:b", "2:b"}},
	}
	for _, c := range cases {
		t.Run(c.pattern, func(t *testing.T) {
			p := MustCompile(c.pattern, WithFlavor(Oniguruma))
			wantEqual(t, spans(t, p, c.subject), c.want)
		})
	}
	wantText(t, MustCompile("(?~\u00E9)\u00E9b", WithFlavor(Oniguruma)), "a\u00E9b\u00E9", "a\u00E9b")
}

// The absent operator keeps no capture its absent made, and a capture
// around it holds what it took.
func TestAbsentOperatorCaptures(t *testing.T) {
	onig := WithFlavor(Oniguruma)
	wantEqual(t, groups(t, MustCompile(`(?~(a)\1)`, onig), "xab"), []string{"xab", "-"})
	wantEqual(t, groups(t, MustCompile(`(?~(a))`, onig), "xay"), []string{"x", "-"})
	wantEqual(t, groups(t, MustCompile(`(?<n>(?~a))b`, onig), "xab"), []string{"b", ""})
	wantEqual(t, groups(t, MustCompile(`(?<n>(?~a))`, onig), "xab"), []string{"x", "x"})
}

func TestAbsentOperatorRefusals(t *testing.T) {
	onig := WithFlavor(Oniguruma)
	for pattern, construct := range map[string]string{
		`(?~|a|b)`:      "absent expression (?~|...)",
		`(?~|a)`:        "absent expression (?~|...)",
		`(?~|)`:         "absent expression (?~|...)",
		`(?~a(?~b))`:    "absent operator (?~...) inside an absent operator",
		`(?~(?=(?~b)))`: "absent operator (?~...) inside an absent operator",
	} {
		_, err := Compile(pattern, onig)
		var u *UnsupportedError
		wantErrAs(t, err, &u)
		wantEqual(t, u.Construct, construct)
	}
	_, err := Compile(`(?~a)`)
	var u *UnsupportedError
	wantErrAs(t, err, &u)
	wantEqual(t, u.Construct, "absent operator (?~...)")

	// A lookbehind needs one length, which an absent operator after a
	// literal does not have.
	wantSyntaxError(t, `(?<=x(?~a))b`, 0, "invalid pattern in look-behind", onig)
	wantSyntaxError(t, `(?~a`, 0, "missing closing parenthesis", onig)
}

// Each start the absent operator tries draws on the step budget, so a
// long subject without the absent cannot run unpriced.
func TestAbsentOperatorIsCharged(t *testing.T) {
	p := MustCompile(`(?~z)[yz]`, WithFlavor(Oniguruma), WithBudget(Budget{Steps: 100000}))
	_, err := p.Find(context.Background(), strings.Repeat("x", 100000), 0)
	wantErrIs(t, err, ErrBudget)
}

func TestAbsentProgramSizeEstimateIsExact(t *testing.T) {
	for _, pat := range []string{`(?~ab)c`, `(?~a|b)+`, `(?i)(?~x(y))*z`, `(?<=(?~a)|q)b`} {
		opts := []Option{WithFlavor(Oniguruma)}
		cfg := newConfig(opts)
		n, _, err := parse(pat, cfg)
		wantNil(t, err)
		if got, want := programSize(lower(n, cfg), cfg.budget.MaxInstructions)+3, len(MustCompile(pat, opts...).prog); got != want {
			t.Errorf("%q: estimated %d instructions, emitted %d", pat, got, want)
		}
	}
}
