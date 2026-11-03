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
