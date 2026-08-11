package graft_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/fivetwenty-io/graft/pkg/graft"
	_ "github.com/fivetwenty-io/graft/pkg/graft/operators" // registers grab
)

// TestA7Predicate_EndToEnd pins the spec's "Done means" worked examples for
// cluster A7 (§6, §11 Stage A-i): both the bracketed and dotted predicate
// forms resolve through a real merge, and normalize to the same result.
func TestA7Predicate_EndToEnd(t *testing.T) {
	yamlSrc := `
servers:
- name: primary
  host: 10.0.0.1
- name: secondary
  host: 10.0.0.2
`

	t.Run("bracketed form", func(t *testing.T) {
		doc, err := mergeYAML(t, yamlSrc+"r: (( grab servers[name=primary].host ))\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, err := doc.Get("r")
		if err != nil {
			t.Fatalf("failed to read r: %v", err)
		}
		if got != "10.0.0.1" {
			t.Fatalf("expected '10.0.0.1', got %v", got)
		}
	})

	t.Run("dotted form", func(t *testing.T) {
		doc, err := mergeYAML(t, yamlSrc+"r: (( grab servers.name=primary.host ))\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, err := doc.Get("r")
		if err != nil {
			t.Fatalf("failed to read r: %v", err)
		}
		if got != "10.0.0.1" {
			t.Fatalf("expected '10.0.0.1', got %v", got)
		}
	})

	t.Run("name-field auto-match still works (B-6)", func(t *testing.T) {
		doc, err := mergeYAML(t, yamlSrc+"r: (( grab servers.primary.host ))\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, err := doc.Get("r")
		if err != nil {
			t.Fatalf("failed to read r: %v", err)
		}
		if got != "10.0.0.1" {
			t.Fatalf("expected '10.0.0.1', got %v", got)
		}
	})

	t.Run("not-found predicate reports the cursor path through the failing segment (§6.5)", func(t *testing.T) {
		_, err := mergeYAML(t, yamlSrc+"r: (( grab servers.name=nope.host ))\n")
		if err == nil {
			t.Fatalf("expected a not-found error")
		}
		if !strings.Contains(err.Error(), "$.servers.name=nope") {
			t.Fatalf("expected the error to report path through the failing predicate segment, got: %v", err)
		}
		if !strings.Contains(err.Error(), "could not be found in the datastructure") {
			t.Fatalf("expected the standard not-found message, got: %v", err)
		}
	})
}

// TestA7Predicate_DoesNotHang is the CLI-level regression pin for the
// tokenizer livelock (§6.1): the previously-hanging forms now return
// promptly and successfully once A7 lands, not merely without hanging.
func TestA7Predicate_DoesNotHang(t *testing.T) {
	yamlSrc := "servers:\n- name: primary\n  host: 10.0.0.1\nr: (( grab servers.name=primary.host ))\n"

	// The engine and parse steps happen on the test's own goroutine:
	// mergeYAML reports their failures with t.Fatalf, which is only valid
	// from the goroutine running the test.
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	parsed, err := engine.ParseYAML([]byte(yamlSrc))
	if err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}

	type result struct {
		doc graft.Document
		err error
	}
	done := make(chan result, 1)
	go func() {
		evaluated, evalErr := engine.Evaluate(context.Background(), parsed)
		done <- result{evaluated, evalErr}
	}()

	var got result
	select {
	case got = <-done:
	case <-time.After(3 * time.Second):
		t.Fatalf("merge did not return within 3s; tokenizer livelock regression")
	}

	if got.err != nil {
		t.Fatalf("unexpected error: %v", got.err)
	}
	doc := got.doc
	gotValue, gerr := doc.Get("r")
	if gerr != nil {
		t.Fatalf("failed to read r: %v", gerr)
	}
	if gotValue != "10.0.0.1" {
		t.Fatalf("expected '10.0.0.1', got %v", gotValue)
	}
}

// TestA7Predicate_MapKeyRegressionGuard pins §6.4's regression vector: a map
// key that literally contains "=" must not be reinterpreted as a predicate
// — only list containers get predicate matching.
func TestA7Predicate_MapKeyRegressionGuard(t *testing.T) {
	doc, err := mergeYAML(t, "m:\n  name=primary: literal-value\nr: (( grab m.name=primary ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("r")
	if err != nil {
		t.Fatalf("failed to read r: %v", err)
	}
	if got != "literal-value" {
		t.Fatalf("expected the literal map key's value, got %v", got)
	}
}
