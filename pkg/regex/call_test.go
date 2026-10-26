package regex

import (
	"strings"
	"testing"
)

// The expected matches in this file were recorded from jq 1.8.2.

func TestSubexpressionCalls(t *testing.T) {
	cases := []struct {
		pattern, subject string
		want             []string
	}{
		{`(?<a>[ab])\g<a>`, "ab", []string{"ab", "b"}},
		{`^(?<p>\((?:a|\g<p>)\))$`, "((a))", []string{"((a))", "((a))"}},
		{`(a|b)\g<1>\1`, "abb", []string{"abb", "b"}},
		{`x\g<1>(a)`, "xaa", []string{"xaa", "a"}},
		{`(a)(?1)`, "aa", []string{"aa", "a"}},
		{`(?&n)(?<n>a)`, "aa", []string{"aa", "a"}},
		{`(a)(?-1)`, "aa", []string{"aa", "a"}},
		{`(a)(?01)`, "aa", []string{"aa", "a"}},
		{`(a)\g'1'`, "aa", []string{"aa", "a"}},
		{`(a)\g<01>`, "aa", []string{"aa", "a"}},
		{`(a)\g<-1>`, "aa", []string{"aa", "a"}},
		{`(?<n>a)\g<1>`, "aa", []string{"aa", "a"}},
		{`\g<+1>(a)(b)`, "aab", []string{"aab", "a", "b"}},
		{`(?:a|b\g<0>c)`, "bbacc", []string{"bbacc"}},
		{`a\g<0>?`, "aab", []string{"aa"}},
		{`(?<a>x)\g<a>`, "xx", []string{"xx", "x"}},
		{`(?<a>(?<b>x)\g<b>)\g<a>`, "xxxx", []string{"xxxx", "xx", "x"}},
		{`(?<d>\d+){0}\g<d>-\g<d>`, "12-345", []string{"12-345", "345"}},
		{`(?<d>\d+){0}(?:\g<d>,)*\g<d>`, "1,22,333", []string{"1,22,333", "333"}},
		{`(?<a>\g<b>|x)(?<b>y\g<a>)`, "aabcxyxyx", []string{"xyx", "x", "yx"}},

		// A group re-entered while it is open keeps the start of each
		// entry, so each capture spans one level.
		{`^(?<p>\((?<r>a|\g<p>)\))$`, "((a))", []string{"((a))", "((a))", "(a)"}},
		{`^(?<p>\(((a)|\g<p>)\))$`, "((a))", []string{"((a))", "((a))", "(a)", "a"}},
		{`(?<p>x(?<r>y\g<p>?z))`, "xyxyzz", []string{"xyxyzz", "xyxyzz", "yxyzz"}},

		// A loop inside a recursive group keeps its own iterations at
		// each level.
		{`(?<a>(?:x\g<a>?y)*)`, "xxyxyyxy", []string{"xxyxyyxy", "xxyxyyxy"}},
		{`(?<a>[a-z](?:\g<a>|)*)`, "abc", []string{"abc", "abc"}},
		{`^(?<a>(?:\((?:\g<a>)\)|[^()])*)$`, "(a(b)c)(d)", []string{"(a(b)c)(d)", "(a(b)c)(d)"}},

		// A called group keeps the flags in force where it stands.
		{`(?i:(?<a>a))\g<a>`, "aA", []string{"aA", "A"}},
		{`(?i)(?<a>a)(?-i)\g<a>`, "aA", []string{"aA", "A"}},
		{`(?<a>a)(?i)\g<a>\k<a>`, "aaA", []string{"aaA", "a"}},

		// A lookbehind may call a group of one length.
		{`(a)(?<=\g<1>)b`, "aab", []string{"ab", "a"}},
		{`(?<a>a|b)(?<=\g<a>)b`, "aab", []string{"ab", "a"}},
		{`(?<=(?<a>a)\g<a>)b`, "aab", []string{"b", "a"}},
	}
	for _, c := range cases {
		t.Run(c.pattern, func(t *testing.T) {
			p := MustCompile(c.pattern, WithFlavor(Oniguruma))
			wantEqual(t, groups(t, p, c.subject), c.want)
		})
	}
	onig := WithFlavor(Oniguruma)
	wantNoMatch(t, MustCompile(`(a|b)\g<1>\1`, onig), "aba")
	wantNoMatch(t, MustCompile(`(?<a>a)(?i)\g<a>`, onig), "aA")
	wantNoMatch(t, MustCompile(`(?<a>a)(?i:\g<a>)`, onig), "aA")
	wantEqual(t, spans(t, MustCompile(`(?<a>a\g<a>|)`, onig), "aaa"), []string{"0:aaa", "3:"})
}

