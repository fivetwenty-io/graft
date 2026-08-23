package main

import "github.com/ergochat/readline"

// debugInputPainter returns a readline.Painter that renders the typed
// command line in roleInput's style: st's SGR code prepended, a reset
// appended, wrapping whatever slice readline hands it - never adding,
// removing, or reordering a visible rune.
//
// That wrap-only shape is load-bearing, not a simplification: readline
// calls its Painter with two different argument shapes depending on
// cursor position - the whole buffer on a full redraw (backspace, arrow
// keys, history recall), or only the newly typed suffix on the append
// fast path taken when the cursor sits at the end of the line (see
// research recorded against plans/colorizing-backlog-closeout.md decision
// 9). Every width and cursor calculation in the library reads the raw,
// unpainted buffer directly and never this function's return value, so a
// painter that only wraps existing runes in escape codes is correct
// under both call shapes with no extra branching; one that inserted or
// dropped a visible rune would desync the terminal's real cursor column
// from what the library computes.
//
// The returned closure is a strict identity when st is disabled or has
// no theme resolved (decision 10: color off means zero escape bytes,
// the same guarantee every other styled call site in the debugger
// already gives), and identity on an empty line (an empty buffer wrapped
// in an SGR-and-reset pair would emit two escape sequences bracketing no
// visible text at all, which is pure noise, not styling). Both identity
// paths return the input slice itself, not a re-built copy, so a caller
// holding a reference to what it passed in sees the same slice back.
func debugInputPainter(st debugStyler) readline.Painter {
	return func(line []rune, _ int) []rune {
		if !st.enabled || st.theme == nil || len(line) == 0 {
			return line
		}
		return []rune(st.apply(roleInput, string(line)))
	}
}
