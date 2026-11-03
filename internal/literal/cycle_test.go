package literal_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/fivetwenty-io/graft/internal/literal"
)

// panicText runs f and returns the text of the panic it raises, or ""
// when it returns. It fails the test when f runs longer than a second,
// since a walk that follows a cycle never ends.
func panicText(t *testing.T, f func()) string {
	t.Helper()
	done := make(chan string, 1)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Sprint(r)
				return
			}
			done <- ""
		}()
		f()
	}()
	select {
	case text := <-done:
		return text
	case <-time.After(time.Second):
		t.Fatal("the walk did not finish within a second")
		return ""
	}
}

// cyclic returns values that hold themselves, each with two references
// back so that a walk without cycle detection branches at every level.
func cyclic() map[string]any {
	m := map[string]any{"secret": "cyc-probe"}
	m["a"], m["b"] = m, m

	mm := map[any]any{1: "cyc-probe"}
	mm[2], mm[3] = mm, mm

	s := make([]any, 3)
	s[0], s[1], s[2] = s, s, "cyc-probe"

	deep := map[string]any{}
	deep["x"] = []any{map[string]any{"y": deep, "z": deep}}

	return map[string]any{"map": m, "any-map": mm, "list": s, "deep": deep}
}

func TestNewPanicsOnACycle(t *testing.T) {
	for name, v := range cyclic() {
		t.Run(name, func(t *testing.T) {
			text := panicText(t, func() { literal.New(v) })
			if text == "" {
				t.Fatal("New returned on a value that holds itself")
			}
			if !strings.Contains(text, "contains itself") || strings.Contains(text, "cyc-probe") {
				t.Fatalf("panic text is not plain: %q", text)
			}
			wrapped := panicText(t, func() { literal.New([]any{literal.Wrap(v)}) })
			if wrapped != text {
				t.Fatalf("a cycle inside a wrapper gave %q", wrapped)
			}
		})
	}
}

func TestCopyPanicsOnACycle(t *testing.T) {
	for name, v := range cyclic() {
		t.Run(name, func(t *testing.T) {
			text := panicText(t, func() { literal.Copy(literal.Wrap(v)) })
			if !strings.Contains(text, "contains itself") || strings.Contains(text, "cyc-probe") {
				t.Fatalf("Copy did not panic with a plain message: %q", text)
			}
		})
	}
}

// TestNewAcceptsSharedValues checks that a container used in more than
// one place, which is not a cycle, is copied without a panic.
func TestNewAcceptsSharedValues(t *testing.T) {
	shared := map[string]any{"k": "v"}
	list := []any{"a", nil}
	list[1] = list[:1] // the same backing array, but a shorter list
	v := map[string]any{"a": shared, "b": shared, "c": []any{shared, shared}, "list": list}
	if text := panicText(t, func() { literal.New(v) }); text != "" {
		t.Fatalf("New panicked on shared, acyclic input: %s", text)
	}
	if text := panicText(t, func() { literal.Copy(literal.Wrap(v)) }); text != "" {
		t.Fatalf("Copy panicked on shared, acyclic input: %s", text)
	}
}
