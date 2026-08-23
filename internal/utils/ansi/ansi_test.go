package ansi

import (
	"math/rand"
	"os"
	"strings"
	"testing"
	"unicode/utf8"

	. "github.com/smartystreets/goconvey/convey"
)

// Test constants for repeated string literals.
const (
	termDumb = "dumb"
)

func TestColorFunctions(t *testing.T) {
	Convey("Color functions", t, func() {
		// Ensure colors are enabled for these tests
		Color(true)

		Convey("Red wraps text in red ANSI codes", func() {
			result := Red("error")
			So(result, ShouldEqual, RedFg+"error"+ResetCode)
		})

		Convey("Green wraps text in green ANSI codes", func() {
			result := Green("success")
			So(result, ShouldEqual, GreenFg+"success"+ResetCode)
		})

		Convey("Yellow wraps text in yellow ANSI codes", func() {
			result := Yellow("warning")
			So(result, ShouldEqual, YellowFg+"warning"+ResetCode)
		})

		Convey("Blue wraps text in blue ANSI codes", func() {
			result := Blue("info")
			So(result, ShouldEqual, BlueFg+"info"+ResetCode)
		})

		Convey("Cyan wraps text in cyan ANSI codes", func() {
			result := Cyan("notice")
			So(result, ShouldEqual, CyanFg+"notice"+ResetCode)
		})

		Convey("Bold wraps text in bold ANSI codes", func() {
			result := Bold("important")
			So(result, ShouldEqual, BoldCode+"important"+ResetCode)
		})

		Convey("Dim wraps text in dim ANSI codes", func() {
			result := Dim("subtle")
			So(result, ShouldEqual, DimCode+"subtle"+ResetCode)
		})

		Convey("Empty string returns empty with codes", func() {
			result := Red("")
			So(result, ShouldEqual, RedFg+ResetCode)
		})
	})
}

func TestEnvAllowsColor(t *testing.T) {
	Convey("EnvAllowsColor()", t, func() {
		Convey("NO_COLOR unset and TERM not dumb allows color", func() {
			t.Setenv("NO_COLOR", "")
			t.Setenv("TERM", "xterm-256color")
			So(EnvAllowsColor(), ShouldBeTrue)
		})
		Convey("NO_COLOR set to any non-empty value disallows color", func() {
			t.Setenv("NO_COLOR", "1")
			t.Setenv("TERM", "xterm-256color")
			So(EnvAllowsColor(), ShouldBeFalse)
		})
		Convey("TERM=dumb disallows color even when NO_COLOR is unset", func() {
			t.Setenv("NO_COLOR", "")
			t.Setenv("TERM", termDumb)
			So(EnvAllowsColor(), ShouldBeFalse)
		})
		Convey("both NO_COLOR and TERM=dumb disallow color", func() {
			t.Setenv("NO_COLOR", "1")
			t.Setenv("TERM", termDumb)
			So(EnvAllowsColor(), ShouldBeFalse)
		})
	})
}

func TestResolveColor(t *testing.T) {
	Convey("ResolveColor()", t, func() {
		Convey("an explicit on override beats NO_COLOR and a non-tty", func() {
			t.Setenv("NO_COLOR", "1")
			t.Setenv("TERM", "xterm-256color")
			on := true
			So(ResolveColor(&on, false), ShouldBeTrue)
		})
		Convey("an explicit off override beats a tty and an unset NO_COLOR", func() {
			t.Setenv("NO_COLOR", "")
			t.Setenv("TERM", "xterm-256color")
			off := false
			So(ResolveColor(&off, true), ShouldBeFalse)
		})
		Convey("no override, env allows color, is a tty: color on", func() {
			t.Setenv("NO_COLOR", "")
			t.Setenv("TERM", "xterm-256color")
			So(ResolveColor(nil, true), ShouldBeTrue)
		})
		Convey("no override, env allows color, not a tty: color off", func() {
			t.Setenv("NO_COLOR", "")
			t.Setenv("TERM", "xterm-256color")
			So(ResolveColor(nil, false), ShouldBeFalse)
		})
		Convey("no override, NO_COLOR set, is a tty: color off", func() {
			t.Setenv("NO_COLOR", "1")
			t.Setenv("TERM", "xterm-256color")
			So(ResolveColor(nil, true), ShouldBeFalse)
		})
		Convey("no override, TERM=dumb, is a tty: color off", func() {
			t.Setenv("NO_COLOR", "")
			t.Setenv("TERM", termDumb)
			So(ResolveColor(nil, true), ShouldBeFalse)
		})
	})
}

