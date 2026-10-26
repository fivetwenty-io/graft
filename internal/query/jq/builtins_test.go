package jq_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/query/jq"
	"github.com/fivetwenty-io/graft/internal/query/runtime"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

func TestJQBuiltinNamesPresentInJ1(t *testing.T) {
	r := jq.New().Builtins()
	defined := map[string]bool{}
	for _, n := range r.Names() {
		defined[n] = true
	}
	for _, want := range strings.Fields(j1Names) {
		name, arity := splitArity(want)
		if _, ok := r.Lookup(name, arity); !ok && !defined[want] && want != "path/1" { // the evaluator runs path itself
			t.Errorf("missing %s", want)
		}
	}
}

func TestCoreBuiltinBehaviors(t *testing.T) {
	wantOutputs(t, `keys, keys_unsorted`, `{"b":1,"a":2}`, `["a","b"]`, `["b","a"]`)
	wantOutputs(t, `to_entries`, `{"b":1,"a":2}`, `[{"key":"b","value":1},{"key":"a","value":2}]`)
	wantOutputs(t, `[.[] | select(. > 1)]`, `[1,2,3]`, `[2,3]`)
	wantOutputs(t, `group_by(.a) | map(length)`, `[{"a":1},{"a":2},{"a":1}]`, `[2,1]`)
	wantOutputs(t, `[splits(", ")]`, `"a, b"`, `["a","b"]`) // splits takes a regex
	wantOutputs(t, `@csv, @tsv, @sh`, `["a b",1]`, `"\"a b\",1"`, `"a b\t1"`, `"'a b' 1"`)
	wantOutputs(t, `@html, @uri, @base64, (@base64 | @base64d)`, `[1,"a"]`, `"[1,&quot;a&quot;]"`, `"%5B1%2C%22a%22%5D"`, `"WzEsImEiXQ=="`, `"[1,\"a\"]"`) // non-strings format their JSON text, pinned jq 1.8.2
	wantOutputs(t, `@html "<\(.)>&"`, `"x"`, `"<x>&"`)                                                                                                     // only the interpolated segment is escaped
	wantOutputs(t, `@json "v=\(.)"`, `{"a":1}`, `"v={\"a\":1}"`)
	wantOutputs(t, `tostream`, `{"a":[1]}`, `[["a",0],1]`, `[["a",0]]`, `[["a"]]`)
	wantOutputs(t, `fromstream(tostream)`, `{"a":[1,{"b":2}]}`, `{"a":[1,{"b":2}]}`)
	wantOutputs(t, `[.[] | tostring]`, `[1,"1",[1],{"a":1},null]`, `["1","1","[1]","{\"a\":1}","null"]`)
	wantOutputs(t, `walk(if type == "number" then . + 1 else . end)`, `[1,{"a":2}]`, `[2,{"a":3}]`)
	wantOutputs(t, `pick(.a.b, .c[1])`, `{"a":{"b":1,"x":2},"c":[1,2,3]}`, `{"a":{"b":1},"c":[null,2]}`)
	wantOutputs(t, `have_literal_numbers, have_decnum`, `null`, `true`, `true`) // both true in the pinned Homebrew jq 1.8.2
	wantOutputs(t, `[0.1 + 0.2, 100000000000000000000 + 1, 9007199254740993 - 9007199254740992, 1.5 * 2, 1e18 + 0, 1 / 4, 1 / 3, 5 % 3, -5 % 3, 0 * -1, -0 + 0]`, `null`,
		`[0.3,100000000000000000001,1,3,1000000000000000000,0.25,0.3333333333333333333333333333333333,2,-2,-0,0]`) // exact decimals, the ruled improvement over jq's doubles
	wantOutputs(t, `[1e1000 + 0, infinite - infinite, (nan < 1), ([nan, 1, -1] | sort)]`, `null`, `[1e+1000,null,true,[null,-1,1]]`)
	wantOutputs(t, `[nan == nan, nan < nan, nan > nan, nan != nan, ([nan, nan] | group_by(.) | length), ([nan, nan] | unique | length), ([nan] | index(nan)), ({a: nan} == {a: nan})]`, `null`, `[false,true,false,true,2,2,null,false]`) // jq 1.8.2 answers these exactly
	wantOutputsFloat(t, `[(100000000000000000000 + 0) == 100000000000000000001, ([100000000000000000001, (100000000000000000000 + 0)] | unique | length)]`, `null`, `[true,1]`)                                                           // --float-numbers compares as doubles
	wantOutputs(t, `[(100000000000000000000 + 0) == 100000000000000000001, ([100000000000000000001, (100000000000000000000 + 0)] | unique | length)]`, `null`, `[false,2]`)
	wantOutputs(t, `[.[] | floor, ceil, round, fabs]`, `[-1.5]`, `[-2,-1,-2,1.5]`)
	wantError(t, `1 / 0`, `null`, `number (1) and number (0) cannot be divided because the divisor is zero`)
	wantError(t, `. * .`, `1e999999999`, `number exponent exceeds 999999999`)
	huge := strings.Repeat("9", 100001)
	wantOutputs(t, `., tostring, tojson, (. | @text)`, huge, huge, `"`+huge+`"`, `"`+huge+`"`, `"`+huge+`"`) // an over-bound literal passes through as text, as jq passes it
	wantError(t, `. + 0`, huge, `number exceeds 100000 significant digits`)                                  // the bound bites only when the math needs the value
	wantError(t, `. < 1`, huge, `number exceeds 100000 significant digits`)
	wantOutputsFloat(t, `. + 0`, huge, `1.7976931348623157e+308`) // --float-numbers converts to the double jq holds
	wantOutputs(t, `., tojson`, `1e1000000000`, `1E+1000000000`, `"1E+1000000000"`)
	wantOutputs(t, `[.[] | ltrimstr("a"), rtrimstr("a"), trimstr("a")]`, `["aba"]`, `["ba","ab","b"]`)
	wantOutputs(t, `getpath(["a","b"]), getpath(["x"])`, `{"a":{"b":1}}`, `1`, `null`)
	wantOutputs(t, `del(.[1,2])`, `[1,2,3,4]`, `[1,4]`)
	wantOutputs(t, `abs, (-1 | abs), ("a" | try abs catch "e")`, `-0`, `-0`, `1`, `"a"`) // jq 1.8.2 returns a string unchanged
	wantOutputs(t, `[paths]`, `{"a":[1]}`, `[["a"],["a",0]]`)
}

