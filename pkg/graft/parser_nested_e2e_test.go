package graft_test

import (
	"testing"

	_ "github.com/fivetwenty-io/graft/pkg/graft/operators" // registers grab, concat, sort, +, ==, &&, etc.
)

// TestNestedOperatorEndToEnd_Merge pins the doc-level worked examples from
// §2 and §11 Stage A-ii through a real merge, matching the a6 test file's
// end-to-end style. mergeYAML is defined in a6_backward_compat_test.go
// (same package).
func TestNestedOperatorEndToEnd_Merge(t *testing.T) {
	t.Run("concat with nested grab", func(t *testing.T) {
		doc, err := mergeYAML(t, "host: h1\nx: (( concat \"a-\" (grab host) ))\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, err := doc.Get("x")
		if err != nil {
			t.Fatalf("failed to read x: %v", err)
		}
		if got != "a-h1" {
			t.Fatalf("expected a-h1, got %v", got)
		}
	})

	t.Run("sum of two nested grabs", func(t *testing.T) {
		doc, err := mergeYAML(t, "a: 1\nb: 2\nx: (( (grab a) + (grab b) ))\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, err := doc.Get("x")
		if err != nil {
			t.Fatalf("failed to read x: %v", err)
		}
		if got != int64(3) {
			t.Fatalf("expected 3, got %v (%T)", got, got)
		}
	})

	t.Run("grab a && grab b unaffected by A2", func(t *testing.T) {
		doc, err := mergeYAML(t, "a: true\nb: true\nx: (( grab a && grab b ))\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, err := doc.Get("x")
		if err != nil {
			t.Fatalf("failed to read x: %v", err)
		}
		if got != true {
			t.Fatalf("expected true, got %v", got)
		}
	})

	t.Run("grouping parens around infix arithmetic, unchanged", func(t *testing.T) {
		doc, err := mergeYAML(t, "x: (( (1 + 2) * 3 ))\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, err := doc.Get("x")
		if err != nil {
			t.Fatalf("failed to read x: %v", err)
		}
		if got != int64(9) {
			t.Fatalf("expected 9, got %v", got)
		}
	})

	t.Run("quoted ternary preserved verbatim (B-9)", func(t *testing.T) {
		doc, err := mergeYAML(t, "large: true\nx: '(( grab large ? \"8Gi\" : \"2Gi\" ))'\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, err := doc.Get("x")
		if err != nil {
			t.Fatalf("failed to read x: %v", err)
		}
		if got != "8Gi" {
			t.Fatalf("expected 8Gi, got %v", got)
		}
	})

	t.Run("list of literal arguments to concat preserved verbatim", func(t *testing.T) {
		doc, err := mergeYAML(t, "x: (( concat \"a\" \"-\" \"b\" ))\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, err := doc.Get("x")
		if err != nil {
			t.Fatalf("failed to read x: %v", err)
		}
		if got != "a-b" {
			t.Fatalf("expected a-b, got %v", got)
		}
	})

	t.Run("|| chain inside operator args preserved verbatim (B-5)", func(t *testing.T) {
		doc, err := mergeYAML(t, "x: (( grab missing || \"dflt\" ))\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, err := doc.Get("x")
		if err != nil {
			t.Fatalf("failed to read x: %v", err)
		}
		if got != "dflt" {
			t.Fatalf("expected dflt, got %v", got)
		}
	})

	t.Run("nested calls to arbitrary depth", func(t *testing.T) {
		// The middle "( ... )" is an extra grouping level wrapped around an
		// addition of two nested opcalls, itself added to a third nested
		// opcall — three levels of nesting depth, with natural spacing so
		// no two "(" or ")" characters are adjacent (adjacent chars merge
		// into a single "((" / "))" token — interfaces/tokenizer.go:473-479
		// — which is a pre-existing, unrelated tokenizer property, not
		// something this test is exercising).
		doc, err := mergeYAML(t, "a: 1\nb: 2\nc: 3\nx: (( ( (grab a) + (grab b) ) + (grab c) ))\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, err := doc.Get("x")
		if err != nil {
			t.Fatalf("failed to read x: %v", err)
		}
		if got != int64(6) {
			t.Fatalf("expected 6, got %v (%T)", got, got)
		}
	})

	t.Run("nested opcall as the first argument", func(t *testing.T) {
		// The documented nested-call examples overwhelmingly put the nested
		// call first — "(( base64 (file ... ) ))", "(( file (concat ... ) ))",
		// "(( concat (grab cpu_cores) ))" — where the "(" sits immediately
		// after the operator name and would otherwise be read as function-call
		// syntax, "op(arg1, arg2)".
		doc, err := mergeYAML(t, "cores: 4\nx: (( concat (grab cores) \"-cpu\" ))\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, err := doc.Get("x")
		if err != nil {
			t.Fatalf("failed to read x: %v", err)
		}
		if got != "4-cpu" {
			t.Fatalf("expected 4-cpu, got %v", got)
		}
	})

	t.Run("nested opcall inside a nested opcall argument", func(t *testing.T) {
		doc, err := mergeYAML(t, "env: prod\nenvironments:\n  prod:\n    settings: ok\nx: (( grab (concat \"environments.\" (grab env) \".settings\") ))\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, err := doc.Get("x")
		if err != nil {
			t.Fatalf("failed to read x: %v", err)
		}
		if got != "ok" {
			t.Fatalf("expected ok, got %v", got)
		}
	})

	t.Run("function-call syntax still parses as function-call syntax", func(t *testing.T) {
		// "op(a, b)" must keep working: the first token inside the parens
		// names no registered operator, so the group is not an opcall.
		doc, err := mergeYAML(t, "a: x\nb: y\nx: (( concat(a, b) ))\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, err := doc.Get("x")
		if err != nil {
			t.Fatalf("failed to read x: %v", err)
		}
		if got != "xy" {
			t.Fatalf("expected xy, got %v", got)
		}
	})

	t.Run("nested opcall usable as an infix operand", func(t *testing.T) {
		doc, err := mergeYAML(t, "a: 1\nx: (( (grab a) == 1 ))\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, err := doc.Get("x")
		if err != nil {
			t.Fatalf("failed to read x: %v", err)
		}
		if got != true {
			t.Fatalf("expected true, got %v", got)
		}
	})
}
