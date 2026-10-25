package regex

import (
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
	// and case-insensitive matching folds every item of a bracket class.
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
}

func newConfig(opts []Option) *config {
	cfg := &config{}
	for _, opt := range opts {
		opt(cfg)
	}
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

// Pattern is a compiled regular expression. A Pattern is safe for
// concurrent use.
type Pattern struct {
	names []string // one per group, group 0 first, "" when unnamed
}

// Compile parses pattern and compiles it into a Pattern.
func Compile(pattern string, opts ...Option) (*Pattern, error) {
	cfg := newConfig(opts)
	_, names, err := parse(pattern, cfg)
	if err != nil {
		return nil, err
	}
	return &Pattern{names: names}, nil
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