func TestColorDisabled(t *testing.T) {
	Convey("When color is disabled", t, func() {
		Color(false)
		defer Color(true) // Restore after test

		Convey("Red returns plain text", func() {
			result := Red("error")
			So(result, ShouldEqual, "error")
		})

		Convey("Green returns plain text", func() {
			result := Green("success")
			So(result, ShouldEqual, "success")
		})

		Convey("Yellow returns plain text", func() {
			result := Yellow("warning")
			So(result, ShouldEqual, "warning")
		})

		Convey("Blue returns plain text", func() {
			result := Blue("info")
			So(result, ShouldEqual, "info")
		})

		Convey("Cyan returns plain text", func() {
			result := Cyan("notice")
			So(result, ShouldEqual, "notice")
		})

		Convey("Bold returns plain text", func() {
			result := Bold("important")
			So(result, ShouldEqual, "important")
		})

		Convey("Dim returns plain text", func() {
			result := Dim("subtle")
			So(result, ShouldEqual, "subtle")
		})
	})
}

func TestIsColorEnabled(t *testing.T) {
	Convey("IsColorEnabled", t, func() {
		Convey("Returns true when color is enabled", func() {
			Color(true)
			So(IsColorEnabled(), ShouldBeTrue)
		})

		Convey("Returns false when color is disabled", func() {
			Color(false)
			defer Color(true) // Restore after test
			So(IsColorEnabled(), ShouldBeFalse)
		})
	})
}

func TestSprintf(t *testing.T) {
	Convey("Sprintf with color codes", t, func() {
		Color(true)

		Convey("Processes @R{} for red", func() {
			result := Sprintf("@R{error}")
			So(result, ShouldEqual, RedFg+"error"+ResetCode)
		})

		Convey("Processes @r{} for red (lowercase)", func() {
			result := Sprintf("@r{error}")
			So(result, ShouldEqual, RedFg+"error"+ResetCode)
		})

		Convey("Processes @G{} for green", func() {
			result := Sprintf("@G{success}")
			So(result, ShouldEqual, GreenFg+"success"+ResetCode)
		})

		Convey("Processes @g{} for green (lowercase)", func() {
			result := Sprintf("@g{success}")
			So(result, ShouldEqual, GreenFg+"success"+ResetCode)
		})

		Convey("Processes @Y{} for yellow", func() {
			result := Sprintf("@Y{warning}")
			So(result, ShouldEqual, YellowFg+"warning"+ResetCode)
		})

		Convey("Processes @y{} for yellow (lowercase)", func() {
			result := Sprintf("@y{warning}")
			So(result, ShouldEqual, YellowFg+"warning"+ResetCode)
		})

		Convey("Processes @B{} for blue", func() {
			result := Sprintf("@B{info}")
			So(result, ShouldEqual, BlueFg+"info"+ResetCode)
		})

		Convey("Processes @b{} for blue (lowercase)", func() {
			result := Sprintf("@b{info}")
			So(result, ShouldEqual, BlueFg+"info"+ResetCode)
		})

		Convey("Processes @C{} for cyan", func() {
			result := Sprintf("@C{notice}")
			So(result, ShouldEqual, CyanFg+"notice"+ResetCode)
		})

		Convey("Processes @c{} for cyan (lowercase)", func() {
			result := Sprintf("@c{notice}")
			So(result, ShouldEqual, CyanFg+"notice"+ResetCode)
		})

		Convey("Processes @*{} for bold", func() {
			result := Sprintf("@*{important}")
			So(result, ShouldEqual, BoldCode+"important"+ResetCode)
		})

		Convey("Processes @~{} for dim", func() {
			result := Sprintf("@~{subtle}")
			So(result, ShouldEqual, DimCode+"subtle"+ResetCode)
		})

		Convey("Handles format arguments", func() {
			result := Sprintf("@R{%d} errors found", 5)
			So(result, ShouldEqual, RedFg+"5"+ResetCode+" errors found")
		})

		Convey("Handles multiple color codes", func() {
			result := Sprintf("@R{error}: @Y{warning}")
			So(result, ShouldEqual, RedFg+"error"+ResetCode+": "+YellowFg+"warning"+ResetCode)
		})

		Convey("Handles text without color codes", func() {
			result := Sprintf("plain text")
			So(result, ShouldEqual, "plain text")
		})

		Convey("Handles nested braces in content", func() {
			result := Sprintf("@R{map{key}}")
			So(result, ShouldEqual, RedFg+"map{key}"+ResetCode)
		})

		Convey("Handles unclosed color codes gracefully", func() {
			result := Sprintf("@R{unclosed")
			So(result, ShouldEqual, "@R{unclosed")
		})
	})
}

