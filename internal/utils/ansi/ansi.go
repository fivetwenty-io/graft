// Package ansi provides ANSI escape code utilities for terminal output.
package ansi

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

var colorEnabled = true

// Color enables or disables color output.
func Color(enabled bool) {
	colorEnabled = enabled
}

// IsColorEnabled returns whether color output is enabled.
func IsColorEnabled() bool {
	return colorEnabled
}

// ANSI escape codes.
const (
	ResetCode     = "\033[0m"
	BoldCode      = "\033[1m"
	DimCode       = "\033[2m"
	UnderlineCode = "\033[4m"
	ReverseCode   = "\033[7m"
	RedFg         = "\033[31m"
	GreenFg       = "\033[32m"
	YellowFg      = "\033[33m"
	BlueFg        = "\033[34m"
	MagentaFg     = "\033[35m"
	CyanFg        = "\033[36m"
	WhiteFg       = "\033[37m"
)

// Red wraps the string in red ANSI color codes.
func Red(s string) string {
	if !colorEnabled {
		return s
	}
	return RedFg + s + ResetCode
}

// Green wraps the string in green ANSI color codes.
func Green(s string) string {
	if !colorEnabled {
		return s
	}
	return GreenFg + s + ResetCode
}

// Yellow wraps the string in yellow ANSI color codes.
func Yellow(s string) string {
	if !colorEnabled {
		return s
	}
	return YellowFg + s + ResetCode
}

// Blue wraps the string in blue ANSI color codes.
func Blue(s string) string {
	if !colorEnabled {
		return s
	}
	return BlueFg + s + ResetCode
}

// Cyan wraps the string in cyan ANSI color codes.
func Cyan(s string) string {
	if !colorEnabled {
		return s
	}
	return CyanFg + s + ResetCode
}

// Bold wraps the string in bold ANSI codes.
func Bold(s string) string {
	if !colorEnabled {
		return s
	}
	return BoldCode + s + ResetCode
}

// Dim wraps the string in dim ANSI codes.
func Dim(s string) string {
	if !colorEnabled {
		return s
	}
	return DimCode + s + ResetCode
}

// Style is a set of SGR (Select Graphic Rendition) attributes, expressed
// as the parameter list of a CSI SGR sequence (for example "1;35" for
// bold magenta), with no leading "\033[" and no trailing "m". The zero
// value is the empty Style, which renders as no styling at all.
type Style string

// Apply wraps text in this Style's escape code and a trailing reset,
// unconditionally. Unlike every other helper in this package, which
// early-returns plain text when colorEnabled is false, Apply always
// renders its escape codes: it has no opinion on whether color output
// belongs on the stream it is writing to. Callers own that decision -
// resolve it once (see ResolveColor) and only call Apply when the
// answer is yes, the way debugStyler in cmd/graft does. An empty Style
// returns text unchanged.
func (s Style) Apply(text string) string {
	if s == "" {
		return text
	}
	return "\033[" + string(s) + "m" + text + ResetCode
}

// csiPattern matches a full ANSI CSI sequence: ESC "[", any parameter
// bytes (0x30-0x3F) and intermediate bytes (0x20-0x2F), then a single
// final byte (0x40-0x7E). This covers SGR color/style codes (final byte
// "m") as well as other CSI sequences such as cursor movement, which can
// end up embedded in error text alongside color codes.
var csiPattern = regexp.MustCompile("\033\\[[0-9:;<=>?]*[ !\"#$%&'()*+,\\-./]*[@-~]")

// StripEscapes removes ANSI CSI escape sequences (including SGR color
// and style codes) from s, leaving every other byte untouched. A
// sequence missing its final byte, such as truncated or malformed
// input, is left in place rather than guessed at. It has no dependency
// on colorEnabled: call it whenever the origin of s is unknown, such as
// an error message that may have been rendered with color already baked
// in.
func StripEscapes(s string) string {
	return csiPattern.ReplaceAllString(s, "")
}

// Sprintf formats with color codes like @R{red text} @c{cyan text}.
func Sprintf(format string, args ...interface{}) string {
	result := fmt.Sprintf(format, args...)
	return processColorCodes(result)
}

// Errorf creates a formatted error with color codes.
func Errorf(format string, args ...interface{}) error {
	msg := Sprintf(format, args...)
	return fmt.Errorf("%s", msg)
}

