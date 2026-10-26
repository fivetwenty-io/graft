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
	start    int     // the position the current run started at
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
// start, or under Longest the longest, and leaves its slots in m.caps. It
// clears the slots once, since a failed attempt unwinds every slot it saved
// and so leaves them clear for the next start. That clearing, and building
// a match from the slots afterward, are charged here.
func (m *machine) search(start int) (bool, error) {
	if err := m.ctx.Err(); err != nil {
		return false, err
	}
	if start < 0 || start > len(m.subject) || (m.p.anchored && start > 0) {
		return false, nil
	}
	if err := m.clearCaps(); err != nil {
		return false, err
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
		if !ok {
			continue
		}
		if m.caps[1]-m.caps[0] > bestLen {
			if err := m.charge(len(m.caps)); err != nil {
				return false, err
			}
			found, bestLen = true, m.caps[1]-m.caps[0]
			copy(m.best, m.caps)
		}
		if err := m.clearCaps(); err != nil {
			return false, err
		}
	}
	if found {
		copy(m.caps, m.best)
	}
	return found, nil
}

// charge draws n steps for work that grows with the pattern's slots rather
// than with the instructions run, such as clearing or copying them.
func (m *machine) charge(n int) error {
	m.steps += n
	if m.steps > m.p.cfg.budget.Steps {
		return ErrBudget
	}
	return m.ctx.Err()
}

