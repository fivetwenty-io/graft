package eval

import (
	"testing"

	"github.com/fivetwenty-io/graft/internal/query/ast"
)

// mustReduce parses src, which must be a reduce, and returns it.
func mustReduce(t *testing.T, src string) *ast.Reduce {
	t.Helper()
	n, ok := mustParse(t, src).(*ast.Reduce)
	if !ok {
		t.Fatalf("%s: not a reduce", src)
	}
	return n
}

// TestOwnsStateShapes pins which reduce updates hold their state in an
// Owner. Every update that is let in may only pass the state from one
// assignment to the next, and every one that is kept out could give the
// state to something that outlives the update.
func TestOwnsStateShapes(t *testing.T) {
	owned := []string{
		`.x = 1`,
		`. = .`,
		`.a.b = .`,
		`.x = .`,
		`.["k\($i)"] = $i`,
		`.[$i] = 0`,
		`.[$i + 1] = .[0]`,
		`.[0] = .[0:]`,
		`.a += 1`,
		`.a -= 1`,
		`.a *= 2`,
		`.a /= 2`,
		`.a %= 2`,
		`.a //= 0`,
		`.a |= . + 1`,
		`.a = $i | .b = .a`,
		`.a = $i | .b += . | .c //= $i`,
		`.a = (if $i > 1 then . elif $i > 0 then 1 else .b end)`,
		`.a = (.b // .c)`,
		`.a = -$i`,
		`.a = "\(.b)-\($i)"`,
		`.a = (.b | .c)`,
		`.a = (.b and .c)`,
	}
	held := []string{
		// The right side builds a container that may hold the state.
		`.a = [.]`,
		`.a = {b: .}`,
		`.a = {(.k): 1}`,
		`.a = (. + {x: 1} | .x)`,
		// A call can do anything with its input.
		`.a = length`,
		`.a = first(.)`,
		`.a = ([.] | first)`,
		`.a = $__loc__`,
		// A binding, a generator, a try, or a nested reduce can keep it.
		`.a = (. as $s | $s)`,
		`.a = (1, 2)`,
		`.a = .[]`,
		`.a = ..`,
		`.a = (try . catch 0)`,
		`.a = (label $l | .)`,
		`.a = (reduce range(1) as $j (.; .))`,
		`.a = (.x = 1)`,
		`.a = .b?`,
		`.a = .[0]?`,
		`.a = .[1:]?`,
		`.a = (.b | f)`,
		// The place must be one path of plain fields and indexes.
		`.a? = 1`,
		`.[0]? = 1`,
		`.[] = 1`,
		`.a[1:2] = [1]`,
		`.[length] = 1`,
		`(.a, .b) = 1`,
		`.a.b |= empty`,
		// An update that is not a pipe of assignments.
		`. + [$i]`,
		`.a = 1 | . + {}`,
		`.a = 1, .b = 2`,
		`del(.a)`,
		`setpath(["a"]; 1)`,
	}
	ev := evaluator{state: &state{}}
	for _, u := range owned {
		src := `reduce range(3) as $i ({}; ` + u + `)`
		if !ev.ownsState(mustReduce(t, src)) {
			t.Errorf("%s: want an Owner, got none", src)
		}
	}
	for _, u := range held {
		src := `def f: .; reduce range(3) as $i ({}; ` + u + `)`
		n := mustParse(t, src)
		r := findReduce(n)
		if r == nil {
			t.Fatalf("%s: no reduce", src)
		}
		if ev.ownsState(r) {
			t.Errorf("%s: want no Owner, got one", src)
		}
	}

	// Patterns with "?//" retry the update on the state a failed attempt
	// left, so they never own it.
	if ev.ownsState(mustReduce(t, `reduce .[] as [$a] ?// $a ({}; .x = $a)`)) {
		t.Error(`"?//" patterns: want no Owner, got one`)
	}
	// Tracking paths or cursors carries the state's identity elsewhere.
	simple := mustReduce(t, `reduce range(3) as $i ({}; .x = $i)`)
	if (evaluator{state: &state{}, paths: true}).ownsState(simple) {
		t.Error("paths: want no Owner, got one")
	}
	if (evaluator{state: &state{track: true}}).ownsState(simple) {
		t.Error("cursors: want no Owner, got one")
	}
}

// findReduce returns the reduce that follows n's definitions.
func findReduce(n ast.Node) *ast.Reduce {
	switch n := n.(type) {
	case *ast.Reduce:
		return n
	case *ast.FuncDef:
		return findReduce(n.Rest)
	}
	return nil
}
