package graft_test

import (
	"context"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
	_ "github.com/fivetwenty-io/graft/pkg/graft/operators" // registers grab, concat, sort, ==, &&, etc.
)

// mergeYAML runs a real merge through the engine, mirroring what `graft
// merge` does at the CLI, and returns the evaluated document or the error.
func mergeYAML(t *testing.T, yamlSrc string) (graft.Document, error) {
	t.Helper()
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	doc, err := engine.ParseYAML([]byte(yamlSrc))
	if err != nil {
		t.Fatalf("failed to parse YAML: %v", err)
	}
	return engine.Evaluate(context.Background(), doc)
}

// TestA6EndToEnd_NewBehavior pins the spec's "Done means" worked examples
// for cluster A6 (§3, §11 Stage A-i): bare identifiers now resolve as
// references in operand position.
func TestA6EndToEnd_NewBehavior(t *testing.T) {
	t.Run("env == production", func(t *testing.T) {
		doc, err := mergeYAML(t, "env: production\nx: (( env == \"production\" ))\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, err := doc.Get("x")
		if err != nil {
			t.Fatalf("failed to read x: %v", err)
		}
		if got != true {
			t.Fatalf("expected true, got %v (%T)", got, got)
		}
	})

	t.Run("env == production, false branch", func(t *testing.T) {
		doc, err := mergeYAML(t, "env: staging\nx: (( env == \"production\" ))\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, err := doc.Get("x")
		if err != nil {
			t.Fatalf("failed to read x: %v", err)
		}
		if got != false {
			t.Fatalf("expected false, got %v (%T)", got, got)
		}
	})

	t.Run("bare && bare", func(t *testing.T) {
		doc, err := mergeYAML(t, "a: true\nb: true\nx: (( a && b ))\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, err := doc.Get("x")
		if err != nil {
			t.Fatalf("failed to read x: %v", err)
		}
		if got != true {
			t.Fatalf("expected true, got %v (%T)", got, got)
		}
	})
}

// TestA6EndToEnd_BackwardCompat pins the §9.2 parse-shape invariants
// relevant to A6 (B-1, B-2, B-4, B-11, B-12) end to end.
func TestA6EndToEnd_BackwardCompat(t *testing.T) {
	t.Run("B-1: unregistered bare identifier alone passes through unchanged", func(t *testing.T) {
		doc, err := mergeYAML(t, "a: 5\nx: (( a ))\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, err := doc.Get("x")
		if err != nil {
			t.Fatalf("failed to read x: %v", err)
		}
		if got != "(( a ))" {
			t.Fatalf("expected literal '(( a ))' to pass through, got %v (%T)", got, got)
		}
	})

	t.Run("B-2: unregistered operator with an argument passes through intact", func(t *testing.T) {
		// The reference argument round-trips as its real source text
		// (NullOperator's renderPassthroughArg), not the "..."
		// placeholder that used to mangle the call.
		doc, err := mergeYAML(t, "foo: 5\nx: (( bogus foo ))\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, err := doc.Get("x")
		if err != nil {
			t.Fatalf("failed to read x: %v", err)
		}
		if got != "(( bogus foo ))" {
			t.Fatalf("expected '(( bogus foo ))', got %v (%T)", got, got)
		}
	})

	t.Run("B-4: a registered operator name as a bare argument stays a reference", func(t *testing.T) {
		doc, err := mergeYAML(t, "ips:\n- 10.0.0.1\n- 10.0.0.2\nx: (( grab ips ))\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, err := doc.Get("x")
		if err != nil {
			t.Fatalf("failed to read x: %v", err)
		}
		list, ok := got.([]interface{})
		if !ok || len(list) != 2 {
			t.Fatalf("expected the ips list to come through via grab, got %v (%T)", got, got)
		}
	})

	t.Run("B-11: 'a == b' with spaces lexes as two identifiers, not one reference", func(t *testing.T) {
		doc, err := mergeYAML(t, "a: 1\nb: 1\nx: (( a == b ))\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, err := doc.Get("x")
		if err != nil {
			t.Fatalf("failed to read x: %v", err)
		}
		if got != true {
			t.Fatalf("expected true, got %v (%T)", got, got)
		}
	})

	t.Run("B-12: 'a==b' (no spaces) still stops at the doubled '='", func(t *testing.T) {
		// A7's ArrayReferencePattern change (same stage) teaches a dotted
		// path segment to absorb a single "field=value" predicate, and this
		// pins its guard: a doubled '=' still stops the segment, so "a==b"
		// without surrounding spaces lexes as it always has rather than
		// becoming one reference named "a==b".
		doc, err := mergeYAML(t, "a: 1\nb: 1\nx: (( a==b ))\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, err := doc.Get("x")
		if err != nil {
			t.Fatalf("failed to read x: %v", err)
		}
		if got != true {
			t.Fatalf("expected true (a==b without spaces still tokenizes as a == b), got %v (%T)", got, got)
		}
	})
}

// TestA6EndToEnd_AmbiguityHazards pins §3.4's enumerated hazards.
func TestA6EndToEnd_AmbiguityHazards(t *testing.T) {
	t.Run("H1: a document key named after a registered operator still resolves as an operator call", func(t *testing.T) {
		// (( sort == 5 )) parses "sort" as an operator call (registered),
		// not a reference — unaffected by A6, which only changes
		// unregistered names. Post-A1 this fails with a "sort operator
		// requires..." arity/argument error rather than comparing values.
		_, err := mergeYAML(t, "sort: 5\nx: (( sort == 5 ))\n")
		if err == nil {
			t.Fatalf("expected an error: 'sort' is a registered operator name, not a reference")
		}
		if strings.Contains(err.Error(), "unsupported expression type") {
			t.Fatalf("expected a sort-operator error, not an unsupported-expression-type error: %v", err)
		}
	})

	t.Run("H1 workaround: (grab sort) == 5, enabled by A2", func(t *testing.T) {
		// H1: "sort" names a registered operator, so "(( sort == 5 ))" with
		// "sort: 5" in the document parses "sort" as an operator call, not
		// a reference — not a regression, today's behavior (§3.4). The
		// spec's documented workaround wraps it in its own parens so A2's
		// nested-opcall parsing applies: "(grab sort)" is now itself an
		// operator call, comparable against 5 via "==".
		doc, err := mergeYAML(t, "sort: 5\nx: (( (grab sort) == 5 ))\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, err := doc.Get("x")
		if err != nil {
			t.Fatalf("failed to read x: %v", err)
		}
		if got != true {
			t.Fatalf("expected true, got %v (%T)", got, got)
		}
	})

	t.Run("H2: a path segment matching an operator name is unaffected", func(t *testing.T) {
		doc, err := mergeYAML(t, "meta:\n  env: production\nx: (( meta.env == \"production\" ))\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, err := doc.Get("x")
		if err != nil {
			t.Fatalf("failed to read x: %v", err)
		}
		if got != true {
			t.Fatalf("expected true, got %v (%T)", got, got)
		}
	})

	t.Run("H4: 'a -1' (no space) is read as infix subtraction", func(t *testing.T) {
		doc, err := mergeYAML(t, "a: 5\nx: (( a -1 ))\n")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		got, err := doc.Get("x")
		if err != nil {
			t.Fatalf("failed to read x: %v", err)
		}
		if got != int64(4) {
			t.Fatalf("expected int64(4), got %v (%T)", got, got)
		}
	})
}
