package regex

import (
	"context"
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
	_, err = MustCompile(`(a|b)*c`, WithBudget(Budget{MaxStack: 100})).Find(context.Background(), strings.Repeat("a", 1000), 0)
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
	_, err := MustCompile(`(a+)+$`).Find(ctx, strings.Repeat("a", 60)+"b", 0)
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

	_, err = MustCompile(`(a|b)*c`, WithBudget(Budget{MaxStack: 100})).Find(context.Background(), strings.Repeat("a", 1000), 0)
	wantErrContains(t, err, "regex: backtrack stack exceeds the budget of 100 frames")

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
	} {
		for _, opts := range [][]Option{nil, {WithFlags(IgnoreCase)}, {WithFlavor(Oniguruma), WithFlags(IgnoreCase)}} {
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
}
