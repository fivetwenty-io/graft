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

# Supported syntax

The engine supports literals and the escapes \a, \e, \f, \n, \r, \t, \0,
\cX, \xhh, \x{h...}, and \o{o...}, with \uhhhh under PCRE. It supports the
classes ., \N, \d, \w, and \s, with \h and \v under PCRE, and their
negations. It supports \p{...} and \P{...} over Unicode categories, scripts,
and binary properties, and bracket classes with ranges, negation, and POSIX
names. It also supports the anchors ^ and $, the assertions \A, \z, \Z, \b,
and \B, quoted sequences \Q...\E, greedy and lazy quantifiers, alternation,
numbered groups, named groups written (?<name>...), (?'name'...), or
(?P<name>...), non-capturing groups, and comments. ^ and \A match only at the
start of the subject, $ and \Z only at its end or before a final newline,
and \z only at its end. \b and \B test for a boundary between a \w rune and
a rune that is not one, so they follow the flavor's \w.

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
in PCRE2, while Oniguruma lets a lookbehind look back without limit. PCRE
treats a quantified assertion as PCRE2 does, and Oniguruma refuses one as
jq does.

Backreferences \1 through \9, longer numbers that name a group, \k<name>,
and \k'name' are supported. PCRE also takes \k{name}, \g with a number or
a braced name, and (?P=name), and resolves a name defined later in the
pattern. Oniguruma also takes \k<n>, \k<-n>, and \k<+n>, and wants a
name defined before its reference. Oniguruma lets several groups share a
name, and a reference to that name tries the last of them first and then
each earlier one, while PCRE refuses a repeated name. A reference to a group that has not
captured fails, and one to a group that does not exist is a SyntaxError. A
reference compares case by the IgnoreCase setting in force where it
stands. A reference inside the group it names fails under Oniguruma, and
under PCRE matches the group's last complete capture.

The inline flags n, J, U, and ^, the assertion \G, \g under Oniguruma, and
the rarer PCRE constructs such as recursion and conditionals are recognized
and refused with an UnsupportedError that names them.

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

# Stability

This package is experimental. Until the release that completes graft jq,
its exported names may change between minor releases, and the changelog
lists every such change. From that release on, the module's v1 promise
applies, which is that we add exported names and never remove or change
one.
*/
package regex