// jq's Oniguruma fails a call made while 20 called groups are already
// running, and the group a call names counts as one where it stands.
func TestSubexpressionCallDepth(t *testing.T) {
	nested := MustCompile(`^(?<p>\((?:a|\g<p>)\))$`, WithFlavor(Oniguruma))
	parens := func(n int) string { return strings.Repeat("(", n) + "a" + strings.Repeat(")", n) }
	wantMatch(t, nested, parens(20))
	wantNoMatch(t, nested, parens(21))
	wantText(t, MustCompile(`(?<a>a\g<a>?)`, WithFlavor(Oniguruma)), strings.Repeat("a", 25), strings.Repeat("a", 20))
}

func TestSubexpressionCallErrors(t *testing.T) {
	for pattern, msg := range map[string]string{
		`(?<a>\g<a>)`:               "never ending recursion",
		`\g<0>`:                     "never ending recursion",
		`(a)\g<-2>`:                 "never ending recursion",
		`(a)(?0)`:                   "never ending recursion",
		`(a)(?-2)`:                  "never ending recursion",
		`\g<-1>`:                    "never ending recursion",
		`(?<a>(?~\g<a>))`:           "never ending recursion",
		`(?<a>a|(?~\g<a>))`:         "never ending recursion",
		`(?<a>(?=\g<a>)a)`:          "never ending recursion",
		`(?<a>a(?=\g<a>))`:          "never ending recursion",
		`(?<a>(?:a|\g<a>)+)`:        "never ending recursion",
		`(?<a>\g<a>?a)`:             "never ending recursion",
		`(?<a>\g<a>*a)`:             "never ending recursion",
		`(?<a>(?:\g<a>|a)b)`:        "never ending recursion",
		`(?<a>(?:b|)\g<a>|c)`:       "never ending recursion",
		`(?<a>(?>\g<a>)|c)`:         "never ending recursion",
		`(?<a>x|\g<b>)(?<b>\g<a>)`:  "never ending recursion",
		`(?<a>\g<b>x)(?<b>\g<a>|y)`: "never ending recursion",
		`(a)\g<+0>`:                 "invalid group name <+0>",
		`(?-0)`:                     "invalid group name <-0>",
		`(?+0)`:                     "invalid group name <+0>",
		`\g<>`:                      "group name is empty",
		`\g<`:                       "group name is empty",
		`\g'`:                       "group name is empty",
		`(?&)`:                      "group name is empty",
		`(a)\g<1`:                   "invalid group name <1>",
		`\g<n`:                      "invalid group name <n>",
		`(?&n`:                      "invalid group name <n>",
		`(a)(?1`:                    "invalid group name <1>",
		`(a)\g<1x>`:                 "undefined name <1x> reference",
		`\g<x>`:                     "undefined name <x> reference",
		`(a)\g<2>`:                  "undefined group <2> reference",
		`(a)(?2)`:                   "undefined group <2> reference",
		`(a)\g<-3>`:                 "undefined group <-3> reference",
		`(?<n>a)(?<n>b)\g<n>`:       "multiplex definition name <n> call",
		`(?P>n)`:                    "undefined group option",
		`(a+)(?<=\g<1>)b`:           "invalid pattern in look-behind",
		`(?<a>a|bc)(?<=\g<a>)b`:     "invalid pattern in look-behind",
		`(?<a>a\g<a>?)(?<=\g<a>)b`:  "invalid pattern in look-behind",
		`(?<=x\g<1>)b(a*)`:          "invalid pattern in look-behind",
		`(?<=\g<a>)b(?<a>a+)`:       "invalid pattern in look-behind",
	} {
		_, err := Compile(pattern, WithFlavor(Oniguruma))
		var se *SyntaxError
		wantErrAs(t, err, &se)
		if se.Msg != msg {
			t.Errorf("%q: got %q, want %q", pattern, se.Msg, msg)
		}
	}
	// jq reads (?R) as no group it knows, and recursion stays refused
	// under PCRE.
	for _, c := range []struct {
		pattern string
		opts    []Option
	}{{`(?R)`, []Option{WithFlavor(Oniguruma)}}, {`(a)\g<1>`, nil}, {`(a)(?1)`, nil}} {
		_, err := Compile(c.pattern, c.opts...)
		var u *UnsupportedError
		wantErrAs(t, err, &u)
	}
}

