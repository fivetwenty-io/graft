package graft

import (
	"fmt"
	"strings"
	"unicode/utf8"

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
//
// Two things sanitizeDisplay deliberately does NOT do. It does not
// neutralize "@X{...}" color directives: whether those render or strip
// is a decision the outer ansi layer makes based on whether color is
// enabled on the destination stream (see plans/cycle-detection-provenance.md,
// "Closing the color-directive hole"), and it is orthogonal to display
// safety - a directive left in the text is inert document content until
// something downstream chooses to interpret it. And it does not
// guarantee the result is inert against every possible downstream
// interpreter; it guarantees plain, single-line, valid-UTF-8 output
// containing no raw escape bytes, which is the contract this package's
// callers need.
func sanitizeDisplay(s string) string {
	s = ansi.StripEscapes(s)

	var b strings.Builder
	b.Grow(len(s))

	runeCount := 0
	truncated := false
	for _, r := range s {
		rep := displayReplacement(r)

		repRunes := utf8.RuneCountInString(rep)
		if runeCount+repRunes > cycleExprMaxRunes {
			truncated = true
			break
		}
		b.WriteString(rep)
		runeCount += repRunes
	}

	out := b.String()
	if truncated {
		out += "..."
	}
	return out
}

// displayReplacement returns how one rune renders inside sanitizeDisplay's
// output: itself, unchanged, for ordinary printable text, or an escaped
// form for anything that could break the single-line-output guarantee.
func displayReplacement(r rune) string {
	switch {
	case r == '\n':
		return `\n`
	case r == '\r':
		return `\r`
	case r == '\t':
		return `\t`
	case r == '\u0085', r == '\u2028', r == '\u2029':
		// NEL, LINE SEPARATOR, and PARAGRAPH SEPARATOR: like \n and \r,
		// terminals and editors treat these as line breaks, so leaving
		// them literal would violate the single-output-line guarantee.
		return fmt.Sprintf(`\u%04x`, r)
	case r < 0x20 || r == 0x7f:
		return fmt.Sprintf(`\x%02x`, r)
	default:
		return string(r)
	}
}
