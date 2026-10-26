package regex

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// The search shortcuts may only make a search cheaper. These tests run every
// search twice, once with the shortcuts and once without, and require the
// same matches, the same offsets, the same captures, and the same errors,
// unless the search without them runs out of budget, which is the one
// difference the shortcuts are there to make.

// withoutShortcuts compiles a pattern whose searches take no shortcut.
func withoutShortcuts() Option {
	return func(c *config) { c.plain = true }
}

// searchRun is what one search found and what it cost.
type searchRun struct {
	matches []*Match
	err     error
	steps   int
	stack   int
}

// runSearch searches subject the way FindAll does and keeps every match, the
// error that ended the search, and the steps and backtrack frames it used.
func runSearch(p *Pattern, subject string, opts MatchOptions) searchRun {
	m := newMachine(context.Background(), p, subject, opts.SkipEmpty)
	var run searchRun
	p.findAll(m, opts.Global, func(match *Match, err error) bool {
		if err != nil {
			run.err = err
			return false
		}
		run.matches = append(run.matches, match)
		return true
	})
	run.steps, run.stack = m.steps, cap(m.stack)
	return run
}

// shortcutCase is one search the differential tests run both ways.
type shortcutCase struct {
	pattern, subject string
	opts             []Option
	match            MatchOptions
}

func (c shortcutCase) String() string {
	cfg := newConfig(c.opts)
	return fmt.Sprintf("%q against %q (flavor %d, flags %#x, %+v)", c.pattern, c.subject, cfg.flavor, cfg.flags, c.match)
}

// compareShortcuts fails unless c's search finds the same thing with the
// shortcuts as without them. It returns the two runs for callers that check
// more.
func compareShortcuts(t testing.TB, c shortcutCase) (fast, plain searchRun) {
	t.Helper()
	pf, pp, ok := compileBothWays(t, c.pattern, c.opts)
	if !ok {
		return fast, plain
	}
	return compareRuns(t, c, pf, pp)
}

// compileBothWays compiles pattern with the shortcuts and without them, and
// fails unless both compile or both fail with the same error.
func compileBothWays(t testing.TB, pattern string, opts []Option) (fast, plain *Pattern, ok bool) {
	t.Helper()
	fast, errFast := Compile(pattern, opts...)
	plain, errPlain := Compile(pattern, append(append([]Option{}, opts...), withoutShortcuts())...)
	if fmt.Sprint(errFast) != fmt.Sprint(errPlain) {
		t.Fatalf("%q: compiles to %v with the shortcuts and to %v without", pattern, errFast, errPlain)
	}
	return fast, plain, errFast == nil
}

// compareRuns searches c's subject with pf, compiled with the shortcuts, and
// with pp, compiled without them, and fails unless they agree.
func compareRuns(t testing.TB, c shortcutCase, pf, pp *Pattern) (fast, plain searchRun) {
	t.Helper()
	fast, plain = runSearch(pf, c.subject, c.match), runSearch(pp, c.subject, c.match)
	if errors.Is(plain.err, ErrBudget) {
		// Without the shortcuts the search ran out of budget, so with them
		// it may find more, but it must find what the plain search found
		// first, and it may fail only by running out of budget too.
		if len(fast.matches) < len(plain.matches) || !slices.EqualFunc(fast.matches[:len(plain.matches)], plain.matches, matchesEqual) {
			t.Fatalf("%v: found %s with the shortcuts, and %s before the plain search ran out of budget", c, describeMatches(fast.matches), describeMatches(plain.matches))
		}
		if fast.err != nil && !errors.Is(fast.err, ErrBudget) {
			t.Fatalf("%v: failed with %v with the shortcuts, and ran out of budget without them", c, fast.err)
		}
		return fast, plain
	}
	if !slices.EqualFunc(fast.matches, plain.matches, matchesEqual) {
		t.Fatalf("%v: found %s with the shortcuts and %s without", c, describeMatches(fast.matches), describeMatches(plain.matches))
	}
	if fmt.Sprint(fast.err) != fmt.Sprint(plain.err) {
		t.Fatalf("%v: ended with %v with the shortcuts and %v without", c, fast.err, plain.err)
	}
	// A shortcut that cost more than the plain search could run out of
	// budget where the plain search does not.
	if fast.steps > plain.steps {
		t.Fatalf("%v: took %d steps with the shortcuts and %d without", c, fast.steps, plain.steps)
	}
	return fast, plain
}

