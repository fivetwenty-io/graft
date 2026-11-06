package graft_test

import (
	"fmt"
	"strings"
	"testing"
)

// emptySegmentDoc holds the keys that each empty-segment path would
// reach if graft dropped the empty segment, as spruce does, so a path
// that slipped through would resolve rather than fail.
const emptySegmentDoc = `
meta:
  a: 1
  "5abc": five
list: [one]
"5abc": top-five
`

// TestEmptyPathSegmentIsAnError pins that a reference path with an empty
// segment fails with one message that names the problem and quotes none
// of the path, wherever a reference can sit. Spruce drops the empty
// segment and reads the path without it, which graft refuses on purpose.
func TestEmptyPathSegmentIsAnError(t *testing.T) {
	paths := []string{
		"meta..a",
		"meta...a",
		"$..list",
		"$.meta..a",
		"meta..",
		"list..[0]",
		".meta",
		".5abc",
	}
	shapes := []struct {
		name string
		expr func(path string) string
	}{
		{"grab", func(p string) string { return "(( grab " + p + " ))" }},
		{"concat", func(p string) string { return `(( concat "v=" ` + p + ` "!" ))` }},
		{"left of ||", func(p string) string { return "(( grab " + p + ` || "fallback" ))` }},
		{"right of ||", func(p string) string { return "(( grab missing.key || " + p + " ))" }},
	}
	for _, mode := range slashModes {
		for _, sh := range shapes {
			for _, p := range paths {
				t.Run(mode.name+"/"+sh.name+"/"+p, func(t *testing.T) {
					_, err := evalSlashDoc(t, mode.opts, emptySegmentDoc+"result: "+sh.expr(p)+"\n")
					if err == nil {
						t.Fatalf("expected an error")
					}
					if !strings.Contains(err.Error(), "a reference path has an empty segment") {
						t.Fatalf("error %q does not name the empty segment", err.Error())
					}
					if strings.Contains(err.Error(), p) {
						t.Fatalf("error %q quotes the path", err.Error())
					}
				})
			}
		}
	}
}

// TestPathsNextToAnEmptySegmentStillRead pins the shapes next to an
// empty segment that still read, so the check stays on empty segments.
// A single trailing dot reads the map before it, as spruce reads it, and
// a fraction with no integer part is still a number.
func TestPathsNextToAnEmptySegmentStillRead(t *testing.T) {
	cases := []struct {
		expr string
		want string
	}{
		{"(( grab meta. ))", "map[5abc:five a:1]"},
		{"(( grab $.meta.a ))", "1"},
		{"(( grab meta.a ))", "1"},
		{`(( concat "v=" .5 ))`, "v=0.5"},
		{"(( .5 * 2 ))", "1"},
		{"(( grab list.[0] ))", "one"},
	}
	for _, mode := range slashModes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.expr, func(t *testing.T) {
				d, err := evalSlashDoc(t, mode.opts, emptySegmentDoc+"result: "+tc.expr+"\n")
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				got, err := d.Get("result")
				if err != nil {
					t.Fatalf("failed to read result: %v", err)
				}
				if fmt.Sprint(got) != tc.want {
					t.Fatalf("result = %v (%T), want %q", got, got, tc.want)
				}
			})
		}
	}
}
