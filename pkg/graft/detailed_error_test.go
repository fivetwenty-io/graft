package graft

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/errtext"
	"github.com/fivetwenty-io/graft/internal/utils/ansi"
)

// withAnsiColor sets the package-wide ansi toggle for one test and
// restores it afterwards.
func withAnsiColor(t *testing.T, on bool) {
	t.Helper()
	was := ansi.IsColorEnabled()
	ansi.Color(on)
	t.Cleanup(func() { ansi.Color(was) })
}

func TestDetailedErrorOfNil(t *testing.T) {
	if got := DetailedError(nil); got != "" {
		t.Fatalf("DetailedError(nil) = %q, want empty", got)
	}
}

func TestDetailedErrorWalksTheAggregateTypes(t *testing.T) {
	withAnsiColor(t, true)
	leaf := errtext.WithPlain(errors.New("bad probe-7"), "bad value")
	multi := MultiError{Errors: []error{
		&PathError{Path: "meta.b", Cause: leaf},
		&PathError{Path: "meta.a", Cause: WithCode(errors.New("plain"), CodeParamRequired)},
	}}
	err := fmt.Errorf("failed to evaluate document: %w", &PartialEvaluationError{Err: multi})

	plain := err.Error()
	if strings.Contains(plain, "probe-7") || strings.Contains(plain, "\x1b[") {
		t.Fatalf("Error() leaks a value or color: %q", plain)
	}
	want := "failed to evaluate document: " + ansi.Sprintf("@r{%d}", 2) +
		" error(s) detected:\n - $.meta.a: plain\n - $.meta.b: bad probe-7\n\n"
	if got := DetailedError(err); got != want {
		t.Fatalf("DetailedError =\n%q\nwant\n%q", got, want)
	}
}

func TestMultiErrorDetailSortsByDetailedText(t *testing.T) {
	// The CLI has always sorted the lines it prints, and the detailed
	// text keeps that order even where the plain lines sort otherwise.
	withAnsiColor(t, false)
	multi := MultiError{Errors: []error{
		errtext.WithPlain(errors.New("b detail"), "a plain"),
		errtext.WithPlain(errors.New("a detail"), "b plain"),
	}}
	if got, want := multi.Error(), "2 error(s) detected:\n - a plain\n - b plain\n\n"; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if got, want := DetailedError(multi), "2 error(s) detected:\n - a detail\n - b detail\n\n"; got != want {
		t.Fatalf("DetailedError = %q, want %q", got, want)
	}
}

func TestBackendErrorTextOmitsThePath(t *testing.T) {
	cause := errtext.WithPlain(fmt.Errorf("vault: reading %q: boom", "secret/probe-7"), "vault: reading the secret failed")
	be := &BackendError{Backend: "vault", Target: "prod", Path: "secret/probe-7:k", Message: cause.Error(), Cause: cause}

	if got, want := be.Error(), `backend "vault"@prod: vault: reading the secret failed`; got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	want := `backend "vault"@prod path "secret/probe-7:k": vault: reading "secret/probe-7": boom`
	if got := DetailedError(be); got != want {
		t.Fatalf("DetailedError = %q, want %q", got, want)
	}

	own := &BackendError{Backend: "aws", Path: "probe-7", Message: "throttled"}
	if got := DetailedError(own); got != `backend "aws" path "probe-7": throttled` {
		t.Fatalf("DetailedError without a cause = %q", got)
	}
}

func TestGraftErrorDetailWalksTheCauseChain(t *testing.T) {
	cause := errtext.WithPlain(fmt.Errorf("vault: reading %q: boom", "secret/probe-7"), "vault: reading the secret failed")
	be := &BackendError{Backend: "vault", Path: "secret/probe-7:k", Message: cause.Error(), Cause: cause}
	err := NewExternalError("vault", cause.Error(), be)

	if strings.Contains(err.Error(), "probe-7") {
		t.Fatalf("Error() quotes the path: %q", err.Error())
	}
	want := `external_error: vault: vault: reading "secret/probe-7": boom`
	if got := DetailedError(err); got != want {
		t.Fatalf("DetailedError = %q, want %q", got, want)
	}
}

func TestCycleErrorTextOmitsTheExpressions(t *testing.T) {
	err := &CycleError{
		Inputs: []string{"a.yml"},
		Nodes: []CycleNode{
			{Path: "a", Expr: `(( concat "probe-7" b ))`},
			{Path: "b", Expr: "(( grab a ))"},
		},
	}
	plain := err.Error()
	if strings.Contains(plain, "probe-7") || strings.Contains(plain, "concat") {
		t.Fatalf("Error() quotes an expression:\n%s", plain)
	}
	if !strings.Contains(plain, "<unknown>  a\n") || !strings.HasSuffix(plain, "<unknown>  b") {
		t.Fatalf("Error() lost the node lines:\n%s", plain)
	}
	detail := DetailedError(err)
	if !strings.Contains(detail, `<unknown>  a: (( concat "probe-7" b ))`) {
		t.Fatalf("DetailedError dropped the expression:\n%s", detail)
	}
	if !errors.Is(err, ErrDependencyCycle) {
		t.Fatal("the cycle sentinel is gone")
	}
}