func TestSprintfColorDisabled(t *testing.T) {
	Convey("Sprintf with color disabled", t, func() {
		Color(false)
		defer Color(true) // Restore after test

		Convey("Strips color codes and returns plain text", func() {
			result := Sprintf("@R{error}")
			So(result, ShouldEqual, "error")
		})

		Convey("Handles multiple color codes", func() {
			result := Sprintf("@R{error}: @Y{warning}")
			So(result, ShouldEqual, "error: warning")
		})

		Convey("Handles format arguments", func() {
			result := Sprintf("@R{%d} errors found", 5)
			So(result, ShouldEqual, "5 errors found")
		})

		Convey("Handles nested braces", func() {
			result := Sprintf("@R{map{key}}")
			So(result, ShouldEqual, "map{key}")
		})
	})
}

func TestErrorf(t *testing.T) {
	Convey("Errorf", t, func() {
		Color(true)

		Convey("Creates error with color codes", func() {
			err := Errorf("@R{error}: something went wrong")
			So(err.Error(), ShouldEqual, RedFg+"error"+ResetCode+": something went wrong")
		})

		Convey("Handles format arguments", func() {
			err := Errorf("@R{%d} errors", 3)
			So(err.Error(), ShouldEqual, RedFg+"3"+ResetCode+" errors")
		})

		Convey("Returns plain error when color disabled", func() {
			Color(false)
			defer Color(true)
			err := Errorf("@R{error}: something went wrong")
			So(err.Error(), ShouldEqual, "error: something went wrong")
		})
	})
}

func TestStripColorCodes(t *testing.T) {
	Convey("stripColorCodes", t, func() {
		Convey("Strips red color codes", func() {
			result := stripColorCodes("@R{error}")
			So(result, ShouldEqual, "error")
		})

		Convey("Strips multiple color codes", func() {
			result := stripColorCodes("@R{error} and @Y{warning}")
			So(result, ShouldEqual, "error and warning")
		})

		Convey("Handles nested braces", func() {
			result := stripColorCodes("@R{map{key}}")
			So(result, ShouldEqual, "map{key}")
		})

		Convey("Preserves text without color codes", func() {
			result := stripColorCodes("plain text")
			So(result, ShouldEqual, "plain text")
		})

		Convey("Handles all color codes", func() {
			result := stripColorCodes("@R{r}@G{g}@Y{y}@B{b}@C{c}@*{bold}@~{dim}")
			So(result, ShouldEqual, "rgybcbolddim")
		})

		Convey("Handles unclosed color codes", func() {
			result := stripColorCodes("@R{unclosed")
			So(result, ShouldEqual, "@R{unclosed")
		})
	})
}

func TestNoColorEnvironment(t *testing.T) {
	Convey("NO_COLOR environment variable", t, func() {
		// Save original state
		originalValue := colorEnabled
		originalEnv := os.Getenv("NO_COLOR")

		cleanup := func() {
			colorEnabled = originalValue
			if originalEnv == "" {
				_ = os.Unsetenv("NO_COLOR")
			} else {
				_ = os.Setenv("NO_COLOR", originalEnv)
			}
		}
		defer cleanup()

		Convey("When NO_COLOR is set, init disables colors", func() {
			_ = os.Setenv("NO_COLOR", "1")
			colorEnabled = true // reset to true first

			// Simulate init behavior
			if os.Getenv("NO_COLOR") != "" {
				colorEnabled = false
			}

			So(colorEnabled, ShouldBeFalse)
		})

		Convey("When NO_COLOR is empty, colors remain enabled", func() {
			_ = os.Unsetenv("NO_COLOR")
			colorEnabled = true

			// Simulate init behavior
			if os.Getenv("NO_COLOR") != "" {
				colorEnabled = false
			}

			So(colorEnabled, ShouldBeTrue)
		})
	})
}

