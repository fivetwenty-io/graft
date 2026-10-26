package jq_test

import "testing"

// These tests pin, through whole programs, that a reduce which changes
// its state in place never lets anything see the change. Each program
// would print something else if the state were changed after it reached
// a variable, a path, a constructed container, another part of the
// state, or a caught error. The outputs are jq 1.8.2's, each run by hand
// on the input {"a":1}.

// stateCase is a program with the outputs jq prints, or the message of
// the error it fails with when err is set.
type stateCase struct {
	prog string
	want []string
	err  string
}

// pinState runs every case on {"a":1}.
func pinState(t *testing.T, cases []stateCase) {
	t.Helper()
	for _, c := range cases {
		if c.err != "" {
			wantError(t, c.prog, `{"a":1}`, c.err)
			continue
		}
		wantOutputs(t, c.prog, `{"a":1}`, c.want...)
	}
}

// TestReduceStateCycles looks a fixed depth into a state that was set
// inside itself. A state changed in place there would contain itself,
// so these probes read an object where jq reads null, and they stay
// bounded where printing the state would recurse without end. They run
// first so that they fail cleanly before a later test prints a cycle.
func TestReduceStateCycles(t *testing.T) {
	pinState(t, []stateCase{
		{`reduce range(2) as $i ({a:{}}; .a.b = .) | .a.b.a.b.a.b.a.b | type`, []string{`"null"`}, ``},
		{`reduce range(2) as $i ({}; .a = [.]) | .a[0].a[0].a[0] | type`, []string{`"null"`}, ``},
		{`reduce range(2) as $i ({}; .a = {b: .}) | .a.b.a.b.a.b | type`, []string{`"null"`}, ``},
		{`def w: [.]; reduce range(2) as $i ({}; .a = w) | .a[0].a[0].a[0] | type`, []string{`"null"`}, ``},
		{`reduce range(2) as $i ({}; .a = (. as $s | [$s])) | .a[0].a[0].a[0] | type`, []string{`"null"`}, ``},
		{`reduce range(2) as $i ({}; .a = (try [.] catch 0)) | .a[0].a[0].a[0] | type`, []string{`"null"`}, ``},
		{`reduce range(2) as $i ({}; .a = (label $l | [.])) | .a[0].a[0].a[0] | type`, []string{`"null"`}, ``},
		{`reduce range(2) as $i ({}; .a = (reduce range(1) as $j (.; [.]))) | .a[0].a[0].a[0] | type`, []string{`"null"`}, ``},
	})
}

// TestReduceStateSelfReference sets the state inside itself, at the top
// and below it, where a change made in place would build a cycle.
func TestReduceStateSelfReference(t *testing.T) {
	pinState(t, []stateCase{
		{`reduce range(3) as $i ({}; .x = .)`, []string{`{"x":{"x":{"x":{}}}}`}, ``},
		{`reduce range(3) as $i ({a:{}}; .a.b = .)`, []string{`{"a":{"b":{"a":{"b":{"a":{"b":{"a":{}}}}}}}}`}, ``},
		{`reduce range(3) as $i ({}; .["k\($i)"] = .)`, []string{`{"k0":{},"k1":{"k0":{}},"k2":{"k0":{},"k1":{"k0":{}}}}`}, ``},
		{`reduce range(3) as $i ([]; .[length] = .)`, []string{`[[],[[]],[[],[[]]]]`}, ``},
		{`reduce range(3) as $i ([]; .[$i] = $i | .[$i+10] = .)`, []string{`[0,1,2,null,null,null,null,null,null,null,[0],[0,1,null,null,null,null,null,null,null,null,[0]],[0,1,2,null,null,null,null,null,null,null,[0],[0,1,null,null,null,null,null,null,null,null,[0]]]]`}, ``},
		{`reduce range(3) as $i ({}; .a = $i | . = .)`, []string{`{"a":2}`}, ``},
		{`reduce range(3) as $i ({}; .a = $i | . |= .)`, []string{`{"a":2}`}, ``},
		{`reduce range(3) as $i ({}; .a = $i | .b |= .)`, []string{`{"a":2,"b":null}`}, ``},
		{`reduce range(3) as $i ({}; .a = $i | .b += .)`, []string{`{"a":2,"b":{"a":2,"b":{"a":1,"b":{"a":0}}}}`}, ``},
		{`reduce range(3) as $i ({}; .a //= $i | .b //= .)`, []string{`{"a":0,"b":{"a":0}}`}, ``},
		{`reduce range(3) as $i (null; .a = $i | .b = .)`, []string{`{"a":2,"b":{"a":2,"b":{"a":1,"b":{"a":0}}}}`}, ``},
		{`reduce range(20) as $i ([]; .[$i] = $i | .[$i+1] = .) | .[19] | length`, []string{`19`}, ``},
	})
}