func TestHaltAndHaltError(t *testing.T) {
	err := runErr(t, `halt_error(3)`, `"bye\n"`)
	var he *query.HaltError
	if !errors.As(err, &he) {
		t.Fatalf("got %v, want a halt", err)
	}
	wantEqual(t, he.Code, 3)
	wantEqual(t, he.Message.Str(), "bye\n")
	err = runErr(t, `halt_error`, `{"a":1}`)
	if !errors.As(err, &he) || he.Code != 5 || runtime.JSON(he.Message) != `{"a":1}` {
		t.Errorf("halt_error/0: got %v", err)
	}
	err = runErr(t, `halt`, `1`)
	if !errors.As(err, &he) || he.Code != 0 || he.Message != nil {
		t.Errorf("halt: got %v", err)
	}
	wantError(t, `halt_error("x")`, `null`, `string ("x") halt_error/1: number required`)
	if err := runErr(t, `try halt_error catch .`, `1`); !errors.As(err, &he) { // no catch stops a halt
		t.Errorf("try halt_error: got %v", err)
	}
}

func TestJQTypesAndConversions(t *testing.T) {
	wantOutputs(t, `[.[] | length]`, `[null,-2.5,"aé",[1,2],{"a":1}]`, `[0,2.5,2,2,1]`)
	wantError(t, `length`, `true`, `boolean (true) has no length`)
	wantOutputs(t, `utf8bytelength`, `"aé"`, `3`)
	wantError(t, `utf8bytelength`, `[]`, `array ([]) only strings have UTF-8 byte length`)
	wantOutputs(t, `[.[] | type]`, `[null,true,1,"a",[],{}]`, `["null","boolean","number","string","array","object"]`)
	wantOutputs(t, `[.[] | tonumber]`, `["1.50","-2","1e3",3,"nan"]`, `[1.50,-2,1E+3,3,null]`)
	wantError(t, `tonumber`, `"1x"`, `string ("1x") cannot be parsed as a number`)
	wantOutputs(t, `[.[] | tonumber]`, `["1.",".5","+1","00012","1E5","-0.0","-nan"]`, `[1,0.5,1,12,1E+5,-0.0,null]`)
	for _, bad := range []string{`" 1"`, `"1 "`, `"1e"`, `"-"`, `"."`, `"1e+-5"`, `"0x10"`, `"1.2.3"`} {
		wantError(t, `tonumber`, bad, `string (`+bad+`) cannot be parsed as a number`)
	}
	wantOutputs(t, `[.[] | toboolean]`, `["true","false",true]`, `[true,false,true]`)
	wantOutputs(t, `[.[] | not]`, `[null,false,0]`, `[true,true,false]`)
	wantOutputs(t, `has("a"), has("z")`, `{"a":1}`, `true`, `false`)
	wantOutputs(t, `has(0), has(2)`, `[1]`, `true`, `false`)
	wantOutputs(t, `[.[] | in({"a":1})]`, `["a","b"]`, `[true,false]`)
	wantOutputs(t, `contains({"a":[1]}), inside({"x":1,"a":[1,2,3]}), inside({"a":[1]})`, `{"a":[1,2]}`, `true`, `true`, `false`)
	wantError(t, `contains("b")`, `{}`, `object ({}) and string ("b") cannot have their containment checked`)
	// Below the top level, values of different kinds are simply not
	// contained, so only the outermost pair can raise the kind error.
	wantOutputs(t, `contains({"a":"x"})`, `{"a":1}`, `false`)
	wantOutputs(t, `contains({"a":[1]})`, `{"a":{"b":1}}`, `false`)
	wantOutputs(t, `[contains("bar"), ("ar" | inside("bar"))]`, `"foobar"`, `[true,true]`)
	wantOutputs(t, `keys`, `[5,6]`, `[0,1]`)
	wantError(t, `keys`, `1`, `number (1) has no keys`)
	wantOutputs(t, `[.[] | isvalid(tonumber)]`, `["1","x"]`, `[true,false]`)
	wantOutputs(t, `[.[] | toarray]`, `[1,[2]]`, `[[1],[2]]`)
	wantOutputs(t, `fromjson`, `"[1,{\"a\":nan}]"`, `[1,{"a":null}]`)
	wantError(t, `fromjson`, `"[1,2"`, `Unfinished JSON term at EOF at line 1, column 4 (while parsing '[1,2')`)
	wantOutputs(t, `fromjson`, `"nan"`, `null`)
	wantError(t, `fromjson`, `"[1,2\n,3"`, "Unfinished JSON term at EOF at line 2, column 2 (while parsing '[1,2\n,3')")
	wantError(t, `fromjson`, `""`, `Expected JSON value (while parsing '')`)
	wantError(t, `fromjson`, `"1 2"`, `Unexpected extra JSON values (while parsing '1 2')`)
	wantError(t, `fromjson`, `1`, `number (1) only strings can be parsed`)
	wantOutputs(t, `[error(null)?], (try error("x") catch .), (try error({}) catch .)`, `null`, `[]`, `"x"`, `{}`)
	wantOutputs(t, `[.[] | arrays, objects, iterables, booleans, numbers, strings, nulls, values, scalars]`,
		`[[],{},true,1,"s",null]`, `[[],[],[],{},{},{},true,true,true,1,1,1,"s","s","s",null,null]`)
	wantOutputs(t, `[.[] | normals], [.[] | finites]`, `[0,1,1e-400]`, `[1]`, `[0,1,1E-400]`)
	wantOutputs(t, `env | type, ($ENV | type)`, `null`, `"object"`, `"object"`)
}

