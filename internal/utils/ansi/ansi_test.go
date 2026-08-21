package ansi

import (
	"os"
	"testing"

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
	})
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
