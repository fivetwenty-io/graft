package controlflow

import (
	"fmt"
	"testing"
)

// TestHeaderSubjectReferenceShapes pins that a for-loop or case subject
// written as any one reference the expression lexer reads is a grab of
// that reference. That covers a leading '/', a "$[" bracket, an exponent
// number before a '/', and a bracket index, as well as the plain names
// and "$." paths that always worked.
func TestHeaderSubjectReferenceShapes(t *testing.T) {
	const doc = `list: [L1, L2]
/list: [S1, S2]
k: list
lists:
- [A, B]
- [C]
1e+5/2: [E]
kind: a
/kind: b
kk: kind
names: [list]
`
	cases := []struct {
		name string
		body string
		want string
	}{
		{"for over a leading slash", `out:
(( for y in /list ))
- (( grab y ))
(( done ))
`, "[S1 S2]"},
		{"for over a dollar bracket", `out:
(( for y in $[k] ))
- (( grab y ))
(( done ))
`, "[L1 L2]"},
		{"for over an exponent path", `out:
(( for y in 1e+5/2 ))
- (( grab y ))
(( done ))
`, "[E]"},
		{"for over a bracket index", `out:
(( for y in lists[0] ))
- (( grab y ))
(( done ))
`, "[A B]"},
		{"for over a plain name", `out:
(( for y in list ))
- (( grab y ))
(( done ))
`, "[L1 L2]"},
		{"for over a root path", `out:
(( for y in $.list ))
- (( grab y ))
(( done ))
`, "[L1 L2]"},
		{"case on a dollar bracket", `(( case $[kk] ))
(( when "a" ))
out: plain-kind
(( when "b" ))
out: slash-kind
(( esac ))
`, "plain-kind"},
		{"case on a leading slash", `(( case /kind ))
(( when "a" ))
out: plain-kind
(( when "b" ))
out: slash-kind
(( esac ))
`, "slash-kind"},
		{"nested for over a bracket of the loop variable", `out:
(( for n in names ))
(( for y in $[n] ))
- (( grab y ))
(( done ))
(( done ))
`, "[L1 L2]"},
	}
	for _, mode := range rootRefModes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				data := runRootRefMerge(t, mode.opts, doc+tc.body)
				if got := fmt.Sprint(data["out"]); got != tc.want {
					t.Fatalf("out = %v, want %s", got, tc.want)
				}
			})
		}
	}
}

// TestHeaderSubjectRootRuleInNestedLoop pins that a "$." subject nested
// in a loop over x still reads the top-level x after the subject check
// moved to the lexer, while a bracket subject over the loop variable
// reads the key that the variable names.
func TestHeaderSubjectRootRuleInNestedLoop(t *testing.T) {
	const doc = `x:
  list: [t1, t2]
xs:
- list: [e1]
top: [r1]
`
	cases := []struct {
		name string
		body string
		want string
	}{
		{"root path", `(( for y in $.x.list ))
- (( grab y ))
(( done ))
`, "[t1 t2]"},
		{"bound path", `(( for y in x.list ))
- (( grab y ))
(( done ))
`, "[e1]"},
		{"leading slash beside the binding", `(( for y in /top || x.list ))
- (( grab y ))
(( done ))
`, "[e1]"},
	}
	for _, mode := range rootRefModes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				src := doc + "out:\n(( for x in xs ))\n" + tc.body + "(( done ))\n"
				data := runRootRefMerge(t, mode.opts, src)
				if got := fmt.Sprint(data["out"]); got != tc.want {
					t.Fatalf("out = %v, want %s", got, tc.want)
				}
			})
		}
	}
}
