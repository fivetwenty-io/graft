package value

import (
	"reflect"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/decimal"
)

func wantEqual[T any](t *testing.T, got, want T) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

// lit builds a literal number whose text is s.
func lit(s string) *Value {
	return NumLit(decimal.MustParse(s), s)
}

func nan() *Value { return NumDec(decimal.NaN()) }

func repeat(s string, n int) string { return strings.Repeat(s, n) }

func wantErrContains(t *testing.T, err error, sub string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), sub) {
		t.Errorf("error %v, want one containing %q", err, sub)
	}
}