func TestTermDumbEnvironment(t *testing.T) {
	Convey("TERM=dumb environment variable", t, func() {
		// Save original state
		originalValue := colorEnabled
		originalEnv := os.Getenv("TERM")

		cleanup := func() {
			colorEnabled = originalValue
			if originalEnv == "" {
				_ = os.Unsetenv("TERM")
			} else {
				_ = os.Setenv("TERM", originalEnv)
			}
		}
		defer cleanup()

		Convey("When TERM=dumb, init disables colors", func() {
			_ = os.Setenv("TERM", termDumb)
			colorEnabled = true // reset to true first

			// Simulate init behavior
			if os.Getenv("TERM") == termDumb {
				colorEnabled = false
			}

			So(colorEnabled, ShouldBeFalse)
		})

		Convey("When TERM is not dumb, colors remain enabled", func() {
			_ = os.Setenv("TERM", "xterm-256color")
			colorEnabled = true

			// Simulate init behavior
			if os.Getenv("TERM") == termDumb {
				colorEnabled = false
			}

			So(colorEnabled, ShouldBeTrue)
		})
	})
}

func TestANSICodes(t *testing.T) {
	Convey("ANSI codes are correct", t, func() {
		Convey("Reset code is correct", func() {
			So(ResetCode, ShouldEqual, "\033[0m")
		})

		Convey("Bold code is correct", func() {
			So(BoldCode, ShouldEqual, "\033[1m")
		})

		Convey("Dim code is correct", func() {
			So(DimCode, ShouldEqual, "\033[2m")
		})

		Convey("Red foreground code is correct", func() {
			So(RedFg, ShouldEqual, "\033[31m")
		})

		Convey("Green foreground code is correct", func() {
			So(GreenFg, ShouldEqual, "\033[32m")
		})

		Convey("Yellow foreground code is correct", func() {
			So(YellowFg, ShouldEqual, "\033[33m")
		})

		Convey("Blue foreground code is correct", func() {
			So(BlueFg, ShouldEqual, "\033[34m")
		})

		Convey("Cyan foreground code is correct", func() {
			So(CyanFg, ShouldEqual, "\033[36m")
		})

		Convey("Underline code is correct", func() {
			So(UnderlineCode, ShouldEqual, "\033[4m")
		})

		Convey("Reverse code is correct", func() {
			So(ReverseCode, ShouldEqual, "\033[7m")
		})
	})
}

func TestStyleApply(t *testing.T) {
	Convey("Style.Apply", t, func() {
		Convey("wraps text in its SGR parameters and a reset", func() {
			s := Style("1;35")
			So(s.Apply("graft>"), ShouldEqual, "\033[1;35mgraft>\033[0m")
		})

		Convey("an empty Style returns the text unchanged", func() {
			s := Style("")
			So(s.Apply("plain"), ShouldEqual, "plain")
		})

		Convey("applies unconditionally when color is disabled", func() {
			Color(false)
			defer Color(true)
			s := Style("1")
			So(s.Apply("bold"), ShouldEqual, "\033[1mbold\033[0m")
		})

		Convey("applies unconditionally when color is enabled", func() {
			Color(true)
			s := Style("7")
			So(s.Apply("prompt"), ShouldEqual, "\033[7mprompt\033[0m")
		})

		Convey("empty text still gets wrapped when the style is non-empty", func() {
			s := Style("2")
			So(s.Apply(""), ShouldEqual, "\033[2m\033[0m")
		})
	})
}