// TestReduceStatePartOfState keeps a part of the state, such as a slice
// or a child, beside it and then changes the state.
func TestReduceStatePartOfState(t *testing.T) {
	pinState(t, []stateCase{
		{`reduce range(3) as $i ([0,0,0]; .[0] = .[0:])`, []string{`[[[[0,0,0],0,0],0,0],0,0]`}, ``},
		{`reduce range(2) as $i ([0,0,0]; .[0] = $i + 5 | .[$i+1] = .[0:1])`, []string{`[6,[5],[6]]`}, ``},
		{`reduce range(2) as $i ([0,0,0,0,0,0,0,0,0,0]; .[0] = $i + 5 | .[$i+1] = .[0:2])`, []string{`[6,[5,0],[6,[5,0]],0,0,0,0,0,0,0]`}, ``},
		{`reduce range(2) as $i ({a:[1,2,3]}; .a[0] = $i | .b = .a[0:2])`, []string{`{"a":[1,2,3],"b":[1,2]}`}, ``},
		{`reduce range(3) as $i ({a:{}}; .a.x = $i | .b = .a)`, []string{`{"a":{"x":2},"b":{"x":2}}`}, ``},
		{`reduce range(3) as $i ({x:[]}; .x[$i] = $i | .y = .x[1:])`, []string{`{"x":[0,1,2],"y":[1,2]}`}, ``},
		{`reduce range(3) as $i ([[0]]; .[0][0] = $i | .[1] = .[0])`, []string{`[[2],[2]]`}, ``},
		{`reduce range(3) as $i ({}; .a = $i | .b = (. + {x:1}))`, []string{`{"a":2,"b":{"a":2,"b":{"a":1,"b":{"a":0,"x":1},"x":1},"x":1}}`}, ``},
		{`reduce range(3) as $i ([1]; .[0] = $i | .[1] = (. + [9]))`, []string{`[2,[2,[1,[0,9],9],9]]`}, ``},
		{`reduce range(3) as $i ({}; .a = $i | .b = ({} + .))`, []string{`{"a":2,"b":{"a":2,"b":{"a":1,"b":{"a":0}}}}`}, ``},
		{`reduce range(3) as $i ({}; .a = $i | .b = (.zz // .))`, []string{`{"a":2,"b":{"a":2,"b":{"a":1,"b":{"a":0}}}}`}, ``},
		{`reduce range(3) as $i ({}; .a = $i | .b = (if .a > 1 then . else .a end))`, []string{`{"a":2,"b":{"a":2,"b":1}}`}, ``},
	})
}

