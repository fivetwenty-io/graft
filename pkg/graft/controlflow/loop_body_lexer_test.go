package controlflow

import (
	"fmt"
	"testing"
)

// loopBodyDoc is the document every loop-body shape below runs against.
// The top level has its own "svc", and the loop binds "svc" to the one
// element of items, so each shape shows which of the two it reads.
const loopBodyDoc = `svc: {name: top, ip: 9.9.9.9, num: 3, flag: false}
items: [{name: n0, ip: 1.1.1.1, num: 2, flag: true}]
jobs: [{name: svc, ip: 7.7.7.7}, {name: n0, ip: 8.8.8.8}]
lookup: {n0: L-n0, top: L-top}
svc-1: dash-key
svc/0: slash-zero
"/svc": lead-slash
"a/$X+y": slash-dollar-plus
"1e+5/2": exp-slash
"1": {"5e3/2": dec-exp-slash}
`

// TestLoopBodyReadsLikeTheLexer pins that a loop body reads each
// expression the way the expression lexer reads it outside a loop. A
// name the lexer reads as a reference from the top of the document is
// the loop variable. An environment variable, a selector value, a key
// that only starts with the name, and a "$." path all keep the meaning
// they have outside the loop.
func TestLoopBodyReadsLikeTheLexer(t *testing.T) {
	t.Setenv("svc", "from-env")
	t.Setenv("X", "XV")
	cases := []struct {
		name, varName, expr, want string
	}{
		{"bare variable", "svc", "grab svc.name", "n0"},
		{"dotted tail", "svc", "grab svc.ip", "1.1.1.1"},
		{"concat on both sides of a string", "svc", `concat svc.name "/" svc.name`, "n0/n0"},
		{"spaced division", "svc", "4 / svc.num", "2"},
		{"left of ||", "svc", "svc.nope || svc.name", "n0"},
		{"ternary on the variable", "svc", `svc.flag ? "yes" : "no"`, "yes"},
		{"bracket index", "svc", "grab lookup[svc.name]", "L-n0"},
		{"environment variable", "svc", "grab $svc", "from-env"},
		{"selector value in a dotted path", "svc", "grab jobs.name=svc.ip", "7.7.7.7"},
		{"selector value in a bracket", "svc", "grab jobs[name=svc].ip", "7.7.7.7"},
		{"dollar plus segment", "y", "grab a/$X+y", "slash-dollar-plus"},
		{"leading slash", "svc", "grab /svc", "lead-slash"},
		{"leading slash after a modifier", "svc", "grab:nocache /svc", "lead-slash"},
		{"modifier named like the variable", "nocache", "grab:nocache /svc", "lead-slash"},
		{"unspaced subtraction is one key", "svc", "grab svc-1", "dash-key"},
		{"slash key that starts with the name", "svc", "grab svc/0", "slash-zero"},
		{"exponent slash path with a variable named e", "e", "grab 1e+5/2", "exp-slash"},
		{"decimal exponent slash path with a variable named e", "e", "grab 1.5e3/2", "dec-exp-slash"},
		{"exponent slash path beside a variable named e", "e", `concat 1e+5/2 "-" e.name`, "exp-slash-n0"},
		{"decimal exponent slash path beside a variable named e", "e", `concat 1.5e3/2 "-" e.name`, "dec-exp-slash-n0"},
		{"root path named like the variable", "svc", "grab $.svc.name", "top"},
		{"root path beside the variable", "svc", `concat $.svc.name "-" svc.name`, "top-n0"},
	}
	for _, mode := range rootRefModes {
		for _, tc := range cases {
			t.Run(mode.name+"/"+tc.name, func(t *testing.T) {
				src := loopBodyDoc + fmt.Sprintf("out:\n(( for %s in items ))\n- '(( %s ))'\n(( done ))\n", tc.varName, tc.expr)
				data := runRootRefMerge(t, mode.opts, src)
				if got := fmt.Sprint(data["out"]); got != "["+tc.want+"]" {
					t.Fatalf("out = %v, want [%s]", got, tc.want)
				}
			})
		}
	}
}

// TestLoopBodyRootPathIsTheDocumentRoot pins that "$.svc" in a loop over
// svc reads the top-level svc, while a plain "svc" in the same body is
// the loop variable.
func TestLoopBodyRootPathIsTheDocumentRoot(t *testing.T) {
	src := loopBodyDoc + `out:
(( for svc in items ))
- root: (( grab $.svc ))
  bound: (( grab svc ))
(( done ))
`
	for _, mode := range rootRefModes {
		t.Run(mode.name, func(t *testing.T) {
			data := runRootRefMerge(t, mode.opts, src)
			out, ok := data["out"].([]interface{})
			if !ok || len(out) != 1 {
				t.Fatalf("out = %#v, want one entry", data["out"])
			}
			entry, ok := out[0].(map[string]interface{})
			if !ok {
				t.Fatalf("out[0] = %#v, want a map", out[0])
			}
			if got := fmt.Sprint(entry["root"]); got != "map[flag:false ip:9.9.9.9 name:top num:3]" {
				t.Fatalf("root = %s, want the top-level svc", got)
			}
			if got := fmt.Sprint(entry["bound"]); got != "map[flag:true ip:1.1.1.1 name:n0 num:2]" {
				t.Fatalf("bound = %s, want the loop element", got)
			}
		})
	}
}
