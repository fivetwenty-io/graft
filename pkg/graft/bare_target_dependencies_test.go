package graft_test

// A bare @name in argument position (an orphaned target with no operator
// call attached) parses as a Reference expression with a nil cursor.
// operators.ResolveOperatorArgument guards this (see target_fallback_test.go),
// but several operators bypass that guard and dereference arg.Reference
// directly in their own Dependencies or Run methods, so the same input
// still crashes the process instead of surfacing as an ordinary error.

import (
	"strings"
	"testing"

	_ "github.com/fivetwenty-io/graft/pkg/graft/operators"
)

// TestBareAtArgument_InjectDependenciesDoesNotPanic covers
// InjectOperator.Dependencies, which calls arg.Reference.Canonical without
// checking for nil when @A is not the operator's own target (i.e. it
// follows another argument).
func TestBareAtArgument_InjectDependenciesDoesNotPanic(t *testing.T) {
	_, err := mergeYAML(t, "a:\n  x: 1\nb: (( inject a @A ))\n")
	if err == nil {
		t.Fatalf("expected an error for bare @A argument, got success")
	}
	if strings.Contains(err.Error(), "invalid memory address") {
		t.Fatalf("nil-pointer dereference surfaced as error: %s", err)
	}
}

// TestBareAtArgument_InjectRunNestedDoesNotPanic covers InjectOperator.Run,
// which special-cases Reference-typed arguments before ResolveOperatorArgument
// would have caught the nil cursor. Nesting inject inside join reaches Run
// through evaluateNestedOperator's opcall.Run path, which never calls
// Dependencies, so this exercises a distinct code path from the test above.
func TestBareAtArgument_InjectRunNestedDoesNotPanic(t *testing.T) {
	_, err := mergeYAML(t, "a:\n  x: 1\nb: (( join \",\" (inject a @A) ))\n")
	if err == nil {
		t.Fatalf("expected an error for bare @A argument, got success")
	}
	if strings.Contains(err.Error(), "invalid memory address") {
		t.Fatalf("nil-pointer dereference surfaced as error: %s", err)
	}
}

// TestBareAtArgument_IpsDependenciesDoesNotPanic covers IpsOperator.Dependencies,
// which calls other.Under(arg.Reference) without checking for nil.
func TestBareAtArgument_IpsDependenciesDoesNotPanic(t *testing.T) {
	_, err := mergeYAML(t, "net:\n  x: 10.0.0.0/24\nb: (( ips net.x 2 @A ))\n")
	if err == nil {
		t.Fatalf("expected an error for bare @A argument, got success")
	}
	if strings.Contains(err.Error(), "invalid memory address") {
		t.Fatalf("nil-pointer dereference surfaced as error: %s", err)
	}
}

// TestBareAtArgument_CartesianProductDependenciesDoesNotPanic covers
// CartesianProductOperator.Dependencies, which has the same unguarded
// other.Under(arg.Reference) call as IpsOperator.Dependencies.
func TestBareAtArgument_CartesianProductDependenciesDoesNotPanic(t *testing.T) {
	_, err := mergeYAML(t, "l1: [1, 2]\nb: (( cartesian-product l1 @A ))\n")
	if err == nil {
		t.Fatalf("expected an error for bare @A argument, got success")
	}
	if strings.Contains(err.Error(), "invalid memory address") {
		t.Fatalf("nil-pointer dereference surfaced as error: %s", err)
	}
}
