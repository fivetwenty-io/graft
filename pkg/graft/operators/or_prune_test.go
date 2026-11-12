package operators_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
	"github.com/fivetwenty-io/graft/pkg/graft/operators"
)

// mergeWithin merges srcs in order, which evaluates the result and then
// prunes it, in the parallel or the sequential mode, and returns it.
func mergeWithin(t *testing.T, parallel bool, srcs ...string) (map[string]interface{}, error) {
	t.Helper()
	engine, err := graft.NewEngine(graft.WithParallel(parallel))
	if err != nil {
		t.Fatal(err)
	}
	docs := make([]graft.Document, 0, len(srcs))
	for _, src := range srcs {
		doc, err := engine.ParseYAML([]byte(src))
		if err != nil {
			t.Fatal(err)
		}
		docs = append(docs, doc)
	}
	out, err := engine.Merge(context.Background(), docs...).Execute()
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

// join over a map leaves out a key that is pruned, the way a pruned
// entry of a list is left out, because the key is not in the output.
func TestJoinLeavesOutAPrunedMapKey(t *testing.T) {
	cases := []struct {
		name string
		srcs []string
		want string
	}{
		{
			name: "prune with no earlier value",
			srcs: []string{"m:\n  a: 1\n  b: (( prune ))\nj: (( join \",\" m ))\n"},
			want: "a:1",
		},
		{
			name: "prune over an earlier value",
			srcs: []string{
				"m:\n  a: 1\n  b: 2\n",
				"m:\n  b: (( prune ))\nj: (( join \",\" m ))\n",
			},
			want: "a:1",
		},
		{
			name: "|| falling back to prune",
			srcs: []string{"m:\n  a: 1\n  b: (( nope || (( prune )) ))\nj: (( join \",\" m ))\n"},
			want: "a:1",
		},
		{
			name: "under a named list entry",
			srcs: []string{"l:\n- name: x\n  m:\n    a: 1\n    b: (( prune ))\nj: (( join \",\" l.x.m ))\n"},
			want: "a:1",
		},
		{
			name: "map from a nested grab",
			srcs: []string{"m:\n  a: 1\n  b: (( prune ))\n" +
				"j: (( join \",\" (( grab m )) ))\n"},
			want: "a:1",
		},
		{
			name: "deeper key",
			srcs: []string{"m:\n  a: 1\n  n:\n    x: (( prune ))\n    y: 2\n" +
				"j: (( join \",\" m ))\n"},
			want: "a:1,n:map[y:2]",
		},
		{
			name: "deeper key in a map from a nested grab",
			srcs: []string{"m:\n  a: 1\n  n:\n    x: (( prune ))\n    y: 2\n" +
				"j: (( join \",\" (( grab m )) ))\n"},
			want: "a:1,n:map[y:2]",
		},
		{
			name: "deeper prune over an earlier value",
			srcs: []string{
				"m:\n  a: 1\n  n:\n    x: 1\n    y: 2\n",
				"m:\n  n:\n    x: (( prune ))\nj: (( join \",\" (( grab m.n )) ))\n",
			},
			want: "y:2",
		},
	}
	for _, tc := range cases {
		for _, parallel := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/parallel=%v", tc.name, parallel), func(t *testing.T) {
				got, err := mergeWithin(t, parallel, tc.srcs...)
				if err != nil {
					t.Fatalf("err = %v", err)
				}
				if got["j"] != tc.want {
					t.Fatalf("j = %#v, want %q", got["j"], tc.want)
				}
			})
		}
	}
}