// processColorCodes replaces @X{text} patterns with ANSI codes.
func processColorCodes(s string) string {
	if !colorEnabled {
		// Strip color codes when disabled
		return stripColorCodes(s)
	}

	// Process color codes one at a time to handle nested braces correctly
	result := s

	// Define color code mappings
	colorPrefixes := map[string]string{
		"@R{": RedFg,     // Red
		"@r{": RedFg,     // red
		"@G{": GreenFg,   // Green
		"@g{": GreenFg,   // green
		"@Y{": YellowFg,  // Yellow
		"@y{": YellowFg,  // yellow
		"@B{": BlueFg,    // Blue
		"@b{": BlueFg,    // blue
		"@M{": MagentaFg, // Magenta
		"@m{": MagentaFg, // magenta
		"@C{": CyanFg,    // Cyan
		"@c{": CyanFg,    // cyan
		"@W{": WhiteFg,   // White
		"@w{": WhiteFg,   // white
		"@*{": BoldCode,  // Bold
		"@~{": DimCode,   // Dim
	}

	// Process each color code type
	for prefix, ansiCode := range colorPrefixes {
		result = processColorPrefix(result, prefix, ansiCode)
	}

	return result
}

// processColorPrefix handles a single color prefix, properly matching braces.
func processColorPrefix(s, prefix, ansiCode string) string {
	var result strings.Builder
	i := 0

	for i < len(s) {
		// Look for the prefix
		if i+len(prefix) <= len(s) && s[i:i+len(prefix)] == prefix {
			// Find the matching closing brace
			start := i + len(prefix)
			depth := 1
			end := start

			for end < len(s) && depth > 0 {
				switch s[end] {
				case '{':
					depth++
				case '}':
					depth--
				}
				if depth > 0 {
					end++
				}
			}

			if depth == 0 {
				// Found matching brace
				content := s[start:end]
				// Recursively process content in case there are nested color codes
				result.WriteString(ansiCode)
				result.WriteString(content)
				result.WriteString(ResetCode)
				i = end + 1
			} else {
				// No matching brace, write the prefix as-is
				result.WriteString(prefix)
				i += len(prefix)
			}
		} else {
			result.WriteByte(s[i])
			i++
		}
	}

	return result.String()
}

// stripColorCodes removes @X{...} patterns, leaving just the content.
func stripColorCodes(s string) string {
	colorPrefixes := []string{"@R{", "@r{", "@G{", "@g{", "@Y{", "@y{", "@B{", "@b{", "@M{", "@m{", "@C{", "@c{", "@W{", "@w{", "@*{", "@~{"}

	result := s
	for _, prefix := range colorPrefixes {
		result = stripColorPrefix(result, prefix)
	}

	return result
}

// stripColorPrefix removes a single color prefix pattern, properly matching braces.
func stripColorPrefix(s, prefix string) string {
	var result strings.Builder
	i := 0

	for i < len(s) {
		// Look for the prefix
		if i+len(prefix) <= len(s) && s[i:i+len(prefix)] == prefix {
			// Find the matching closing brace
			start := i + len(prefix)
			depth := 1
			end := start

			for end < len(s) && depth > 0 {
				switch s[end] {
				case '{':
					depth++
				case '}':
					depth--
				}
				if depth > 0 {
					end++
				}
			}

			if depth == 0 {
				// Found matching brace - write just the content (no color codes)
				content := s[start:end]
				result.WriteString(content)
				i = end + 1
			} else {
				// No matching brace, write the prefix as-is
				result.WriteString(prefix)
				i += len(prefix)
			}
		} else {
			result.WriteByte(s[i])
			i++
		}
	}

	return result.String()
}

//nolint:gochecknoinits // Environment-based color detection must happen at startup
func init() {
	// Auto-detect if terminal supports colors
	if !EnvAllowsColor() {
		colorEnabled = false
	}
}

// EnvAllowsColor reports whether the NO_COLOR and TERM environment
// variables permit color output: false when NO_COLOR is set to any
// non-empty value (https://no-color.org), or when TERM is "dumb"; true
// otherwise. It does not consider whether output is a terminal - see
// ResolveColor for that.
func EnvAllowsColor() bool {
	return os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
}

// ResolveColor decides whether color output should be enabled, given an
// optional explicit override and whether the relevant output stream is a
// terminal. Precedence, highest first:
//
//  1. explicit - non-nil when the caller passed an explicit flag such as
//     --color/--no-color; its value wins outright.
//  2. environment - EnvAllowsColor() (NO_COLOR/TERM) must permit color.
//  3. isTTY - the output stream must be a terminal.
//
// Both 2 and 3 must hold for color to be enabled when explicit is nil.
func ResolveColor(explicit *bool, isTTY bool) bool {
	if explicit != nil {
		return *explicit
	}
	return EnvAllowsColor() && isTTY
}
