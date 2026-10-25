package parser

import (
	"errors"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/query/ast"
)

func TestParseErrors(t *testing.T) {
	_, err := Parse(`.a |`, JQ())
	wantErrContains(t, err, "unexpected end of input")
	_, err = Parse(`if . then 1`, JQ())
	wantErrContains(t, err, "expected 'end'")
	_, err = Parse(`{(1): 2`, JQ())
	wantErrContains(t, err, "expected '}'")
	_, err = Parse(`1 2`, JQ())
	wantErrContains(t, err, "unexpected")
	_, err = Parse(`)`, JQ())
	wantErrContains(t, err, "unexpected ')'")
	_, err = Parse(`[1, 2`, JQ())
	wantErrContains(t, err, "expected ']'")
	_, err = Parse(`reduce . as $x (0)`, JQ())
	wantErrContains(t, err, "expected ';'")
	_, err = Parse(`"abc`, JQ())
	wantErrContains(t, err, "unterminated string")
}

func TestParseErrorCarriesPosition(t *testing.T) {
	_, err := Parse(`1 + )`, JQ())
	var pe *Error
	if !errors.As(err, &pe) {
		t.Fatalf("error type = %T, want *Error", err)
	}
	wantEqual(t, pe.Pos, 4)
}

func TestObjectConstructionShorthands(t *testing.T) {
	wantEqual(t, ast.Print(mustParse(t, `{a, $b, "c": 1, (.d): 2, @base64 "e": 3, $__loc__}`, JQ())),
		`{a: .a, $b, "c": 1, (.d): 2, @base64 "e": 3, "__loc__": $__loc__}`)
	wantEqual(t, ast.Print(mustParse(t, `{"a b"}`, JQ())), `{"a b": .["a b"]}`)
	wantEqual(t, ast.Print(mustParse(t, `{if: 1, and: 2}`, JQ())), `{if: 1, and: 2}`)
	_, err := Parse(`{a: 1, }`, JQ())
	wantErrContains(t, err, "unexpected '}'")
}

func TestModuleDirectivesParse(t *testing.T) {
	n := mustParse(t, `module {v: 1}; import "a" as a; include "b"; a::f`, JQ())
	wantEqual(t, ast.Print(n), `module {v: 1}; import "a" as a; include "b"; a::f`)
	n = mustParse(t, `import "d" as $d {search: "."}; include "b" {x: 1}; $d`, JQ())
	wantEqual(t, ast.Print(n), `import "d" as $d {search: "."}; include "b" {x: 1}; $d`)
	n = mustParse(t, `import "a" as a;`, JQ())
	wantEqual(t, ast.Print(n), `import "a" as a;`)
	_, err := Parse(`1; import "a" as a; 2`, JQ())
	wantErrContains(t, err, "unexpected")
}

func TestLocResolvesToFileAndLine(t *testing.T) {
	n := mustParse(t, "1,\n\n$__loc__", JQ())
	c := n.(*ast.Comma)
	loc := c.R.(*ast.Loc)
	wantEqual(t, loc.File, "<top-level>")
	wantEqual(t, loc.Line, 3)
	n, err := ParseFile("$__loc__", "mod.jq", JQ())
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, n.(*ast.Loc).File, "mod.jq")
	wantEqual(t, n.(*ast.Loc).Line, 1)
}

func TestNodeShapes(t *testing.T) {
	f := mustParse(t, `.a[0]?`, JQ()).(*ast.Index)
	wantEqual(t, f.Opt, true)
	if _, ok := f.Target.(*ast.Field); !ok {
		t.Fatalf("target = %T, want *ast.Field", f.Target)
	}
	lit := mustParse(t, `1.50`, JQ()).(*ast.Literal)
	wantEqual(t, lit.Value.(ast.Number), ast.Number("1.50"))
	s := mustParse(t, `"aé"`, JQ()).(*ast.Str)
	wantEqual(t, s.Parts[0].(*ast.Literal).Value.(string), "aé")
}

func TestDeepNestingIsBounded(t *testing.T) {
	src := ""
	for i := 0; i < 20000; i++ {
		src += "("
	}
	_, err := Parse(src, JQ())
	wantErrContains(t, err, "nesting")
}

