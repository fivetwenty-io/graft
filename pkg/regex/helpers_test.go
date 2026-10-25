package regex

import (
	"errors"
	"reflect"
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

func wantErrAs(t *testing.T, err error, target any) {
	t.Helper()
	if !errors.As(err, target) {
		t.Fatalf("got error %v (%T), want %T", err, err, target)
	}
}