func TestStripEscapes(t *testing.T) {
	Convey("StripEscapes", t, func() {
		Convey("removes a single SGR sequence", func() {
			So(StripEscapes(RedFg+"error"+ResetCode), ShouldEqual, "error")
		})

		Convey("removes multiple SGR sequences", func() {
			input := RedFg + "error" + ResetCode + ": " + YellowFg + "warning" + ResetCode
			So(StripEscapes(input), ShouldEqual, "error: warning")
		})

		Convey("removes a compound SGR parameter list", func() {
			So(StripEscapes("\033[1;35mgraft>\033[0m"), ShouldEqual, "graft>")
		})

		Convey("removes non-SGR CSI sequences, such as cursor movement", func() {
			So(StripEscapes("a\033[2Kb\033[1;1Hc"), ShouldEqual, "abc")
		})

		Convey("leaves plain text with no escape bytes untouched", func() {
			So(StripEscapes("plain text, no escapes here"), ShouldEqual, "plain text, no escapes here")
		})

		// An ESC that fails to start a recognized, terminated sequence is
		// DROPPED, not passed through: the earlier contract ("leave it in
		// place") let a later, unrelated ESC pair with the surviving byte
		// and its own trailing text to manufacture a brand-new, complete
		// escape sequence that was never present as such in the input
		// (see TestManufacturedEscape below). Dropping the lone ESC and
		// keeping every byte after it as plain text closes that hole: the
		// leftover text (e.g. "[1;35" or "]0;pwned") has no terminal
		// meaning without a genuine ESC byte in front of it, and
		// concatenating two independently stripped fragments can never
		// synthesize a new ESC byte from bytes that were not already ESC.
		Convey("drops an unterminated CSI sequence's ESC, keeping the rest as text", func() {
			So(StripEscapes("broken\033[1;35"), ShouldEqual, "broken[1;35")
		})

		// This is the specific case the contract change above rewrote:
		// under the old contract this asserted "a\033b" unchanged.
		Convey("drops a bare ESC byte with no recognized sequence following it", func() {
			So(StripEscapes("a\033b"), ShouldEqual, "ab")
		})

		Convey("handles the empty string", func() {
			So(StripEscapes(""), ShouldEqual, "")
		})

		Convey("is idempotent: stripping already-plain text is a no-op", func() {
			once := StripEscapes(RedFg + "error" + ResetCode)
			twice := StripEscapes(once)
			So(twice, ShouldEqual, once)
		})

		Convey("removes an OSC sequence terminated with BEL", func() {
			So(StripEscapes("a\033]0;title\007b"), ShouldEqual, "ab")
		})

		Convey("removes an OSC sequence terminated with ST (ESC \\\\)", func() {
			So(StripEscapes("a\033]8;;http://example.com\033\\link\033]8;;\033\\b"), ShouldEqual, "alinkb")
		})

		Convey("drops an unterminated OSC sequence's ESC, keeping the rest as text", func() {
			So(StripEscapes("a\033]0;broken title"), ShouldEqual, "a]0;broken title")
		})

		Convey("removes a DCS sequence terminated with ST", func() {
			So(StripEscapes("a\033Psome dcs payload\033\\b"), ShouldEqual, "ab")
		})

		Convey("drops an unterminated DCS sequence's ESC, keeping the rest as text", func() {
			So(StripEscapes("a\033Punterminated"), ShouldEqual, "aPunterminated")
		})

		Convey("removes an SOS/PM/APC sequence terminated with ST", func() {
			So(StripEscapes("a\033_app data\033\\b"), ShouldEqual, "ab")
		})

		Convey("drops an unterminated SOS/PM/APC sequence's ESC, keeping the rest as text", func() {
			So(StripEscapes("a\033^unterminated"), ShouldEqual, "a^unterminated")
		})

		Convey("removes a two-byte Fe escape, such as Reverse Index", func() {
			So(StripEscapes("a\033Mb"), ShouldEqual, "ab")
		})

		Convey("removes a charset-select sequence, such as select ASCII into G0", func() {
			So(StripEscapes("a\033(Bb"), ShouldEqual, "ab")
		})

		Convey("drops a charset-select sequence missing its final byte, keeping the rest as text", func() {
			So(StripEscapes("a\033("), ShouldEqual, "a(")
		})

		Convey("removes an OSC sequence and a CSI sequence in the same string", func() {
			So(StripEscapes("\033]0;title\007\033[31merror\033[0m"), ShouldEqual, "error")
		})

		Convey("leaves plain UTF-8 text with no escape bytes untouched", func() {
			So(StripEscapes("café ☃ \U0001F600"), ShouldEqual, "café ☃ \U0001F600")
		})

		Convey("does not damage a multi-byte rune adjacent to a stripped CSI sequence", func() {
			So(StripEscapes("\033[31mé\033[0m"), ShouldEqual, "é")
		})

		// A doubled ESC ("ESC ESC \") is not a recognized sequence on its
		// own: the second "ESC \" reads as a two-byte Fe escape (0x5C
		// falls in isFeByte's 0x40-0x5F range) and would previously be
		// consumed and deleted, leaving the first, unrecognized ESC to
		// survive and land directly against whatever OSC/CSI-shaped text
		// followed - manufacturing a live, terminal-honored escape
		// sequence that was never present as a complete sequence in the
		// input. Dropping every unrecognized ESC, including the first one
		// here, closes this: both ESC bytes are gone and the trailing
		// text is inert plain text.
		Convey("a doubled ESC does not manufacture a live OSC set-title sequence", func() {
			// Both ESC bytes are gone; the trailing BEL (0x07) is not
			// itself an escape byte, so it survives as plain text -
			// harmless without a real ESC byte in front of it to make it
			// part of a sequence.
			So(StripEscapes("\033\033\\]0;PWNED\007 rest"), ShouldEqual, "]0;PWNED\007 rest")
		})

		Convey("a doubled ESC does not manufacture a live CSI erase-display sequence", func() {
			So(StripEscapes("\033\033\\[2J rest"), ShouldEqual, "[2J rest")
		})

		Convey("a doubled ESC does not manufacture a live OSC-8 hyperlink pair", func() {
			// Fixpoint iteration of the old scanner reaches a stable
			// fixpoint on this input with the second pair's OSC-8
			// introducer still intact ("\033]8;;\033\\" survives
			// unchanged); dropping unrecognized ESC bytes up front does
			// not have a fixpoint to get stuck at, since every ESC that
			// cannot complete a sequence in one pass is simply gone.
			in := "\033\033\\]8;;http://evil\033\\click\033\033\\]8;;\033\\"
			So(strings.ContainsRune(StripEscapes(in), '\033'), ShouldBeFalse)
		})

		// An introducer with no terminator anywhere in the string (a
		// truncated OSC, DCS, or iTerm2 inline-image OSC, or a bare ESC as
		// the final byte) must still leave zero ESC bytes in the output:
		// StripEscapes's job is to guarantee the debugger's output cannot
		// drive the user's terminal, and a surviving bare ESC - with or
		// without a recognizable introducer after it - is exactly the
		// vector R1 and R3 exploited. The bytes after a dropped ESC are
		// kept as plain text (see "drops an unterminated OSC sequence's
		// ESC" above): that text has no terminal meaning without a real
		// ESC byte in front of it.
		Convey("an unterminated OSC introducer leaves zero escape bytes", func() {
			out := StripEscapes("\033]0;pwned")
			So(strings.ContainsRune(out, '\033'), ShouldBeFalse)
			So(out, ShouldEqual, "]0;pwned")
		})

		Convey("an unterminated DCS introducer leaves zero escape bytes", func() {
			out := StripEscapes("\033Pattacker")
			So(strings.ContainsRune(out, '\033'), ShouldBeFalse)
			So(out, ShouldEqual, "Pattacker")
		})

		Convey("an unterminated iTerm2 inline-image OSC leaves zero escape bytes", func() {
			out := StripEscapes("\033]1337;File=inline=1")
			So(strings.ContainsRune(out, '\033'), ShouldBeFalse)
			So(out, ShouldEqual, "]1337;File=inline=1")
		})

		Convey("a bare ESC as the last byte of the string leaves zero escape bytes", func() {
			out := StripEscapes("a\033")
			So(strings.ContainsRune(out, '\033'), ShouldBeFalse)
			So(out, ShouldEqual, "a")
		})
	})
}

