package graft

import "testing"

// TestConfigure_FailedOperatorRegistrationLeavesConfigUnchanged is the F4
// regression guard. Configure used to apply the skip flags, cache rebuild,
// and logging changes from a delta *before* attempting to register any
// pending custom operator, so a Configure call combining a valid option
// (e.g. WithSkipVault(true)) with an invalid pending operator registration
// (a nil Operator, or an empty name) returned an error but still left the
// valid part of the change applied - contradicting Configure's own doc
// comment, which claims the engine's configuration is left unchanged when
// the result is invalid. Configure must now validate every pending
// operator registration before mutating any engine state, so a failing
// registration leaves the engine exactly as it was before the call.
func TestConfigure_FailedOperatorRegistrationLeavesConfigUnchanged(t *testing.T) {
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	de, ok := engine.(*DefaultEngine)
	if !ok {
		t.Fatalf("NewEngine returned %T, want *DefaultEngine", engine)
	}

	if de.IsVaultSkipped() {
		t.Fatal("expected vault not skipped before Configure")
	}

	err = de.Configure(WithSkipVault(true), WithCustomOperator("bad-op", nil))
	if err == nil {
		t.Fatal("Configure with a nil custom operator succeeded, want an error")
	}
	if got, want := err.Error(), "operator implementation cannot be nil"; got != want {
		t.Fatalf("Configure error = %q, want %q", got, want)
	}

	if de.IsVaultSkipped() {
		t.Fatal("Configure(WithSkipVault(true), WithCustomOperator(\"bad-op\", nil)) applied SkipVault despite the failing operator registration in the same call - Configure is not atomic")
	}
	if de.opts.SkipVault {
		t.Fatal("Configure with a failing operator registration changed opts.SkipVault - Configure is not atomic")
	}
}

// TestConfigure_OperatorValidationErrorIsDeterministic is the F12
// regression guard. Configure iterated newOpts.CustomOperators (a Go map)
// directly when registering pending operators, so when more than one
// pending registration was invalid, which error surfaced varied run to
// run with Go's randomized map iteration order. Configure must validate
// (and, on success, register) pending operators in sorted-name order.
//
// This uses two invalid pending operators whose failures produce
// different error text depending on which one is reached first: "" (an
// empty name, which fails Register's name check regardless of its
// Operator value) and "zzz-nil-op" (a non-empty name mapped to a nil
// Operator, which fails Register's implementation check). "" sorts before
// any non-empty string, so a correctly deterministic Configure always
// reports the name error, never the implementation error - run
// repeatedly to make map-order-dependent flakiness in the un-fixed
// behavior observable rather than a one-in-N-runs coincidence.
func TestConfigure_OperatorValidationErrorIsDeterministic(t *testing.T) {
	const runs = 30
	const wantErr = "operator name cannot be empty"

	for i := 0; i < runs; i++ {
		engine, err := NewEngine()
		if err != nil {
			t.Fatalf("run %d: NewEngine failed: %v", i, err)
		}
		de, ok := engine.(*DefaultEngine)
		if !ok {
			t.Fatalf("run %d: NewEngine returned %T, want *DefaultEngine", i, engine)
		}

		err = de.Configure(WithOperators(map[string]Operator{
			"":            nil,
			"zzz-nil-op":  nil,
			"aaa-nil-op2": nil,
		}))
		if err == nil {
			t.Fatalf("run %d: Configure with invalid pending operators succeeded, want an error", i)
		}
		if got := err.Error(); got != wantErr {
			t.Fatalf("run %d: Configure error = %q, want %q (deterministic sorted-name order requires the empty name to be validated first)", i, got, wantErr)
		}
	}
}
