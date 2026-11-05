package controlflow

import (
	"context"
	"fmt"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// rootRefModes runs a merge through both evaluator modes: the library's
// sequential default and the parallel mode the graft CLI uses.
var rootRefModes = []struct {
	name string
	opts []graft.EngineOption
}{
	{"sequential", nil},
	{"parallel", []graft.EngineOption{graft.WithParallel(true)}},
}

func runRootRefMerge(t *testing.T, opts []graft.EngineOption, src string) map[string]interface{} {
	t.Helper()
	engine, err := graft.NewEngine(opts...)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	doc, err := engine.ParseYAML([]byte(src))
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}
	result, err := engine.Merge(context.Background(), doc).Execute()
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	data, ok := result.RawData().(map[string]interface{})
	if !ok {
		t.Fatalf("result is not a map: %#v", result.RawData())
	}
	return data
}

// TestRootReferenceInControlFlowHeaders pins that a "$."-rooted path
// works as a for-loop subject, an if condition, and a case subject, and
// gives the same result as the path without the "$.". A bare path in a
// header is read as a grab of that path, and the "$." form gets the
// same treatment.
func TestRootReferenceInControlFlowHeaders(t *testing.T) {
	const meta = `meta:
  list: [x, z]
  m:
    k1: v1
  flag: true
  kind: big
jobs:
  pg/0:
    ips: [10.0.0.5]
`
	cases := []struct {
		name string
		body string
		want string
	}{
		{"for over a list", `out:
(( for v in $.meta.list ))
- (( grab v ))
(( done ))
`, "[x z]"},
		{"for over a list with grab", `out:
(( for v in grab $.meta.list ))
- (( grab v ))
(( done ))
`, "[x z]"},
		{"for over a map", `out:
(( for k, v in $.meta.m ))
- (( concat k "=" v ))
(( done ))
`, "[k1=v1]"},
		{"for over a slash path", `out:
(( for v in $.jobs.pg/0.ips ))
- (( grab v ))
(( done ))
`, "[10.0.0.5]"},
		{"if condition", `(( if $.meta.flag ))
out: yes-branch
(( else ))
out: else-branch
(( fi ))
`, "yes-branch"},
		{"if comparison", `(( if $.meta.kind == "big" ))
out: yes-branch
(( fi ))
`, "yes-branch"},
		{"case subject", `(( case $.meta.kind ))
(( when "big" ))
out: big-branch
(( when "small" ))
out: small-branch
(( esac ))
`, "big-branch"},
	}
	for _, mode := range rootRefModes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				data := runRootRefMerge(t, mode.opts, meta+tc.body)
				if got := fmt.Sprint(data["out"]); got != tc.want {
					t.Fatalf("out = %v, want %s", got, tc.want)
				}
			})
		}
	}
}

// TestRootReferenceInNestedHeaders pins that a "$." path in a header
// nested inside a loop over x reads the top-level x, while the same path
// without the "$." reads the loop variable.
func TestRootReferenceInNestedHeaders(t *testing.T) {
	const doc = `x:
  list: [t1, t2]
  flag: false
  kind: top
  num: 2
items:
- list: [e1]
  flag: true
  kind: elem
  num: 1
`
	cases := []struct {
		name string
		body string
		want string
	}{
		{"for over a root path", `(( for y in $.x.list ))
- (( grab y ))
(( done ))
`, "[t1 t2]"},
		{"for over the bound path", `(( for y in x.list ))
- (( grab y ))
(( done ))
`, "[e1]"},
		{"for with grab over a root path", `(( for y in grab $.x.list ))
- (( grab y ))
(( done ))
`, "[t1 t2]"},
		{"if on a root path", `(( if $.x.flag ))
- root-true
(( else ))
- root-false
(( fi ))
`, "[root-false]"},
		{"if on the bound path", `(( if x.flag ))
- bound-true
(( else ))
- bound-false
(( fi ))
`, "[bound-true]"},
		{"if comparing both", `(( if $.x.kind == "top" && x.kind == "elem" ))
- both
(( fi ))
`, "[both]"},
		{"case on a root path", `(( case $.x.kind ))
(( when "top" ))
- top-branch
(( when "elem" ))
- elem-branch
(( esac ))
`, "[top-branch]"},
		{"range to a root path", `(( for i in range 1 $.x.num ))
- (( grab i ))
(( done ))
`, "[1 2]"},
	}
	for _, mode := range rootRefModes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				src := doc + "out:\n(( for x in items ))\n" + tc.body + "(( done ))\n"
				data := runRootRefMerge(t, mode.opts, src)
				if got := fmt.Sprint(data["out"]); got != tc.want {
					t.Fatalf("out = %v, want %s", got, tc.want)
				}
			})
		}
	}
}