// matchesEqual reports whether two matches agree in every offset and group.
func matchesEqual(a, b *Match) bool { return reflect.DeepEqual(a, b) }

func describeMatches(ms []*Match) string {
	parts := make([]string, len(ms))
	for i, m := range ms {
		groups := make([]string, len(m.Groups))
		for j, g := range m.Groups {
			groups[j] = fmt.Sprintf("%d-%d", g.Start, g.End)
		}
		parts[i] = "[" + strings.Join(groups, " ") + "]"
	}
	return "{" + strings.Join(parts, ", ") + "}"
}

// shortcutFlags are the flag sets the table test tries every case under.
var shortcutFlags = func() []Flags {
	var out []Flags
	for _, base := range []Flags{0, IgnoreCase, Extended, DotAll, IgnoreCase | DotAll, Extended | DotAll, IgnoreCase | Extended | DotAll} {
		out = append(out, base, base|Longest)
	}
	return out
}()

// shortcutBudget keeps the plain searches that backtrack without bound
// short, since the table runs tens of thousands of them.
var shortcutBudget = WithBudget(Budget{Steps: 100000})

// shortcutMatchOptions are the ways the table test searches.
var shortcutMatchOptions = []MatchOptions{{}, {Global: true}, {Global: true, SkipEmpty: true}}

// shortcutPatterns aim at the shortcuts: patterns that lead with .*, that
// hold text every match needs, and that repeat a single rune, alongside the
// forms each shortcut must leave alone.
var shortcutPatterns = []string{
	`.*x`, `.*?x`, `.+x`, `.+?x`, `(.*)x`, `(?:.*)x`, `(.*?)(x)`, `.*`, `.*?`, `.+`, `.*$`, `.*\n`, `.*x|y`,
	`(?s).*x`, `(?s:.*)x`, `\O*x`, `\N*x`, `^.*x`, `(?m)^.*$`, `.*(?<=a)x`, `.*\bx`, `.*+x`, `(?>.*)x`, `.*x.*y`,
	`(.*)\1`, `(.*)x\1`, `(a*)\1`, `(?<n>.*)\k<n>`, `(.*x)+`, `(.*)*x`, `.**x`, `.{0,}x`, `.{2,}x`, `.{0,3}x`,
	`a*`, `a*b`, `a*?b`, `a+b`, `a+?b`, `a{2}`, `a{2,4}`, `a{2,4}?b`, `a{0,3}`, `a?b`, `a??b`, `[ab]*c`,
	`[^\n]*x`, `\w*\d`, `\d+\.\d*`, `x*+x`, `a*+a`, `(?>a*)a`, `a{2,}+b`, `(a*)*b`, `(a|b)*c`, `(?:a{2,3})+b`,
	`é*e`, `\x{1F600}*\x{1F600}`, `ß*s`, `(?i)ß*s`, `(?i)s*ß`, `[à-ÿ]+x`, `\x{FFFD}*x`, `\x{FFFD}`, `\xff*`,
	`abc`, `a.c`, `ab(?=c)cd`, `(?<=ab)c`, `a\bb`, `foo|foo`, `foo|bar`, `(ab){2}c`, `(ab){2,}c`, `x(ab)?y`,
	`(?i)abc`, `(?i:a)bc`, `a(?i)bc`, `(a)\1x`, `(?=x)`, `(?!x).*`, `\Ax*`, `x*\z`, `x*\Z`, `^$`, `$`,
}

// shortcutSubjects exercise newlines, runes of several widths, invalid
// UTF-8, and subjects with and without the text the patterns need.
var shortcutSubjects = []string{
	"", "x", "a", "ax", "aaa", "aaax", "xa", "abcabc", "abcd", "ab\ncd", "a\nx", "aa\naa\nx", "aa\naa\n", "\n", "\n\nx",
	"x\n", "aaxaaxy", "aabb", "aaab", "aaaab", "abab c", "ababc", "fooxbar", "xaby", "xy", "éeé", "ßs", "SSß", "ſs",
	"ss", "àéx", "😀😀", "a\xffb\xc3x", "\xff\xfex", "\xe2\x82", "\xef\xbf\xbdx", "aaaa\xc3\xa9x\n\xc3", "ab x\nab\ny",
	strings.Repeat("ab\n", 20) + "x", strings.Repeat("a", 50) + "b", strings.Repeat("aé\n", 15),
}

