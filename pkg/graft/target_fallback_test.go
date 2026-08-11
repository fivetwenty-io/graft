package graft_test

// Targeted operator calls ((( op@target ... ))) appearing on the right side
// of a || fallback must parse as nested targeted calls, not disintegrate
// into a bare reference plus an orphaned @target token. The orphaned form
// produced a Reference expression with a nil cursor, which crashed
// ResolveOperatorArgument with a nil-pointer dereference (SIGSEGV) on
// shapes like:
//
//	x: (( awsparam@production "/a" || awsparam@staging "/a" ))
//
// shipped in examples/aws-targets/example.yml.

import (
	"strings"
	"testing"

	_ "github.com/fivetwenty-io/graft/pkg/graft/operators"
)

func TestTargetedCallRightOfOrFallbackDoesNotPanic(t *testing.T) {
	// Both operands are targeted calls with no credentials configured: the
	// merge must fail with an ordinary error, never a panic.
	_, err := mergeYAML(t, `x: (( awsparam@production "/a" || awsparam@staging "/a" ))`)
	if err == nil {
		t.Fatalf("expected an error (no aws targets configured), got success")
	}
	if strings.Contains(err.Error(), "invalid memory address") {
		t.Fatalf("nil-pointer dereference surfaced as error: %s", err)
	}
}

func TestTargetedCallRightOfOrIsUnreachedWhenLeftSucceeds(t *testing.T) {
	// When the left side resolves, the targeted right side must never run
	// and must not have corrupted the argument list during parsing.
	doc, err := mergeYAML(t, "v: hello\nx: (( grab v || awsparam@staging \"/a\" ))\n")
	if err != nil {
		t.Fatalf("expected success, got: %v", err)
	}
	got, err := doc.Get("x")
	if err != nil {
		t.Fatalf("failed to read x: %v", err)
	}
	if got != "hello" {
		t.Fatalf("expected x=hello, got %v", got)
	}
}

func TestVaultTargetedFallbackChainDoesNotPanic(t *testing.T) {
	_, err := mergeYAML(t, `x: (( vault@a "secret/x:y" || vault@b "secret/x:y" ))`)
	if err == nil {
		t.Fatalf("expected an error (no vault targets configured), got success")
	}
}

func TestBareAtArgumentErrorsCleanly(t *testing.T) {
	// A bare @name in argument position is not a value; it must produce a
	// parse or evaluation error, not a nil-cursor Reference that crashes
	// argument resolution.
	_, err := mergeYAML(t, `x: (( concat @nowhere "a" ))`)
	if err == nil {
		t.Fatalf("expected an error for bare @nowhere argument, got success")
	}
}
