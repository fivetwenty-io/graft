package operators_test

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// Two grabs that each copy a map holding a reader of the other map
// wait on each other, so one reader runs before the grab above a key
// the caller prunes. The reader still leaves the key out, because the
// grab hands on the map it reads, and the key is found in that map
// whether or not the grab has run. Both readers then agree with the
// output in either mode.
func TestReadersAgreeWhenPruneHoldersWaitOnThem(t *testing.T) {
	cases := []struct {
		name   string
		src    string
		prunes []string
		want   map[string]interface{}
	}{
		{
			name: "grab",
			src: "m:\n  sub: {a: 1, b: 2}\n  j: (( join \",\" o.sub ))\n" +
				"o:\n  sub: {a: 3, b: 4}\n  j: (( join \",\" m.sub ))\n" +
				"x: (( grab m ))\ny: (( grab o ))\n",
			prunes: []string{"x.sub.a", "y.sub.a"},
			want:   map[string]interface{}{"m.j": "b:4", "o.j": "b:2"},
		},
		{
			name: "bare or",
			src: "m:\n  sub: {a: 1, b: 2}\n  j: (( join \",\" o.sub ))\n" +
				"o:\n  sub: {a: 3, b: 4}\n  j: (( join \",\" m.sub ))\n" +
				"x: (( nope || m ))\ny: (( o || nope ))\n",
			prunes: []string{"x.sub.a", "y.sub.a"},
			want:   map[string]interface{}{"m.j": "b:4", "o.j": "b:2"},
		},
		{
			name: "stringify through a grab of a grab",
			src: "m:\n  sub: {a: 1, b: 2}\n  s: (( stringify o.sub ))\n" +
				"o:\n  sub: {a: 3, b: 4}\n  s: (( stringify m.sub ))\n" +
				"x: (( grab m ))\ny: (( grab z ))\nz: (( grab o ))\n",
			prunes: []string{"x.sub.a", "y.sub.a"},
			want:   map[string]interface{}{"m.s": "b: 4\n", "o.s": "b: 2\n"},
		},
		{
			name: "grab of two maps",
			src: "m:\n  sub: {a: 1, b: 2}\n  j: (( join \",\" o.sub ))\n" +
				"o:\n  sub: {a: 3, b: 4}\n  j: (( join \",\" m.sub ))\n" +
				"x: (( grab m o ))\n",
			prunes: []string{"x.0.sub.a", "x.1.sub.a"},
			want:   map[string]interface{}{"m.j": "b:4", "o.j": "b:2"},
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
				for path, w := range tc.want {
					m, key := path[:1], path[2:]
					v := got[m].(map[string]interface{})[key]
					if !reflect.DeepEqual(v, w) {
						t.Errorf("%s = %#v, want %#v", path, v, w)
					}
				}
			})
		}
	}
}
