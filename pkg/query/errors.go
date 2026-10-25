package query

import (
	"fmt"

	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// CompileError is a program that failed to parse or compile. Line and Col
// are 1-based and point at where the problem starts.
type CompileError struct {
	Msg       string
	Line, Col int
}

// Error returns the message with the position it points at.
func (e *CompileError) Error() string {
	return fmt.Sprintf("%s at line %d, column %d", e.Msg, e.Line, e.Col)
}

// RuntimeError is an error raised while a program ran. Msg is the text jq
// prints for it, and Value is the value the program raised, which is nil
// for an error the engine raised itself.
type RuntimeError struct {
	Msg   string
	Value *value.Value
}

// Error returns Msg.
func (e *RuntimeError) Error() string { return e.Msg }

// HaltError ends a run on purpose with an exit code. Message is the value
// to print, which the caller prints raw when it is a string and as JSON
// otherwise.
type HaltError struct {
	Code    int
	Message *value.Value
}

// Error returns the message when it is a string, and otherwise names the
// exit code.
func (e *HaltError) Error() string {
	if e.Message != nil && e.Message.Kind() == value.KindString {
		return e.Message.Str()
	}
	return fmt.Sprintf("halted with exit code %d", e.Code)
}