// TestReduceStateVariables holds the state, its start, or the input in a
// variable and reads the variable again after the reduce.
func TestReduceStateVariables(t *testing.T) {
	pinState(t, []stateCase{
		{`{a:1} as $s | reduce range(3) as $i ($s; .a = $i) | [., $s]`, []string{`[{"a":2},{"a":1}]`}, ``},
		{`[1,2] as $s | reduce range(3) as $i ($s; .[0] = $i) | [., $s]`, []string{`[[2,2],[1,2]]`}, ``},
		{`. as $s | reduce range(3) as $i (.; .a = $i | .["k\($i)"] = $i) | [., $s]`, []string{`[{"a":2,"k0":0,"k1":1,"k2":2},{"a":1}]`}, ``},
		{`reduce range(3) as $i (.; .a += $i) as $x | [$x, .]`, []string{`[{"a":4},{"a":1}]`}, ``},
		{`[reduce range(3) as $i (.; .a += $i), .]`, []string{`[{"a":4},{"a":1}]`}, ``},
		{`reduce range(3) as $i ({}; .a = $i) as $r | [$r, ($r | .b = 1), $r]`, []string{`[{"a":2},{"a":2,"b":1},{"a":2}]`}, ``},
		{`reduce (range(3) | {k: .}) as $o ({}; .x = $o | .y = $o.k)`, []string{`{"x":{"k":2},"y":2}`}, ``},
		{`def f: reduce range(3) as $i (.; .a = $i); [f, f, .]`, []string{`[{"a":2},{"a":2},{"a":1}]`}, ``},
	})
}

// TestReduceStatePaths reads the state through path, paths, getpath,
// $__loc__, and interpolation.
func TestReduceStatePaths(t *testing.T) {
	pinState(t, []stateCase{
		{`reduce range(3) as $i ({}; .a = $i) | [path(..)], [paths], getpath(["a"])`, []string{`[[],["a"]]`, `[["a"]]`, `2`}, ``},
		{`[path(reduce range(3) as $i (.; .a = $i))]`, nil, `Invalid path expression with result {"a":2}`},
		{`reduce range(3) as $i ({}; .a = $i | .b = $__loc__)`, []string{`{"a":2,"b":{"file":"<top-level>","line":1}}`}, ``},
		{`reduce range(3) as $i ({}; .a = $i | .s = "\(.)")`, []string{`{"a":2,"s":"{\"a\":2,\"s\":\"{\\\"a\\\":1,\\\"s\\\":\\\"{\\\\\\\"a\\\\\\\":0}\\\"}\"}"}`}, ``},
	})
}

// TestReduceStateConstruction wraps the state in an array or an object,
// directly or through a call, a binding, try, label, or a nested reduce.
func TestReduceStateConstruction(t *testing.T) {
	pinState(t, []stateCase{
		{`reduce range(3) as $i ({}; .a = [.])`, []string{`{"a":[{"a":[{"a":[{}]}]}]}`}, ``},
		{`reduce range(3) as $i ({}; .a = {b: .})`, []string{`{"a":{"b":{"a":{"b":{"a":{"b":{}}}}}}}`}, ``},
		{`def w: [.]; reduce range(3) as $i ({}; .a = w)`, []string{`{"a":[{"a":[{"a":[{}]}]}]}`}, ``},
		{`reduce range(3) as $i ({}; .a = ([.] | first) | .b = $i)`, []string{`{"a":{"a":{"a":{},"b":0},"b":1},"b":2}`}, ``},
		{`reduce range(2) as $i ({}; . as $s | .x = $i | .y = $s)`, []string{`{"x":1,"y":{"x":0,"y":{}}}`}, ``},
		{`reduce range(3) as $i ({}; .a = (. as $s | [$s]))`, []string{`{"a":[{"a":[{"a":[{}]}]}]}`}, ``},
		{`reduce range(3) as $i ({}; .a = (try [.] catch 0))`, []string{`{"a":[{"a":[{"a":[{}]}]}]}`}, ``},
		{`reduce range(3) as $i ({}; .a = (label $l | [.]))`, []string{`{"a":[{"a":[{"a":[{}]}]}]}`}, ``},
		{`reduce range(3) as $i ({}; .a = ([.], 1) | .b = $i)`, []string{`{"a":1,"b":2}`}, ``},
		{`reduce range(3) as $i ({}; .a = (reduce range(1) as $j (.; [.])))`, []string{`{"a":[{"a":[{"a":[{}]}]}]}`}, ``},
		{`reduce range(3) as $i ({}; .a = [.[]?])`, []string{`{"a":[[[]]]}`}, ``},
		{`reduce range(3) as $i ({}; .a = [..])`, []string{`{"a":[{"a":[{"a":[{}]},[{}],{}]},[{"a":[{}]},[{}],{}],{"a":[{}]},[{}],{},[{}],{},{}]}`}, ``},
		{`reduce range(3) as $i ({}; .a = (.x = 1))`, []string{`{"a":{"a":{"a":{"x":1},"x":1},"x":1}}`}, ``},
	})
}