// clearCaps marks every slot unset.
func (m *machine) clearCaps() error {
	if err := m.charge(len(m.caps)); err != nil {
		return err
	}
	for i := range m.caps {
		m.caps[i] = -1
	}
	return nil
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

// run tries the program at one start position, with every slot clear.
// Under longest it explores every path and keeps the match that ends last.
func (m *machine) run(start int, longest bool) (bool, error) {
	m.start = start
	m.stack = m.stack[:0]
	_, ok, err := m.exec(0, start, -1, longest)
	m.stack = m.stack[:0]
	return ok, err
}

// exec runs the program from pc at pos on the shared backtrack stack, and
// backtracks no further than the frames it found there. It returns at a
// match, or at the opSucceed that ends a body, with the position it reached
// and with the frames it pushed still on the stack, for the caller to keep
// or drop. When every path fails it has unwound the stack to where it
// began. A body run with end >= 0 succeeds only where it reaches end, which
// pins a lookbehind's body to the position it looks behind. Only the run at
// the top level reaches opMatch, so longest applies there alone.
//
//nolint:gocyclo // the dispatch loop runs once per step, so it stays one function
func (m *machine) exec(pc, pos, end int, longest bool) (int, bool, error) {
	prog, s, caps := m.p.prog, m.subject, m.caps
	maxSteps, maxStack := m.p.cfg.budget.Steps, m.p.cfg.budget.MaxStack
	stack := m.stack
	base := len(stack)
	defer func() { m.stack = stack }()
	found := false
	for {
		m.steps++
		if m.steps > maxSteps {
			return 0, false, ErrBudget
		}
		if m.steps&ctxCheckMask == 0 {
			if err := m.ctx.Err(); err != nil {
				return 0, false, err
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
				return 0, false, overBudget("backtrack stack exceeds the budget of %d frames", maxStack)
			}
			stack = append(stack, frame{pc: in.y, pos: pos, slot: -1})
			pc = in.x
			continue
		case opJmp:
			pc = in.x
			continue
		case opSave:
			if len(stack) >= maxStack {
				return 0, false, overBudget("backtrack stack exceeds the budget of %d frames", maxStack)
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
			if m.assert(in, pos) {
				pc++
				continue
			}
		case opAhead, opBehind:
			m.stack = stack
			ok, err := m.look(in, pos)
			stack = m.stack
			if err != nil {
				return 0, false, err
			}
			if ok {
				pc = in.y
				continue
			}
		case opAtomic:
			m.stack = stack
			next, ok, err := m.atomic(in, pos)
			stack = m.stack
			if err != nil {
				return 0, false, err
			}
			if ok {
				pc, pos = in.y, next
				continue
			}
		case opSucceed:
			if end < 0 || pos == end {
				return pos, true, nil
			}
		case opMatch:
			if m.notEmpty && pos == m.start {
				break
			}
			if !longest {
				return pos, true, nil
			}
			if !found || pos > m.runBest[1] {
				if err := m.charge(len(caps)); err != nil {
					return 0, false, err
				}
				found = true
				copy(m.runBest, caps)
			}
		}

		// The instruction failed, or under longest a match was recorded:
		// resume at the most recent branch, undoing the slots saved since.
		for {
			if len(stack) == base {
				if found {
					if err := m.charge(len(caps)); err != nil {
						return 0, false, err
					}
					copy(caps, m.runBest)
				}
				return 0, found, nil
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

// look runs a lookaround's body at pos and reports whether the assertion
// holds. An assertion is atomic, so one that holds keeps the slots its body
// set and drops the body's branches, and a negative one keeps nothing.
func (m *machine) look(in *inst, pos int) (bool, error) {
	base := len(m.stack)
	var ok bool
	var err error
	if in.op == opAhead {
		_, ok, err = m.exec(in.x, pos, -1, false)
	} else {
		ok, err = m.behind(in, pos)
	}
	switch {
	case err != nil:
		return false, err
	case ok && in.neg:
		return false, m.unwind(base)
	case ok:
		return true, m.commit(base)
	}
	return in.neg, nil
}

// behind runs a lookbehind's body from each position between in.lo and
// in.hi runes before pos until the body matches exactly up to pos. A
// bounded lookbehind tries the farthest start first, as PCRE2 does, and an
// unbounded one, which only the Oniguruma flavor allows, tries the nearest
// first so that it stops stepping back once the body matches. Each rune
// stepped over is charged a step.
func (m *machine) behind(in *inst, pos int) (bool, error) {
	s := m.subject
	if in.hi == unbounded {
		for at, k := pos, 0; ; k++ {
			if k >= in.lo {
				if _, ok, err := m.exec(in.x, at, pos, false); err != nil || ok {
					return ok, err
				}
			}
			if at == 0 {
				return false, nil
			}
			_, w := utf8.DecodeLastRuneInString(s[:at])
			at -= w
			if err := m.spend(1); err != nil {
				return false, err
			}
		}
	}
	at, k := pos, 0
	for k < in.hi && at > 0 {
		_, w := utf8.DecodeLastRuneInString(s[:at])
		at -= w
		k++
	}
	if err := m.spend(k); err != nil {
		return false, err
	}
	for ; k >= in.lo; k-- {
		if _, ok, err := m.exec(in.x, at, pos, false); err != nil || ok {
			return ok, err
		}
		_, w := decodeAt(s, at)
		at += w
	}
	return false, nil
}

// atomic runs an atomic group's body at pos and keeps its first match,
// dropping the branches that would let backtracking try another.
func (m *machine) atomic(in *inst, pos int) (int, bool, error) {
	base := len(m.stack)
	next, ok, err := m.exec(in.x, pos, -1, false)
	if err != nil || !ok {
		return 0, false, err
	}
	return next, true, m.commit(base)
}

// commit drops the branch frames above base and keeps the slot frames, so
// that backtracking past an assertion or an atomic group still restores
// the slots its body set. Each frame it passes over is charged a step.
func (m *machine) commit(base int) error {
	if err := m.spend(len(m.stack) - base); err != nil {
		return err
	}
	kept := base
	for _, f := range m.stack[base:] {
		if f.slot >= 0 {
			m.stack[kept] = f
			kept++
		}
	}
	m.stack = m.stack[:kept]
	return nil
}

// unwind pops the frames above base, restoring the slots they saved. Each
// frame is charged a step.
func (m *machine) unwind(base int) error {
	if err := m.spend(len(m.stack) - base); err != nil {
		return err
	}
	for i := len(m.stack) - 1; i >= base; i-- {
		if f := m.stack[i]; f.slot >= 0 {
			m.caps[f.slot] = f.old
		}
	}
	m.stack = m.stack[:base]
	return nil
}

// spend draws n steps for work the dispatch loop does not count, and checks
// the context whenever the count crosses a multiple of the check interval,
// as the loop does.
func (m *machine) spend(n int) error {
	before := m.steps
	m.steps += n
	if m.steps > m.p.cfg.budget.Steps {
		return ErrBudget
	}
	if before>>12 != m.steps>>12 {
		return m.ctx.Err()
	}
	return nil
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

func (m *machine) assert(in *inst, pos int) bool {
	s := m.subject
	switch in.assert {
	case assertBeginText:
		return pos == 0
	case assertEndText:
		return pos == len(s) || (pos == len(s)-1 && s[pos] == '\n')
	case assertEndSubject:
		return pos == len(s)
	case assertBeginLine:
		return pos == 0 || pos < len(s) && s[pos-1] == '\n'
	case assertEndLine:
		return pos == len(s) || s[pos] == '\n'
	case assertWordBoundary, assertNotBoundary:
		before, after := false, false
		if pos > 0 {
			c, _ := utf8.DecodeLastRuneInString(s[:pos])
			before = in.set.contains(c)
		}
		if c, w := decodeAt(s, pos); w > 0 {
			after = in.set.contains(c)
		}
		return (before != after) == (in.assert == assertWordBoundary)
	}
	return false
}
