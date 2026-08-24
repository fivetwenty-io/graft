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
