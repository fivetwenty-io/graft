package graft

import (
	"reflect"
	"testing"
)

func TestSubstituteEnvNodes(t *testing.T) {
	t.Setenv("GRAFT_T_ONE", "x")
	t.Setenv("GRAFT_T_EMPTY", "")
	t.Setenv("GRAFT_T_DOLLAR", "$GRAFT_T_ONE")
	in := []string{"a", "$GRAFT_T_ONE", "$", "$GRAFT_T_EMPTY", "$GRAFT_T_UNSET", "$GRAFT_T_DOLLAR"}
	got := SubstituteEnvNodes(in)
	want := []string{"a", "x", "$", "$GRAFT_T_EMPTY", "$GRAFT_T_UNSET", "$GRAFT_T_ONE"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got, want)
	}
	if in[1] != "$GRAFT_T_ONE" {
		t.Error("SubstituteEnvNodes changed its input")
	}
}
