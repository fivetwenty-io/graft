package eval

import (
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/query/ast"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

func TestGeneratorsAndPipes(t *testing.T) {
	wantOutputs(t, `.[] | . * 2`, `[1,2,3]`, `2`, `4`, `6`)
	wantOutputs(t, `(1,2) as $x | ($x, 10) | . + $x`, `null`, `2`, `11`, `4`, `12`)
	wantOutputs(t, `[limit(3; range(10))]`, `null`, `[0,1,2]`)
	wantOutputs(t, `empty, 1`, `null`, `1`)
}

func TestBinaryOperatorsRunTheRightOperandOutermost(t *testing.T) {
	wantOutputs(t, `(1,2) + (10,20)`, `null`, `11`, `12`, `21`, `22`)
	wantOutputs(t, `[(1,2) == (1,2)]`, `null`, `[true,false,false,true]`)
	wantOutputs(t, `[(true,false) and (true,false)]`, `null`, `[true,false,false]`)
	wantOutputs(t, `[(true,false) or (true,false)]`, `null`, `[true,true,false]`)
	wantOutputs(t, `[1 < 2, 2 <= 2, 3 > 4, 4 >= 5, 1 != 1, "a" < "b"]`, `null`, `[true,true,false,false,false,true]`)
	wantOutputs(t, `-(1, 2)`, `null`, `-1`, `-2`)
	wantOutputs(t, `.[0,1][0,1]`, `[[1,2],[3,4]]`, `1`, `3`, `2`, `4`)
}

func TestComparisonRaisesTheNumberBound(t *testing.T) {
	huge := strings.Repeat("9", 100001)
	wantError(t, `. == 1`, huge, "number exceeds 100000 significant digits")
	wantError(t, `. < 1`, `1e1000000000`, "number exponent exceeds 999999999")
	wantError(t, `1 < .`, `1e1000000000`, "number exponent exceeds 999999999")
	// Containers compare totally, by literal text for a number past the
	// bounds.
	wantOutputs(t, `[.] < [1]`, `1e1000000000`, `false`)
	wantOutputs(t, `[1] < [.]`, `1e1000000000`, `true`)
	wantOutputs(t, `[.] == [.]`, `1e1000000000`, `true`)
	wantOutputs(t, `{"a": .} == {"a": 1}`, `1e1000000000`, `false`)
	wantOutputs(t, `.`, `1e1000000000`, `1e1000000000`)
	ctx := newCtx()
	ctx.Numbers = runtime.NumbersFloat
	wantOutputsWithCtx(t, ctx, `. == 1`, huge, `false`)
}

func TestAlternativeAndConditionals(t *testing.T) {
	wantOutputs(t, `[(false, null) // (3, 4)]`, `null`, `[3,4]`)
	wantOutputs(t, `[(1, null, 2) // 3]`, `null`, `[1,2]`)
	wantOutputs(t, `[empty // 3]`, `null`, `[3]`)
	wantError(t, `(1, error("x")) // 2`, `null`, "x")
	wantOutputs(t, `if . then "t" elif . == false then "f" else "n" end`, `false`, `"f"`)
	wantOutputs(t, `[.[] | if . then "t" end]`, `[true,null]`, `["t",null]`)
	wantOutputs(t, `[if (true, false) then 1 else 2 end]`, `null`, `[1,2]`)
}

func TestConstructors(t *testing.T) {
	wantOutputs(t, `{a:(1,2), b:(3,4)}`, `null`, `{"a":1,"b":3}`, `{"a":1,"b":4}`, `{"a":2,"b":3}`, `{"a":2,"b":4}`)
	wantOutputs(t, `{(("a","b")):(1,2)}`, `null`, `{"a":1}`, `{"a":2}`, `{"b":1}`, `{"b":2}`)
	wantOutputs(t, `1 as $x | {$x, a, "b": 2, "a": 3}`, `{"a":0}`, `{"x":1,"a":3,"b":2}`)
	wantError(t, `{(.k):2}`, `{"k":1}`, "Cannot use number (1) as object key")
	wantError(t, `{(.k):2}`, `{"k":null}`, "Cannot use null (null) as object key")
	wantOutputs(t, `[]`, `null`, `[]`)
	wantOutputs(t, `[.[] | . + 1]`, `[1,2]`, `[2,3]`)
	wantOutputs(t, `"a\(1,2)b\(3,4)"`, `null`, `"a1b3"`, `"a2b3"`, `"a1b4"`, `"a2b4"`)
	wantOutputs(t, `"\([1,"a"])\("s")\(null)"`, `null`, `"[1,\"a\"]snull"`)
	wantOutputs(t, `""`, `null`, `""`)
}

func TestFormatsCallTheirNative(t *testing.T) {
	ctx := newCtx()
	ctx.Dialect.Builtins().Register("@up", 0, func(_ *runtime.Ctx, in *value.Value, _ []runtime.Thunk, emit runtime.Emit) error {
		return emit(value.Str(strings.ToUpper(dump(in))), nil)
	})
	wantOutputsWithCtx(t, ctx, `@up "x\("a")y\(1)"`, `null`, `"x\"A\"y1"`)
	wantOutputsWithCtx(t, ctx, `@up`, `"q"`, `"\"Q\""`)
	wantErrorWithCtx(t, ctx, `@nope`, `null`, "nope is not a valid format")
}

func TestDestructuringAlternatives(t *testing.T) {
	wantOutputs(t, `.[] as {a: $x} ?// [$x] | $x`, `[{"a":1},[2]]`, `1`, `2`)
	wantOutputs(t, `. as [$a, [$b]] | $a + $b`, `[1,[2]]`, `3`)
}

func TestDestructuringPatterns(t *testing.T) {
	wantOutputs(t, `{"a":{"b":2}} as {a: {b: $x}, $a} | [$x, $a]`, `null`, `[2,{"b":2}]`)
	wantOutputs(t, `. as {$a: [$b]} | [$a, $b]`, `{"a":[5]}`, `[[5],5]`)
	wantOutputs(t, `. as {"a": $x, a: $y, ("a","b"): $z} | [$x, $y, $z]`, `{"a":1,"b":2}`, `[1,1,1]`, `[1,1,2]`)
	wantOutputs(t, `. as [$a] ?// $a | $a`, `null`, `null`)
	wantOutputs(t, `. as [$a] ?// $a | if $a == 1 then error("e") else $a end`, `[1]`, `[1]`)
	// An error or a break raised downstream of a pattern that is not the
	// last also moves on to the next pattern, as in jq 1.8.2.
	wantOutputs(t, `[([1] as [$a] ?// $a | $a) | if . == 1 then error("n") else . end]`, `null`, `[[1]]`)
	wantOutputs(t, `[label $f | ([1] as [$a] ?// $a | $a) | ., break $f]`, `null`, `[1,[1]]`)
	wantOutputs(t, `[label $f | (try ([1] as [$a] ?// $a | $a) catch "c") | ., break $f]`, `null`, `[1,[1]]`)
	wantError(t, `[1] as [$a] ?// $a | $a | ., error("z")`, `null`, "z")
	// foreach keeps the state the first pattern's update set when it moves
	// on.
	wantOutputs(t, `[foreach ([1]) as [$a] ?// $a (10; . + 1) | if . == 11 then error("n") else . end]`, `null`, `[12]`)
	wantOutputs(t, `[.[] as [$a] ?// {$b} | [$a, $b]]`, `[[1],{"b":2}]`, `[[1,null],[null,2]]`)
	wantError(t, `. as [$a] ?// [$b] | $a`, `{}`, "Cannot index object with number (0)")
	wantError(t, `. as [$a] | $a`, `"x"`, "Cannot index string with number (0)")
	// A key expression sees the variables bound outside its pattern, not
	// the ones its own pattern binds, except under ?// where every
	// pattern's variables are bound before any of them destructures.
	wantOutputs(t, `"x" as $k | {"a":"y","y":3,"x":4} as {a: $k, ($k): $v} | [$k, $v]`, `null`, `["y",4]`)
	wantOutputs(t, `"x" as $k | {"a":{"b":"y"},"y":3,"x":4} as {a: {b: $k}, ($k): $v} | $v`, `null`, `4`)
	wantOutputs(t, `"o" as $k | {"a":"x","x":2,"o":5} as {a: $k, ($k): $v} ?// [$k] | $v`, `null`, `2`)
}

func TestCandidateSetRoots(t *testing.T) {
	wantOutputsRoots(t, `[.a]`, []string{`{"a":1}`, `{"a":2}`}, `[1,2]`)
	wantOutputsRoots(t, `.a`, []string{`{"a":1}`, `{"a":2}`}, `1`, `2`)
	wantOutputsRoots(t, `.a = 3`, []string{`{"a":1}`, `{"a":2}`}, `{"a":3}`, `{"a":3}`)
	// .[] over each object yields its values, and .a? on a number is empty.
	wantOutputsRoots(t, `[.[] | .a?]`, []string{`{"a":1}`, `{"a":2}`}, `[]`)
	wantOutputsRoots(t, `[.a + 1]`, []string{`{"a":1}`, `{"a":2}`}, `[2,3]`)
	wantOutputsRoots(t, `1`, []string{`{"a":1}`, `{"a":2}`}, `1`)
}

func TestLocAndEnv(t *testing.T) {
	wantOutputs(t, "1 |\n$__loc__", `null`, `{"file":"<top-level>","line":2}`)
	wantOutputs(t, `$__loc__`, `1`, `{"file":"<top-level>","line":1}`)
	wantOutputsWithCtx(t, ctxWithEnv(map[string]string{"QUERY_T": "v"}), `$ENV.QUERY_T, env.QUERY_T`, `null`, `"v"`, `"v"`)
	wantErrorWithCtx(t, ctxWithCaps(runtime.Capabilities{Env: false, Files: true}), `env.QUERY_T`, `null`, "env operations are disabled")
	wantErrorWithCtx(t, ctxWithCaps(runtime.Capabilities{Env: false, Files: true}), `$ENV`, `null`, "env operations are disabled")
}

func TestUndefinedNames(t *testing.T) {
	wantError(t, `$nope`, `null`, "$nope is not defined")
	wantError(t, `nope(1)`, `null`, "nope/1 is not defined")
	wantError(t, `import "a" as a; 1`, `null`, "modules are not supported")
}

func TestPreludeDefinitionsSeeEachOther(t *testing.T) {
	ctx := newCtx()
	ctx.Dialect.Builtins().AddPrelude(mustParse(t, `def twice(f): f | f; def quad(f): twice(twice(f));`))
	wantOutputsWithCtx(t, ctx, `quad(. + 1)`, `0`, `4`)
}

func TestEnvBindingsShadow(t *testing.T) {
	env := NewEnv(nil).Bind("x", value.Num(1)).Bind("x", value.Num(2))
	v, ok := env.Lookup("x")
	if !ok || v.Float() != 2 {
		t.Fatalf("Lookup(x) = %v, %v", v, ok)
	}
	if _, ok := env.Lookup("y"); ok {
		t.Fatal("y should be unbound")
	}
	env = env.Def(&Closure{Name: "one", Body: &ast.Literal{Value: ast.Number("1")}})
	var got []*value.Value
	err := Run(newCtx(), mustParse(t, `[$x, one]`), env, []*value.Value{value.Null()}, func(v *value.Value, _ value.Path) error {
		got = append(got, v)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, "[$x, one]", got, []string{`[2,1]`})
}

func TestDumpTruncatesLikeJQ(t *testing.T) {
	cases := map[string]string{
		`"a\u0000\u007f/é"`:                      `"a\u0000\u007f/é"`,
		`1.0`:                                    `1.0`,
		`1e1000`:                                 `1E+1000`,
		`{"a":[1,true,null]}`:                    `{"a":[1,true,null]}`,
		`"abcdefghijklmnopqrstuvwxyz0123456789"`: `"abcdefghijklmnopqrstuvwx..."`,
	}
	for in, want := range cases {
		if got := dump(mustJSON(t, in)); got != want {
			t.Errorf("dump(%s) = %s, want %s", in, got, want)
		}
	}
}

func TestErrorsAreTyped(t *testing.T) {
	_, err := mustEval(t, newCtx(), `error({"a":1})`, []string{`null`})
	re, ok := err.(*query.RuntimeError) //nolint:errorlint // Run returns the error itself, never a wrapper
	if !ok {
		t.Fatalf("error %T is not a *query.RuntimeError", err)
	}
	if !value.Equal(re.Value, mustJSON(t, `{"a":1}`)) {
		t.Fatalf("Value = %s", dump(re.Value))
	}
	wantErrContains(t, "error", err, `{"a":1} (not a string)`)
}
