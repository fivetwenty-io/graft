package jqout

import (
	"bytes"
	"reflect"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/decimal"
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
		t.Fatalf("want an error containing %q, got nil", sub)
	}
	if !strings.Contains(err.Error(), sub) {
		t.Errorf("error %q does not contain %q", err, sub)
	}
}

// lit builds a number that remembers the literal it was written as.
func lit(s string) *value.Value { return value.NumLit(decimal.MustParse(s), s) }

// dec builds a computed number, which carries no literal.
func dec(s string) *value.Value { return value.NumDec(decimal.MustParse(s)) }

func obj(pairs ...value.Pair) *value.Value { return value.Obj(pairs...) }

func pair(k string, v *value.Value) value.Pair { return value.Pair{Key: k, Value: v} }

func encodeErr(v *value.Value, o Options) (string, error) {
	var buf bytes.Buffer
	e := New(&buf, o)
	if err := e.Write(v); err != nil {
		return buf.String(), err
	}
	err := e.Flush()
	return buf.String(), err
}

func encode(t *testing.T, v *value.Value, o Options) string {
	t.Helper()
	s, err := encodeErr(v, o)
	if err != nil {
		t.Fatal(err)
	}
	return s
}