func TestShortcutsKeepResults(t *testing.T) {
	for _, c := range loadParityCases(t) {
		crossCheck(t, c.Pattern, []string{c.Subject})
	}
	for _, pat := range shortcutPatterns {
		crossCheck(t, pat, shortcutSubjects)
	}
}

// crossCheck compares pattern's searches of every subject under both
// flavors and every flag set and way of searching.
func crossCheck(t *testing.T, pattern string, subjects []string) {
	t.Helper()
	for _, flavor := range []Flavor{PCRE, Oniguruma} {
		for _, flags := range shortcutFlags {
			opts := []Option{WithFlavor(flavor), WithFlags(flags), shortcutBudget}
			pf, pp, ok := compileBothWays(t, pattern, opts)
			if !ok {
				continue
			}
			for _, subj := range subjects {
				for _, match := range shortcutMatchOptions {
					compareRuns(t, shortcutCase{pattern: pattern, subject: subj, opts: opts, match: match}, pf, pp)
				}
			}
		}
	}
}

// A search may start past the start of the subject, and every shortcut
// counts from there.
func TestShortcutsKeepResultsFromLaterStarts(t *testing.T) {
	for _, pat := range shortcutPatterns {
		for _, subj := range shortcutSubjects {
			for _, flavor := range []Flavor{PCRE, Oniguruma} {
				for _, flags := range []Flags{0, DotAll, Longest} {
					fast := compileOrNil(pat, WithFlavor(flavor), WithFlags(flags), shortcutBudget)
					plain := compileOrNil(pat, WithFlavor(flavor), WithFlags(flags), shortcutBudget, withoutShortcuts())
					if fast == nil || plain == nil {
						continue
					}
					for start := 0; start <= len(subj); start++ {
						got, gotErr := fast.Find(context.Background(), subj, start)
						want, wantErr := plain.Find(context.Background(), subj, start)
						if errors.Is(wantErr, ErrBudget) {
							continue
						}
						if !reflect.DeepEqual(got, want) || fmt.Sprint(gotErr) != fmt.Sprint(wantErr) {
							t.Fatalf("%q against %q from %d (flavor %d, flags %#x): found %+v, %v with the shortcuts and %+v, %v without", pat, subj, start, flavor, flags, got, gotErr, want, wantErr)
						}
					}
				}
			}
		}
	}
}

// compileOrNil compiles pattern, or returns nil when it does not compile.
func compileOrNil(pattern string, opts ...Option) *Pattern {
	p, err := Compile(pattern, opts...)
	if err != nil {
		return nil
	}
	return p
}

// FuzzShortcuts runs a pattern and a subject with the shortcuts and without
// them. The mode's bits choose the flavor, the flags, and the way of
// searching.
func FuzzShortcuts(f *testing.F) {
	for _, s := range []string{`a`, `(a|b)*c`, `(?<n>\d+)-\k<n>`, `[[:alpha:]]+`, `(?<=a)b`, `(a+)+$`} {
		f.Add(s, "aaaaaaaaaaaaaaaaaaaab", uint8(0))
	}
	for i, c := range fuzzParityCases(f) {
		f.Add(c.Pattern, c.Subject, uint8(i))
	}
	mode := uint8(0)
	for i, pat := range shortcutPatterns {
		f.Add(pat, shortcutSubjects[i%len(shortcutSubjects)], mode)
		mode += 37
	}
	f.Fuzz(func(t *testing.T, pat, subj string, mode uint8) {
		var flags Flags
		for bit, flag := range []Flags{IgnoreCase, Extended, DotAll, Longest} {
			if mode&(2<<bit) != 0 {
				flags |= flag
			}
		}
		flavor := PCRE
		if mode&1 != 0 {
			flavor = Oniguruma
		}
		compareShortcuts(t, shortcutCase{
			pattern: pat,
			subject: subj,
			opts:    []Option{WithFlavor(flavor), WithFlags(flags), shortcutBudget},
			match:   MatchOptions{Global: mode&32 != 0, SkipEmpty: mode&64 != 0},
		})
	})
}

