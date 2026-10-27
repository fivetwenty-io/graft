package main

import (
	"fmt"
	"strings"

	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/printer"
)

// The SGR codes yq colors its output with.
const (
	yqCyan      = 36
	yqGreen     = 32
	yqHiBlack   = 90
	yqHiYellow  = 93
	yqHiMagenta = 95
	yqReset     = 0
)

func yqSGR(code int) string { return fmt.Sprintf("\x1b[%dm", code) }

func yqProperty(code int) printer.PrintFunc {
	return func() *printer.Property {
		return &printer.Property{Prefix: yqSGR(code), Suffix: yqSGR(yqReset)}
	}
}

// yqColorize colors the text of one YAML or JSON document as yq does, by
// printing the tokens of the text with a color for each kind, and then
// ending the output with a line break. The tokens carry the white space
// and line breaks that lead them, so a color can start before a line break
// and leave an empty colored span on the line it ends.
func yqColorize(text string) string {
	p := printer.Printer{
		Bool:    yqProperty(yqHiMagenta),
		Number:  yqProperty(yqHiMagenta),
		MapKey:  yqProperty(yqCyan),
		Anchor:  yqProperty(yqHiYellow),
		Alias:   yqProperty(yqHiYellow),
		String:  yqProperty(yqGreen),
		Comment: yqProperty(yqHiBlack),
	}
	return p.PrintTokens(lexer.Tokenize(text)) + "\n"
}

// yqColorLeading colors a document's leading comments as yq does, one
// whole line at a time. A blank line and a "---" line stay plain.
func yqColorLeading(leading string) string {
	var b strings.Builder
	for _, line := range strings.SplitAfter(leading, "\n") {
		if strings.TrimSpace(line) == "" || strings.TrimSpace(line) == "---" {
			b.WriteString(line)
			continue
		}
		b.WriteString(yqSGR(yqHiBlack) + line + yqSGR(yqReset))
	}
	return b.String()
}
