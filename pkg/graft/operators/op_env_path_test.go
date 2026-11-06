package operators_test

import (
	"context"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
	"github.com/fivetwenty-io/graft/pkg/graft/operators"
)

func evalEnvPathDoc(t *testing.T, src string) graft.Document {
	t.Helper()
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	doc, err := engine.ParseYAML([]byte(src))
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}
	out, err := engine.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	return out
}

// TestPathVariableSubstitution pins that a $NAME node in an operator
// path is replaced by the variable's value for every name length,
// including a one-letter name, and for inject as well as grab.
func TestPathVariableSubstitution(t *testing.T) {
	t.Setenv("X", "foo")
	t.Setenv("XY", "foo")
	t.Setenv("QQ", "q")
	t.Setenv("Q", "q")

	cases := []struct {
		name string
		src  string
		path string
	}{
		{"grab one letter", "mm: {foo: 1}\nr: (( grab mm.$X ))\n", "r"},
		{"grab two letters", "mm: {foo: 1}\nr: (( grab mm.$XY ))\n", "r"},
		{"inject long", "mm: {q: {a: 1}}\nr: (( inject mm.$QQ ))\n", "a"},
		{"inject one", "mm: {q: {a: 1}}\nr: (( inject mm.$Q ))\n", "a"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := evalEnvPathDoc(t, tc.src)
			if v, err := out.Get(tc.path); err != nil || v != 1 {
				t.Fatalf("%s = %v, %v; want 1", tc.path, v, err)
			}
		})
	}
}

// TestPathVariableUnsetStaysLiteral pins that an unset variable keeps
// its literal $NAME text, so a key spelled that way still resolves.
func TestPathVariableUnsetStaysLiteral(t *testing.T) {
	out := evalEnvPathDoc(t, "mm: {$UNSET_P2: lit}\nr: (( grab mm.$UNSET_P2 ))\n")
	if v, err := out.Get("r"); err != nil || v != "lit" {
		t.Fatalf("r = %v, %v; want lit", v, err)
	}
}

func TestResolveEnvLoneDollarUntouched(t *testing.T) {
	t.Setenv("X", "foo")
	got := operators.ResolveEnv([]string{"$", "$X", "x"})
	if got[0] != "$" || got[1] != "foo" || got[2] != "x" {
		t.Fatalf("got %v", got)
	}
}