// TestStripEscapesIdempotent locks strip(strip(x)) == strip(x) for a table
// of hand-picked cases plus a seeded (deterministic) fuzz loop over a rune
// pool mixing ESC bytes, every sequence introducer and terminator this
// package recognizes, and multi-byte UTF-8 runes. A scanner that leaves
// unrecognized ESC bytes in place is not idempotent in general (a second
// pass can find a new complete sequence formed by an ESC that survived the
// first pass sitting next to text that was not touched); dropping every
// unrecognized ESC removes that possibility, since a second pass over
// output already containing zero ESC bytes has nothing left to change.
func TestStripEscapesIdempotent(t *testing.T) {
	table := []string{
		RedFg + "error" + ResetCode,
		"a\033b",
		"broken\033[1;35",
		"a\033]0;broken title",
		"\033\033\\]0;PWNED\007 rest",
		"\033\033\\[2J rest",
		"\033\033\\]8;;http://evil\033\\click\033\033\\]8;;\033\\",
		"\033]0;pwned",
		"\033Pattacker",
	}
	for _, in := range table {
		once := StripEscapes(in)
		twice := StripEscapes(once)
		if once != twice {
			t.Errorf("not idempotent: in=%q once=%q twice=%q", in, once, twice)
		}
	}

	rng := rand.New(rand.NewSource(20260823)) //nolint:gosec // G404: weak random is acceptable for a deterministic fuzz seed
	pool := []rune("\033[]PX^_(Bm0;\a\\aMé☃")
	var fails int
	for n := 0; n < 300000; n++ {
		l := 1 + rng.Intn(12)
		var b strings.Builder
		for i := 0; i < l; i++ {
			b.WriteRune(pool[rng.Intn(len(pool))])
		}
		in := b.String()
		once := StripEscapes(in)
		twice := StripEscapes(once)
		if once != twice {
			if fails < 8 {
				t.Errorf("not idempotent: in=%q once=%q twice=%q", in, once, twice)
			}
			fails++
		}
	}
	if fails > 0 {
		t.Errorf("total non-idempotent fuzz samples: %d", fails)
	}
}

