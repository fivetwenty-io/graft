package graft_test

// A reference path like "X.$ENV.field" embeds an environment-variable
// segment in the middle of a dotted path, not only as the whole
// reference (bare "$ENV") or the leading segment. Confirmed against the
// spruce reference implementation (v1.35.16, /opt/homebrew/bin/spruce):
// spruce substitutes the real OS environment variable named ENV into the
// path before resolving it — "X.$ENV.field" with ENV=staging in the
// process environment resolves the same as "X.staging.field" written
// directly. graft's tokenizer (interfaces/tokenizer.go,
// ArrayReferencePattern.Match) only recognized a leading '$' as opening
// an env-var segment, not one appearing after a '.' mid-path: "X.$ENV.field"
// tokenized as two separate, unglued reference tokens ("X." and
// "$ENV.field") instead of one, and the parser silently dropped the
// leading "X." segment, resolving against the wrong (and usually
// nonexistent) top-level path.

import (
	"testing"

	_ "github.com/fivetwenty-io/graft/pkg/graft/operators"
)

// TestGrabMidPathEnvVar_Resolves is the exact repro from the bug report:
// an environment-variable segment embedded between two ordinary dotted
// path segments.
func TestGrabMidPathEnvVar_Resolves(t *testing.T) {
	t.Setenv("GRAFT_BUGFIX_MIDPATH_ENV", "staging")

	doc, err := mergeYAML(t, "X:\n  staging:\n    field: hi\nresult: (( grab X.$GRAFT_BUGFIX_MIDPATH_ENV.field ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("result")
	if err != nil {
		t.Fatalf("failed to read result: %v", err)
	}
	if got != "hi" {
		t.Fatalf("expected hi, got %v", got)
	}
}

// TestGrabMidPathEnvVar_MatchesConcatWorkaround pins that the mid-path
// $VAR form now produces the identical result to the documented
// concat-based workaround the bug report says already works.
func TestGrabMidPathEnvVar_MatchesConcatWorkaround(t *testing.T) {
	t.Setenv("GRAFT_BUGFIX_MIDPATH_ENV", "staging")

	direct, err := mergeYAML(t, "X:\n  staging:\n    field: hi\nresult: (( grab X.$GRAFT_BUGFIX_MIDPATH_ENV.field ))\n")
	if err != nil {
		t.Fatalf("unexpected error (direct form): %v", err)
	}
	workaround, err := mergeYAML(t, "X:\n  staging:\n    field: hi\nresult: (( grab (concat \"X.\" (grab $GRAFT_BUGFIX_MIDPATH_ENV) \".field\") ))\n")
	if err != nil {
		t.Fatalf("unexpected error (workaround form): %v", err)
	}

	directVal, err := direct.Get("result")
	if err != nil {
		t.Fatalf("failed to read direct result: %v", err)
	}
	workaroundVal, err := workaround.Get("result")
	if err != nil {
		t.Fatalf("failed to read workaround result: %v", err)
	}
	if directVal != workaroundVal {
		t.Fatalf("direct form %v does not match workaround form %v", directVal, workaroundVal)
	}
}

// TestGrabMidPathEnvVar_TrailingSegment pins the env-var segment as the
// LAST path component, not the middle, still resolves correctly (a
// narrower variant of the same mid-path — as opposed to leading-only —
// substitution).
func TestGrabMidPathEnvVar_TrailingSegment(t *testing.T) {
	t.Setenv("GRAFT_BUGFIX_MIDPATH_ENV2", "field")

	doc, err := mergeYAML(t, "X:\n  field: hi\nresult: (( grab X.$GRAFT_BUGFIX_MIDPATH_ENV2 ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("result")
	if err != nil {
		t.Fatalf("failed to read result: %v", err)
	}
	if got != "hi" {
		t.Fatalf("expected hi, got %v", got)
	}
}

// TestGrabLeadingEnvVar_StillWorks pins the already-working leading-$
// case (the whole reference starts with the env var) is unaffected.
func TestGrabLeadingEnvVar_StillWorks(t *testing.T) {
	t.Setenv("GRAFT_BUGFIX_MIDPATH_ENV3", "staging")

	doc, err := mergeYAML(t, "staging:\n  field: hi\nresult: (( grab $GRAFT_BUGFIX_MIDPATH_ENV3.field ))\n")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got, err := doc.Get("result")
	if err != nil {
		t.Fatalf("failed to read result: %v", err)
	}
	if got != "hi" {
		t.Fatalf("expected hi, got %v", got)
	}
}