func TestRealWorldProgramsKeepTheirShape(t *testing.T) {
	cases := map[string]string{
		`.. | .a? // empty`: `(.. | ((.a)? // empty))`,
		`.[] | select(.id == 1) | {id, name: .n, tags: [.t[]?]}`: `(.[] | (select((.id == 1)) | {id: .id, name: .n, tags: [(.t[]?)]}))`,
		`def walk(f): . as $in | if type == "object" then reduce keys_unsorted[] as $k ({}; . + {($k): ($in[$k] | walk(f))}) | f else f end; walk(.)`: `def walk(f): . as $in | if (type == "object") then (reduce (keys_unsorted[]) as $k ({}; (. + {$k: (($in[$k]) | walk(f))})) | f) else f end; walk(.)`,
		`to_entries | map(select(.value != null)) | from_entries`:                                                                                     `(to_entries | (map(select((.value != null))) | from_entries))`,
		`[.[] | tostring] | join(",")`:          `([(.[] | tostring)] | join(","))`,
		`.a as [$x, $y] ?// {a: $x} | [$x, $y]`: `.a as [$x, $y] ?// {a: $x} | [($x, $y)]`,
		`try (.a | error) catch "x\(.)y"`:       `try (.a | error) catch "x\(.)y"`,
		`limit(3; range(10)) | . as $n | label $done | if $n > 1 then break $done else $n end`: `(limit(3; range(10)) | (. as $n | label $done | if ($n > 1) then break $done else $n end))`,
		`"\(.a) and \("nested \(.b)")"`:                 `"\(.a) and \("nested \(.b)")"`,
		`.a += 1 | .b //= 2 | .c |= map(. * 2)`:         `((.a += 1) | ((.b //= 2) | (.c |= map((. * 2)))))`,
		`path(..) | select(length > 0)`:                 `(path(..) | select((length > 0)))`,
		`.[-1:] | .[0] , .["x"]`:                        `(.[(-1):] | (.[0], .["x"]))`,
		`@sh "echo \(.a)", @json, @csv`:                 `((@sh "echo \(.a)", @json), @csv)`,
		`input_line_number, $ENV.HOME, $__prog_args`:    `((input_line_number, ($ENV.HOME)), $__prog_args)`,
		`reduce range(5) as $i ([]; . + [$i]) | .[2:4]`: `(reduce range(5) as $i ([]; (. + [$i])) | .[2:4])`,
		`first(.[] | select(. > 1)) // "none"`:          `(first((.[] | select((. > 1)))) // "none")`,
		`not`:                                           `not`,
		`.a.b.c?`:                                       `((.a.b).c?)`,
	}
	for src, want := range cases {
		wantEqual(t, ast.Print(mustParse(t, src, JQ())), want)
	}
}

func TestModuleAndBreakAreKeywords(t *testing.T) {
	wantEqual(t, ast.Print(mustParse(t, `label $l | break $l`, JQ())), `label $l | break $l`)
	_, err := Parse(`break`, JQ())
	wantErrContains(t, err, "expected '$name'")
	_, err = Parse(`1 | module {}; 2`, JQ())
	wantErrContains(t, err, "unexpected 'module'")
	wantEqual(t, ast.Print(mustParse(t, `module 1; 2`, JQ())), `module 1; 2`)
}

func TestLexerErrorsArriveAsParserErrors(t *testing.T) {
	_, err := Parse(`"abc`, JQ())
	var pe *Error
	if !errors.As(err, &pe) {
		t.Fatalf("error type = %T, want *Error", err)
	}
	wantErrContains(t, err, "unterminated string")
}

func TestNestingGuardCoversPatternsAndObjectValues(t *testing.T) {
	_, err := Parse(". as "+strings.Repeat("[", 20000)+"$a"+strings.Repeat("]", 20000)+" | 1", JQ())
	wantErrContains(t, err, "nesting")
	_, err = Parse("{a: "+strings.Repeat("1 | ", 20000)+"1}", JQ())
	wantErrContains(t, err, "nesting")
}

func TestStringShorthandKeyAndIndexAreDistinctNodes(t *testing.T) {
	e := mustParse(t, `{"a b"}`, JQ()).(*ast.ObjectCons).Entries[0]
	if e.Key == e.Val.(*ast.Index).Key {
		t.Fatal("shorthand key and index key are the same node")
	}
	wantEqual(t, ast.Print(e.Key), ast.Print(e.Val.(*ast.Index).Key))
}

func TestOptionalFieldHasOneShape(t *testing.T) {
	f := mustParse(t, `.a?`, JQ()).(*ast.Field)
	wantEqual(t, f.Opt, true)
	f = mustParse(t, `.b.a?`, JQ()).(*ast.Field)
	wantEqual(t, f.Opt, true)
	if _, ok := mustParse(t, `(.b.a)?`, JQ()).(*ast.Optional); !ok {
		t.Fatal("a parenthesized term should wrap in ast.Optional")
	}
}
