// Package log provides debug and trace logging utilities for graft.
package log

import (
	"fmt"
	"os"
	"strings"
)

// DebugOn enables debug level logging when set to true.
var DebugOn = false

// TraceOn enables trace level logging when set to true.
var TraceOn = false

// PrintStdErrf is a configurable hook to print to error output.
var PrintStdErrf func(string, ...interface{})

//nolint:gochecknoinits // Default stderr handler must be set before any logging calls
func init() {
	PrintStdErrf = func(format string, args ...interface{}) {
		fmt.Fprintf(os.Stderr, format, args...)
	}
}

// DEBUG - Prints out a debug message.
//
//nolint:goprintffuncname // DEBUG is an established name used throughout the codebase
func DEBUG(format string, args ...interface{}) {
	if DebugOn {
		content := fmt.Sprintf(format, args...)
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			lines[i] = "DEBUG> " + line
		}
		content = strings.Join(lines, "\n")
		PrintStdErrf("%s\n", content)
	}
}

// TRACE - Prints out a trace message.
//
//nolint:goprintffuncname // TRACE is an established name used throughout the codebase
func TRACE(format string, args ...interface{}) {
	if TraceOn {
		content := fmt.Sprintf(format, args...)
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			lines[i] = "-----> " + line
		}
		content = strings.Join(lines, "\n")
		PrintStdErrf("%s\n", content)
	}
}
