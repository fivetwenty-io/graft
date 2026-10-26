package regex

import (
	"testing"
)

// Outside a class, \ and a number of two or more digits is a backreference
// only when that many groups have opened before it, as in PCRE2 and jq.
// Otherwise up to three octal digits name a character and the rest are
// literal. The expected matches were recorded from pcre2test 10.49 and jq
// 1.8.2.
func TestMultiDigitEscapesPastTheGroupCountAreOctal(t *testing.T) {
	twelve := `(a)(b)(c)(d)(e)(f)(g)(h)(i)(j)(k)(l)`
	cases := []struct{ pattern, subject string }{
		{`^\101$`, "A"},
		{`^\18$`, "\x018"},
		{`^\11$`, "\t"},
		{`^\1011$`, "A1"},
		{`^\12` + twelve + `$`, "\nabcdefghijkl"},
	}
	for _, flavor := range []Flavor{PCRE, Oniguruma} {
		for _, c := range cases {
			wantMatch(t, MustCompile(c.pattern, WithFlavor(flavor)), c.subject)
		}
	}
	wantMatch(t, MustCompile(`^\400$`), "\u0100")

	// Otherwise the number is a backreference, which must name a group.
	var se *SyntaxError
	for _, flavor := range []Flavor{PCRE, Oniguruma} {
		wantMatch(t, MustCompile(twelve+`\12`, WithFlavor(flavor)), "abcdefghijkll")
		_, err := Compile(`\1`, WithFlavor(flavor))
		wantErrAs(t, err, &se)
	}
	_, err := Compile(`\81`)
	wantErrAs(t, err, &se)
}

func TestUnsupportedConstructNames(t *testing.T) {
	cases := map[string]string{
		`\Gx`:                      `assertion \G`,
		`\X`:                       `extended grapheme cluster \X`,
		`(?R)`:                     "recursion (?R)",
		`(a)(?1)`:                  "recursion (?1)",
		`(?&n)`:                    "recursion (?&n)",
		`(?P>n)`:                   "recursion (?P>n)",
		`(a)\g<1>`:                 `recursion \g<1>`,
		`(?(1)a|b)`:                "conditional group (?(...)",
		`(?C1)a`:                   "callout (?C...)",
		`(*FAIL)`:                  "backtracking control verb (*FAIL)",
		`(*pla:a)`:                 "alpha assertion (*pla:...)",
		`(*positive_lookahead:a)b`: "alpha assertion (*positive_lookahead:...)",
		`(*atomic:a)`:              "alpha assertion (*atomic:...)",
		`(?*a)`:                    "non-atomic lookahead (?*...)",
		`(?<*a)b`:                  "non-atomic lookbehind (?<*...)",
		`(?~a)`:                    "absent operator (?~...)",
		`(?|a)`:                    "branch reset group (?|...)",
		`\N{U+41}`:                 `named character \N{...}`,
	}
	for pat, construct := range cases {
		_, err := Compile(pat)
		var u *UnsupportedError
		wantErrAs(t, err, &u)
		if u.Construct != construct {
			t.Errorf("%q: got construct %q, want %q", pat, u.Construct, construct)
		}
	}
	_, err := Compile(`(?R)`)
	wantEqual(t, err.Error(), "regex: recursion (?R) is not supported")
}

func TestSyntaxErrorHasPosition(t *testing.T) {
	_, err := Compile(`a(b`)
	var se *SyntaxError
	wantErrAs(t, err, &se)
	wantEqual(t, se.Pos, 1)
	wantEqual(t, se.Error(), "regex: missing closing parenthesis at position 1")
}

func TestSyntaxErrors(t *testing.T) {
	cases := []struct {
		pattern string
		pos     int
		msg     string
	}{
		{`a)`, 1, "unmatched closing parenthesis"},
		{`*a`, 0, "quantifier does not follow a repeatable item"},
		{`a|*`, 2, "quantifier does not follow a repeatable item"},
		{`(*)`, 1, "quantifier does not follow a repeatable item"},
		{`^*a`, 1, "quantifier does not follow a repeatable item"},
		{`a**`, 2, "quantifier does not follow a repeatable item"},
		{`a{2}{3}`, 4, "quantifier does not follow a repeatable item"},
		{`a{3,2}`, 1, "numbers out of order in {} quantifier"},
		{`a{70000}`, 1, "number too big in {} quantifier"},
		{`[a`, 0, "missing terminating ] for character class"},
		{`[z-a]`, 1, "range out of order in character class"},
		{`[\d-z]`, 1, "invalid range in character class"},
		{`\`, 0, `\ at end of pattern`},
		{`\c`, 0, `\c at end of pattern`},
		{`\y`, 0, `unrecognized character follows \`},
		{`\p{Foo}`, 0, `unknown property after \P or \p`},
		{`\p{Greek`, 0, `malformed \P or \p sequence`},
		{`\x{110000}`, 0, `character code point value in \x{} or \o{} is too large`},
		{`\x{d800}`, 0, "disallowed Unicode code point (>= 0xd800 && <= 0xdfff)"},
		{`\x{12`, 0, `missing } after \x{ or \o{`},
		{`\u12`, 0, `\u must be followed by four hexadecimal digits`},
		{`[:alpha:]`, 0, "POSIX named classes are supported only within a class"},
		{`[[:alfa:]]`, 1, "unknown POSIX class name"},
		{`(?<1a>x)`, 3, "subpattern name must start with a non-digit"},
		{`(?<n>a)(?<n>b)`, 10, "two named subpatterns have the same name"},
		{`(?<n`, 3, "syntax error in subpattern name (missing terminator?)"},
		{`(?Q)`, 2, "unrecognized character after (? or (?-"},
		{"a\xffb", 1, "invalid UTF-8"},
		{`[\N]`, 1, `\N is not supported in a class`},
	}
	for _, c := range cases {
		_, err := Compile(c.pattern)
		var se *SyntaxError
		wantErrAs(t, err, &se)
		if se.Pos != c.pos || se.Msg != c.msg {
			t.Errorf("%q: got %q at %d, want %q at %d", c.pattern, se.Msg, se.Pos, c.msg, c.pos)
		}
	}
}

func TestGroupNamesAndNumbering(t *testing.T) {
	p := MustCompile(`(a)(?:b)(?<x>c)((d))`)
	wantEqual(t, p.GroupNames(), []string{"", "", "x", "", ""})
	names := p.GroupNames()
	names[2] = "changed"
	wantEqual(t, p.GroupNames()[2], "x") // the caller gets a copy
}

func TestLiteralRunsMerge(t *testing.T) {
	n, _, err := parse(`ab(?:cd)e*f`, newConfig(nil))
	wantNil(t, err)
	wantEqual(t, n.kind, kindConcat)
	wantEqual(t, len(n.subs), 3)
	wantEqual(t, string(n.subs[0].runes), "abcd")
	wantEqual(t, n.subs[1].kind, kindRepeat)
	wantEqual(t, string(n.subs[2].runes), "f")
}