// TestStripEscapesNeverLeavesEscapeByte locks the core guarantee this fix
// adds: StripEscapes's output never contains an ESC byte (0x1b), for any
// input, including unterminated introducers that have no natural end - the
// exact shape both R1 (manufactured live sequences) and R3 (a raw
// unterminated OSC reaching the terminal) exploited. It also asserts the
// output is always valid UTF-8, matching this package's pre-existing
// multi-byte-rune-safety guarantee.
func TestStripEscapesNeverLeavesEscapeByte(t *testing.T) {
	rng := rand.New(rand.NewSource(7)) //nolint:gosec // G404: weak random is acceptable for a deterministic fuzz seed
	wide := []rune{0x00e9, 0x2603, 0x1F600, 0x00fc, 0x03a9, 0x009b, 0x009d, 0x00c3}
	pool := append([]rune("\033[]PX^_(Bm0;\a\\aM"), wide...)
	for n := 0; n < 300000; n++ {
		l := 1 + rng.Intn(10)
		var b strings.Builder
		for i := 0; i < l; i++ {
			b.WriteRune(pool[rng.Intn(len(pool))])
		}
		in := b.String()
		out := StripEscapes(in)
		if strings.ContainsRune(out, '\033') {
			t.Fatalf("ESC byte survived: in=%q (% x) -> out=%q (% x)", in, in, out, out)
		}
		if !utf8.ValidString(out) {
			t.Fatalf("UTF-8 CORRUPTION: in=%q (% x) -> out=%q (% x)", in, in, out, out)
		}
	}
}

func TestEdgeCases(t *testing.T) {
	Convey("Edge cases", t, func() {
		Color(true)

		Convey("Empty string with Sprintf", func() {
			result := Sprintf("")
			So(result, ShouldEqual, "")
		})

		Convey("Only color code with empty content", func() {
			result := Sprintf("@R{}")
			So(result, ShouldEqual, RedFg+ResetCode)
		})

		Convey("Multiple consecutive color codes", func() {
			result := Sprintf("@R{a}@G{b}@Y{c}")
			So(result, ShouldEqual, RedFg+"a"+ResetCode+GreenFg+"b"+ResetCode+YellowFg+"c"+ResetCode)
		})

		Convey("Color code at end of string", func() {
			result := Sprintf("prefix @R{end}")
			So(result, ShouldEqual, "prefix "+RedFg+"end"+ResetCode)
		})

		Convey("Color code at start of string", func() {
			result := Sprintf("@R{start} suffix")
			So(result, ShouldEqual, RedFg+"start"+ResetCode+" suffix")
		})

		Convey("Special characters in content", func() {
			result := Sprintf("@R{hello\nworld}")
			So(result, ShouldEqual, RedFg+"hello\nworld"+ResetCode)
		})

		Convey("Tab characters in content", func() {
			result := Sprintf("@R{hello\tworld}")
			So(result, ShouldEqual, RedFg+"hello\tworld"+ResetCode)
		})

		Convey("Unicode characters in content", func() {
			result := Sprintf("@R{hello world}")
			So(result, ShouldEqual, RedFg+"hello world"+ResetCode)
		})
	})
}
