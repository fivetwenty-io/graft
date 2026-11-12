package operators_test

import (
	"fmt"
	"testing"
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
	out, err := evaluateWithin(t, true, src)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := out.GetString("r"); v != "dflt" {
		t.Errorf("r = %q", v)
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

// TestDeferKeepsEnvSegment pins that (( defer )) writes a $NAME path
// segment back as written, so a later pass reads the variable then.
func TestDeferKeepsEnvSegment(t *testing.T) {
	t.Setenv("GRAFT_T_XX", "ab")
	src := "d: (( defer grab mm.$GRAFT_T_XX.v ))\nmm:\n  ab:\n    v: 1\n"
	for _, parallel := range []bool{true, false} {
		t.Run(fmt.Sprintf("parallel=%v", parallel), func(t *testing.T) {
			out, err := evaluateWithin(t, parallel, src)
			if err != nil {
				t.Fatal(err)
			}
			if v, _ := out.GetString("d"); v != "(( grab mm.$GRAFT_T_XX.v ))" {
				t.Errorf("d = %q", v)
			}
		})
	}
}
