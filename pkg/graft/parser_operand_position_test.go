package graft_test

import (
	"fmt"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"

	_ "github.com/fivetwenty-io/graft/pkg/graft/operators" // registers grab, sort, type, empty, ips, keys, ...
)

// TestOperandPositionOperatorNames pins the §2.3–§2.4 two-token
// disambiguation rule at every operand position, not just inside "(" groups:
// an identifier that happens to name a registered operator only opens an
// operator call when the token after it can actually start an argument. A
// document key called "type", "sort", "keys", or "empty" — all ordinary
// names in BOSH and genesis manifests — stays a plain reference when it sits
// at the end of an infix expression, a ternary branch, or a unary operand,
// exactly as it did before A2.
//
// mergeYAML is defined in a6_backward_compat_test.go (same package).
func TestOperandPositionOperatorNames(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		key  string
		want interface{}
	}{
		{
			name: "&& right operand naming an operator stays a reference",
			yaml: "a: true\ntype: worker\nx: (( a && type ))\n",
			key:  "x",
			want: true,
		},
		{
			name: "== right operand naming an operator stays a reference",
			yaml: "left: worker\ntype: worker\nx: (( left == type ))\n",
			key:  "x",
			want: true,
		},
		{
			name: "+ right operand naming an operator stays a reference",
			yaml: "a: 3\nkeys: 5\nx: (( a + keys ))\n",
			key:  "x",
			want: int64(8),
		},
		{
			name: "unary ! operand naming an operator stays a reference",
			yaml: "empty: false\nx: (( ! empty ))\n",
			key:  "x",
			want: true,
		},
		{
			name: "unary - operand naming an operator stays a reference",
			yaml: "ips: 5\nx: (( - ips ))\n",
			key:  "x",
			want: int64(-5),
		},
		{
			name: "ternary true branch naming an operator stays a reference",
			yaml: "flag: true\nsort: 7\nx: '(( flag ? sort : 3 ))'\n",
			key:  "x",
			// A ternary branch passes its value through untouched, so this
			// stays the int the YAML parser produced rather than the int64 the
			// arithmetic path normalizes to.
			want: 7,
		},
		{
			name: "ternary false branch naming an operator stays a reference",
			yaml: "flag: false\nsort: 7\nx: '(( flag ? 3 : sort ))'\n",
			key:  "x",
			want: 7,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := mergeYAML(t, tc.yaml)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got, err := doc.Get(tc.key)
			if err != nil {
				t.Fatalf("failed to read %s: %v", tc.key, err)
			}
			if got != tc.want {
				t.Fatalf("expected %v (%T), got %v (%T)", tc.want, tc.want, got, got)
			}
		})
	}
}

// TestOperandPositionOperatorCalls pins the other half of the same rule: an
// operator name followed by something that can start an argument does open a
// call in operand position, which is what makes `(( grab a && grab b ))`
// evaluate both sides (§11 Stage A-i "Done means").
func TestOperandPositionOperatorCalls(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want interface{}
	}{
		{
			name: "&& right operand is a full operator call when arguments follow",
			yaml: "a: 1\nb: 2\nx: (( grab a && grab b ))\n",
			want: true,
		},
		{
			name: "== right operand is a full operator call when arguments follow",
			yaml: "b: 2\nx: (( 2 == grab b ))\n",
			want: true,
		},
		{
			name: "unary ! operand is a full operator call when arguments follow",
			yaml: "b: false\nx: (( ! grab b ))\n",
			want: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			doc, err := mergeYAML(t, tc.yaml)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			got, err := doc.Get("x")
			if err != nil {
				t.Fatalf("failed to read x: %v", err)
			}
			if got != tc.want {
				t.Fatalf("expected %v, got %v (%T)", tc.want, got, got)
			}
		})
	}
}

// TestOperatorCallsSpanTheirArguments pins where OperatorCalls says each
// call ends: just past its last argument, so a call nested in another
// call or after a fallback ends before the text that follows it.
func TestOperatorCallsSpanTheirArguments(t *testing.T) {
	cases := []struct {
		expr string
		want map[int]graft.CallSpan
	}{
		{`defer grab a.b`, map[int]graft.CallSpan{0: {"defer", 14}}},
		{`concat x (defer grab a) y`, map[int]graft.CallSpan{0: {"concat", 25}, 10: {"defer", 22}}},
		{`grab x || defer grab y`, map[int]graft.CallSpan{0: {"grab", 22}, 10: {"defer", 22}}},
	}
	for _, tc := range cases {
		got := graft.OperatorCalls(nil, tc.expr)
		if fmt.Sprint(got) != fmt.Sprint(tc.want) {
			t.Errorf("OperatorCalls(%q) = %v, want %v", tc.expr, got, tc.want)
		}
	}
}
