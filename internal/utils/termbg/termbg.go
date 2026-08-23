// Package termbg detects whether a terminal's background is dark or
// light, so a caller can choose a legible color palette without asking
// the user. Detection never blocks or writes to a stream it cannot
// safely probe: a non-terminal stream, a multiplexer, and a terminal
// that never answers are all reported as Unknown, leaving the caller
// free to fall back to a documented default.
package termbg

import (
	"os"
	"strconv"
	"strings"

	"github.com/mattn/go-isatty"
)

// Background is a terminal's detected background brightness.
type Background int

const (
	// Unknown means detection did not run, or ran and could not tell.
	Unknown Background = iota
	// Dark is a detected dark background.
	Dark
	// Light is a detected light background.
	Light
)

// IsTerminal reports whether f is a terminal. A function var, matching
// cmd/graft's isStderrTTY/isStdoutTTY and debugInputIsInteractive, so
// tests can simulate a terminal (or its absence) without a real pty.
var IsTerminal = func(f *os.File) bool { return isatty.IsTerminal(f.Fd()) }

// Detect guesses in and out's shared terminal's background. It guards
// against every context where guessing would be unreliable or where a
// live query (not yet implemented here) could hang or leak stray
// bytes: either stream not being a terminal, a multiplexer in the way
// (a non-empty TMUX, or a TERM starting with "screen" or "tmux"), an
// unset or "dumb" TERM, and a terminal-emulating editor (a non-empty
// INSIDE_EMACS). Past every guard it falls back to the COLORFGBG
// environment variable, which costs no I/O and is often already set
// correctly by the terminal or its user.
//
// Detect reports Unknown, never Dark or Light, whenever nothing past
// the guards resolves an answer - it performs no terminal query of its
// own; a caller wanting one gets it from a separate, later probe. The
// guard order is deliberate: the multiplexer/terminal guards run before
// COLORFGBG is even read, so a value merely inherited from a parent
// shell inside tmux can never be trusted as this terminal's own.
func Detect(in, out *os.File) Background {
	if !IsTerminal(in) || !IsTerminal(out) {
		return Unknown
	}
	if os.Getenv("TMUX") != "" {
		return Unknown
	}
	term := os.Getenv("TERM")
	if strings.HasPrefix(term, "screen") || strings.HasPrefix(term, "tmux") {
		return Unknown
	}
	if term == "" || term == "dumb" || os.Getenv("INSIDE_EMACS") != "" {
		return Unknown
	}
	return ParseColorFGBG(os.Getenv("COLORFGBG"))
}

// ParseColorFGBG parses the COLORFGBG environment variable's "fg;bg" or
// "fg;default;bg" form (the rxvt/urxvt/Konsole convention) and
// classifies its background palette index: 0-6 and 8 are the dark
// palette slots, 7 and 9-15 are light. An empty value, a value with
// fewer than two ";"-separated fields, or a background field that is
// not an integer in 0-15 all report Unknown rather than guessing.
func ParseColorFGBG(v string) Background {
	fields := strings.Split(v, ";")
	if len(fields) < 2 {
		return Unknown
	}
	bg, err := strconv.Atoi(fields[len(fields)-1])
	if err != nil {
		return Unknown
	}
	switch {
	case bg == 7 || (bg >= 9 && bg <= 15):
		return Light
	case bg == 8 || (bg >= 0 && bg <= 6):
		return Dark
	default:
		return Unknown
	}
}