func TestJQPathBuiltins(t *testing.T) {
	wantOutputs(t, `[paths(type == "number")], [leaf_paths]`, `{"a":[1,{"b":2}]}`, `[["a",0],["a",1,"b"]]`, `[["a",0],["a",1,"b"]]`)
	wantOutputs(t, `setpath(["a",1]; 5), setpath([]; 1)`, `null`, `{"a":[null,5]}`, `1`)
	wantError(t, `setpath(1; 1)`, `null`, `Path must be specified as an array`)
	wantError(t, `getpath(1)`, `null`, `Path must be specified as an array`)
	wantOutputs(t, `delpaths([[-1],[-2]]), delpaths([[0],[0]]), delpaths([])`, `[1,2,3]`, `[1]`, `[2,3]`, `[1,2,3]`)
	wantOutputs(t, `delpaths([["a","x","y"]])`, `{"a":{"b":1}}`, `{"a":{"b":1}}`)
	wantError(t, `delpaths([[0]])`, `"abc"`, `Cannot delete fields from string`)
	wantError(t, `delpaths([["a","b"]])`, `{"a":1}`, `Cannot delete fields from number`)
	wantError(t, `delpaths(1)`, `{}`, `Paths must be specified as an array`)
	wantOutputs(t, `del(.a, .b[0]), del(.[]), del(..)`, `{"a":1,"b":[1,2],"c":3}`, `{"b":[2],"c":3}`, `{}`, `null`)
	wantOutputs(t, `del(.[1:3])`, `[1,2,3,4]`, `[1,4]`)
	wantOutputs(t, `to_entries, with_entries(.key |= ascii_upcase)`, `{"a":1}`, `[{"key":"a","value":1}]`, `{"A":1}`)
	wantOutputs(t, `from_entries`, `[{"name":"b","value":false},{"Name":"c"},{"Key":"d","Value":1},{"key":"e","value":null,"Value":2}]`, `{"b":false,"c":null,"d":1,"e":null}`)
	// The key comes from key, Key, name, and Name in that order, and k, K,
	// and v are not read at all.
	wantOutputs(t, `from_entries`, `[{"name":"n","Key":"K"}]`, `{"K":null}`)
	wantOutputs(t, `from_entries`, `[{"key":"a","v":1}]`, `{"a":null}`)
	wantError(t, `from_entries`, `[{"k":"a","v":1}]`, `Cannot use null (null) as object key`)
	wantError(t, `from_entries`, `[{"K":"a"}]`, `Cannot use null (null) as object key`)
	wantError(t, `from_entries`, `[{"key":null,"value":2}]`, `Cannot use null (null) as object key`)
	wantError(t, `from_entries`, `[{"key":1,"value":1}]`, `Cannot use number (1) as object key`)
	wantOutputs(t, `[path(..)]`, `[[1]]`, `[[],[0],[0,0]]`)
	wantOutputs(t, `map_values(. + 1), map_values(empty)`, `{"a":1,"b":2}`, `{"a":2,"b":3}`, `{}`)
	wantOutputs(t, `pick(first(.[]))`, `[1,2]`, `[1]`)
	wantOutputs(t, `[1 | truncate_stream([[0],1],[[1,0],2],[[1,0]],[[1]])]`, `null`, `[[[0],2],[[0]]]`)
	wantOutputs(t, `fromstream(1 | truncate_stream([[0],1],[[1,0],2],[[1,0]],[[1]]))`, `null`, `[2]`)
	wantOutputs(t, `[tostream]`, `[[],{}]`, `[[[0],[]],[[1],{}],[[1]]]`)
	wantOutputs(t, `[1 | tostream]`, `null`, `[[[],1]]`)
	wantOutputs(t, `[fromstream([[0],1],[[1],2],[[1]])]`, `null`, `[[1,2]]`)
}

