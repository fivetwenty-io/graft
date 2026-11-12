package operators_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

func TestEnvSegmentIsSeenByDependencies(t *testing.T) {
	t.Setenv("GRAFT_T_XX", "ab")
	src := "aa: (( concat \"x-\" mm.$GRAFT_T_XX.v ))\nmm:\n  ab:\n    v: (( concat \"a\" \"b\" ))\n"
	for _, parallel := range []bool{true, false} {
		t.Run(fmt.Sprintf("parallel=%v", parallel), func(t *testing.T) {
			out, err := evaluateWithin(t, parallel, src)
			if err != nil {
				t.Fatal(err)
			}
			if v, _ := out.GetString("aa"); v != "x-ab" {
				t.Errorf("aa = %q, want x-ab", v)
			}
		})
	}
}

func TestEnvSegmentIsExpandedOnce(t *testing.T) {
	t.Setenv("GRAFT_T_A", "$GRAFT_T_B")
	t.Setenv("GRAFT_T_B", "wrong")
	src := "mm:\n  $GRAFT_T_B: one\n  wrong: two\nr: (( grab mm.$GRAFT_T_A ))\n"
	for _, parallel := range []bool{true, false} {
		out, err := evaluateWithin(t, parallel, src)
		if err != nil {
			t.Fatalf("parallel=%v: %v", parallel, err)
		}
		if v, _ := out.GetString("r"); v != "one" {
			t.Errorf("parallel=%v: r = %q, want one", parallel, v)
		}
	}
}

func TestUnsetEnvSegmentFallsBack(t *testing.T) {
	src := "mm: {}\nr: (( grab mm.$GRAFT_T_UNSET_XYZ || \"dflt\" ))\n"
	for _, parallel := range []bool{true, false} {
		t.Run(fmt.Sprintf("parallel=%v", parallel), func(t *testing.T) {
			out, err := evaluateWithin(t, parallel, src)
			if err != nil {
				t.Fatal(err)
			}
			if v, _ := out.GetString("r"); v != "dflt" {
				t.Errorf("r = %q", v)
			}
		})
	}
}

// TestMinusGluedEnvSegmentIsSeenByDependencies pins that a name with a
// '-' glued in front of it, which the parser reads as one reference,
// also has its $NAME segment replaced before operators are ordered.
func TestMinusGluedEnvSegmentIsSeenByDependencies(t *testing.T) {
	t.Setenv("GRAFT_T_XX", "ab")
	src := "aa: (( concat \"m-\" -3abc.$GRAFT_T_XX ))\n-3abc:\n  ab: (( concat \"a\" \"b\" ))\n"
	for _, parallel := range []bool{true, false} {
		t.Run(fmt.Sprintf("parallel=%v", parallel), func(t *testing.T) {
			out, err := evaluateWithin(t, parallel, src)
			if err != nil {
				t.Fatal(err)
			}
			if v, _ := out.GetString("aa"); v != "m-ab" {
				t.Errorf("aa = %q, want m-ab", v)
			}
		})
	}
}

// TestEnvBracketKeyIsReadByName pins that a bracket key written as
// $NAME is the variable's value used as a key, not a path to resolve,
// and that the grab still waits for the operator under that key.
func TestEnvBracketKeyIsReadByName(t *testing.T) {
	t.Setenv("GRAFT_T_KEY", "ab")
	src := "aa: (( grab mm[$GRAFT_T_KEY] ))\nab: wrong\nmm:\n  ab: (( concat \"a\" \"b\" ))\n"
	for _, parallel := range []bool{true, false} {
		t.Run(fmt.Sprintf("parallel=%v", parallel), func(t *testing.T) {
			out, err := evaluateWithin(t, parallel, src)
			if err != nil {
				t.Fatal(err)
			}
			if v, _ := out.GetString("aa"); v != "ab" {
				t.Errorf("aa = %q, want ab", v)
			}
		})
	}
}

// evaluateWithOptions evaluates src on an engine built with opts, in
// parallel mode when parallel is set and in sequence when it is not,
// and fails the test when the engine is not in the mode asked for.
func evaluateWithOptions(t *testing.T, parallel bool, src string, opts ...graft.EngineOption) graft.Document {
	t.Helper()
	engine, err := graft.NewEngine(append([]graft.EngineOption{graft.WithParallel(parallel)}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	de, ok := engine.(*graft.DefaultEngine)
	if !ok {
		t.Fatalf("engine is %T, want *graft.DefaultEngine", engine)
	}
	if got := de.GetWorkerPool() != nil; got != parallel {
		t.Fatalf("worker pool present = %v, want %v", got, parallel)
	}
	doc, err := engine.ParseYAML([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	out, err := engine.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// TestWrittenBackCallsKeepEnvSegments pins that every operator call
// graft writes back out as text for a later pass keeps a $NAME path
// segment as written, rather than the variable's value.
func TestWrittenBackCallsKeepEnvSegments(t *testing.T) {
	t.Setenv("GRAFT_T_K", "ab")
	cases := []struct {
		name, src, want string
		opts            []graft.EngineOption
	}{
		{
			name: "defer",
			src:  "v: (( defer grab mm.$GRAFT_T_K.p ))\n",
			want: "(( grab mm.$GRAFT_T_K.p ))",
		},
		{
			name: "skipped vault",
			src:  "v: (( vault mm.$GRAFT_T_K.p \":key\" ))\n",
			want: "(( vault mm.$GRAFT_T_K.p \":key\" ))",
			opts: []graft.EngineOption{graft.WithSkipVault(true)},
		},
		{
			name: "skipped awsparam",
			src:  "v: (( awsparam mm.$GRAFT_T_K.p ))\n",
			want: "(( awsparam mm.$GRAFT_T_K.p ))",
			opts: []graft.EngineOption{graft.WithSkipAws(true)},
		},
		{
			name: "skipped vault with a fallback",
			src:  "v: (( vault mm.$GRAFT_T_K.p \":key\" || \"d\" ))\n",
			want: "(( vault mm.$GRAFT_T_K.p \":key\" || \"d\" ))",
			opts: []graft.EngineOption{graft.WithSkipVault(true)},
		},
		{
			name: "unregistered operator",
			src:  "v: (( nosuchop mm.$GRAFT_T_K.p ))\n",
			want: "(( nosuchop mm.$GRAFT_T_K.p ))",
		},
		{
			name: "unregistered operator, negated path",
			src:  "v: (( nosuchop !mm.$GRAFT_T_K.p ))\n",
			want: "(( nosuchop !mm.$GRAFT_T_K.p ))",
		},
	}
	for _, tc := range cases {
		for _, parallel := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/parallel=%v", tc.name, parallel), func(t *testing.T) {
				out := evaluateWithOptions(t, parallel, "mm:\n  ab:\n    p: x\n"+tc.src, tc.opts...)
				if v, _ := out.GetString("v"); v != tc.want {
					t.Errorf("v = %q, want %q", v, tc.want)
				}
			})
		}
	}
}
