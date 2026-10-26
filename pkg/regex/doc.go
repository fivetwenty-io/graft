/*
Package regex is a backtracking regular expression engine with PCRE syntax
and an Oniguruma flavor, written for graft's jq and yq commands and usable on
its own. It imports nothing outside the standard library.

# Overview

Compile parses a pattern into a tree, lowers the tree to a flat program, and
returns a Pattern. Find, FindAll, Replace, and Split run that program over a
UTF-8 subject on a virtual machine with an explicit backtrack stack, so
classes and . match whole runes, and every Match reports its offsets in bytes
and in runes.

	p := regex.MustCompile(`(?<user>\w+)@(?<host>[\w.]+)`)
	m, err := p.Find(ctx, "mail: wayne@example.org", 0)
	if err != nil {
		return err
	}
	if m != nil {
		fmt.Println(m.Groups[2].Text("mail: wayne@example.org")) // example.org
	}

# Flavors

The PCRE flavor, the default, reads patterns the way PCRE2 does in UTF mode,
and the Oniguruma flavor reads them the way jq does. They share one parser
and differ in a small table of rules. Under PCRE, \d, \w, \s, and the POSIX
classes are ASCII, \h and \v are horizontal and vertical space, and \uhhhh
names a code point. Under Oniguruma, those classes are Unicode-aware, and
\h, \v, and \u stand for their letters, as jq reads them. Under IgnoreCase
the Oniguruma flavor also applies full case folding, so ss matches ß and ß
matches ss, where PCRE folds one code point to one.

The other rules the Oniguruma flavor changes follow jq 1.8.2 as well.

  - A backslash before a letter that names nothing is that letter, so \U
    is U and \q is q, where PCRE refuses the escape. In the same way \k
    and \g without a reference, \p and \P without a brace, \o without a
    brace, and a \E that ends no quote are letters.
  - {,n} is literal text rather than a quantifier, a quantifier may follow
    another one and repeats the repetition, so a{2}{3} is six a's, and a
    count may reach 100,000, where PCRE stops at 65,535.
  - A hyphen after a class escape, a property, or a POSIX name inside a
    bracket class is a literal hyphen, and [:alpha:] outside a bracket
    class is the plain class of its runes, where PCRE refuses both.
  - \p{...} also takes the POSIX names, such as Alpha and XDigit, and the
    long names of the general categories, such as Lowercase_Letter.
    [[:punct:]] holds punctuation and symbols together, while \p{Punct}
    holds only punctuation.
  - Extended skips only the ASCII space, tab, newline, carriage return,
    and form feed, where PCRE also skips the other spaces UTF mode names.
  - \8 or \9 at the start of a number above the group count is that digit,
    and \O matches any rune, a newline included.
  - \xhh and the octal escapes are bytes of the pattern's UTF-8 encoding,
    so a run of them spells one character, as \xc3\xa9 and [\xc3\xa9] spell
    é, and a quantifier after the run repeats the whole character. A run
    that stops before its character is complete, or a byte that cannot
    start a character outside a class, is a SyntaxError, so \xe9 alone is
    refused. An octal escape past \377 keeps its low byte, so \400 is NUL,
    and bytes that UTF-8 gives no rune for, such as \xff, match nothing.
    Inside a class a lone byte from \x80 up holds no rune, but it ends a
    range at its own value when the other end is a longer character.
    \x{...} and \o{...} still name code points, and PCRE reads every one of
    these escapes as a code point.
  - (?P<name>...) and (?P=name) are refused, as jq refuses them.

The tests/regex-parity directory holds a corpus of patterns with the
matches pcre2test and jq printed for them, and the tests replay those
goldens without either program. A PCRE golden for a global search is
recorded one search at a time and steps past an empty match as this package
does, so it pins that stepping rather than PCRE2's own. A few differences
remain on purpose. PCRE takes \uhhhh, which PCRE2 refuses. A global search
moves one rune past an empty match, where jq moves one byte. Oniguruma
accepts a lookbehind that holds a lookahead, and a capture inside a
negative or varying lookbehind, both of which jq refuses. jq's Oniguruma
makes a greedy + or * possessive when what follows cannot start the same
way, and it no longer counts a possessive repeat at its fewest, so jq
refuses a lookbehind such as (?<=a+b) that this package accepts. jq also
refuses a lookbehind that holds $ or \z, or a (?i:...) group whose length
varies, and both are accepted here. The escapes \G, \X, \y, and \Y and
subroutine calls are refused under both flavors, though jq accepts some of
them, and so is a \K inside a lookaround, which jq accepts too. A condition
other than a group's number or bracketed name is refused under both
flavors, though jq and PCRE2 accept some, such as a bare name, (?(R)...),
and (?(DEFINE)...). The Oniguruma flavor also refuses a conditional whose
one branch is not empty, such as (?(1)b), because jq's search plans as if
that branch matched nothing and misses matches through it. That flavor
refuses a group name such as n-1 that jq accepts as well, and it reads a \x
at the end of the pattern as NUL where jq matches nothing. A byte escape
that jq would match against part of a character, such as a lone \x80 in a
negated class, holds no rune here. Error messages follow each engine's
wording where the tests pin it, and a golden for a pattern jq or PCRE2
refuses checks only that Compile refuses it too.

# Supported syntax

The engine supports literals and the escapes \a, \e, \f, \n, \r, \t, \0,
\cX, \xhh, \x{h...}, and \o{o...}, with \uhhhh under PCRE. It supports the
classes ., \N, \d, \w, and \s, with \h and \v under PCRE, and their
negations. Under both flavors \R matches one line break, which is a CR LF
pair or any one vertical space, and \K moves the start of the match to
where it stands. It supports \p{...} and \P{...} over Unicode categories,
scripts, and binary properties, and bracket classes with ranges, negation,
and POSIX names. It also supports the anchors ^ and $, the assertions \A,
\z, \Z, \b, and \B, quoted sequences \Q...\E, greedy and lazy quantifiers,
alternation, numbered groups, named groups written (?<name>...) or
(?'name'...), with (?P<name>...) under PCRE, non-capturing groups, and
comments. ^ and \A match only at the start of the subject, $ and \Z only at
its end or before a final newline, and \z only at its end. \b and \B test
for a boundary between a \w rune and a rune that is not one, so they follow
the flavor's \w. PCRE also reads \Q...\E as a quote inside a bracket class,
while Oniguruma reads \Q and \E there as the letters Q and E.

Inline flag groups such as (?i), (?-s), and (?im:...) turn the flags i, m,
s, and x on or off up to the end of the group that holds them, the way both
PCRE2 and jq read them. i is IgnoreCase, s is DotAll, and x is Extended.
m makes ^ match after every newline that is not the subject's last rune and
$ match before every newline, which no Flags value does.

Lookahead (?=...) and (?!...), lookbehind (?<=...) and (?<!...), atomic
groups (?>...), and possessive quantifiers such as a*+ are supported. An
assertion is atomic, so once it holds no later failure retries it, and the
groups set inside a positive one keep their values. Each branch of a
lookbehind looks back by its own lengths, the longest first. Under PCRE a
branch must have a length limit and may vary by no more than 255 runes, as
in PCRE2, while Oniguruma lets a lookbehind look back without limit.
Oniguruma needs a lookbehind to have one length, as jq does, but first
counts a repeat of one literal, class, or backreference at its fewest
repetitions when the repeat starts a top-level branch, and when that is
none it does the same for the repeat after it. So (?<=a+|b) and
(?<=aa|b{2,3}) are accepted, while (?<=xa+), (?<=a|bc), and (?<=bb+|bc) are
refused. A lookbehind that can match the empty string without testing the
text always holds, so it is accepted too. PCRE treats a quantified
assertion as PCRE2 does, and Oniguruma refuses one as jq does.

Backreferences \1 through \9, longer numbers that name a group, \k<name>,
and \k'name' are supported. PCRE also takes \k{name}, \g with a number or
a braced name, and (?P=name), and resolves a name defined later in the
pattern. Oniguruma also takes \k<n>, \k<-n>, and \k<+n>, and wants a name
defined before its reference. Oniguruma lets several groups share a name,
and a reference to that name tries the last of them first and then each
earlier one, while PCRE refuses a repeated name. A reference to a group
that has not captured fails, and one to a group that does not exist is a
SyntaxError. A reference compares case by the IgnoreCase setting in force
where it stands. A reference inside the group it names fails under
Oniguruma, and under PCRE matches the group's last complete capture.

A conditional group (?(cond)yes|no) runs its yes branch when the group its
condition names has captured and its no branch otherwise, and under PCRE a
missing no branch matches the empty string. The condition names the group
by number, which may be signed as a reference's is, or by a name in angle
brackets or single quotes. A condition reads a capture the way a reference
does, including inside the group it names. Under Oniguruma every branch
after the first makes up the no branch, a number in <> names a group by
number, a name several groups share holds when any of them has captured,
and a conditional of one empty branch fails when its group has not
captured. PCRE refuses a third branch and a name that starts with a digit,
as PCRE2 does.

The inline flags n, J, U, and ^, the assertion \G, the escapes \X, \y,
and \Y, and the rarer constructs such as recursion, subroutine calls, and
conditions other than a group's number or name are recognized and refused
with an UnsupportedError that names them. A \K inside a lookaround is
refused as well, with a SyntaxError under PCRE, as PCRE2 refuses it, and
an UnsupportedError under Oniguruma.

# Budgets

A pattern on untrusted input can take exponential time on a backtracking
engine. Every VM step counts against a Budget, ten million steps by default,
and a call that runs out returns ErrBudget. One count covers every start
position a search tries, every match a global search finds, and the bodies
of every assertion and atomic group, so neither repetition nor nesting can
multiply the limit. The context passed to each call is checked every
few thousand steps, so a deadline ends a long match with the context's
error. The budget also bounds the pattern length, group nesting, the size of
the compiled program, and the backtrack stack, so a pattern cannot exhaust
memory before its first step.

Three shortcuts let common patterns finish well inside the budget without
changing any result. A search first checks that the subject holds the text
every match of the pattern must contain, such as the x in .*x, and reports
no match at once when it does not. A pattern that begins with .* is retried
only where the search starts and just after each newline that . refuses,
since a start in the middle of a line finds nothing the line's first start
missed. The same holds for .+, .{n,}, \N, and \O, and for any of them inside
a capture group, but a pattern that holds a backreference is retried at
every start, because a later start captures different text. A repeat of a
single rune, such as a*, [^,]+, or .{2,}, counts the runes it takes and
gives them back one at a time from one stack entry, paying a step for each
rune it takes or gives back, so it never reaches the backtrack stack's
bound. A repeat of anything longer, such as (a|b)*, keeps one stack entry
per iteration and stays bounded as before.

# Stability

This package is experimental. Until the release that completes graft jq,
its exported names may change between minor releases, and the changelog
lists every such change. The flavor rules follow the jq and PCRE2 versions
that tests/regex-parity/versions.env pins, so a rule changes only when
those goldens are recorded again, and the changelog says so. From that
release on, the module's v1 promise applies, which is that we add exported
names and never remove or change one.
*/
package regex
