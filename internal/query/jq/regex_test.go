package jq_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/query/jq"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query/value"
	"github.com/fivetwenty-io/graft/pkg/regex"
)

func TestRegexFamily(t *testing.T) {
	wantOutputs(t, `[match("a(?<n>b)"; "g")] | map(.captures[0].name, .offset)`, `"abab"`, `["n",0,"n",2]`)
	wantOutputs(t, `test("A"; "i"), test("A")`, `"a"`, `true`, `false`)
	wantOutputs(t, `capture("(?<y>\\d+)-(?<m>\\d+)")`, `"2026-10"`, `{"y":"2026","m":"10"}`)
	wantOutputs(t, `[scan("\\d")]`, `"a1b2"`, `["1","2"]`)
	wantOutputs(t, `split(", *"; null)`, `"a, b,c"`, `["a","b","c"]`)
	wantOutputs(t, `sub("(?<x>a)"; "<\(.x)>"), gsub("a"; "b")`, `"aXa"`, `"<a>Xa"`, `"bXb"`)
	wantOutputs(t, `[match("é"; "g").offset]`, `"aé"`, `[1]`) // offsets in codepoints
	wantOutputs(t, `[match(""; "g").offset]`, `"ab"`, `[0,1,2]`)
	wantOutputs(t, `[match("a*"; "gn").string]`, `"baab"`, `["aa"]`)
	wantOutputs(t, `test("(?=a)"), test("(a)\\1")`, `"aa"`, `true`, `true`)
	wantOutputs(t, `[match("a|ab"; "l").length]`, `"ab"`, `[2]`)
	wantOutputs(t, `match("(a)|(b)"; "g") | .captures | map(.string)`, `"b"`, `[null,"b"]`)
	// m is DotAll, and s keeps ^ at the start of the subject, which it
	// already is.
	wantOutputs(t, `test("a.b"), test("a.b"; "m"), test("^b"; "s"), test("^b")`, `"a\nb"`, `false`, `true`, `false`, `false`)
	wantError(t, `test("a"; "q")`, `"a"`, `q is not a valid modifier string`)
	wantError(t, `test("(a+)+$")`, `"`+strings.Repeat("a", 40)+`b"`, `regex step budget exhausted for (a+)+$`)
}

func TestMatchObjects(t *testing.T) {
	wantOutputs(t, `match("(a)|(b)")`, `"b"`,
		`{"offset":0,"length":1,"string":"b","captures":[{"offset":-1,"string":null,"length":0,"name":null},{"offset":0,"length":1,"string":"b","name":null}]}`)
	wantOutputs(t, `match("(?<n>)y")`, `"xy"`,
		`{"offset":1,"length":1,"string":"y","captures":[{"offset":1,"string":"","length":0,"name":"n"}]}`)
	// Inside an empty match, jq reports every group that took part as an
	// empty capture at the match's offset, whatever the group captured.
	wantOutputs(t, `match("(?=(a))")`, `"a"`,
		`{"offset":0,"length":0,"string":"","captures":[{"offset":0,"string":"","length":0,"name":null}]}`)
	wantOutputs(t, `match("(?<=(a))|(z)") | .captures`, `"ab"`,
		`[{"offset":1,"string":"","length":0,"name":null},{"offset":-1,"string":null,"length":0,"name":null}]`)
	wantOutputs(t, `capture("(?=(?<x>a))"), gsub("(?=(?<x>a))"; "[\(.x)]"), [scan("(?=(a))")]`, `"ba"`, `{"x":""}`, `"b[]a"`, `[[""]]`)
	wantOutputs(t, `([match(["a","g"])] | length), test(["A","i"]), test(["A"])`, `"aXa"`, `2`, `true`, `false`)
	wantOutputs(t, `_match_impl("a"; null; true), _match_impl("a"; null; 1)`, `"a"`,
		`true`, `[{"offset":0,"length":1,"string":"a","captures":[]}]`)
	wantOutputs(t, `test("a.b"; "p"), test("^b"; "p"), test("a b"; "x"), test("a"; "gx")`, `"a\nb"`, `true`, `false`, `false`, `true`)
}