// A copy of a map, made by grab or inject, leaves out every key under
// it that is pruned, at any depth, as spruce does, because the copy
// and the map share the keys the prune step removes.
func TestCopiesLeaveOutPrunedKeys(t *testing.T) {
	t.Setenv("GRAFT_TEST_PRUNE_UNSET", "")
	am := map[string]interface{}{"a": 1}
	deep := map[string]interface{}{
		"a": 1, "n": map[string]interface{}{"c": 3},
	}
	cases := []struct {
		name string
		srcs []string
		at   string
		want interface{}
	}{
		{
			name: "prune with no earlier value",
			srcs: []string{"m:\n  a: 1\n  b: (( prune ))\nx: (( grab m ))\n"},
			at:   "x", want: am,
		},
		{
			name: "deeper key",
			srcs: []string{"m:\n  a: 1\n  n:\n    b: (( prune ))\n    c: 3\n" +
				"x: (( grab m ))\n"},
			at: "x", want: deep,
		},
		{
			name: "prune over an earlier value",
			srcs: []string{
				"m:\n  a: 1\n  b: 2\n",
				"m:\n  b: (( prune ))\nx: (( grab m ))\n",
			},
			at: "x", want: am,
		},
		{
			name: "|| falling back to prune",
			srcs: []string{"m:\n  a: 1\n  b: (( nope || (( prune )) ))\n" +
				"x: (( grab m ))\n"},
			at: "x", want: am,
		},
		{
			name: "unset variable falling back to prune",
			srcs: []string{"m:\n  a: 1\n" +
				"  b: (( $GRAFT_TEST_PRUNE_UNSET || (( prune )) ))\n" +
				"x: (( grab m ))\n"},
			at: "x", want: am,
		},
		{
			name: "grab of the map that holds the key",
			srcs: []string{"m:\n  a: 1\n  n:\n    b: (( prune ))\n    c: 3\n" +
				"x: (( grab m.n ))\n"},
			at: "x", want: map[string]interface{}{"c": 3},
		},
		{
			name: "grab with a fallback",
			srcs: []string{"m:\n  a: 1\n  b: (( prune ))\n" +
				"x: (( grab m || \"fb\" ))\n"},
			at: "x", want: am,
		},
		{
			name: "grab in a list",
			srcs: []string{"m:\n  a: 1\n  b: (( prune ))\n" +
				"x:\n- (( grab m ))\n"},
			at: "x", want: []interface{}{am},
		},
		{
			name: "inject",
			srcs: []string{"m:\n  a: 1\n  n:\n    b: (( prune ))\n    c: 3\n" +
				"x:\n  _: (( inject m ))\n"},
			at: "x", want: deep,
		},
	}
	for _, tc := range cases {
		for _, parallel := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/parallel=%v", tc.name, parallel), func(t *testing.T) {
				got, err := mergeWithin(t, parallel, tc.srcs...)
				if err != nil {
					t.Fatalf("err = %v", err)
				}
				if !reflect.DeepEqual(got[tc.at], tc.want) {
					t.Fatalf("%s = %#v, want %#v", tc.at, got[tc.at], tc.want)
				}
			})
		}
	}
}

// A key the caller prunes, as graft merge --prune does, is left out of
// a joined map the way grab leaves it out, although the prune runs only
// after evaluation.
func TestJoinLeavesOutAKeyTheCallerPrunes(t *testing.T) {
	src := "m:\n  a: 1\n  b: 2\n  n:\n    x: 1\n    y: 2\n" +
		"j: (( join \",\" m ))\nk: (( join \",\" (( grab m.n )) ))\n" +
		"x: (( grab m ))\n"
	for _, parallel := range []bool{true, false} {
		t.Run(fmt.Sprintf("parallel=%v", parallel), func(t *testing.T) {
			engine, err := graft.NewEngine(graft.WithParallel(parallel))
			if err != nil {
				t.Fatal(err)
			}
			doc, err := engine.ParseYAML([]byte(src))
			if err != nil {
				t.Fatal(err)
			}
			out, err := engine.Merge(context.Background(), doc).
				WithPrune("m.b", "$.m.n.x").Execute()
			if err != nil {
				t.Fatalf("err = %v", err)
			}
			got := out.RawData().(map[string]interface{})
			want := map[string]interface{}{
				"j": "a:1,n:map[y:2]",
				"k": "y:2",
				"x": map[string]interface{}{
					"a": 1, "n": map[string]interface{}{"y": 2},
				},
			}
			for key, w := range want {
				if !reflect.DeepEqual(got[key], w) {
					t.Errorf("%s = %#v, want %#v", key, got[key], w)
				}
			}
		})
	}
}

