package regex

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func wantNil(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func wantEqual[T any](t *testing.T, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func wantErrIs(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("got error %v, want one wrapping %v", err, target)
	}
}

func wantErrAs(t *testing.T, err error, target any) {
	t.Helper()
	if !errors.As(err, target) {
		t.Fatalf("got error %v (%T), want %T", err, err, target)
	}
}

func wantErrContains(t *testing.T, err error, text string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), text) {
		t.Fatalf("got error %v, want one containing %q", err, text)
	}
}

func find(t *testing.T, p *Pattern, subject string) *Match {
	t.Helper()
	m, err := p.Find(context.Background(), subject, 0)
	wantNil(t, err)
	return m
}

func wantMatch(t *testing.T, p *Pattern, subject string) {
	t.Helper()
	if find(t, p, subject) == nil {
		t.Fatalf("%q does not match %q", p.source, subject)
	}
}

func wantNoMatch(t *testing.T, p *Pattern, subject string) {
	t.Helper()
	if m := find(t, p, subject); m != nil {
		t.Fatalf("%q matches %q at %d", p.source, subject, m.Start)
	}
}

func wantText(t *testing.T, p *Pattern, subject, want string) {
	t.Helper()
	m := find(t, p, subject)
	if m == nil {
		t.Fatalf("%q does not match %q", p.source, subject)
	}
	wantEqual(t, m.Text(subject), want)
}

func runeStarts(t *testing.T, p *Pattern, subject string, opts MatchOptions) []int {
	t.Helper()
	starts := []int{}
	for m, err := range p.FindAll(context.Background(), subject, opts) {
		wantNil(t, err)
		starts = append(starts, m.RuneStart)
	}
	return starts
}

// texts returns the text of every match FindAll yields.
func texts(t *testing.T, p *Pattern, subject string, opts MatchOptions) []string {
	t.Helper()
	out := []string{}
	for m, err := range p.FindAll(context.Background(), subject, opts) {
		wantNil(t, err)
		out = append(out, m.Text(subject))
	}
	return out
}

// matching returns the subjects p finds a match in.
func matching(t *testing.T, p *Pattern, subjects []string) []string {
	t.Helper()
	var got []string
	for _, s := range subjects {
		m, err := p.Find(context.Background(), s, 0)
		wantNil(t, err)
		if m != nil {
			got = append(got, s)
		}
	}
	return got
}
