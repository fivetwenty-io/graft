package aws

import (
	"bytes"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/utils/mfa"
)

// TestResolveTargetMFAEnv_PrefixedSpellingAlwaysWins proves the
// target-prefixed AWS_{TARGET}_MFA_SERIAL/MFA_TOKEN spelling is used
// whenever set, for both the "default" target and an ordinary one.
func TestResolveTargetMFAEnv_PrefixedSpellingAlwaysWins(t *testing.T) {
	t.Setenv("AWS_DEFAULT_MFA_SERIAL", "prefixed-serial")
	t.Setenv("AWS_DEFAULT_MFA_TOKEN", "prefixed-token")
	t.Setenv("AWS_MFA_SERIAL", "plain-serial")
	t.Setenv("AWS_MFA_TOKEN", "plain-token")

	serial, token := resolveTargetMFAEnv("default", "AWS_DEFAULT_")
	if serial != "prefixed-serial" {
		t.Errorf("serial = %q, want %q (prefixed spelling should win)", serial, "prefixed-serial")
	}
	if token != "prefixed-token" {
		t.Errorf("token = %q, want %q (prefixed spelling should win)", token, "prefixed-token")
	}
}

// TestResolveTargetMFAEnv_DefaultTargetFallsBackToPlainSpelling proves
// D1: the "default" target falls back to the plain AWS_MFA_SERIAL/
// AWS_MFA_TOKEN spelling when the prefixed one is unset.
func TestResolveTargetMFAEnv_DefaultTargetFallsBackToPlainSpelling(t *testing.T) {
	t.Setenv("AWS_MFA_SERIAL", "plain-serial")
	t.Setenv("AWS_MFA_TOKEN", "plain-token")

	serial, token := resolveTargetMFAEnv("default", "AWS_DEFAULT_")
	if serial != "plain-serial" {
		t.Errorf("serial = %q, want %q", serial, "plain-serial")
	}
	if token != "plain-token" {
		t.Errorf("token = %q, want %q", token, "plain-token")
	}
}

// TestResolveTargetMFAEnv_DefaultTargetNameIsCaseInsensitive proves the
// "default" comparison does not depend on the caller's casing (op_aws.go
// always passes the literal lowercase "default", but the comparison
// itself should not be case-sensitive).
func TestResolveTargetMFAEnv_DefaultTargetNameIsCaseInsensitive(t *testing.T) {
	t.Setenv("AWS_MFA_SERIAL", "plain-serial")

	serial, _ := resolveTargetMFAEnv("Default", "AWS_DEFAULT_")
	if serial != "plain-serial" {
		t.Errorf("serial = %q, want %q", serial, "plain-serial")
	}
}

// TestResolveTargetMFAEnv_OtherTargetsIgnorePlainSpelling proves a
// non-default target never falls back to the plain AWS_MFA_SERIAL/
// AWS_MFA_TOKEN spelling - only its own AWS_{TARGET}_MFA_SERIAL/
// MFA_TOKEN, or nothing.
func TestResolveTargetMFAEnv_OtherTargetsIgnorePlainSpelling(t *testing.T) {
	t.Setenv("AWS_MFA_SERIAL", "plain-serial")
	t.Setenv("AWS_MFA_TOKEN", "plain-token")

	serial, token := resolveTargetMFAEnv("prod", "AWS_PROD_")
	if serial != "" {
		t.Errorf("serial = %q, want empty (non-default targets must not read the plain spelling)", serial)
	}
	if token != "" {
		t.Errorf("token = %q, want empty (non-default targets must not read the plain spelling)", token)
	}
}

// swapMFAPromptIO replaces the package-level mfaPromptIO for the
// duration of a test, restoring the original via t.Cleanup - mfaPromptIO
// is shared, unsynchronized package state, so every test that swaps it
// must run alone with respect to any other test touching it (none of the
// tests in this file run in parallel).
func swapMFAPromptIO(t *testing.T, pio mfa.PromptIO) {
	t.Helper()
	original := mfaPromptIO
	mfaPromptIO = pio
	t.Cleanup(func() { mfaPromptIO = original })
}

// TestMFATokenProvider_StaticTokenIsOneShot proves an env-sourced token
// is returned once, then errors (naming the env var) on a second call,
// mirroring mfa.TokenProvider's one-shot contract.
func TestMFATokenProvider_StaticTokenIsOneShot(t *testing.T) {
	provider := mfaTokenProvider("arn:aws:iam::123456789012:mfa/user", "654321", "AWS_TARGET_MFA_TOKEN")

	got, err := provider()
	if err != nil {
		t.Fatalf("first call: unexpected error: %v", err)
	}
	if got != "654321" {
		t.Fatalf("first call: got %q, want %q", got, "654321")
	}

	if _, err := provider(); err == nil {
		t.Fatal("second call: expected an error, got nil")
	}
}

// TestMFATokenProvider_PromptsOnStderrWhenTTY proves that with no env
// token and an injected isTTY returning true, mfaTokenProvider prompts on
// the injected writer (standing in for stderr, never stdout) and returns
// the trimmed line from the injected reader.
func TestMFATokenProvider_PromptsOnStderrWhenTTY(t *testing.T) {
	var stderr bytes.Buffer
	swapMFAPromptIO(t, mfa.PromptIO{
		In:    strings.NewReader("111222\n"),
		Out:   &stderr,
		IsTTY: func() bool { return true },
	})

	provider := mfaTokenProvider("arn:aws:iam::123456789012:mfa/user", "", "AWS_TARGET_MFA_TOKEN")
	got, err := provider()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "111222" {
		t.Fatalf("got %q, want %q", got, "111222")
	}
	if !strings.Contains(stderr.String(), "arn:aws:iam::123456789012:mfa/user") {
		t.Errorf("prompt %q does not name the serial", stderr.String())
	}
}

// TestMFATokenProvider_NoTTYNoTokenErrors proves that with no env token
// and isTTY returning false, mfaTokenProvider errors, naming the env var
// a caller needs to set.
func TestMFATokenProvider_NoTTYNoTokenErrors(t *testing.T) {
	swapMFAPromptIO(t, mfa.PromptIO{IsTTY: func() bool { return false }})

	provider := mfaTokenProvider("arn:aws:iam::123456789012:mfa/user", "", "AWS_TARGET_MFA_TOKEN")
	_, err := provider()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "AWS_TARGET_MFA_TOKEN") {
		t.Errorf("error %q does not name AWS_TARGET_MFA_TOKEN", err.Error())
	}
}
