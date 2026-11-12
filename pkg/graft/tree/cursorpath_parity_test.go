package tree

import (
	"errors"
	"testing"
)

func TestParseCursorKeepsItsErrorType(t *testing.T) {
	_, err := ParseCursor("a]")
	var se SyntaxError
	if !errors.As(err, &se) || se.Problem != "unexpected ']'" || se.Position != 1 {
		t.Fatalf("got %v, want a tree.SyntaxError at position 1", err)
	}
	if !errors.Is(err, ErrInvalidPath) {
		t.Error("errors.Is(err, ErrInvalidPath) is false")
	}
	if err.Error() != "syntax error: unexpected ']' at position 1" {
		t.Errorf("text changed: %q", err.Error())
	}
}

func TestNameFieldsIsTheSharedSlice(t *testing.T) {
	if len(NameFields) != 3 || NameFields[0] != "name" || NameFields[1] != "key" || NameFields[2] != "id" {
		t.Fatalf("NameFields = %q", NameFields)
	}
}
