package operators_test

import "testing"

// A bare environment variable on the left of a || falls back when the
// variable is unset or empty, as (( grab $NAME || ... )) does, and a
// set variable gives its value.
func TestOrFallsBackOnAnUnsetOrEmptyVariable(t *testing.T) {
	t.Setenv("GRAFT_TEST_ENV_EMPTY", "")
	t.Setenv("GRAFT_TEST_ENV_SET", "val")
	t.Setenv("GRAFT_TEST_ENV_LIST", "[1, 2]")
	runTargetCases(t, []targetCase{
		{
			name: "unset",
			src:  "a: (( $GRAFT_TEST_ENV_UNSET || \"fb\" ))\n",
			path: "a", want: "fb",
		},
		{
			name: "empty",
			src:  "a: (( $GRAFT_TEST_ENV_EMPTY || \"fb\" ))\n",
			path: "a", want: "fb",
		},
		{
			name: "empty through grab",
			src:  "a: (( grab $GRAFT_TEST_ENV_EMPTY || \"fb\" ))\n",
			path: "a", want: "fb",
		},
		{
			name: "set",
			src:  "a: (( $GRAFT_TEST_ENV_SET || \"fb\" ))\n",
			path: "a", want: "val",
		},
		{
			name: "set to a list",
			src:  "a: (( $GRAFT_TEST_ENV_LIST || \"fb\" ))\n",
			path: "a", want: []interface{}{1, 2},
		},
		{
			name: "in a list",
			src:  "a:\n- (( $GRAFT_TEST_ENV_UNSET || \"fb\" ))\n",
			path: "a", want: []interface{}{"fb"},
		},
		{
			name: "nested",
			src:  "a: (( (( $GRAFT_TEST_ENV_UNSET || \"fb\" )) ))\n",
			path: "a", want: "fb",
		},
		{
			name: "nested in concat",
			src:  "a: (( concat \"x\" (( $GRAFT_TEST_ENV_UNSET || \"fb\" )) ))\n",
			path: "a", want: "xfb",
		},
		{
			name: "two variables",
			src:  "a: (( $GRAFT_TEST_ENV_UNSET || $GRAFT_TEST_ENV_EMPTY || \"fb\" ))\n",
			path: "a", want: "fb",
		},
		{
			name: "second variable set",
			src:  "a: (( $GRAFT_TEST_ENV_UNSET || $GRAFT_TEST_ENV_SET || \"fb\" ))\n",
			path: "a", want: "val",
		},
		{
			name: "then a missing reference",
			src:  "a: (( $GRAFT_TEST_ENV_UNSET || nope || \"fb\" ))\n",
			path: "a", want: "fb",
		},
		{
			name: "last operand stays empty",
			src:  "a: (( nope || $GRAFT_TEST_ENV_UNSET ))\n",
			path: "a", want: "",
		},
		{
			name: "in a path it keeps its spelling",
			src:  "mm:\n  $GRAFT_TEST_ENV_UNSET: lit\na: (( grab mm.$GRAFT_TEST_ENV_UNSET || \"fb\" ))\n",
			path: "a", want: "lit",
		},
		{
			name: "alone stays empty",
			src:  "a: (( $GRAFT_TEST_ENV_UNSET ))\n",
			path: "a", want: "",
		},
	})
}