// stringify leaves out every key that will be pruned, at any depth, as
// grab and join do, because the key is not in the output.
func TestStringifyLeavesOutPrunedKeys(t *testing.T) {
	t.Setenv("GRAFT_TEST_PRUNE_UNSET", "")
	cases := []struct {
		name   string
		srcs   []string
		prunes []string
		want   string
	}{
		{
			name: "prune with no earlier value",
			srcs: []string{"m:\n  a: 1\n  b: (( prune ))\n" +
				"s: (( stringify m ))\n"},
			want: "a: 1\n",
		},
		{
			name: "prune over an earlier value",
			srcs: []string{
				"m:\n  a: 1\n  b: 2\n",
				"m:\n  b: (( prune ))\ns: (( stringify m ))\n",
			},
			want: "a: 1\n",
		},
		{
			name: "|| falling back to prune",
			srcs: []string{"m:\n  a: 1\n  b: (( nope || (( prune )) ))\n" +
				"s: (( stringify m ))\n"},
			want: "a: 1\n",
		},
		{
			name: "grab of an unset variable falling back to prune",
			srcs: []string{"m:\n  a: 1\n" +
				"  b: (( grab $GRAFT_TEST_PRUNE_UNSET || (( prune )) ))\n" +
				"s: (( stringify m ))\n"},
			want: "a: 1\n",
		},
		{
			name: "deeper key",
			srcs: []string{"m:\n  a: 1\n  nn:\n    b: (( prune ))\n" +
				"    c: 3\ns: (( stringify m ))\n"},
			want: "a: 1\nnn:\n  c: 3\n",
		},
		{
			name: "map in a list",
			srcs: []string{"l:\n- name: e\n  b: (( prune ))\n  c: 4\n" +
				"s: (( stringify l ))\n"},
			want: "- c: 4\n  name: e\n",
		},
		{
			name: "under a named list entry",
			srcs: []string{"l:\n- name: e\n  m:\n    a: 1\n" +
				"    b: (( prune ))\ns: (( stringify l.e.m ))\n"},
			want: "a: 1\n",
		},
		{
			name:   "key the caller prunes",
			srcs:   []string{"m:\n  a: 1\n  bb: 2\ns: (( stringify m ))\n"},
			prunes: []string{"m.bb"},
			want:   "a: 1\n",
		},
	}
	for _, tc := range cases {
		for _, parallel := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/parallel=%v", tc.name, parallel), func(t *testing.T) {
				engine, err := graft.NewEngine(graft.WithParallel(parallel))
				if err != nil {
					t.Fatal(err)
				}
				docs := make([]graft.Document, 0, len(tc.srcs))
				for _, src := range tc.srcs {
					doc, err := engine.ParseYAML([]byte(src))
					if err != nil {
						t.Fatal(err)
					}
					docs = append(docs, doc)
				}
				out, err := engine.Merge(context.Background(), docs...).
					WithPrune(tc.prunes...).Execute()
				if err != nil {
					t.Fatalf("err = %v", err)
				}
				got := out.RawData().(map[string]interface{})["s"]
				if got != tc.want {
					t.Fatalf("s = %#v, want %q", got, tc.want)
				}
			})
		}
	}
}

// A key the caller prunes through a map that an operator hands back, as
// --prune x.bb is with x: (( grab m )), leaves the map that operator
// reads, so join and stringify leave it out of that map whichever order
// they run in, and agree with the map the output holds.
func TestReadersLeaveOutAKeyPrunedThroughAGrab(t *testing.T) {
	cases := []struct {
		name   string
		src    string
		prunes []string
		want   map[string]interface{}
	}{
		{
			name: "grab of the map",
			src: "m: {a: 1, bb: 2}\nx: (( grab m ))\n" +
				"aj: (( join \",\" m ))\nzj: (( join \",\" m ))\n" +
				"as: (( stringify m ))\nzs: (( stringify m ))\n",
			prunes: []string{"x.bb"},
			want: map[string]interface{}{
				"m": map[string]interface{}{"a": 1},
				"aj": "a:1", "zj": "a:1",
				"as": "a: 1\n", "zs": "a: 1\n",
			},
		},
		{
			name: "key below the grab",
			src: "m: {k: {a: 1, bb: 2}}\nx: (( grab m ))\n" +
				"aj: (( join \",\" m.k ))\nzj: (( join \",\" m.k ))\n",
			prunes: []string{"x.k.bb"},
			want: map[string]interface{}{
				"aj": "a:1", "zj": "a:1",
			},
		},
		{
			name: "grab under a named list entry",
			src: "m: {a: 1, bb: 2}\nl:\n- name: e\n  x: (( grab m ))\n" +
				"aj: (( join \",\" m ))\nzj: (( join \",\" m ))\n",
			prunes: []string{"l.e.x.bb"},
			want: map[string]interface{}{
				"aj": "a:1", "zj": "a:1",
			},
		},
		{
			name: "join the grab reads",
			src: "m: {a: 1}\nx: (( grab y ))\n" +
				"y:\n  bb: 1\n  j: (( join \",\" m ))\n",
			prunes: []string{"x.bb"},
			want: map[string]interface{}{
				"x": map[string]interface{}{"j": "a:1"},
				"y": map[string]interface{}{"j": "a:1"},
			},
		},
	}
	for _, tc := range cases {
		for _, parallel := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/parallel=%v", tc.name, parallel), func(t *testing.T) {
				engine, err := graft.NewEngine(graft.WithParallel(parallel))
				if err != nil {
					t.Fatal(err)
				}
				doc, err := engine.ParseYAML([]byte(tc.src))
				if err != nil {
					t.Fatal(err)
				}
				out, err := engine.Merge(context.Background(), doc).
					WithPrune(tc.prunes...).Execute()
				if err != nil {
					t.Fatalf("err = %v", err)
				}
				got := out.RawData().(map[string]interface{})
				for key, w := range tc.want {
					if !reflect.DeepEqual(got[key], w) {
						t.Errorf("%s = %#v, want %#v", key, got[key], w)
					}
				}
			})
		}
	}
}