// pinSearch runs one search of subject for pattern under the Oniguruma
// flavor, as jq's test runs it, and the default budget, and returns whether
// it matched, the steps it took, and the backtrack frames it held.
func pinSearch(t *testing.T, pattern, subject string) (bool, int, int) {
	t.Helper()
	m := newMachine(context.Background(), MustCompile(pattern, WithFlavor(Oniguruma)), subject, false)
	ok, err := m.search(0)
	wantNil(t, err)
	return ok, m.steps, cap(m.stack)
}

// A loop whose body is one rune counts the runes it takes and gives them
// back from a single backtrack entry, so a long run costs a step a rune
// and no frame a rune.
func TestSingleRuneLoopsTakeOneFrame(t *testing.T) {
	as := strings.Repeat("a", 2_000_000)
	for _, c := range []struct {
		pattern, subject string
		want             bool
	}{
		{`.*x`, as + "x", true},
		{`a+x`, as + "x", true},
		{`[^x]*x`, as + "x", true},
		{`a{2,100000}a*x`, as + "x", true},
		{`.*?x`, as + "x", true},
	} {
		ok, steps, frames := pinSearch(t, c.pattern, c.subject)
		if ok != c.want {
			t.Errorf("%s against %d runes: matched %v, want %v", c.pattern, len(c.subject), ok, c.want)
		}
		if steps > defaultSteps/2 || frames > 16 {
			t.Errorf("%s against %d runes took %d steps and %d frames", c.pattern, len(c.subject), steps, frames)
		}
	}
}

// A loop whose body is more than one rune keeps a frame a rune, so the
// budget still bounds it as it did.
func TestGroupLoopsStayBounded(t *testing.T) {
	_, err := MustCompile(`^(a|b)*$`, WithBudget(Budget{MaxStack: 100})).Find(context.Background(), strings.Repeat("a", 1000), 0)
	wantErrIs(t, err, ErrBudget)
	_, err = MustCompile(`^(a|b)*$`, WithFlavor(Oniguruma)).Find(context.Background(), strings.Repeat("a", 2_000_000), 0)
	wantErrIs(t, err, ErrBudget)
}

// A pattern that leads with .* fails at every start up to the next newline
// once it fails at the first, so a search tries only the first start and
// the start of each line after it.
func TestLeadingDotStarSkipsStarts(t *testing.T) {
	lines := strings.Repeat(strings.Repeat("a", 99)+"\n", 2000)
	for _, c := range []struct {
		pattern, subject string
		want             bool
	}{
		{`.*x`, lines, false},
		{`.*x`, lines + "x", true},
		{`.*?x`, lines + "x", true},
		{`(.*)[xy]`, lines, false},
		{`.*[xy]`, strings.Repeat("a", 1_000_000), false},
		{`(?s).*[xy]`, lines, false},
	} {
		ok, steps, _ := pinSearch(t, c.pattern, c.subject)
		if ok != c.want {
			t.Errorf("%s against %d runes: matched %v, want %v", c.pattern, len(c.subject), ok, c.want)
		}
		if steps > defaultSteps/2 {
			t.Errorf("%s against %d runes took %d steps", c.pattern, len(c.subject), steps)
		}
	}
}

// TestShortcutsKeepResultsOnGeneratedSearches pairs heads and tails that
// lead with .* or need a literal with subjects drawn from a few runes of
// different widths, newlines and invalid bytes among them.
func TestShortcutsKeepResultsOnGeneratedSearches(t *testing.T) {
	heads := []string{``, `.*`, `.*?`, `.+`, `.+?`, `(.*)`, `(?s).*`, `(?s:.*?)`, `\O*`, `.{2,}`, `.*+`, `(?:.*)`, `a*`, `[^a]*?`, `x?`}
	tails := []string{`x`, `x$`, `(?<=a)x`, `\bx`, `a*x`, `[xb]`, `x|b`, `(?=x)`, `$`, `^x`, `(?m)^x`, `\n`, `é`, `(a|x)+`, `ax`, `(?i)ax`, `x\z`, ``}
	runes := []string{"a", "b", "x", "\n", "é", "\xff", "X"}
	rng := rand.New(rand.NewPCG(31, 2)) //nolint:gosec // G404: a fixed seed keeps the subjects reproducible
	subjects := []string{""}
	for range 40 {
		var b strings.Builder
		for range rng.IntN(12) {
			b.WriteString(runes[rng.IntN(len(runes))])
		}
		subjects = append(subjects, b.String())
	}
	for _, head := range heads {
		for _, tail := range tails {
			crossCheck(t, head+tail, subjects)
		}
	}
}

