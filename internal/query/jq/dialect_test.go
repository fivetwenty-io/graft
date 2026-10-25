package jq_test

import (
	"errors"
	goruntime "runtime"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/query/jq"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

func TestJQTruthinessAndErrors(t *testing.T) {
	wantOutputs(t, `[.[] | if . then 1 else 0 end]`, `[null,false,0,"",[],{}]`, `[0,0,1,1,1,1]`)
	wantError(t, `.a`, `[1]`, `Cannot index array with string ("a")`)
	wantError(t, `{} + 1`, `null`, `object ({}) and number (1) cannot be added`)
	wantError(t, `error`, `{"a":1}`, `{"a":1} (not a string)`)
	wantError(t, `undefined_fn`, `null`, `undefined_fn/0 is not defined`)
}

func TestJQIndexing(t *testing.T) {
	wantOutputs(t, `.[-0.5], .[-1.5], .[1.7], .[nan]`, `[1,2,3]`, `1`, `3`, `2`, `null`)
	wantOutputs(t, `.[[]], .[[1,2]]`, `[1,2,1,2]`, `[]`, `[0,2]`)
	wantOutputs(t, `.[1.2:2.5], .[-2:], .[:1]`, `[1,2,3,4]`, `[2,3]`, `[3,4]`, `[1]`)
	wantOutputs(t, `.[1:], .[:-1]`, `"aéb"`, `"éb"`, `"aé"`)
	wantOutputs(t, `.a, .[0], .[1:]`, `null`, `null`, `null`, `null`)
	wantError(t, `.[0]`, `"abc"`, `Cannot index string with number (0)`)
	wantError(t, `.[true]`, `null`, `Cannot index null with boolean (true)`)
	wantError(t, `.[{}]`, `[1]`, `Array/string slice indices must be integers`)
	wantError(t, `.["a":]`, `[1]`, `Array/string slice indices must be integers`)
	wantError(t, `.[]`, `1`, `Cannot iterate over number (1)`)
	wantOutputs(t, `[.[]?]`, `1`, `[]`)
	wantOutputs(t, `[..]`, `{"a":[1]}`, `[{"a":[1]},[1],1]`)
}

func TestJQAssignment(t *testing.T) {
	wantOutputs(t, `.[] |= empty`, `[1,2,3]`, `[]`)
	wantOutputs(t, `.[] |= (., 9)`, `[1,2]`, `[1,2]`)
	wantOutputs(t, `(.a, .b) = (1, 2)`, `{}`, `{"a":1,"b":1}`, `{"a":2,"b":2}`)
	wantOutputs(t, `.a += .b`, `{"a":1,"b":2}`, `{"a":3,"b":2}`)
	wantOutputs(t, `.[] -= 1, .[] *= 2, .[] /= 2, .[] %= 2`, `[3,4]`, `[2,3]`, `[6,8]`, `[1.5,2]`, `[1,0]`)
	wantOutputs(t, `.a //= 3, .b //= 3`, `{"a":false,"b":1}`, `{"a":3,"b":1}`, `{"a":false,"b":1}`)
	wantOutputs(t, `.[1] = 1`, `null`, `[null,1]`)
	wantOutputs(t, `.[1:] = ["x"]`, `[1,2,3]`, `[1,"x"]`)
	wantOutputs(t, `.a.b |= . + 1`, `{"a":{"b":1,"c":2}}`, `{"a":{"b":2,"c":2}}`)
	wantOutputs(t, `.[-1] = 9`, `[1,2]`, `[1,9]`)
	wantError(t, `.[-3] = 9`, `[1,2]`, `Out of bounds negative array index`)
	wantError(t, `.[1] = 1`, `{}`, `Cannot index object with number (1)`)
	wantError(t, `.[1:] = 1`, `[1]`, `A slice of an array can only be assigned another array`)
	wantOutputs(t, `with_entries(.value += 1)`, `{"a":1}`, `{"a":2}`)
}

func TestUpdateDeletesUnderTheCallersContext(t *testing.T) {
	// The paths an update deletes are sorted with the run's own context,
	// so its number mode applies there too.
	p, err := jq.Compile(`.[] |= empty`)
	if err != nil {
		t.Fatal(err)
	}
	ctx := runtime.NewCtx(jq.New(), runtime.NumbersExact)
	compared := 0
	compare := ctx.Compare
	ctx.Compare = func(a, b *value.Value) int {
		compared++
		return compare(a, b)
	}
	var got []string
	err = p.Run(ctx, p.Env, []*value.Value{mustJSON(t, `[1,2,3]`)}, func(v *value.Value, _ value.Path) error {
		got = append(got, runtime.JSON(v))
		return nil
	})
	if err != nil || strings.Join(got, " ") != "[]" {
		t.Fatalf("got %v, %v", got, err)
	}
	if compared == 0 {
		t.Errorf("the final delete did not compare paths through the run's context")
	}
}

func TestAssignUsesBuildersOncePerContainer(t *testing.T) {
	in := mustJSON(t, `[`+strings.Repeat("1,", 9999)+`1]`)
	var before, after goruntime.MemStats
	goruntime.ReadMemStats(&before)
	allocs := testing.AllocsPerRun(3, func() { runOne(t, `.[] |= . + 1`, in) })
	goruntime.ReadMemStats(&after)
	// Copying the array for each element would allocate about 800 MB per
	// run, so a few MB proves the array is copied once.
	if perRun := (after.TotalAlloc - before.TotalAlloc) / 4; perRun > 32<<20 {
		t.Fatalf("per-element container copies: %d bytes per run", perRun)
	}
	// The assignment itself costs nothing per element. The rest is the
	// evaluator's: the path to each element, the arithmetic call, and the
	// new number, about thirteen allocations for each element.
	if allocs > 150000 {
		t.Fatalf("allocations grew past linear: %v allocs", allocs)
	}
}

func TestJQCompileResolvesNamesStatically(t *testing.T) {
	cases := []struct{ prog, msg string }{
		{`[(1, break $f)?]`, `$*label-f is not defined`},
		{`$x`, `$x is not defined`},
		{`. as [$a, {($a): $b}] | $b`, `$a is not defined`},
		{`. as {a: $x, ($x): $y} | $y`, `$x is not defined`},
		{`map(1; 2)`, `map/2 is not defined`},
		{`reduce .[] as $x (0; . + $y)`, `$y is not defined`},
		{`def f($a): $b; f(1)`, `$b is not defined`},
	}
	for _, c := range cases {
		_, err := jq.Compile(c.prog)
		var ce *query.CompileError
		if !errors.As(err, &ce) {
			t.Errorf("%s: want a compile error, got %v", c.prog, err)
			continue
		}
		if ce.Msg != c.msg {
			t.Errorf("%s: got %q, want %q", c.prog, ce.Msg, c.msg)
		}
	}
	for _, prog := range []string{
		`label $f | 1, break $f`,
		`def f: g; 1`, // jq resolves only the definitions a program reaches
		`def f(g): g; f(.)`,
		`def f($a): $a, a; f(1)`,
		`def f: def g: f; 1; f`,
		`. as [$a] ?// $a | $a`,
		`. as {$a, b: [$c]} | $a, $c`,
		`$ENV, $ARGS, $__loc__`,
		`. as [$a, $b] | {($a): $b}`,
		`reduce .[] as $x (0; . + $x)`,
		`foreach .[] as $x (0; . + $x; [$x, .])`,
	} {
		if _, err := jq.Compile(prog); err != nil {
			t.Errorf("%s: %v", prog, err)
		}
	}
	for _, c := range []struct {
		prog      string
		line, col int
	}{
		{`[(1, break $f)?]`, 1, 6},
		{`. as [$a, {($a): $b}] | $b`, 1, 13},
		{`1 + foo(1)`, 1, 5},
		{"1 |\n  $nope", 2, 3},
	} {
		_, err := jq.Compile(c.prog)
		var ce *query.CompileError
		if !errors.As(err, &ce) || ce.Line != c.line || ce.Col != c.col {
			t.Errorf("%q: got %v, want line %d, column %d", c.prog, err, c.line, c.col)
		}
	}
	_, err := jq.Compile(`1 +`)
	var ce *query.CompileError
	if !errors.As(err, &ce) || ce.Line != 1 {
		t.Errorf("syntax error: got %v", err)
	}
	if _, err := jq.Compile(`$x`, "x"); err != nil {
		t.Errorf("caller variable: %v", err)
	}
}

func TestJQDialectSurface(t *testing.T) {
	d := jq.New()
	wantEqual(t, d.Name(), "jq")
	caps := d.Capabilities()
	wantEqual(t, caps.Env, true)
	wantEqual(t, caps.Files, true)
	wantEqual(t, caps.System, false)
	if jq.Prelude == "" || !strings.HasPrefix(jq.Prelude, "def ") {
		t.Errorf("prelude is not embedded")
	}
	if len(jq.Grammar().Tokens.Keywords) == 0 {
		t.Errorf("grammar has no keywords")
	}
}