func TestJQCollections(t *testing.T) {
	wantOutputs(t, `sort, sort_by(-.)`, `[3,1,2]`, `[1,2,3]`, `[3,2,1]`)
	wantOutputs(t, `sort_by(.a, .b)`, `[{"a":1,"b":2},{"a":1,"b":1},{"a":0,"b":3}]`, `[{"a":0,"b":3},{"a":1,"b":1},{"a":1,"b":2}]`)
	wantOutputs(t, `sort`, `[{"a":1},[1],"b",2,true,false,null]`, `[null,false,true,2,"b",[1],{"a":1}]`)
	wantError(t, `sort`, `{}`, `object ({}) cannot be sorted, as it is not an array`)
	wantOutputs(t, `unique, unique_by(length), group_by(length)`, `["b","a","bb","a"]`, `["a","b","bb"]`, `["b","bb"]`, `[["b","a","a"],["bb"]]`)
	wantOutputs(t, `min, max, min_by(.a), max_by(.a)`, `[{"a":1,"i":0},{"a":0,"i":1},{"a":1,"i":2},{"a":0,"i":3}]`,
		`{"a":0,"i":1}`, `{"a":1,"i":2}`, `{"a":0,"i":1}`, `{"a":1,"i":2}`)
	wantOutputs(t, `min, max`, `[]`, `null`, `null`)
	wantOutputs(t, `reverse`, `[1,2,3]`, `[3,2,1]`)
	wantOutputs(t, `[.[] | reverse]`, `[null,[],{},""]`, `[[],[],[],[]]`)
	wantError(t, `reverse`, `"ab"`, `Cannot index string with number (1)`) // jq 1.8.2 does not reverse strings
	wantOutputs(t, `indices("b"), index("b"), rindex("b")`, `"aébéb"`, `[2,4]`, `2`, `4`)
	wantOutputs(t, `indices(1), indices([1,2]), index(9)`, `[1,2,1,2]`, `[0,2]`, `[0,2]`, `null`)
	wantOutputs(t, `indices(1)`, `null`, `null`)
	wantOutputs(t, `flatten, flatten(1), flatten(0)`, `[1,[2,[3]]]`, `[1,2,3]`, `[1,2,[3]]`, `[1,[2,[3]]]`)
	wantError(t, `flatten(-1)`, `[]`, `flatten depth must not be negative`)
	wantOutputs(t, `add, add(.[] | . * 2), ([] | add)`, `[1,2]`, `3`, `6`, `null`)
	wantOutputs(t, `add`, `[[1],[2]]`, `[1,2]`)
	wantOutputs(t, `any, all, any(. > 1), all(. > 0), any(.[]; . == 2), all(empty; false)`, `[1,2]`, `true`, `true`, `true`, `true`, `true`, `true`)
	wantOutputs(t, `[any, all]`, `[]`, `[false,true]`)
	wantOutputs(t, `join(", "), join("")`, `["a",1,null,true]`, `"a, 1, , true"`, `"a1true"`)
	wantOutputs(t, `[combinations], [[1,2] | combinations(2)]`, `[[1,2],[3]]`, `[[1,3],[2,3]]`, `[[1,1],[1,2],[2,1],[2,2]]`)
	wantOutputs(t, `transpose`, `[[1],[2,3]]`, `[[1,2],[null,3]]`)
	wantOutputs(t, `[.[] | ascii]`, `[0,65,97,127]`, `["\u0000","A","a","\u007f"]`)
	wantError(t, `ascii`, `200`, `number (200) is not an ASCII code point`)
	wantError(t, `ascii`, `-1`, `number (-1) is not an ASCII code point`)
	wantError(t, `ascii`, `65.5`, `number (65.5) is not an ASCII code point`)
	wantError(t, `ascii`, `"A"`, `string ("A") is not an ASCII code point`)
}

