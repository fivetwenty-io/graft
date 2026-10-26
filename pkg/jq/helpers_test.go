package jq

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/query/value"
)

func wantEqual[T any](t *testing.T, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func wantErrContains(t *testing.T, err error, sub string) {
	t.Helper()
	if err == nil {
		t.Fatalf("got no error, want one containing %q", sub)
	}
	if !strings.Contains(err.Error(), sub) {
		t.Errorf("error %q does not contain %q", err, sub)
	}
}

// wantErrAs checks that err holds an error of target's type and sets
// target to it.
func wantErrAs(t *testing.T, err error, target any) {
	t.Helper()
	if !errors.As(err, target) {
		t.Fatalf("got %T (%v), want %T", err, err, target)
	}
}

// wantJSON checks that v prints as the compact JSON text want.
func wantJSON(t *testing.T, v *value.Value, want string) {
	t.Helper()
	if got := compactJSON(t, v); got != want {
		t.Errorf("got %s, want %s", got, want)
	}
}

func compactJSON(t *testing.T, v *value.Value) string {
	t.Helper()
	var buf bytes.Buffer
	enc := NewEncoder(&buf, EncodeOptions{Compact: true})
	if err := enc.Encode(v); err != nil {
		t.Fatal(err)
	}
	if err := enc.Flush(); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSuffix(buf.String(), "\n")
}

func mustCompile(t *testing.T, src string, opts ...Option) *Query {
	t.Helper()
	q, err := Compile(src, opts...)
	if err != nil {
		t.Fatalf("compiling %s: %v", src, err)
	}
	return q
}

// mustDecode reads the one JSON or YAML value in text.
func mustDecode(t *testing.T, text string) *value.Value {
	t.Helper()
	v, err := NewDecoder(strings.NewReader(text), DecodeOptions{}).Next()
	if err != nil {
		t.Fatalf("decoding %q: %v", text, err)
	}
	return v
}

// outputs runs src on the JSON text in and returns each output as compact
// JSON.
func outputs(t *testing.T, src, in string, opts ...Option) []string {
	t.Helper()
	vals, err := mustCompile(t, src, opts...).RunAll(t.Context(), mustDecode(t, in))
	if err != nil {
		t.Fatalf("running %s: %v", src, err)
	}
	out := []string{}
	for _, v := range vals {
		out = append(out, compactJSON(t, v))
	}
	return out
}
