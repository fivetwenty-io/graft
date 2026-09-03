package mfa_test

import (
	"bytes"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/utils/mfa"
)

// TestTokenProvider_EnvTokenIsOneShot proves a non-empty envToken is
// returned once, and a second call to the same provider errors instead
// of replaying the stale code.
func TestTokenProvider_EnvTokenIsOneShot(t *testing.T) {
	provider := mfa.TokenProvider("arn:aws:iam::123456789012:mfa/user", "123456", "AWS_MFA_TOKEN", mfa.PromptIO{})

	got, err := provider()
	if err != nil {
		t.Fatalf("first call: unexpected error: %v", err)
	}
	if got != "123456" {
		t.Fatalf("first call: got %q, want %q", got, "123456")
	}

	_, err = provider()
	if err == nil {
		t.Fatal("second call: expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "AWS_MFA_TOKEN") {
		t.Errorf("second call: error %q does not name AWS_MFA_TOKEN", err.Error())
	}
}

// TestTokenProvider_PromptsOnTTY proves the interactive branch writes to
// the injected writer (never a real terminal in this test) and returns
// the trimmed line read from the injected reader.
func TestTokenProvider_PromptsOnTTY(t *testing.T) {
	var out bytes.Buffer
	in := strings.NewReader("654321\n")

	provider := mfa.TokenProvider("arn:aws:iam::123456789012:mfa/user", "", "AWS_MFA_TOKEN", mfa.PromptIO{
		In:    in,
		Out:   &out,
		IsTTY: func() bool { return true },
	})

	got, err := provider()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != "654321" {
		t.Fatalf("got %q, want %q", got, "654321")
	}
	if !strings.Contains(out.String(), "arn:aws:iam::123456789012:mfa/user") {
		t.Errorf("prompt %q does not name the serial", out.String())
	}
}

// TestTokenProvider_NoTTYNoTokenErrors proves the third branch - no
// envToken, and IsTTY reports false (or is nil) - returns an error naming
// envVarName, without ever touching In/Out.
func TestTokenProvider_NoTTYNoTokenErrors(t *testing.T) {
	provider := mfa.TokenProvider("arn:aws:iam::123456789012:mfa/user", "", "AWS_PROD_MFA_TOKEN", mfa.PromptIO{
		IsTTY: func() bool { return false },
	})

	_, err := provider()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !strings.Contains(err.Error(), "AWS_PROD_MFA_TOKEN") {
		t.Errorf("error %q does not name AWS_PROD_MFA_TOKEN", err.Error())
	}
}

// TestTokenProvider_NilIsTTYTreatedAsNoTTY proves a nil IsTTY func (the
// PromptIO zero value) is treated as "not a terminal" rather than
// panicking.
func TestTokenProvider_NilIsTTYTreatedAsNoTTY(t *testing.T) {
	provider := mfa.TokenProvider("serial", "", "AWS_MFA_TOKEN", mfa.PromptIO{})

	_, err := provider()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
}

// TestTokenProvider_EmptyLineErrors proves a blank line from the reader
// (an operator hitting Enter with no code) is reported as an error, not
// silently accepted as an empty token code.
func TestTokenProvider_EmptyLineErrors(t *testing.T) {
	var out bytes.Buffer
	provider := mfa.TokenProvider("serial", "", "AWS_MFA_TOKEN", mfa.PromptIO{
		In:    strings.NewReader("\n"),
		Out:   &out,
		IsTTY: func() bool { return true },
	})

	_, err := provider()
	if err == nil {
		t.Fatal("expected an error for an empty entered token, got nil")
	}
}

// failingReader always returns an error, standing in for a closed or
// broken stdin.
type failingReader struct{ err error }

func (r failingReader) Read(_ []byte) (int, error) { return 0, r.err }

// TestTokenProvider_ReaderErrorPropagates proves an error reading the
// interactive prompt's answer surfaces to the caller instead of being
// swallowed.
func TestTokenProvider_ReaderErrorPropagates(t *testing.T) {
	wantErr := errors.New("boom")
	var out bytes.Buffer
	provider := mfa.TokenProvider("serial", "", "AWS_MFA_TOKEN", mfa.PromptIO{
		In:    failingReader{err: wantErr},
		Out:   &out,
		IsTTY: func() bool { return true },
	})

	_, err := provider()
	if err == nil {
		t.Fatal("expected an error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("error %v does not wrap the reader's error %v", err, wantErr)
	}
}

// TestDefaultPromptIO_UsesStderrNotStdout proves DefaultPromptIO's Out is
// os.Stderr, not os.Stdout - `graft merge` writes its merged document to
// stdout, and an MFA prompt sharing that stream would corrupt it.
func TestDefaultPromptIO_UsesStderrNotStdout(t *testing.T) {
	pio := mfa.DefaultPromptIO()
	if pio.Out != os.Stderr {
		t.Errorf("expected Out to be os.Stderr, got %v", pio.Out)
	}
	if pio.In != os.Stdin {
		t.Errorf("expected In to be os.Stdin, got %v", pio.In)
	}
	if pio.IsTTY == nil {
		t.Error("expected a non-nil IsTTY func")
	}
}
