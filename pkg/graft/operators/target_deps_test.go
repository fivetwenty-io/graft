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

func TestJoinWaitsForTheOperatorsItReads(t *testing.T) {
	runTargetCases(t, []targetCase{
		{
			name: "sibling map holding an operator",
			src:  "z:\n  k: (( concat \"p\" \"q\" ))\na: (( join \",\" z ))\n",
			path: "a", want: "k:pq",
		},
		{
			name: "sibling list holding an operator",
			src:  "z:\n- x\n- (( concat \"p\" \"q\" ))\na: (( join \",\" z ))\n",
			path: "a", want: "x,pq",
		},
		{
			name: "sibling through ||",
			src:  "z:\n- x\n- (( concat \"p\" \"q\" ))\na: (( join \",\" z || \"fb\" ))\n",
			path: "a", want: "x,pq",
		},
		{
			name: "sibling through a nested ||",
			src:  "z:\n- x\n- (( concat \"p\" \"q\" ))\na: (( join \",\" (( z || \"fb\" )) ))\n",
			path: "a", want: "x,pq",
		},
		{
			name: "fallback side of ||",
			src:  "z:\n- x\n- (( concat \"p\" \"q\" ))\na: (( join \",\" nope || z ))\n",
			path: "a", want: "x,pq",
		},
		{
			name: "fallback after a missing reference",
			src:  "a: (( join \",\" nope || \"fb\" ))\n",
			path: "a", want: "fb",
		},
		{
			name:  "its own list through ||",
			src:   "l:\n- x\n- (( join \",\" l || \"fb\" ))\n",
			cycle: true,
		},
		{
			name:  "its own list through a nested ||",
			src:   "l:\n- x\n- (( join \",\" (( l || \"fb\" )) ))\n",
			cycle: true,
		},
		{
			name:  "an ancestor map",
			src:   "q:\n  a: (( join \",\" q ))\n",
			cycle: true,
		},
		{
			name:  "an ancestor two levels up",
			src:   "q:\n  r:\n    a: (( join \",\" q ))\n",
			cycle: true,
		},
	})
}

func TestShuffleWaitsForTheOperatorsItReads(t *testing.T) {
	runTargetCases(t, []targetCase{
		{
			name: "sibling holding an operator",
			src:  "z:\n- (( concat \"p\" \"q\" ))\na: (( shuffle z ))\n",
			path: "a", want: []interface{}{"pq"},
		},
		{
			name: "sibling through ||",
			src:  "z:\n- (( concat \"p\" \"q\" ))\na: (( shuffle z || \"fb\" ))\n",
			path: "a", want: []interface{}{"pq"},
		},
		{
			name: "sibling through a nested ||",
			src:  "z:\n- (( concat \"p\" \"q\" ))\na: (( shuffle (( z || \"fb\" )) ))\n",
			path: "a", want: []interface{}{"pq"},
		},
		{
			name: "fallback side of ||",
			src:  "z:\n- (( concat \"p\" \"q\" ))\na: (( shuffle nope || z ))\n",
			path: "a", want: []interface{}{"pq"},
		},
		{
			name:  "its own list",
			src:   "l:\n- 1\n- (( shuffle l ))\n",
			cycle: true,
		},
		{
			name:  "an ancestor two levels up",
			src:   "q:\n  l:\n  - 1\n  - x: (( shuffle q.l ))\n",
			cycle: true,
		},
	})
}