// TestReduceStateGenerators gives the right side several values or none,
// and stops the reduce with limit and with label and break.
func TestReduceStateGenerators(t *testing.T) {
	pinState(t, []stateCase{
		{`[reduce range(3) as $i ({}; .a = ($i, $i + 10) | .b = .a)]`, []string{`[{"a":12,"b":12}]`}, ``},
		{`reduce range(3) as $i ({}; .a = (1, 2) | .b = .)`, []string{`{"a":2,"b":{"a":2,"b":{"a":2,"b":{"a":2}}}}`}, ``},
		{`reduce range(3) as $i ({}; .a = empty)`, []string{`null`}, ``},
		{`[limit(1; reduce range(5) as $i ({}; .["k\($i)"] = $i))]`, []string{`[{"k0":0,"k1":1,"k2":2,"k3":3,"k4":4}]`}, ``},
		{`[label $out | reduce (range(5) | if . == 3 then break $out else . end) as $i ({}; .["k\($i)"] = $i)]`, []string{`[]`}, ``},
		{`[label $out | (reduce range(5) as $i ({}; .["k\($i)"] = $i) | ., break $out)]`, []string{`[{"k0":0,"k1":1,"k2":2,"k3":3,"k4":4}]`}, ``},
	})
}

// TestReduceStateErrors fails an update part way through a step, under
// try, under a source that catches errors, and under "?//" patterns that
// retry the step.
func TestReduceStateErrors(t *testing.T) {
	pinState(t, []stateCase{
		{`try (reduce range(3) as $i ({a:0}; .a = $i | .b = (. + 1))) catch .`, []string{`"object ({\"a\":0}) and number (1) cannot be added"`}, ``},
		{`try (reduce range(3) as $i ({a:0}; .a = $i | .b.c = (if $i == 2 then .a.x else 1 end))) catch .`, []string{`"Cannot index number with string (\"x\")"`}, ``},
		{`reduce range(3) as $i ({a:0}; .a = $i | .b = ((. + 1) // "alt"))`, nil, `object ({"a":0}) and number (1) cannot be added`},
		{`reduce (try (1,2,3) catch "c") as $i ({}; .a = $i | .b = (if $i == 2 then .a.x else 1 end))`, nil, `Cannot index number with string ("x")`},
		{`try (reduce ((1,2,3) // 0) as $i ({}; .a = $i | .b = (if $i == 2 then .a.x else 1 end))) catch .`, []string{`"Cannot index number with string (\"x\")"`}, ``},
		{`try (reduce ((1,2,3)?) as $i ({}; .a = $i | .b = (if $i == 2 then .a.x else 1 end))) catch .`, []string{`"Cannot index number with string (\"x\")"`}, ``},
		{`try (reduce (label $l | 1,2,3) as $i ({}; .a = $i | .b = (if $i == 2 then .a.x else 1 end))) catch .`, []string{`"Cannot index number with string (\"x\")"`}, ``},
		{`try (reduce (try error("boom") catch (1,2,3)) as $i ({}; .a = $i | .b = (if $i == 2 then .a.x else 1 end))) catch .`, []string{`"Cannot index number with string (\"x\")"`}, ``},
		{`reduce ([1], {"a":"x"}, [2]) as [$x] ?// {a:$x} ({}; .a = $x | .b = (.a + 1))`, nil, `string ("x") and number (1) cannot be added`},
		{`reduce [[1]] as [$x] ?// $y ({n:0}; .n = .n + 1 | .m = ($x + "s"))`, []string{`{"n":1,"m":"s"}`}, ``},
		{`[.[] as [$x] ?// $x | reduce range(2) as $i ({}; .a = $x | .b = (.a + 1))]`, []string{`[{"a":1,"b":2}]`}, ``},
	})
}
