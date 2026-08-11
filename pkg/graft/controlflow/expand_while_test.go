package controlflow

import (
	"strings"
	"testing"
)

func TestExpand_While_ExceedsCap_Errors(t *testing.T) {
	resetMaxLoopIterationsForTest()
	defer resetMaxLoopIterationsForTest()
	SetMaxLoopIterations(5)

	// counter is never incremented anywhere (there is no assignment
	// construct in graft, per spec §8.7) so this condition is always true
	// and must hit the cap.
	src := `counter: 0
out:
(( while counter < 100 ))
  attempt: 1
(( done ))
`
	err := runMergeYAMLErr(t, src)
	if err == nil {
		t.Fatal("expected a while-loop-exceeded-cap error")
	}
	if !strings.Contains(err.Error(), "while loop exceeded maximum iterations (5)") {
		t.Errorf("error = %v, want it to mention the configured cap (5)", err)
	}
}

func TestExpand_While_FalseConditionRunsZeroTimes(t *testing.T) {
	src := `enabled: false
out:
(( while enabled ))
x: 1
(( done ))
placeholder: 1
`
	data := runMergeYAML(t, src)
	if v, present := data["out"]; present && v != nil {
		t.Errorf("out = %#v, want nil/absent (condition was false on the first check)", v)
	}
}

func TestMaxLoopIterations_DefaultAndOverride(t *testing.T) {
	resetMaxLoopIterationsForTest()
	defer resetMaxLoopIterationsForTest()

	if got := MaxLoopIterations(); got != DefaultMaxLoopIterations {
		t.Errorf("MaxLoopIterations() = %d, want default %d", got, DefaultMaxLoopIterations)
	}

	SetMaxLoopIterations(42)
	if got := MaxLoopIterations(); got != 42 {
		t.Errorf("MaxLoopIterations() after SetMaxLoopIterations(42) = %d, want 42", got)
	}

	// Non-positive overrides are ignored.
	SetMaxLoopIterations(0)
	if got := MaxLoopIterations(); got != 42 {
		t.Errorf("MaxLoopIterations() after SetMaxLoopIterations(0) = %d, want unchanged 42", got)
	}
}

func TestMaxLoopIterations_EnvVarFallback(t *testing.T) {
	resetMaxLoopIterationsForTest()
	defer resetMaxLoopIterationsForTest()

	t.Setenv("GRAFT_MAX_LOOP_ITERATIONS", "7")
	if got := MaxLoopIterations(); got != 7 {
		t.Errorf("MaxLoopIterations() with GRAFT_MAX_LOOP_ITERATIONS=7 = %d, want 7", got)
	}
}
