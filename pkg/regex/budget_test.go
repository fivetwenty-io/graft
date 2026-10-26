package regex

import (
	"context"
	"fmt"
	"math"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestBudgetStopsCatastrophicPattern(t *testing.T) {
	p := MustCompile(`(a+)+$`, WithBudget(Budget{Steps: 100000}))
	_, err := p.Find(context.Background(), strings.Repeat("a", 40)+"b", 0)
	wantErrIs(t, err, ErrBudget)
}

func TestBudgetBoundsCompilation(t *testing.T) {
	start := time.Now()
	_, err := Compile(strings.Repeat("(", 201)+"a"+strings.Repeat(")", 201), WithBudget(Budget{MaxNesting: 200}))
	wantErrIs(t, err, ErrBudget)
	_, err = Compile(`(a{1000}){1000}`)
	wantErrIs(t, err, ErrBudget) // expands past MaxInstructions, refused before allocation
	_, err = Compile(strings.Repeat("a", 65537))
	wantErrIs(t, err, ErrBudget)
	_, err = Compile(`(a|b)*`, WithBudget(Budget{MaxStack: 100}))
	if err != nil {
		t.Fatal(err)
	}
	_, err = MustCompile(`(a|b)*c`, WithBudget(Budget{MaxStack: 100})).Find(context.Background(), strings.Repeat("a", 1000)+"c", 0)
	wantErrIs(t, err, ErrBudget)
	if el := time.Since(start); el > time.Second {
		t.Fatalf("compile bounds took %v", el)
	}
}

func TestBudgetIsSharedAcrossGlobalSearch(t *testing.T) {
	p := MustCompile(`(a+)+$`, WithBudget(Budget{Steps: 100000}))
	n := 0
	for _, err := range p.FindAll(context.Background(), strings.Repeat(strings.Repeat("a", 20)+"b ", 50), MatchOptions{Global: true}) {
		if err != nil {
			wantErrIs(t, err, ErrBudget)
			return
		}
		n++
	}
	t.Fatalf("global search finished %d matches without exhausting the shared budget", n)
}

func TestContextDeadlineStopsMatching(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	p := MustCompile(`(a+)+$`, WithBudget(Budget{Steps: math.MaxInt}))
	_, err := p.Find(ctx, strings.Repeat("a", 60)+"b", 0)
	wantErrIs(t, err, context.DeadlineExceeded)
}

func TestBudgetErrorsNameTheirBound(t *testing.T) {
	_, err := Compile(strings.Repeat("a", 11), WithBudget(Budget{MaxPattern: 10}))
	wantErrIs(t, err, ErrBudget)
	wantEqual(t, err.Error(), "regex: pattern length 11 exceeds the budget of 10 bytes")

	_, err = Compile(`((a))`, WithBudget(Budget{MaxNesting: 1}))
	wantErrContains(t, err, "regex: group nesting exceeds the budget of 1 levels")

	_, err = Compile(`a{10}`, WithBudget(Budget{MaxInstructions: 10}))
	wantErrIs(t, err, ErrBudget)
	wantErrContains(t, err, "regex: compiled program exceeds the budget of 10 instructions")

	_, err = Compile(`(a)(b)(c)`, WithBudget(Budget{MaxStack: 7}))
	wantErrIs(t, err, ErrBudget)
	wantErrContains(t, err, "regex: capture storage exceeds the budget of 7 frames")

	_, err = MustCompile(`(a|b)*c`, WithBudget(Budget{MaxStack: 100})).Find(context.Background(), strings.Repeat("a", 1000)+"c", 0)
	wantErrContains(t, err, "regex: backtrack stack exceeds the budget of 100 frames")

	// Without a c the subject cannot hold a match, so the search ends at
	// once instead of reaching the frame cap.
	m, err := MustCompile(`(a|b)*c`, WithBudget(Budget{MaxStack: 100})).Find(context.Background(), strings.Repeat("a", 1000), 0)
	wantNil(t, err)
	wantEqual(t, m, nil)

	_, err = MustCompile(`(a+)+$`, WithBudget(Budget{Steps: 1000})).Find(context.Background(), strings.Repeat("a", 30)+"b", 0)
	wantEqual(t, err, ErrBudget)
}

func TestBudgetIsSharedByReplaceAndSplit(t *testing.T) {
	p := MustCompile(`a+a+$`, WithBudget(Budget{Steps: 5000}))
	subject := strings.Repeat(strings.Repeat("a", 30)+"b", 20)
	_, err := p.Replace(context.Background(), subject, true, func(*Match) (string, error) { return "", nil })
	wantErrIs(t, err, ErrBudget)
	_, err = p.Split(context.Background(), subject)
	wantErrIs(t, err, ErrBudget)
}

func TestCancelledContextStopsBeforeMatching(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := MustCompile(`a`).Find(ctx, "a", 0)
	wantErrIs(t, err, context.Canceled)
}

func TestNestingAtTheBudgetCompiles(t *testing.T) {
	_, err := Compile(strings.Repeat("(", 200) + "a" + strings.Repeat(")", 200))
	wantNil(t, err)
	_, err = Compile(strings.Repeat("(?:", 201) + "a" + strings.Repeat(")", 201))
	wantErrIs(t, err, ErrBudget)
}

func TestProgramSizeEstimateIsExact(t *testing.T) {
	for _, pat := range []string{
		`a`, `abc`, `a|b|c`, `(a)(b)`, `a*`, `a+?`, `(a*)*`, `(a?)+`, `a{2,5}`, `a{3,}`, `(?:ab|c){0,2}`,
		`[a-z]+x`, `^a$`, `.*`, `(a|)*b{2}c{1,}d{0}`, `x{0,}`,
		`(?=ab)c`, `(?<!a|bc)d`, `(?<=a|bc)d`, `(?>a|b)*`, `a*+`, `a{2,3}+`, `(?=a(?<=a))b`, `(?<=ß|k)x`,
		`(a)\1`, `(a|b\1)+`, `(?<n>a)\k<n>`, `\1(a)`,
	} {
		for i, opts := range [][]Option{nil, {WithFlags(IgnoreCase)}, {WithFlavor(Oniguruma), WithFlags(IgnoreCase)}} {
			if i == 2 && (pat == `(?<!a|bc)d` || pat == `(?<=a|bc)d`) {
				continue // Oniguruma refuses a lookbehind of uneven branches
			}
			p := MustCompile(pat, opts...)
			cfg := newConfig(opts)
			n, _, err := parse(pat, cfg)
			wantNil(t, err)
			n = lower(n, cfg)
			if got := programSize(n, cfg.budget.MaxInstructions) + 3; got != len(p.prog) {
				t.Errorf("%q: estimated %d instructions, emitted %d", pat, got, len(p.prog))
			}
		}
	}
	shared := `(?<n>a)(?<n>b)\k<n>`
	cfg := newConfig([]Option{WithFlavor(Oniguruma)})
	n, _, err := parse(shared, cfg)
	wantNil(t, err)
	if got, want := programSize(lower(n, cfg), cfg.budget.MaxInstructions)+3, len(MustCompile(shared, WithFlavor(Oniguruma)).prog); got != want {
		t.Errorf("%q: estimated %d instructions, emitted %d", shared, got, want)
	}
}

// TestLargePatternsCompileCheaply pins that a pattern within the length
// bound cannot exhaust memory before its first step by repeating a class
// whose set is large.
// Clearing, copying, and reporting a pattern's capture slots is work that
// grows with the slot count rather than with the instructions run, so it
// draws on the step budget. Otherwise a pattern with tens of thousands of
// groups could do that much unseen work at every start or every match.
func TestSlotWorkDrawsOnStepBudget(t *testing.T) {
	ctx := context.Background()
	many := strings.Repeat("()", 32000)

	// A search that never matches clears the slots once, not at every start.
	start := time.Now()
	m, err := MustCompile("x"+many).Find(ctx, strings.Repeat("a", 200_000), 0)
	wantNil(t, err)
	wantEqual(t, m, (*Match)(nil))
	if el := time.Since(start); el > time.Second {
		t.Fatalf("a failing search over many slots took %v", el)
	}

	// Under Longest every match at every start copies the slots, even
	// those of groups the match never reached.
	_, err = MustCompile("a|b"+many, WithFlags(Longest)).Find(ctx, strings.Repeat("a", 20_000), 0)
	wantErrIs(t, err, ErrBudget)

	// A global search builds every group of every match.
	p := MustCompile("x|" + strings.Repeat("(y)", 21000))
	for _, err := range p.FindAll(ctx, strings.Repeat("x", 2000), MatchOptions{Global: true}) {
		if err != nil {
			wantErrIs(t, err, ErrBudget)
			return
		}
	}
	t.Fatal("a global search over many groups finished within the step budget")
}

func TestLargePatternsCompileCheaply(t *testing.T) {
	onig, ci := WithFlavor(Oniguruma), WithFlags(IgnoreCase)
	cases := []struct {
		item string
		opts []Option
	}{
		{`\w`, []Option{onig}},
		{`\W`, []Option{onig}},
		{`[\pL]`, []Option{onig, ci}},
		{`[^\pL]`, []Option{onig, ci}},
		{`[[:alpha:]]`, []Option{onig, ci}},
		{`\p{Lu}`, []Option{ci}},
		{`[\x{0}-\x{10ffff}]`, []Option{ci}},
	}
	for _, c := range cases {
		pattern := strings.Repeat(c.item, defaultMaxPattern/len(c.item))
		alloc := allocatedBy(func() { _, _ = Compile(pattern, c.opts...) })
		if alloc > 64<<20 {
			t.Errorf("compiling %d copies of %s allocated %d MiB", len(pattern)/len(c.item), c.item, alloc>>20)
		}
	}

	// Classes that differ in text cannot share their sets, so each one
	// draws on the instruction budget instead.
	var b strings.Builder
	for r := rune(0xe000); b.Len() < defaultMaxPattern-16; r++ {
		fmt.Fprintf(&b, `[\p{L}\x{%x}]`, r)
	}
	var err error
	alloc := allocatedBy(func() { _, err = Compile(b.String(), onig, ci) })
	if alloc > 64<<20 {
		t.Errorf("compiling distinct letter classes allocated %d MiB", alloc>>20)
	}
	wantErrIs(t, err, ErrBudget)
}

func TestClassRangesDrawOnInstructionBudget(t *testing.T) {
	_, err := Compile(`[\p{L}]`, WithFlavor(Oniguruma), WithBudget(Budget{MaxInstructions: 100}))
	wantErrIs(t, err, ErrBudget)
	wantErrContains(t, err, "character classes exceed the budget of 100 instructions")
}

// allocatedBy returns the bytes f allocates.
func allocatedBy(f func()) uint64 {
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	f()
	runtime.ReadMemStats(&after)
	return after.TotalAlloc - before.TotalAlloc
}
