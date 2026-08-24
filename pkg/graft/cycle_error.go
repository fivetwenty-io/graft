package graft

import (
	"fmt"
	"strings"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
)

// cycleExprMaxRunes bounds how much of one file-derived string reaches
// output. A cycle block is a diagnostic, not a document dump.
const cycleExprMaxRunes = 120

// sanitizeDisplay makes one file-derived string safe to print inside an
// error block. It applies to paths as well as expressions and filenames:
// a YAML mapping key carries exactly the same payloads an operator
// expression can, and a path is printed twice - once in the chain line
// and once in its detail line.
//
// Three properties matter, in order:
//
//   - No escape bytes reach a terminal, so stderr stays clean on a
//     non-tty (the genesis stderr contract).
//   - The result occupies exactly one output line, so it cannot forge a
//     line beginning " - $." that genesis would scrape as an error.
//   - The result is bounded, so a pathological value cannot flood the
//     block.
func sanitizeDisplay(s string) string {
	s = ansi.StripEscapes(s)

	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\n':
			b.WriteString(`\n`)
		case r == '\r':
			b.WriteString(`\r`)
		case r == '\t':
			b.WriteString(`\t`)
		case r < 0x20 || r == 0x7f:
			fmt.Fprintf(&b, `\x%02x`, r)
		default:
			b.WriteRune(r)
		}
	}

	out := b.String()
	if runes := []rune(out); len(runes) > cycleExprMaxRunes {
		out = string(runes[:cycleExprMaxRunes]) + "..."
	}
	return out
}