// jq's Oniguruma lets an absent operator that a call runs inside another
// change where the outer one stops, as it does for an absent operator
// written inside another, and its matches then follow neither operator
// alone. A call made inside an absent operator is refused when the group
// it runs holds an absent operator, directly or through further calls.
// jq gives (?<a>b(?~\g<a>)) on "bbb" the match [0,3], for one, and
// (?<a>c(?~\g<1>?b+?)[ab]). on "cccbb" the match [0,5], where reading
// each operator alone gives [0,1] and [2,5].
func TestCallsThatNestAbsentOperatorsAreRefused(t *testing.T) {
	onig := WithFlavor(Oniguruma)
	for _, pattern := range []string{
		`(?<a>b(?~\g<a>))`,
		`(?<a>b(?~(?&a)))`,
		`x(?~a\g<0>)`,
		`(?<a>x(?~a\g<a>)y)`,
		`(?<a>c(?~\g<1>?b+?)[ab]).`,
		`(?<a>(?~b\g<0>?\1)(bb)*[ab]).`,
		`(?<a>(?i:a))(?~\g<0>?a\g<a>?)\g<a>(\g<1>?)`,
		`(?<n>(?~b))(?~\g<n>)`,
		`(?<n>a(?~b)c){0}x(?~z\g<n>)`,
		`(?<n>q){0}(?<m>(?~\g<n>)){0}x(?~\g<m>)`,
		`(?<n>(?~c)){0}(?<m>\g<n>){0}x(?~(?=\g<m>))`,
	} {
		_, err := Compile(pattern, onig)
		var u *UnsupportedError
		wantErrAs(t, err, &u)
		wantEqual(t, u.Construct, "absent operator (?~...) called inside an absent operator")
	}
	// A call inside an absent operator of a group with none, and a call
	// of a group holding one from outside any, both match as in jq.
	wantEqual(t, spans(t, MustCompile(`(?<n>x){0}(?<m>y\g<n>){0}(?~\g<m>)z`, onig), "abyxz"), []string{"3:xz"})
	wantEqual(t, spans(t, MustCompile(`(?<n>a(?~b)c)x\g<n>`, onig), "aqcxazcb"), []string{"0:aqcxazc"})
}

// A recursion that keeps failing deep down draws on the step budget, and
// the depth limit keeps the call stack small.
func TestSubexpressionCallIsCharged(t *testing.T) {
	p := MustCompile(`(?<a>a\g<a>?b|a\g<a>?c)$`, WithFlavor(Oniguruma), WithBudget(Budget{Steps: 100000}))
	_, err := p.Find(t.Context(), strings.Repeat("a", 5000)+"d", 0)
	wantErrIs(t, err, ErrBudget)
}

func TestCallProgramSizeEstimateIsExact(t *testing.T) {
	for _, pat := range []string{
		`(?<a>[ab])\g<a>`, `^(?<p>\((?:a|\g<p>)\))$`, `(?:a|b\g<0>c)`, `(?<d>\d+){0}\g<d>`,
		`(?<a>(?:x\g<a>?y)*)`, `(a)(?<=\g<1>)b`, `(?<a>\g<b>|x)(?<b>y\g<a>)`, `(?i)(?<a>k)\g<a>{2,3}`,
	} {
		opts := []Option{WithFlavor(Oniguruma)}
		cfg := newConfig(opts)
		n, _, err := parse(pat, cfg)
		wantNil(t, err)
		n = lower(n, cfg)
		if got, want := totalSize(n, cfg.budget.MaxInstructions), len(MustCompile(pat, opts...).prog); got != want {
			t.Errorf("%q: estimated %d instructions, emitted %d", pat, got, want)
		}
	}
}
