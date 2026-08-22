package graft_test

import (
	"context"
	"testing"

	. "github.com/fivetwenty-io/graft/pkg/graft"
)

// evaluatePassthrough evaluates src on a fresh engine with no custom
// operators and returns the string the given path holds afterwards.
func evaluatePassthrough(t *testing.T, src, path string) string {
	t.Helper()
	engine, err := NewEngine()
	if err != nil {
		t.Fatalf("failed to create engine: %v", err)
	}
	doc := parseYAML(t, engine, src)
	result, err := engine.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	got, err := result.GetString(path)
	if err != nil {
		t.Fatalf("GetString(%q) failed: %v", path, err)
	}
	return got
}

// TestNullOperatorPassthroughRoundTripsArgs asserts the unregistered-
// operator passthrough (NullOperator) reconstructs every argument from its
// real source, so a call passing through an engine without that operator
// survives for a later pass. Before the fix, non-literal args were
// replaced with the placeholder "...", corrupting the call.
func TestNullOperatorPassthroughRoundTripsArgs(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want string
	}{
		{
			name: "reference argument",
			src:  "meta:\n  foo: x\nv: (( customop meta.foo ))\n",
			want: "(( customop meta.foo ))",
		},
		{
			name: "mixed literal and reference arguments",
			src:  "meta:\n  foo: x\nv: (( customop \"lit\" meta.foo 42 ))\n",
			want: `(( customop "lit" meta.foo 42 ))`,
		},
		{
			name: "environment variable argument",
			src:  "v: (( customop $HOME ))\n",
			want: "(( customop $HOME ))",
		},
		{
			name: "string literal argument (pre-existing behavior)",
			src:  "v: (( customop \"hello\" ))\n",
			want: `(( customop "hello" ))`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := evaluatePassthrough(t, tc.src, "v"); got != tc.want {
				t.Errorf("passthrough = %q, want %q", got, tc.want)
			}
		})
	}
}
