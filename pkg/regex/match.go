package regex

import (
	"context"
	"iter"
	"slices"
	"strings"
	"unicode/utf8"
)

// MatchOptions control FindAll.
type MatchOptions struct {
	// Global finds every match rather than the first. After an empty match
	// the search resumes one rune later, so it always moves forward.
	Global bool

	// SkipEmpty refuses matches that consume nothing. The matcher keeps
	// backtracking past an empty match for a longer one, so a*? finds a
	// rather than nothing, as Oniguruma's find-not-empty option does.
	SkipEmpty bool
}

// Match is one match of a pattern. Start and End are byte offsets into the
// subject and RuneStart and RuneEnd count runes, where an invalid byte
// counts as one rune. Groups holds every group, the whole match first.
type Match struct {
	Start, End         int
	RuneStart, RuneEnd int
	Groups             []Group
}

// Text returns the matched text of subject, which must be the subject the
// match was found in.
func (m *Match) Text(subject string) string {
	return subject[m.Start:m.End]
}

// Group is one group of a match. A group that took no part in the match has
// Matched false and offsets of -1.
type Group struct {
	Name               string
	Start, End         int
	RuneStart, RuneEnd int
	Matched            bool
}

// Text returns the group's text of subject, or "" when the group did not
// take part in the match.
func (g Group) Text(subject string) string {
	if !g.Matched {
		return ""
	}
	return subject[g.Start:g.End]
}

// Find returns the first match that starts at or after the byte offset
// start, which should fall on a rune boundary, or nil when there is none.
// It returns ErrBudget when the step budget runs out and the context's error
// when the context ends first.
func (p *Pattern) Find(ctx context.Context, subject string, start int) (*Match, error) {
	m := newMachine(ctx, p, subject, false)
	ok, err := m.search(start)
	if err != nil || !ok {
		return nil, err
	}
	return p.newMatch(m.caps, &runeCursor{s: subject}), nil
}

// FindAll yields the first match, or every match under Global, with a nil
// error. When matching fails it yields a nil match and the error, and stops.
// One step budget covers the whole sequence, and rune offsets are counted
// from the previous match, so a global search stays linear in the subject.
func (p *Pattern) FindAll(ctx context.Context, subject string, opts MatchOptions) iter.Seq2[*Match, error] {
	return func(yield func(*Match, error) bool) {
		p.findAll(newMachine(ctx, p, subject, opts.SkipEmpty), opts.Global, yield)
	}
}

// findAll runs FindAll's search on m, which the tests build themselves so
// they can read the steps a search took.
func (p *Pattern) findAll(m *machine, global bool, yield func(*Match, error) bool) {
	subject := m.subject
	cursor := &runeCursor{s: subject}
	start := 0
	for {
		ok, err := m.search(start)
		if err != nil {
			yield(nil, err)
			return
		}
		if !ok {
			return
		}
		match := p.newMatch(m.caps, cursor)
		if !yield(match, nil) || !global {
			return
		}
		start = match.End
		if match.End == match.Start {
			if start >= len(subject) {
				return
			}
			_, w := utf8.DecodeRuneInString(subject[start:])
			start += w
		}
	}
}

// Replace returns subject with the first match, or every match when all is
// true, replaced by what repl returns for it. An error from repl or from
// matching ends the replacement and is returned.
func (p *Pattern) Replace(ctx context.Context, subject string, all bool, repl func(*Match) (string, error)) (string, error) {
	var b strings.Builder
	last := 0
	for m, err := range p.FindAll(ctx, subject, MatchOptions{Global: all}) {
		if err != nil {
			return "", err
		}
		r, err := repl(m)
		if err != nil {
			return "", err
		}
		b.WriteString(subject[last:m.Start])
		b.WriteString(r)
		last = m.End
	}
	b.WriteString(subject[last:])
	return b.String(), nil
}

// Split returns the pieces of subject between every match, as jq's split
// does, so an empty match at either end yields an empty piece there.
func (p *Pattern) Split(ctx context.Context, subject string) ([]string, error) {
	var parts []string
	last := 0
	for m, err := range p.FindAll(ctx, subject, MatchOptions{Global: true}) {
		if err != nil {
			return nil, err
		}
		parts = append(parts, subject[last:m.Start])
		last = m.End
	}
	return append(parts, subject[last:]), nil
}

// newMatch builds a Match from the slots a search left.
func (p *Pattern) newMatch(caps []int, cursor *runeCursor) *Match {
	offsets := make([]int, 0, 2*len(p.names))
	for _, c := range caps[:2*len(p.names)] {
		if c >= 0 {
			offsets = append(offsets, c)
		}
	}
	slices.Sort(offsets)
	offsets = slices.Compact(offsets)
	runes := make([]int, len(offsets))
	for i, b := range offsets {
		runes[i] = cursor.runeOffset(b)
	}
	runeAt := func(b int) int {
		i, _ := slices.BinarySearch(offsets, b)
		return runes[i]
	}

	groups := make([]Group, len(p.names))
	for i, name := range p.names {
		start, end := caps[2*i], caps[2*i+1]
		if start < 0 || end < 0 {
			groups[i] = Group{Name: name, Start: -1, End: -1, RuneStart: -1, RuneEnd: -1}
			continue
		}
		groups[i] = Group{Name: name, Start: start, End: end, RuneStart: runeAt(start), RuneEnd: runeAt(end), Matched: true}
	}
	whole := groups[0]
	return &Match{Start: whole.Start, End: whole.End, RuneStart: whole.RuneStart, RuneEnd: whole.RuneEnd, Groups: groups}
}

// runeCursor converts byte offsets to rune offsets, counting from the last
// offset it converted, so ascending offsets cost one pass over the subject.
type runeCursor struct {
	s             string
	byteAt, runes int
}

func (c *runeCursor) runeOffset(b int) int {
	if b < c.byteAt {
		return c.runes - utf8.RuneCountInString(c.s[b:c.byteAt])
	}
	c.runes += utf8.RuneCountInString(c.s[c.byteAt:b])
	c.byteAt = b
	return c.runes
}