func TestJQStrings(t *testing.T) {
	wantOutputs(t, `startswith("ab"), endswith("c"), split(","), split("")`, `"ab,c"`, `true`, `true`, `["ab","c"]`, `["a","b",",","c"]`)
	wantError(t, `startswith(1)`, `"a"`, `startswith() requires string inputs`)
	wantError(t, `rtrimstr(1)`, `"a"`, `endswith() requires string inputs`)
	wantOutputs(t, `trim, ltrim, rtrim`, `" a\t"`, `"a"`, `"a\t"`, `" a"`)
	wantError(t, `trim`, `1`, `trim input must be a string`)
	wantOutputs(t, `explode, (explode | implode)`, `"aé"`, `[97,233]`, `"aé"`)
	wantError(t, `implode`, `"a"`, `implode input must be an array`)
	wantError(t, `implode`, `["a"]`, `string ("a") can't be imploded, unicode codepoint needs to be numeric`)
	wantOutputs(t, `ascii_downcase, ascii_upcase`, `"aBé"`, `"abé"`, `"ABé"`)
	wantOutputs(t, `. * 3, . * 0, . * -1, (2 * .)`, `"ab"`, `"ababab"`, `""`, `null`, `"abab"`)
	wantOutputs(t, `. / ",", . / ""`, `"a,b"`, `["a","b"]`, `["a",",","b"]`)
	wantOutputs(t, `"" / ","`, `null`, `[]`)
}

