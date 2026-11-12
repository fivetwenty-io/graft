package operators_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
	"github.com/fivetwenty-io/graft/pkg/graft/operators"
)

// mergeWithin merges src alone, which evaluates it and then prunes,
// in the parallel or the sequential mode, and returns the result.
func mergeWithin(t *testing.T, parallel bool, src string) (map[string]interface{}, error) {
	t.Helper()
	engine, err := graft.NewEngine(graft.WithParallel(parallel))
	if err != nil {
		t.Fatal(err)
	}
	doc, err := engine.ParseYAML([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	out, err := engine.Merge(context.Background(), doc).Execute()
	if err != nil {
		return nil, err
	}
	return out.RawData().(map[string]interface{}), nil
}

// A (( prune )) on the right of a bare || prunes the key when the left
// side is unset or missing, and the left side's value stays otherwise.
func TestBareOrFallsBackToPrune(t *testing.T) {
	t.Setenv("GRAFT_TEST_PRUNE_SET", "val")
	cases := []struct {
		name, src string
		want      map[string]interface{}
	}{
		{
			name: "unset variable",
			src:  "a: (( $GRAFT_TEST_PRUNE_UNSET || (( prune )) ))\nb: 1\n",
			want: map[string]interface{}{"b": 1},
		},
		{
			name: "missing reference",
			src:  "a: (( nope || (( prune )) ))\nb: 1\n",
			want: map[string]interface{}{"b": 1},
		},
		{
			name: "missing reference then unset variable",
			src:  "a: (( nope || $GRAFT_TEST_PRUNE_UNSET || (( prune )) ))\nb: 1\n",
			want: map[string]interface{}{"b": 1},
		},
		{
			name: "nested key",
			src:  "m:\n  a: (( $GRAFT_TEST_PRUNE_UNSET || (( prune )) ))\n  b: 1\n",
			want: map[string]interface{}{
				"m": map[string]interface{}{"b": 1},
			},
		},
		{
			name: "set variable",
			src:  "a: (( $GRAFT_TEST_PRUNE_SET || (( prune )) ))\nb: 1\n",
			want: map[string]interface{}{"a": "val", "b": 1},
		},
		{
			name: "found reference",
			src:  "a: (( b || (( prune )) ))\nb: 1\n",
			want: map[string]interface{}{"a": 1, "b": 1},
		},
	}
	for _, tc := range cases {
		for _, parallel := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/parallel=%v", tc.name, parallel), func(t *testing.T) {
				got, err := mergeWithin(t, parallel, tc.src)
				if err != nil {
					t.Fatalf("err = %v", err)
				}
				if !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("got %#v, want %#v", got, tc.want)
				}
			})
		}
	}
}

// prune run with no path, as a call built outside a document is, marks
// nothing and gives nil rather than reading a nil path.
func TestPruneWithNoPathGivesNil(t *testing.T) {
	ev := &graft.Evaluator{Tree: map[string]interface{}{"b": 1}}
	resp, err := operators.PruneOperator{}.Run(ev, nil)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if resp.Value != nil {
		t.Fatalf("value = %#v, want nil", resp.Value)
	}
}