// A search for a pattern whose every match holds some text looks for the
// text first, and when the subject lacks it, reports no match at once.
func TestRequiredTextRulesOutSubjects(t *testing.T) {
	for _, subject := range []string{
		strings.Repeat("a", 2_000_000),
		strings.Repeat(strings.Repeat("a", 99)+"\n", 2000),
	} {
		ok, steps, _ := pinSearch(t, `.*x`, subject)
		if ok || steps > 100 {
			t.Errorf(".*x against %d runes: matched %v in %d steps", len(subject), ok, steps)
		}
	}
}

// requiredText finds text only where every match must hold it, and never
// in a lookaround, a backreference, a choice, or a case-insensitive item.
func TestRequiredText(t *testing.T) {
	onig, ci := WithFlavor(Oniguruma), WithFlags(IgnoreCase)
	for _, c := range []struct {
		pattern string
		opts    []Option
		want    string
	}{
		{`abc`, nil, "abc"},
		{`.*x`, nil, "x"},
		{`a.bcd`, nil, "bcd"},
		{`a[b]c`, nil, "abc"},
		{`a\bb`, nil, "ab"},
		{`^ab$`, nil, "ab"},
		{`(ab)c`, nil, "abc"},
		{`(?>ab)c`, nil, "abc"},
		{`(ab){2}c`, nil, "ababc"},
		{`(ab){2,}c`, nil, "abab"},
		{`a{3}`, nil, "aaa"},
		{`x(ab)+y`, nil, "ab"},
		{`a*b`, nil, "b"},
		{`a?b`, nil, "b"},
		{`a{0}b`, nil, "b"},
		{`ab(?=c)cd`, nil, "ab"},
		{`(?<=ab)c`, nil, "c"},
		{`(?!xyz)c`, nil, "c"},
		{`(a)\1b`, nil, "a"},
		{`foo|foo`, nil, "foo"},
		{`foo|bar`, nil, ""},
		{`(foo|bar)baz`, nil, "baz"},
		{`a*`, nil, ""},
		{`(?:ab)?`, nil, ""},
		{`.`, nil, ""},
		{`\x{FFFD}x`, nil, "x"},
		{`[\x{FFFD}]`, nil, ""},
		{`\xffx`, []Option{onig}, "x"},
		{`\xc3\xa9`, []Option{onig}, "é"},
		{`abc`, []Option{ci}, ""},
		{`(?i:a)bc`, nil, "bc"},
		{`a(?i)bc`, nil, "a"},
		{`1-2`, []Option{ci}, "1-2"},
		{`ss`, []Option{onig, ci}, ""},
		{`(?i)k`, nil, ""},
		{`a\Kb`, nil, "ab"},
		{strings.Repeat("a", 300), nil, strings.Repeat("a", maxRequired)},
		{strings.Repeat("é", 300), nil, strings.Repeat("é", maxRequired/2)},
		{`(abc){100}`, nil, strings.Repeat("abc", maxRequired/3) + "a"},
	} {
		p, err := Compile(c.pattern, c.opts...)
		if err != nil {
			if c.want != "" {
				t.Errorf("%q: %v", c.pattern, err)
			}
			continue
		}
		if p.required != c.want {
			t.Errorf("%q: requires %q, want %q", c.pattern, p.required, c.want)
		}
	}
}

func TestCountingLoopRefusesUnknownBody(t *testing.T) {
	defer func() {
		wantEqual(t, fmt.Sprint(recover()), fmt.Sprintf("regex: counting loop over opcode %d", opMatch))
	}()
	in := inst{op: opCount, body: opMatch}
	in.accepts('a')
	t.Fatal("accepts returned for a body no counting loop takes")
}
