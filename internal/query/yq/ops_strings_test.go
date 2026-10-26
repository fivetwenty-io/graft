package yq

import "testing"

func TestYQStrings(t *testing.T) {
	wantYQ(t, `split(",") | join("-")`, `"a,b"`, "a-b\n")
	wantYQ(t, `sub("a+", "b"), test("^a"), [match("a"; "g").string]`, `"aa"`, "b\ntrue\n- a\n- a\n")
	wantYQ(t, `to_number, (. | tostring | type)`, `"3"`, "3\n!!str\n")
	wantYQ(t, `"x\(.a)y"`, "a: 1\n", "x1y\n")
	wantYQ(t, `trim, upcase, downcase`, `" Ab "`, "Ab\n AB \n ab \n")
	wantYQ(t, `capture("(?P<x>[cC])at"), [match("(?i)cat"; "g").offset]`, `"cat Cat"`, "x: c\n- 0\n- 4\n")
	wantYQ(t, `sub("(?P<a>[0-9])-([0-9])", "${a}+$2")`, `"2-3"`, "2+3\n")
}

func TestYQMatchFlags(t *testing.T) {
	wantYQError(t, `test("a"; "gi")`, `"a"`, `'i' is not a valid option for match. To ignore case, use an expression like match("(?i)cat")`)
	wantYQError(t, `match("a"; "c")`, `"a"`, "unrecognised match params 'c', please see docs at https://mikefarah.gitbook.io/yq/operators/string-operators") //nolint:misspell // yq's own spelling
}

func TestYQEncoders(t *testing.T) {
	wantYQ(t, `@base64, (@base64 | @base64d), @json, @yaml`, `"a"`, "YQ==\na\n\"a\"\na\n\n")
	wantYQ(t, `to_yaml, (to_json | from_json), (to_yaml | from_yaml)`, "a: 1\n", "a: 1\n\n{\"a\": 1}\na: 1\n")
	wantYQ(t, `@uri, @sh, (@uri | @urid), ([1, "x,y"] | @csv), (["a", "b"] | @tsv)`, `"a b"`, "a+b\na' b'\na b\n1,\"x,y\"\na\tb\n")
	wantYQ(t, `@base64d`, `"YQ"`, "a\n")
	wantYQ(t, `to_json(0), (1 | to_string)`, "1\n", "1\n1\n")
	wantYQError(t, `@base64`, "1\n", "cannot encode !!int as base64, can only operate on strings")
}