func TestConcatRefusesAMapOrAListFromAnyArgument(t *testing.T) {
	const notScalar = "which is not a string scalar"
	runTargetCases(t, []targetCase{
		{
			name:  "ancestor through a nested ||",
			src:   "q:\n  a: (( concat \"x\" (( q || \"fb\" )) ))\n",
			cycle: true,
		},
		{
			name:  "ancestor through ||",
			src:   "q:\n  a: (( concat \"x\" q || \"fb\" ))\n",
			cycle: true,
		},
		{
			name:  "ancestor list through a nested ||",
			src:   "q:\n  l:\n  - x\n  - (( concat \"p\" (( q.l || \"fb\" )) ))\n",
			cycle: true,
		},
		{
			name: "sibling list holding an operator",
			src:  "z:\n- (( concat \"p\" \"q\" ))\na: (( concat \"x\" (( z || \"fb\" )) ))\n",
			errs: "tried to concat z, " + notScalar,
		},
		{
			name: "map through ||",
			src:  "m:\n  a: 1\nb: (( concat \"x\" m || \"fb\" ))\n",
			errs: "tried to concat m, " + notScalar,
		},
		{
			name: "map from a nested grab",
			src:  "m:\n  a: 1\nb: (( concat \"x\" (( grab m )) ))\n",
			errs: "tried to concat the result of (( grab )), " + notScalar,
		},
		{
			name: "list from a nested grab",
			src:  "m: [1, 2]\nb: (( concat \"x\" (( grab m )) ))\n",
			errs: "tried to concat the result of (( grab )), " + notScalar,
		},
		{
			name: "list from a nested keys",
			src:  "m:\n  a: 1\nb: (( concat \"x\" (( keys m )) ))\n",
			errs: "tried to concat the result of (( keys )), " + notScalar,
		},
		{
			name: "scalar sibling through ||",
			src:  "z: (( concat \"p\" \"q\" ))\na: (( concat \"x\" (( z || \"fb\" )) ))\n",
			path: "a", want: "xpq",
		},
		{
			name: "fallback after a missing reference",
			src:  "a: (( concat \"x\" (( nope || \"fb\" )) ))\n",
			path: "a", want: "xfb",
		},
		{
			name: "scalar from a nested grab",
			src:  "q:\n  b: 1\n  a: (( concat \"x\" (( grab q.b )) ))\n",
			path: "q.a", want: "x1",
		},
	})
}

// namedJobs is a list of named maps whose entry web holds operators
// under l and m, so a read of jobs.web.l or jobs.0.l must wait for them.
const namedJobs = "jobs:\n- name: web\n" +
	"  l:\n  - [1]\n  - [(( concat \"p\" \"q\" ))]\n" +
	"  m:\n    k: (( concat \"p\" \"q\" ))\n"

func TestReadsUnderANamedListEntryWait(t *testing.T) {
	var cases []targetCase
	for _, at := range []string{"jobs.web", "jobs.0"} {
		cases = append(cases,
			targetCase{
				name: "flatten " + at + ".l",
				src:  namedJobs + "a: (( flatten " + at + ".l ))\n",
				path: "a", want: []interface{}{1, "pq"},
			},
			targetCase{
				name: "shuffle " + at + ".l.1",
				src:  namedJobs + "a: (( shuffle " + at + ".l.1 ))\n",
				path: "a", want: []interface{}{"pq"},
			},
			targetCase{
				name: "join " + at + ".l.1",
				src:  namedJobs + "a: (( join \",\" " + at + ".l.1 ))\n",
				path: "a", want: "pq",
			},
			targetCase{
				name: "join " + at + ".m",
				src:  namedJobs + "a: (( join \",\" " + at + ".m ))\n",
				path: "a", want: "k:pq",
			},
			targetCase{
				name: "stringify " + at + ".m",
				src:  namedJobs + "a: (( stringify " + at + ".m ))\n",
				path: "a", want: "k: pq\n",
			},
			targetCase{
				name: "inject " + at + ".m",
				src:  namedJobs + "a:\n  b: (( inject " + at + ".m ))\n",
				path: "a.k", want: "pq",
			},
		)
	}
	runTargetCases(t, cases)
}

// namedLists is a list of named maps whose entry web holds an operator
// in its list l, so a read of jobs.web.l or jobs.0.l must wait for it.
const namedLists = "jobs:\n- name: web\n" +
	"  l:\n  - a\n  - (( concat \"p\" \"q\" ))\n"

func TestListArgumentsUnderANamedListEntryWait(t *testing.T) {
	var cases []targetCase
	for _, at := range []string{"jobs.web", "jobs.0"} {
		cases = append(cases,
			targetCase{
				name: "cartesian-product " + at + ".l",
				src: namedLists +
					"a: (( cartesian-product " + at + ".l \"x\" ))\n",
				path: "a", want: []interface{}{"ax", "pqx"},
			},
			targetCase{
				name: "ips " + at,
				src: "jobs:\n- name: web\n" +
					"  ip: (( ips " + at + " 1 ))\n",
				cycle: true,
			},
		)
	}
	runTargetCases(t, cases)
}
