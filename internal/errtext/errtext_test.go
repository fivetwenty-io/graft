package errtext

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
)

// withColor sets the ansi toggle for one test and restores it after.
func withColor(t *testing.T, on bool) {
	t.Helper()
	was := ansi.IsColorEnabled()
	ansi.Color(on)
	t.Cleanup(func() { ansi.Color(was) })
}

func TestDetailOfAPlainErrorIsItsText(t *testing.T) {
	err := errors.New("plain")
	if got := Detail(err); got != "plain" {
		t.Fatalf("Detail = %q, want %q", got, "plain")
	}
	if got := Detail(nil); got != "" {
		t.Fatalf("Detail(nil) = %q, want empty", got)
	}
}

func TestWithPlainKeepsTheDetailAndTheChain(t *testing.T) {
	sentinel := errors.New("sentinel")
	detail := fmt.Errorf("value secret-42 failed: %w", sentinel)
	err := WithPlain(detail, "a value failed")

	if got := err.Error(); got != "a value failed" {
		t.Fatalf("Error() = %q", got)
	}
	if got := Detail(err); got != "value secret-42 failed: sentinel" {
		t.Fatalf("Detail = %q", got)
	}
	if !errors.Is(err, sentinel) {
		t.Fatal("errors.Is lost the wrapped sentinel")
	}
}

func TestDetailSubstitutesThroughFmtWrapping(t *testing.T) {
	inner := WithPlain(errors.New("bad secret-42"), "bad value")
	outer := fmt.Errorf("$.meta.k: %w", inner)
	outer2 := fmt.Errorf("context: %w", outer)

	if got := outer2.Error(); got != "context: $.meta.k: bad value" {
		t.Fatalf("Error() = %q", got)
	}
	if got := Detail(outer2); got != "context: $.meta.k: bad secret-42" {
		t.Fatalf("Detail = %q", got)
	}
}

func TestDetailSubstitutesThroughJoinedErrors(t *testing.T) {
	a := WithPlain(errors.New("a secret-1"), "a value")
	b := errors.New("b plain")
	c := WithPlain(errors.New("c secret-3"), "c value")
	joined := errors.Join(a, b, c)

	if got := joined.Error(); got != "a value\nb plain\nc value" {
		t.Fatalf("Error() = %q", got)
	}
	if got := Detail(joined); got != "a secret-1\nb plain\nc secret-3" {
		t.Fatalf("Detail = %q", got)
	}

	multi := fmt.Errorf("first %w, second %w", a, c)
	if got := Detail(multi); got != "first a secret-1, second c secret-3" {
		t.Fatalf("Detail of a two-%%w error = %q", got)
	}
}

func TestErrorfCarriesDetailThroughNonWrappingVerbs(t *testing.T) {
	inner := WithPlain(errors.New("bad secret-42"), "bad value")
	err := Errorf("outer: %s", inner)

	if got := err.Error(); got != "outer: bad value" {
		t.Fatalf("Error() = %q", got)
	}
	if got := Detail(err); got != "outer: bad secret-42" {
		t.Fatalf("Detail = %q", got)
	}
	if errors.Unwrap(err) != nil {
		t.Fatal("a percent-s verb must not add the argument to the Unwrap chain")
	}

	wrapped := Errorf("outer: %w", inner)
	if errors.Unwrap(wrapped) != inner { //nolint:errorlint // the direct cause itself
		t.Fatal("a percent-w verb must keep the argument in the Unwrap chain")
	}
	if got := Detail(wrapped); got != "outer: bad secret-42" {
		t.Fatalf("Detail of %%w = %q", got)
	}
}

func TestErrorfWithoutDetailedArgsIsFmtErrorf(t *testing.T) {
	sentinel := errors.New("sentinel")
	err := Errorf("x %d: %w", 3, sentinel)
	want := fmt.Errorf("x %d: %w", 3, sentinel)
	if err.Error() != want.Error() || errors.Unwrap(err) != sentinel { //nolint:errorlint // the direct cause itself
		t.Fatalf("Errorf = %q, want %q", err.Error(), want.Error())
	}
}

func TestColorfIsPlainWhateverTheToggle(t *testing.T) {
	withColor(t, true)
	inner := WithPlain(errors.New("bad secret-42"), "bad value")
	err := Colorf("@R{failed} @c{%s}: %s", "meta.k", inner)

	if got := err.Error(); got != "failed meta.k: bad value" {
		t.Fatalf("Error() = %q", got)
	}
	if strings.Contains(err.Error(), "\x1b[") {
		t.Fatal("Error() holds an escape code")
	}
	want := ansi.Sprintf("@R{failed} @c{%s}: %s", "meta.k", "bad secret-42")
	if got := Detail(err); got != want {
		t.Fatalf("Detail = %q, want %q", got, want)
	}
}

func TestColorfDetailFollowsTheToggleAtCreation(t *testing.T) {
	withColor(t, false)
	err := Colorf("@R{failed} @c{%s}", "meta.k")
	if got := Detail(err); got != "failed meta.k" {
		t.Fatalf("Detail with color off = %q", got)
	}
}

func TestNewAndWrap(t *testing.T) {
	sentinel := errors.New("sentinel")
	err := Wrap(sentinel, "plain", "detail")
	if err.Error() != "plain" || Detail(err) != "detail" || !errors.Is(err, sentinel) {
		t.Fatalf("Wrap: %q / %q", err.Error(), Detail(err))
	}
	n := New("plain", "detail")
	if n.Error() != "plain" || Detail(n) != "detail" || errors.Unwrap(n) != nil {
		t.Fatalf("New: %q / %q", n.Error(), Detail(n))
	}
}

func TestColorfPlainTextKeepsArgumentsVerbatim(t *testing.T) {
	withColor(t, true)
	err := Colorf("@R{bad key} @c{%s}", "a@R{b}")
	if got := err.Error(); got != "bad key a@R{b}" {
		t.Fatalf("Error() = %q, want the argument as written", got)
	}
}
