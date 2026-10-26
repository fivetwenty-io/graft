package query

import (
	"errors"
	"fmt"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/query/value"
)

func TestCompileErrorNamesItsPosition(t *testing.T) {
	err := &CompileError{Msg: "syntax error, unexpected end of file", Line: 2, Col: 4}
	wantEqual(t, err.Error(), "syntax error, unexpected end of file at line 2, column 4")
}

func TestRuntimeErrorIsItsMessage(t *testing.T) {
	err := &RuntimeError{Msg: "null (null) has no keys", Value: value.Null()}
	wantEqual(t, err.Error(), "null (null) has no keys")
}

func TestHaltErrorPrefersAStringMessage(t *testing.T) {
	wantEqual(t, (&HaltError{Code: 1, Message: value.Str("bye\n")}).Error(), "bye\n")
	wantEqual(t, (&HaltError{Code: 7, Message: value.Num(3)}).Error(), "halted with exit code 7")
	wantEqual(t, (&HaltError{Code: 0}).Error(), "halted with exit code 0")
}

func TestErrorsSurviveWrapping(t *testing.T) {
	var re *RuntimeError
	wrapped := fmt.Errorf("evaluating: %w", &RuntimeError{Msg: "x"})
	if !errors.As(wrapped, &re) || re.Msg != "x" {
		t.Errorf("errors.As lost the runtime error from %v", wrapped)
	}
}

func TestStatusHoldsAnExitCodeAndMessage(t *testing.T) {
	s := Status{Code: 5, Message: "Runtime error: boom"}
	wantEqual(t, s, Status{Code: 5, Message: "Runtime error: boom"})
	wantEqual(t, Status{}.Code, 0)
}
