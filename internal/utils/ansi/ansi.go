// Package ansi provides ANSI escape code utilities for terminal output.
package ansi

import (
	"fmt"
	"os"
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
	ResetCode = "\033[0m"
	BoldCode  = "\033[1m"
	DimCode   = "\033[2m"
	RedFg     = "\033[31m"
	GreenFg   = "\033[32m"
	YellowFg  = "\033[33m"
	BlueFg    = "\033[34m"
	MagentaFg = "\033[35m"
	CyanFg    = "\033[36m"
	WhiteFg   = "\033[37m"
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
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		colorEnabled = false
	}
}
