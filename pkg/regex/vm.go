package regex

import (
	"context"
	"unicode/utf8"
)

// ctxCheckMask sets how often the VM checks the context: every 4,096 steps.
const ctxCheckMask = 1<<12 - 1

// frame is one entry of the backtrack stack. A frame with a slot restores
// that slot to old; any other frame resumes matching at pc and pos.
type frame struct {
	pc, pos   int
	slot, old int
}

// machine runs a pattern's program over one subject. One machine serves a
// whole call to Find, FindAll, Replace, or Split, so every start position
// and every match draws on the same step budget.
type machine struct {
	ctx      context.Context
	p        *Pattern
	subject  string
	steps    int
	notEmpty bool    // reject a match that consumes nothing
	caps     []int   // the slots of the match in progress, then of the match found
	runBest  []int   // under Longest, the longest match at the current start
	best     []int   // under Longest, the longest match at any start
	stack    []frame // reused from start to start
}

func newMachine(ctx context.Context, p *Pattern, subject string, notEmpty bool) *machine {
	m := &machine{ctx: ctx, p: p, subject: subject, notEmpty: notEmpty, caps: make([]int, p.nslots)}
	if p.cfg.has(Longest) {
		m.runBest = make([]int, p.nslots)
		m.best = make([]int, p.nslots)
	}
	return m
}

// search finds the first match that starts at or after the byte offset
// start, or under Longest the longest, and leaves its slots in m.caps.
func (m *machine) search(start int) (bool, error) {
	if err := m.ctx.Err(); err != nil {
		return false, err
	}
	if start < 0 || start > len(m.subject) || (m.p.anchored && start > 0) {
		return false, nil
	}
	if m.p.cfg.has(Longest) {
		return m.searchLongest(start)
	}
	for s, more := start, true; more; s, more = m.nextStart(s) {
		if ok, err := m.run(s, false); err != nil || ok {
			return ok, err
		}
	}
	return false, nil
}

// searchLongest tries every start position and keeps the longest match,
// the earliest one when two are equally long.
func (m *machine) searchLongest(start int) (bool, error) {
	found, bestLen := false, -1
	for s, more := start, true; more; s, more = m.nextStart(s) {
		if found && bestLen >= len(m.subject)-s {
			break // no later start leaves room for a longer match
		}
		ok, err := m.run(s, true)
		if err != nil {
			return false, err
		}
		if ok && m.caps[1]-m.caps[0] > bestLen {
			found, bestLen = true, m.caps[1]-m.caps[0]
			copy(m.best, m.caps)
		}
	}
	if found {
		copy(m.caps, m.best)
	}
	return found, nil
}

// nextStart returns the start position one rune after s, and false when
// there is none to try.
func (m *machine) nextStart(s int) (int, bool) {
	if s >= len(m.subject) || m.p.anchored {
		return 0, false
	}
	_, w := utf8.DecodeRuneInString(m.subject[s:])
	return s + w, true
}

// run tries the program at one start position. Under longest it explores
// every path and keeps the match that ends last.
//
//nolint:gocyclo // the dispatch loop runs once per step, so it stays one function
func (m *machine) run(start int, longest bool) (bool, error) {
	prog, s, caps := m.p.prog, m.subject, m.caps
	maxSteps, maxStack := m.p.cfg.budget.Steps, m.p.cfg.budget.MaxStack
	for i := range caps {
		caps[i] = -1
	}
	stack := m.stack[:0]
	defer func() { m.stack = stack[:0] }()
	found := false
	pc, pos := 0, start
	for {
		m.steps++
		if m.steps > maxSteps {
			return false, ErrBudget
		}
		if m.steps&ctxCheckMask == 0 {
			if err := m.ctx.Err(); err != nil {
				return false, err
			}
		}
		in := &prog[pc]
		switch in.op {
		case opRune:
			if c, w := decodeAt(s, pos); w > 0 && c == in.r {
				pc, pos = pc+1, pos+w
				continue
			}
		case opClass:
			if c, w := decodeAt(s, pos); w > 0 && in.set.contains(c) {
				pc, pos = pc+1, pos+w
				continue
			}
		case opAny:
			if _, w := decodeAt(s, pos); w > 0 {
				pc, pos = pc+1, pos+w
				continue
			}
		case opAnyNotNL:
			if c, w := decodeAt(s, pos); w > 0 && c != '\n' {
				pc, pos = pc+1, pos+w
				continue
			}
		case opSplit:
			if len(stack) >= maxStack {
				return false, overBudget("backtrack stack exceeds the budget of %d frames", maxStack)
			}
			stack = append(stack, frame{pc: in.y, pos: pos, slot: -1})
			pc = in.x
			continue
		case opJmp:
			pc = in.x
			continue
		case opSave:
			if len(stack) >= maxStack {
				return false, overBudget("backtrack stack exceeds the budget of %d frames", maxStack)
			}
			stack = append(stack, frame{slot: in.x, old: caps[in.x]})
			caps[in.x] = pos
			pc++
			continue
		case opLoopCheck:
			if caps[in.x] == pos {
				pc = in.y
			} else {
				pc++
			}
			continue
		case opAssert:
			if m.assert(in.assert, pos) {
				pc++
				continue
			}
		case opMatch:
			if m.notEmpty && pos == start {
				break
			}
			if !longest {
				return true, nil
			}
			if !found || pos > m.runBest[1] {
				found = true
				copy(m.runBest, caps)
			}
		}

		// The instruction failed, or under longest a match was recorded:
		// resume at the most recent branch, undoing the slots saved since.
		for {
			if len(stack) == 0 {
				if found {
					copy(caps, m.runBest)
				}
				return found, nil
			}
			f := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if f.slot >= 0 {
				caps[f.slot] = f.old
				continue
			}
			pc, pos = f.pc, f.pos
			break
		}
	}
}

// decodeAt returns the rune at pos and its width, or a width of 0 at the end
// of s. An invalid byte decodes as utf8.RuneError with a width of 1.
func decodeAt(s string, pos int) (rune, int) {
	if pos >= len(s) {
		return 0, 0
	}
	if c := s[pos]; c < utf8.RuneSelf {
		return rune(c), 1
	}
	return utf8.DecodeRuneInString(s[pos:])
}

func (m *machine) assert(kind assertKind, pos int) bool {
	s := m.subject
	switch kind {
	case assertBeginText:
		return pos == 0
	case assertEndText:
		return pos == len(s) || (pos == len(s)-1 && s[pos] == '\n')
	}
	return false
}
