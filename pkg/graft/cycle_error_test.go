package graft

import (
	"strings"
	"testing"
)

func TestSanitizeDisplayEscapesNewlines(t *testing.T) {
	// The payload proved in the spec: a value or key carrying a literal
	// newline followed by text matching genesis's stderr scrape regex.
	got := sanitizeDisplay("(( concat \"a\n - $.evil: boom\" meta.b ))")

	if strings.Contains(got, "\n") {
		t.Errorf("sanitizeDisplay() = %q; a literal newline must not survive", got)
	}
	if !strings.Contains(got, `\n`) {
		t.Errorf("sanitizeDisplay() = %q; the newline must render as the two-character sequence \\n", got)
	}
}

func TestSanitizeDisplayStripsEscapeBytes(t *testing.T) {
	got := sanitizeDisplay("(( grab \033[31mred\033[0m ))")

	if strings.Contains(got, "\033") {
		t.Errorf("sanitizeDisplay() = %q; no escape byte may survive", got)
	}
	if !strings.Contains(got, "red") {
		t.Errorf("sanitizeDisplay() = %q; the readable text must survive", got)
	}
}

func TestSanitizeDisplayPreservesColorDirectivesVerbatim(t *testing.T) {
	// An "@r{...}" sequence is not an escape; it is document text. It
	// must reach output exactly as written, which is what the MultiError
	// change makes possible.
	const in = "(( grab @r{secret} ))"

	if got := sanitizeDisplay(in); got != in {
		t.Errorf("sanitizeDisplay(%q) = %q, want it unchanged", in, got)
	}
}

func TestSanitizeDisplayEscapesCarriageReturnAndControls(t *testing.T) {
	got := sanitizeDisplay("a\rb\x01c")

	if got != `a\rb\x01c` {
		t.Errorf("sanitizeDisplay() = %q, want %q", got, `a\rb\x01c`)
	}
}

func TestSanitizeDisplayTruncatesLongStrings(t *testing.T) {
	in := strings.Repeat("x", cycleExprMaxRunes+50)

	got := sanitizeDisplay(in)

	if len([]rune(got)) != cycleExprMaxRunes+3 {
		t.Errorf("sanitizeDisplay() produced %d runes, want %d", len([]rune(got)), cycleExprMaxRunes+3)
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("sanitizeDisplay() = %q, want a trailing ellipsis", got)
	}
}

func TestSanitizeDisplayHandlesHostilePath(t *testing.T) {
	// The key.yml payload proved in the spec: a mapping key carrying a
	// newline and a forged genesis error line, reaching output through
	// op.canonical.String().
	got := sanitizeDisplay("meta.e\n - $.evil: boom")

	if strings.Contains(got, "\n") {
		t.Errorf("sanitizeDisplay() = %q; a hostile path must not break the line", got)
	}
}

func TestSanitizeDisplayEscapesLineBreakingUnicode(t *testing.T) {
	// NEL, LINE SEPARATOR, and PARAGRAPH SEPARATOR break a line in
	// terminals and editors just as \n and \r do. The spec states the
	// sanitized result "always occupies exactly one output line"; leaving
	// these three unescaped would violate that invariant.
	in := "a\u0085b\u2028c\u2029d"

	got := sanitizeDisplay(in)

	for _, r := range []rune{'\u0085', '\u2028', '\u2029'} {
		if strings.ContainsRune(got, r) {
			t.Errorf("sanitizeDisplay() = %q; must not contain literal %U", got, r)
		}
	}
	want := `a\u0085b\u2028c\u2029d`
	if got != want {
		t.Errorf("sanitizeDisplay() = %q, want %q", got, want)
	}
}

func TestSanitizeDisplayAtExactLimitIsNotTruncated(t *testing.T) {
	in := strings.Repeat("x", cycleExprMaxRunes)

	got := sanitizeDisplay(in)

	if got != in {
		t.Errorf("sanitizeDisplay() = %q, want unchanged %q", got, in)
	}
	if strings.HasSuffix(got, "...") {
		t.Errorf("sanitizeDisplay() = %q; must not truncate exactly at the limit", got)
	}
}

func TestSanitizeDisplayOneUnderLimitIsNotTruncated(t *testing.T) {
	in := strings.Repeat("x", cycleExprMaxRunes-1)

	got := sanitizeDisplay(in)

	if got != in {
		t.Errorf("sanitizeDisplay() = %q, want unchanged %q", got, in)
	}
}

func TestSanitizeDisplayOneOverLimitIsTruncated(t *testing.T) {
	in := strings.Repeat("x", cycleExprMaxRunes+1)

	got := sanitizeDisplay(in)

	want := strings.Repeat("x", cycleExprMaxRunes) + "..."
	if got != want {
		t.Errorf("sanitizeDisplay() = %q, want %q", got, want)
	}
}

func TestSanitizeDisplayTruncationNeverSplitsReplacement(t *testing.T) {
	// 119 plain runes leave exactly 1 rune of budget, but the next
	// character maps to a 2-rune replacement ("\t"). The replacement must
	// not be split across the boundary (a dangling "\" with its "t"
	// sliced off, for example) - it is either written whole or dropped
	// whole, with truncation deciding before any partial write.
	in := strings.Repeat("x", cycleExprMaxRunes-1) + "\t" + "more text that will not fit"

	got := sanitizeDisplay(in)

	want := strings.Repeat("x", cycleExprMaxRunes-1) + "..."
	if got != want {
		t.Errorf("sanitizeDisplay() = %q, want %q", got, want)
	}
}

func TestSanitizeDisplayEscSequenceAdjacentToAtSign(t *testing.T) {
	// Accepted behavior, not a defect. ansi.StripEscapes deletes only the
	// lone ESC byte from an unrecognized two-byte escape (ESC followed by
	// a lowercase letter is not a recognized introducer), so
	// "@" + ESC + "r{whoami}" sanitizes to the literal text "@r{whoami}"
	// - a directive the raw bytes never contained as one complete
	// sequence. This is accepted rather than prevented: on a non-tty,
	// ansi.Color is already false (resolved once in the root command's
	// PersistentPreRunE before any subcommand runs), so ansi.Errorf's
	// processColorCodes strips "@r{...}" instead of emitting escape
	// bytes - the no-escape-bytes-on-a-non-tty contract holds regardless
	// of this composition. An attacker gains nothing from it that writing
	// "@r{" literally does not already grant, and the spec
	// (plans/cycle-detection-provenance.md, "Closing the color-directive
	// hole") accepts in writing that any "@X{...}" sequence in an error
	// message is stripped when color is off and colorized when it is on.
	got := sanitizeDisplay("@\033r{whoami}")

	want := "@r{whoami}"
	if got != want {
		t.Errorf("sanitizeDisplay() = %q, want %q", got, want)
	}
}
