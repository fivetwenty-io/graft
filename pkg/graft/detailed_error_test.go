package graft

import (
	"errors"
	"fmt"
	"math"
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

func TestCheckForCyclesErrorHasNoColor(t *testing.T) {
	withAnsiColor(t, true)
	m := map[string]interface{}{}
	m["self"] = m
	err := CheckForCycles(m, 64)
	if err == nil || strings.Contains(err.Error(), "\x1b[") {
		t.Fatalf("CheckForCycles = %q", err)
	}
	if !strings.Contains(DetailedError(err), "\x1b[") {
		t.Fatalf("DetailedError lost its color: %q", DetailedError(err))
	}
}

func TestDocumentGetterErrorsOmitTheValue(t *testing.T) {
	doc := NewDocument(map[string]interface{}{"n": 3.25, "m": map[string]interface{}{"k": 4077}})
	_, err := doc.GetInt("n")
	if err == nil || strings.Contains(err.Error(), "3.25") {
		t.Fatalf("GetInt error = %v", err)
	}
	if !errors.Is(err, ErrTypeMismatch) || !strings.Contains(DetailedError(err), "3.250000") {
		t.Fatalf("GetInt detail = %q", DetailedError(err))
	}
	var ge *GraftError
	if !errors.As(err, &ge) {
		t.Fatal("GetInt no longer returns a GraftError")
	}

	_, err = doc.GetMapStringString("m")
	if err == nil || strings.Contains(err.Error(), "4077") {
		t.Fatalf("GetMapStringString error = %v", err)
	}
	if !errors.Is(err, ErrTypeMismatch) || !strings.Contains(DetailedError(err), "4077") {
		t.Fatalf("GetMapStringString detail = %q", DetailedError(err))
	}
}

func TestValueConversionErrorsOmitTheValue(t *testing.T) {
	cases := []struct {
		name  string
		probe string
		run   func() error
	}{
		{"uint64 overflows int", "18446744073709551615", func() error { _, err := ToInt(uint64(math.MaxUint64)); return err }},
		{"string to int", "probe-7", func() error { _, err := ToInt("probe-7"); return err }},
		{"string to int64", "probe-7", func() error { _, err := ToInt64("probe-7"); return err }},
		{"string to bool", "probe-7", func() error { _, err := ToBool("probe-7"); return err }},
		{"uint64 overflows int64", "18446744073709551615", func() error { _, err := ToInt64(uint64(math.MaxUint64)); return err }},
		{"float is not an integer", "2.5", func() error { _, err := NewValue(2.5).AsInt(); return err }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			if err == nil {
				t.Fatal("the conversion must fail")
			}
			if strings.Contains(err.Error(), tc.probe) {
				t.Fatalf("Error() quotes the value: %q", err.Error())
			}
			if !strings.Contains(DetailedError(err), tc.probe) {
				t.Fatalf("DetailedError dropped the value: %q", DetailedError(err))
			}
		})
	}
}