func TestJQMath(t *testing.T) {
	wantOutputs(t, `[infinite % 3, -infinite % 3, 5.9 % 2.1, -5 % -3, nan % 1]`, `null`, `[1,-2,1,-2,null]`)
	// Under --float-numbers, % converts both sides to 64-bit integers with
	// saturation, as jq does, so 1e30 stands for the largest one. The
	// exact mode takes the true remainder.
	wantOutputsFloat(t, `[1e30 % 7, -1e30 % 7, 7 % 1e30, 5.9 % 2.1, -5 % 3, 5 % -3, infinite % 3, 1e19 % 1e18, -1e30 % -1, (nan % 2 | isnan)]`, `null`, `[0,-1,7,1,-2,2,1,223372036854775800,0,true]`)
	wantOutputs(t, `1e30 % 7`, `null`, `1`)
	wantError(t, `5 % 0.5`, `null`, `number (5) and number (0.5) cannot be divided (remainder) because the divisor is zero`)
	wantError(t, `null - null`, `null`, `null (null) and null (null) cannot be subtracted`)
	wantError(t, `{} / {}`, `null`, `object ({}) and object ({}) cannot be divided`)
	wantError(t, `[1,2] * 2`, `null`, `array ([1,2]) and number (2) cannot be multiplied`)
	wantOutputs(t, `[null + 1, 1 + null, "a" + "b", [1] + [2], ({"a":1} + {"b":2}), ([1,2,1] - [1])]`, `null`, `[1,1,"ab",[1,2],{"a":1,"b":2},[2]]`)
	wantOutputs(t, `{"a":{"b":1,"c":2}} * {"a":{"b":3}}`, `null`, `{"a":{"b":3,"c":2}}`)
	// Negation keeps a literal's text, as jq 1.8.2 does, and negating a
	// zero gives a positive one.
	wantOutputs(t, `[.[] | -.]`, `[1,-0,1e1000]`, `[-1,0,-1E+1000]`)
	wantOutputs(t, `[pow(2; 10), (16 | sqrt), (1 | exp | log), (8 | log2), (100 | log10), (3 | exp2), (2 | exp10)]`, `null`, `[1024,4,1,3,2,8,100]`)
	wantOutputs(t, `[.[] | round, trunc]`, `[2.5,-2.5]`, `[3,2,-3,-2]`)
	wantOutputs(t, `[infinite, -infinite, nan] | map(isinfinite, isnan, isnormal)`, `null`, `[true,false,false,true,false,false,false,true,false]`)
	wantOutputs(t, `[1, 0, "a"] | map(isnormal)`, `null`, `[true,false,false]`)
	wantError(t, `"a" | floor`, `null`, `string ("a") number required`)
	wantOutputsFloat(t, `[0.1 + 0.2, 1 / 3]`, `null`, `[0.30000000000000004,0.3333333333333333]`)
}

func TestJQGenerators(t *testing.T) {
	wantOutputs(t, `[range(3)], [range(0,1;3,4)], [range(0;1;0.25)], [range(5;0;-2)], [range(0;3;0)]`, `null`,
		`[0,1,2]`, `[0,1,2,0,1,2,3,1,2,1,2,3]`, `[0,0.25,0.5,0.75]`, `[5,3,1]`, `[]`)
	wantError(t, `range("a")`, `null`, `Range bounds must be numeric`)
	wantOutputs(t, `[limit(2; .[])], [limit(0; .[])], [limit(1.5; .[])], [skip(1; .[])], [skip(1.5; .[])]`, `[1,2,3]`,
		`[1,2]`, `[]`, `[1,2]`, `[2,3]`, `[2,3]`)
	wantError(t, `limit(-1; 1)`, `null`, `limit doesn't support negative count`)
	wantOutputs(t, `first, last, first(.[]), last(.[]), nth(1), nth(1; .[]), [nth(5; .[])]`, `[1,2,3]`, `1`, `3`, `1`, `3`, `2`, `2`, `[]`)
	wantError(t, `nth(-1; 1)`, `null`, `nth doesn't support negative indices`)
	wantOutputs(t, `isempty(empty), isempty(1, error("x"))`, `null`, `true`, `false`)
	wantError(t, `isempty(error("x"))`, `null`, `x`)
	wantOutputs(t, `[limit(5; repeat(. * 2))]`, `1`, `[2,2,2,2,2]`)
	wantOutputs(t, `[while(. < 8; . * 2)], until(. > 8; . * 2)`, `1`, `[1,2,4]`, `16`)
	wantOutputs(t, `[recurse(if . < 3 then . + 1 else empty end)], [recurse(. * 2; . < 10)]`, `1`, `[1,2,3]`, `[1,2,4,8]`)
	wantOutputs(t, `[first(range(10; 0; -1))], [limit(1; label $f | 1, break $f)]`, `null`, `[10]`, `[1]`)
	wantOutputs(t, `[limit(3; range(10)) | select(. > 0)]`, `null`, `[1,2]`)
	wantOutputs(t, `[.[] | first(.[]?)]`, `[[1,2],[],3]`, `[1]`)
	wantOutputs(t, `path(first(.a, .b)), path(limit(1; .a, .b))`, `{}`, `["a"]`, `["a"]`)
}

