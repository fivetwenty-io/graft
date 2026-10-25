package regex

import (
	"errors"
	"fmt"
	"slices"
)

// Flavor selects the dialect a pattern is read in. The two flavors share one
// parser and differ only where PCRE2 and Oniguruma give the same syntax
// different meanings.
type Flavor uint8

const (
	// PCRE reads patterns the way PCRE2 does in UTF mode without UCP, so
	// \d, \w, \s, and the POSIX classes are ASCII, \h is horizontal space,
	// and case-insensitive matching folds one code point to one.
	PCRE Flavor = iota

	// Oniguruma reads patterns the way jq's Oniguruma does, so \d, \w, \s,
	// and the POSIX classes are Unicode-aware, \h is a hexadecimal digit,
	// and case-insensitive matching folds every item of a bracket class and
	// applies full case folding, where ß matches ss.
	Oniguruma
)

// Flags change how a pattern matches.
type Flags uint16

const (
	// IgnoreCase matches letters without regard to case.
	IgnoreCase Flags = 1 << iota

	// Extended ignores whitespace outside character classes and treats #
	// as the start of a comment that runs to the end of the line.
	Extended

	// DotAll lets . match a newline.
	DotAll

	// SingleLine makes ^ match only at the start of the subject and $ only
	// at its end or before a final newline. That is already how both
	// anchors behave, so the flag is accepted for callers that map jq's s
	// flag onto it and changes nothing.
	SingleLine

	// Longest searches every start position and returns the longest match,
	// preferring the earliest among matches of equal length, the way
	// Oniguruma's find-longest option does.
	Longest
)

// Budget bounds the work and memory a pattern may use. A zero field takes
// its default, which is ten million VM steps, a pattern of 64 KiB, 200
// levels of group nesting, a million compiled instructions, and a million
// backtrack frames.
type Budget struct {
	// Steps bounds the VM steps of one call to Find, FindAll, Replace, or
	// Split. Every start position and every match of a global search draws
	// on the same count.
	Steps int

	// MaxPattern bounds the length of the pattern in bytes.
	MaxPattern int

	// MaxNesting bounds how deeply groups may nest.
	MaxNesting int

	// MaxInstructions bounds the compiled program. Counted repetitions are
	// sized before they expand, so a pattern such as (a{1000}){1000} is
	// refused before its program is allocated. The ranges and full-folding
	// expansions of the pattern's distinct bracket classes count against
	// the same bound, so a pattern cannot build more class data than it.
	MaxInstructions int

	// MaxStack bounds the backtrack stack of one match and the capture
	// storage the pattern needs.
	MaxStack int
}

const (
	defaultSteps           = 10_000_000
	defaultMaxPattern      = 64 << 10
	defaultMaxNesting      = 200
	defaultMaxInstructions = 1_000_000
	defaultMaxStack        = 1_000_000
)

// withDefaults returns b with every field that is zero or negative set to
// its default.
func (b Budget) withDefaults() Budget {
	orDefault := func(v, def int) int {
		if v <= 0 {
			return def
		}
		return v
	}
	return Budget{
		Steps:           orDefault(b.Steps, defaultSteps),
		MaxPattern:      orDefault(b.MaxPattern, defaultMaxPattern),
		MaxNesting:      orDefault(b.MaxNesting, defaultMaxNesting),
		MaxInstructions: orDefault(b.MaxInstructions, defaultMaxInstructions),
		MaxStack:        orDefault(b.MaxStack, defaultMaxStack),
	}
}

// ErrBudget is the error every budget bound reports. A call that runs out of
// steps returns ErrBudget itself, and the other bounds return an error that
// matches it under errors.Is and names the bound in its message.
var ErrBudget = errors.New("regex: step budget exhausted")

// budgetError reports a bound other than the step count.
type budgetError struct {
	msg string
}

func (e *budgetError) Error() string { return "regex: " + e.msg }

func (e *budgetError) Is(target error) bool { return target == ErrBudget }

func overBudget(format string, args ...any) error {
	return &budgetError{msg: fmt.Sprintf(format, args...)}
}

// SyntaxError reports a pattern that is not well formed. Pos is the byte
// offset in the pattern where the offending construct starts.
type SyntaxError struct {
	Pos int
	Msg string
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("regex: %s at position %d", e.Msg, e.Pos)
}

// UnsupportedError reports a construct the engine recognizes but does not
// implement. Construct names it the way a reader would search for it.
type UnsupportedError struct {
	Construct string
}

func (e *UnsupportedError) Error() string {
	return "regex: " + e.Construct + " is not supported"
}

// config collects what the options set.
type config struct {
	flavor Flavor
	flags  Flags
	budget Budget
}

func newConfig(opts []Option) *config {
	cfg := &config{}
	for _, opt := range opts {
		opt(cfg)
	}
	cfg.budget = cfg.budget.withDefaults()
	return cfg
}

func (c *config) has(f Flags) bool { return c.flags&f != 0 }

// Option configures Compile.
type Option func(*config)

// WithFlavor reads the pattern in the given flavor. The default is PCRE.
func WithFlavor(f Flavor) Option {
	return func(c *config) { c.flavor = f }
}

// WithFlags sets the matching flags. Flags from repeated options combine.
func WithFlags(f Flags) Option {
	return func(c *config) { c.flags |= f }
}

// WithBudget bounds the pattern's compilation and matching.
func WithBudget(b Budget) Option {
	return func(c *config) { c.budget = b }
}

// Pattern is a compiled regular expression. A Pattern is safe for
// concurrent use.
type Pattern struct {
	source   string
	cfg      config
	prog     []inst
	names    []string // one per group, group 0 first, "" when unnamed
	nslots   int      // capture slots followed by loop marks
	anchored bool     // every match starts at the start of the subject
}

// Compile parses pattern and compiles it into a Pattern.
func Compile(pattern string, opts ...Option) (*Pattern, error) {
	cfg := newConfig(opts)
	if len(pattern) > cfg.budget.MaxPattern {
		return nil, overBudget("pattern length %d exceeds the budget of %d bytes", len(pattern), cfg.budget.MaxPattern)
	}
	root, names, err := parse(pattern, cfg)
	if err != nil {
		return nil, err
	}
	root = lower(root, cfg)
	prog, marks, err := compile(root, len(names), cfg)
	if err != nil {
		return nil, err
	}
	nslots := 2*len(names) + marks
	if nslots > cfg.budget.MaxStack {
		return nil, overBudget("capture storage exceeds the budget of %d frames", cfg.budget.MaxStack)
	}
	return &Pattern{
		source:   pattern,
		cfg:      *cfg,
		prog:     prog,
		names:    names,
		nslots:   nslots,
		anchored: anchoredAtStart(root),
	}, nil
}

// MustCompile is like Compile but panics when the pattern does not compile.
// It is meant for patterns written into a program.
func MustCompile(pattern string, opts ...Option) *Pattern {
	p, err := Compile(pattern, opts...)
	if err != nil {
		panic(err)
	}
	return p
}

// GroupNames returns the name of every group in the order the groups open,
// starting with the whole match at index 0. An unnamed group's name is "".
func (p *Pattern) GroupNames() []string {
	return slices.Clone(p.names)
}
