package operators_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// targetCase is one document for an operator that reads a whole value.
// want is the value expected at path, or, when cycle is set, the
// document must fail with a data-flow cycle error instead.
type targetCase struct {
	name, src, path string
	want            interface{}
	cycle           bool
	errs            string
}

// runTargetCases evaluates each case in both evaluator modes. No case
// may hand back operator text, so a value that holds "((" fails it.
func runTargetCases(t *testing.T, cases []targetCase) {
	t.Helper()
	for _, tc := range cases {
		for _, parallel := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/parallel=%v", tc.name, parallel), func(t *testing.T) {
				out, err := evaluateWithin(t, parallel, tc.src)
				switch {
				case tc.cycle:
					if err == nil || !strings.Contains(err.Error(), "cycle detected") {
						t.Fatalf("err = %v, want a cycle error", err)
					}
					return
				case tc.errs != "":
					if err == nil || !strings.Contains(err.Error(), tc.errs) {
						t.Fatalf("err = %v, want it to say %q", err, tc.errs)
					}
					return
				}
				if err != nil {
					t.Fatalf("err = %v", err)
				}
				got, err := out.Get(tc.path)
				if err != nil {
					t.Fatalf("Get(%s): %v", tc.path, err)
				}
				if !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("%s = %#v, want %#v", tc.path, got, tc.want)
				}
			})
		}
	}
}

func TestFlattenWaitsForTheOperatorsItReads(t *testing.T) {
	runTargetCases(t, []targetCase{
		{
			name: "sibling holding an operator",
			src:  "z:\n- [1]\n- [(( concat \"p\" \"q\" ))]\na: (( flatten z ))\n",
			path: "a", want: []interface{}{1, "pq"},
		},
		{
			name: "sibling through ||",
			src:  "z:\n- [1]\n- [(( concat \"p\" \"q\" ))]\na: (( flatten (( z || \"fb\" )) ))\n",
			path: "a", want: []interface{}{1, "pq"},
		},
		{
			name: "fallback side of ||",
			src:  "z:\n- [1]\n- [(( concat \"p\" \"q\" ))]\na: (( flatten nope || z ))\n",
			path: "a", want: []interface{}{1, "pq"},
		},
		{
			name:  "its own list",
			src:   "l:\n- [1, 2]\n- (( flatten l ))\n",
			cycle: true,
		},
		{
			name:  "an ancestor two levels up",
			src:   "q:\n  l:\n  - [1]\n  - x: (( flatten q.l ))\n",
			cycle: true,
		},
	})
}