func TestJQFormats(t *testing.T) {
	wantOutputs(t, `@csv`, `[1,"a",null,true,1.5,"x\"y"]`, `"1,\"a\",,true,1.5,\"x\"\"y\""`)
	wantOutputs(t, `@tsv`, `["a\tb\\c\r\n",1,null,true]`, `"a\\tb\\\\c\\r\\n\t1\t\ttrue"`)
	wantError(t, `@tsv`, `{}`, `object ({}) cannot be tsv-formatted, only array`)
	wantError(t, `@csv`, `1`, `number (1) cannot be csv-formatted, only array`)
	wantError(t, `@csv`, `[[1]]`, `array ([1]) is not valid in a csv row`)
	wantOutputs(t, `@sh`, `[1,"a b",null,false,"it's"]`, `"1 'a b' null false 'it'\\''s'"`)
	wantError(t, `@sh`, `[[1]]`, `array ([1]) can not be escaped for shell`)
	wantOutputs(t, `@uri, (@uri | @urid)`, `"é /?~-_.!*"`, `"%C3%A9%20%2F%3F~-_.%21%2A"`, `"é /?~-_.!*"`)
	wantError(t, `@urid`, `"%zz"`, `string ("%zz") is not a valid uri encoding`)
	wantError(t, `@urid`, `"%ff"`, `string ("%ff") is not a valid uri encoding`)
	wantOutputs(t, `@html`, `"<&>'\""`, `"&lt;&amp;&gt;&apos;&quot;"`)
	wantOutputs(t, `@base64d, ("YQ" | @base64d), ("YQ=x" | @base64d)`, `"aGk="`, `"hi"`, `"a"`, `"a"`)
	wantOutputs(t, `@base64d | explode`, `"/w=="`, `[65533]`)
	wantError(t, `@base64d`, `"Y"`, `string ("Y") trailing base64 byte found`)
	wantError(t, `@base64d`, `"!!!!"`, `string ("!!!!") is not valid base64 data`)
	wantOutputs(t, `@base32, (@base32 | @base32d)`, `"hi"`, `"NBUQ===="`, `"hi"`)
	wantOutputs(t, `@text, @json`, `{"a":1}`, `"{\"a\":1}"`, `"{\"a\":1}"`)
	wantOutputs(t, `format("csv"), format("text")`, `[1]`, `"1"`, `"[1]"`)
	wantError(t, `format(1)`, `null`, `number (1) is not a valid format`)
	wantError(t, `format("foo")`, `null`, `foo is not a valid format`)
	wantError(t, `@foo`, `null`, `foo is not a valid format`)
	wantError(t, `@foo "x\(.)"`, `null`, `foo is not a valid format`)
	wantOutputs(t, `@sh "echo \(.)"`, `"a'b"`, `"echo 'a'\\''b'"`)
}

func TestJQEnvironmentAndInputs(t *testing.T) {
	wantOutputs(t, `$__loc__`, `null`, `{"file":"<top-level>","line":1}`)
	wantOutputs(t, `input_line_number, input_filename`, `null`, `0`, `null`)
	wantError(t, `input`, `null`, `No more inputs`)
	wantOutputs(t, `[inputs]`, `null`, `[]`)
	wantOutputs(t, `[builtins[] | select(startswith("_") or startswith("@"))]`, `null`, `[]`)
	wantOutputs(t, `builtins | index("map/1", "path/1") != null`, `null`, `true`, `true`)

	var buf bytes.Buffer
	if _, err := run(t, `debug, debug("m", .+1), stderr`, value.Num(1), runtime.NumbersExact, &buf); err != nil {
		t.Fatal(err)
	}
	wantEqual(t, buf.String(), "[\"DEBUG:\",1]\n[\"DEBUG:\",\"m\"]\n[\"DEBUG:\",2]\n1")
}

func TestJQEnvDisabled(t *testing.T) {
	p, err := jq.Compile(`env`)
	if err != nil {
		t.Fatal(err)
	}
	ctx := runtime.NewCtx(jq.New(), runtime.NumbersExact)
	ctx.Caps.Env = false
	err = p.Run(ctx, p.Env, []*value.Value{value.Null()}, func(*value.Value, value.Path) error { return nil })
	if err == nil || err.Error() != "env operations are disabled" {
		t.Errorf("got %v", err)
	}
}
