package tree

import (
	"errors"
	"fmt"
	"testing"
)

// TestSentinelIs verifies that SyntaxError, TypeMismatchError, and
// NotFoundError all satisfy errors.Is against their respective package
// sentinels (ErrInvalidPath, ErrTypeMismatch, ErrNotFound), both as bare
// values and wrapped one level deep with fmt.Errorf's %w, and that they do
// NOT match an unrelated sentinel or a plain errors.New with the same
// text.
func TestSentinelIs(t *testing.T) {
	t.Run("SyntaxError matches ErrInvalidPath", func(t *testing.T) {
		var err error = SyntaxError{Problem: "unexpected ']'", Position: 3}
		if !errors.Is(err, ErrInvalidPath) {
			t.Error("expected errors.Is(SyntaxError, ErrInvalidPath) to be true")
		}
		if errors.Is(err, ErrNotFound) {
			t.Error("expected errors.Is(SyntaxError, ErrNotFound) to be false")
		}
		if errors.Is(err, ErrTypeMismatch) {
			t.Error("expected errors.Is(SyntaxError, ErrTypeMismatch) to be false")
		}
	})

	t.Run("TypeMismatchError matches ErrTypeMismatch", func(t *testing.T) {
		var err error = TypeMismatchError{Path: []string{"a", "b"}, Wanted: "map", Got: "string"}
		if !errors.Is(err, ErrTypeMismatch) {
			t.Error("expected errors.Is(TypeMismatchError, ErrTypeMismatch) to be true")
		}
		if errors.Is(err, ErrNotFound) {
			t.Error("expected errors.Is(TypeMismatchError, ErrNotFound) to be false")
		}
		if errors.Is(err, ErrInvalidPath) {
			t.Error("expected errors.Is(TypeMismatchError, ErrInvalidPath) to be false")
		}
	})

	t.Run("NotFoundError matches ErrNotFound", func(t *testing.T) {
		var err error = NotFoundError{Path: []string{"a", "b"}}
		if !errors.Is(err, ErrNotFound) {
			t.Error("expected errors.Is(NotFoundError, ErrNotFound) to be true")
		}
		if errors.Is(err, ErrTypeMismatch) {
			t.Error("expected errors.Is(NotFoundError, ErrTypeMismatch) to be false")
		}
		if errors.Is(err, ErrInvalidPath) {
			t.Error("expected errors.Is(NotFoundError, ErrInvalidPath) to be false")
		}
	})

	t.Run("survives one level of fmt.Errorf %w wrapping", func(t *testing.T) {
		wrapped := fmt.Errorf("resolving: %w", NotFoundError{Path: []string{"x"}})
		if !errors.Is(wrapped, ErrNotFound) {
			t.Error("expected errors.Is to see through a %w wrapper")
		}
	})

	t.Run("does not match a plain error with the same text", func(t *testing.T) {
		lookalike := errors.New("path not found")
		if errors.Is(NotFoundError{Path: []string{"x"}}, lookalike) {
			t.Error("expected NotFoundError to not match an unrelated error value with the same text")
		}
	})
}
