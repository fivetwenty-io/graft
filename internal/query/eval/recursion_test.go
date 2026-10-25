package eval

import "testing"

func TestClosuresAndRecursion(t *testing.T) {
	wantOutputs(t, `def f(g): g | g; 2 | f(. * 3)`, `null`, `18`)
	wantOutputs(t, `def f($a; $b): $a + $b; f(1, 2; 10)`, `null`, `11`, `12`)
	wantOutputs(t, `def fac: if . <= 1 then 1 else . * (. - 1 | fac) end; 10 | fac`, `null`, `3628800`)
}

func TestClosureScoping(t *testing.T) {
	wantOutputs(t, `def f: def g: 3; g; f`, `null`, `3`)
	wantOutputs(t, `def f: 1; def g: f; def f: 2; [g, f]`, `null`, `[1,2]`)
	wantOutputs(t, `1 as $x | def f: $x; 2 as $x | [f, $x]`, `null`, `[1,2]`)
	wantOutputs(t, `def f(g): def h: g; h; 5 | f(. + 1)`, `null`, `6`)
	wantOutputs(t, `def f($a): a + $a; f(1, 2)`, `null`, `2`, `3`, `3`, `4`)
	wantOutputs(t, `def f(x): x | x; [[1]] | f(.[])`, `null`, `1`)
	wantOutputs(t, `def f: 1; def f(a): 2; [f, f(0)]`, `null`, `[1,2]`)
}

func TestDeepRecursionIsBoundedByUs(t *testing.T) {
	wantOutputs(t, `def f: if . == 0 then 0 else (. - 1 | f) + 1 end; 5000 | f`, `null`, `5000`)
	wantOutputs(t, `def f: if . > 1000000 then . else . + 1 | f end; 0 | f`, `null`, `1000001`) // tail call, constant depth
	wantError(t, `def f: if . == 0 then 0 else (. - 1 | f) + 1 end; 100000 | f`, `null`, "exceeds depth limit of 10000")
	wantErrorWithCtx(t, ctxWithDepth(50), `def f: if . == 0 then 0 else (. - 1 | f) + 1 end; 100 | f`, `null`, "exceeds depth limit of 50")
	wantErrorWithCtx(t, ctxCancelled(), `def f: . + 1 | f; 0 | f`, `null`, "context canceled")
}

func TestTailCallsStayCorrect(t *testing.T) {
	// A tail call in one branch of a comma still sees the other branch's
	// outputs first.
	wantOutputs(t, `def f: if . >= 3 then . else ., (. + 1 | f) end; [0 | f]`, `null`, `[0,1,2,3]`)
	// Value parameters change on every round.
	wantOutputs(t, `def f($n; $acc): if $n == 0 then $acc else f($n - 1; $acc + $n) end; f(100000; 0)`, `null`, `5000050000`)
	// A filter parameter is re-bound on every round.
	wantOutputs(t, `def f(g): if . > 3 then . else g | f(g) end; 0 | f(. + 1)`, `null`, `4`)
	// A self call under a nested def of the same name is not a tail call
	// of the outer one.
	wantOutputs(t, `def f: if . > 2 then . else def f: . + 10; f end; 0 | f`, `null`, `10`)
	// A try around the call keeps it out of tail position.
	wantOutputs(t, `def f: if . > 2 then error("x") else try (. + 1 | f) catch "c" end; 0 | f`, `null`, `"c"`)
	wantErrorWithCtx(t, ctxCancelled(), `def f: if . > 1000000 then . else . + 1 | f end; 0 | f`, `null`, "context canceled")
	// jq's own loops reach the self call through a filter parameter, so
	// they loop when the parameter's body has one output.
	wantOutputs(t, `def until(cond; update): def _until: if cond then . else (update | _until) end; _until; 0 | until(. >= 20000; . + 1)`, `null`, `20000`)
	wantOutputs(t, `def while(cond; update): def _while: if cond then ., (update | _while) else empty end; _while; reduce (0 | while(. < 20000; . + 1)) as $x (0; . + 1)`, `null`, `20000`)
	wantOutputs(t, `def repeat(f): def _repeat: ., (f | _repeat); _repeat; [limit(20000; 0 | repeat(. + 1))] | .[19999]`, `null`, `19999`)
	wantOutputs(t, `def select(f): if f then . else empty end; def recurse(f; cond): def r: ., (f | select(cond) | r); r; reduce (0 | recurse(. + 1; . < 20000)) as $x (0; . + 1)`, `null`, `20000`)
	// A parameter with more than one output keeps the call out of tail
	// position, and the loop still runs.
	wantOutputs(t, `def repeat(f): def _repeat: ., (f | _repeat); _repeat; [limit(5; 0 | repeat(. + 1, . + 10))]`, `null`, `[0,1,2,3,4]`)
	// A def that shadows the parameter between its use and the call does
	// not stand in for it.
	wantOutputs(t, `def f(g): if . > 3 then . else g | def g: . + 1; f(g) end; 0 | f(. + 1, . + 2)`, `null`, `4`, `4`)
	// Recursion through a parameter that is not in tail position still
	// hits the depth limit.
	wantError(t, `def f(g): [g | f(g)]; 0 | f(. + 1)`, `null`, "exceeds depth limit of 10000")
}