func TestRegexDerivedBuiltins(t *testing.T) {
	wantOutputs(t, `capture("(?<a>x)|(?<b>y)"), [capture("(?<a>.)"; "g")], capture("(.)")`, `"xy"`,
		`{"a":"x","b":null}`, `[{"a":"x"},{"a":"y"}]`, `{}`)
	wantOutputs(t, `capture("(?<a>x)?(?<a>y)")`, `"xy"`, `{"a":"y"}`)
	wantOutputs(t, `[capture("z")], [capture(["(?<x>x)"])]`, `"xy"`, `[]`, `[{"x":"x"}]`)
	wantOutputs(t, `[scan("(a)(b)?")], [scan("A"; "i")]`, `"abaXab"`, `[["a","b"],["a",null],["a","b"]]`, `["a","a","a"]`)
	wantOutputs(t, `split("a"; null), [splits("a")], [splits("A"; "i")], split(""; null)`, `"abaX"`,
		`["","b","X"]`, `["","b","X"]`, `["","b","X"]`, `["","a","b","a","X",""]`)
	wantOutputs(t, `sub("a"; "b"; "g"), sub("(?<x>a)"; "\(.x)\(.x)"; "g"), gsub(""; "-"), sub("z"; "y"), gsub("A"; "z"; "i")`, `"abaX"`,
		`"bbbX"`, `"aabaaX"`, `"-a-b-a-X-"`, `"abaX"`, `"zbzX"`)
	// A replacement that emits several values gives as many results, each
	// built from the n-th value at every match, as jq's sub does.
	wantOutputs(t, `gsub("(?<x>[ab])"; if .x == "a" then "1", "2" else "3" end)`, `"abaXab"`, `"131X13"`, `"22X2"`)
	wantOutputs(t, `[sub("a"; "1", "2")], sub("(?<a>x)|(?<b>y)"; "[\(.a)|\(.b)]")`, `"ay"`, `["1y","2y"]`, `"a[null|y]"`)
}

func TestRegexErrors(t *testing.T) {
	wantError(t, `test("a")`, `1`, `number (1) cannot be matched, as it is not a string`)
	wantError(t, `test("a")`, `{"a":1}`, `object ({"a":1}) cannot be matched, as it is not a string`)
	wantError(t, `sub("a"; "b")`, `1`, `number (1) cannot be matched, as it is not a string`)
	wantError(t, `[splits("a")]`, `1`, `number (1) cannot be matched, as it is not a string`)
	wantError(t, `test(1)`, `"a"`, `number not a string or array`)
	wantError(t, `test([])`, `"a"`, `array not a string or array`)
	wantError(t, `match({})`, `"a"`, `object not a string or array`)
	wantError(t, `test("a"; 1)`, `"a"`, `number (1) is not a string`)
	wantError(t, `test(1; "q")`, `"a"`, `number (1) is not a string`)
	wantError(t, `test("("; "q")`, `"a"`, `q is not a valid modifier string`)
	wantError(t, `[scan("a"; "q")]`, `"a"`, `gq is not a valid modifier string`)
	wantError(t, `split("a"; "q")`, `"a"`, `qg is not a valid modifier string`)
	wantError(t, `split("a"; 1)`, `"a"`, `number (1) and string ("g") cannot be added`)
	wantError(t, `gsub("a"; 1)`, `"a"`, `string ("") and number (1) cannot be added`)
	wantError(t, `split(1; null)`, `"a"`, `number (1) is not a string`)
	wantError(t, `[splits(1)]`, `"a"`, `number (1) is not a string`)
	wantError(t, `sub(1; "x")`, `"a"`, `number (1) is not a string`)
	wantError(t, `[scan(1)]`, `"a"`, `number (1) is not a string`)
	wantError(t, `test("(")`, `"a"`, `Regex failure: missing closing parenthesis at position 0`)
}

func TestRegexBudgetAndDeadlineComeFromTheCtx(t *testing.T) {
	p, err := jq.Compile(`test("a+b")`)
	if err != nil {
		t.Fatal(err)
	}
	in := mustJSON(t, `"`+strings.Repeat("a", 200)+`"`)
	runWith := func(ctx *runtime.Ctx) error {
		return p.Run(ctx, p.Env, []*value.Value{in}, func(*value.Value, value.Path) error { return nil })
	}

	ctx := runtime.NewCtx(jq.New(), runtime.NumbersExact)
	if err := runWith(ctx); err != nil {
		t.Fatalf("under the default budget: %v", err)
	}
	ctx.RegexBudget = regex.Budget{Steps: 100}
	if err := runWith(ctx); err == nil || err.Error() != "regex step budget exhausted for a+b" {
		t.Errorf("under a 100-step budget: got %v", err)
	}

	ctx = runtime.NewCtx(jq.New(), runtime.NumbersExact)
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	ctx.Deadline = canceled
	if err := runWith(ctx); !errors.Is(err, context.Canceled) {
		t.Errorf("under a canceled context: got %v", err)
	}
}
