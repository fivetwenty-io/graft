package operators_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// An unknown operator is a mistake in the template, never a missing
// value, so a || must not answer it with its fallback.
func TestOrNeverFallsBackOnAnUnknownOperator(t *testing.T) {
	cases := []struct{ name, src string }{
		{"comparison", "a: 1\nb: (( nosuch a == 1 || \"fb\" ))\n"},
		{"not equal", "a: 1\nb: (( nosuch a != 1 || \"fb\" ))\n"},
		{"less than", "a: 1\nb: (( nosuch a < 1 || \"fb\" ))\n"},
		{"and", "a: 1\nb: (( nosuch a && true || \"fb\" ))\n"},
		{"addition", "a: 1\nb: (( nosuch a + 1 || \"fb\" ))\n"},
		{"nested in concat", "a: 1\nb: (( concat \"x\" (( nosuch a )) || \"fb\" ))\n"},
		{"nested left of ||", "a: 1\nb: (( concat \"x\" (( (( nosuch a )) || \"fb\" )) ))\n"},
		{"nested in grab", "a: 1\nb: (( grab (( nosuch a )) || \"fb\" ))\n"},
	}
	for _, tc := range cases {
		for _, parallel := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/parallel=%v", tc.name, parallel), func(t *testing.T) {
				out, err := evaluateWithin(t, parallel, tc.src)
				if err == nil {
					v, _ := out.Get("b")
					t.Fatalf("b = %#v, want an unknown operator error", v)
				}
				if !strings.Contains(err.Error(), "unknown operator: nosuch") {
					t.Fatalf("err = %v, want an unknown operator error", err)
				}
			})
		}
	}
}

// The fallbacks a || is for still happen.
func TestOrStillFallsBack(t *testing.T) {
	cases := []struct {
		name, src string
		want      interface{}
	}{
		{"missing reference", "b: (( nope || \"fb\" ))\n", "fb"},
		{"missing reference in a comparison", "b: (( nope == 1 || \"fb\" ))\n", "fb"},
		{"missing reference nested", "b: (( concat \"x\" (( nope || \"fb\" )) ))\n", "xfb"},
		{"failed nested grab", "b: (( concat \"x\" (( grab nope )) || \"fb\" ))\n", "xfb"},
		{"unset environment variable", "b: (( grab $GRAFT_TEST_SURELY_UNSET || \"fb\" ))\n", "fb"},
		{"nil value is kept", "a: ~\nb: (( a || \"fb\" ))\n", nil},
		{"known operator in a comparison", "a: [1]\nb: (( (( grab a.0 )) == 1 || \"fb\" ))\n", true},
	}
	for _, tc := range cases {
		for _, parallel := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/parallel=%v", tc.name, parallel), func(t *testing.T) {
				out, err := evaluateWithin(t, parallel, tc.src)
				if err != nil {
					t.Fatalf("err = %v", err)
				}
				got, err := out.Get("b")
				if err != nil {
					t.Fatalf("Get(b): %v", err)
				}
				if !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("b = %#v, want %#v", got, tc.want)
				}
			})
		}
	}
}
