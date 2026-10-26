package yq

import (
	"testing"

	"github.com/fivetwenty-io/graft/internal/query/ast"
	"github.com/fivetwenty-io/graft/internal/query/parser"
)

func TestYQPrecedenceIsRightAssociative(t *testing.T) {
	g := parser.YQ()
	wantEqual(t, ast.Print(mustParse(t, `1 - 2 - 3`, g)), `(1 - (2 - 3))`)
	wantEqual(t, ast.Print(mustParse(t, `.a // .b | .c`, g)), `((.a // .b) | .c)`)
	wantEqual(t, ast.Print(mustParse(t, `.a = .b | .c`, g)), `((.a = .b) | .c)`)
	wantEqual(t, ast.Print(mustParse(t, `.a, .b | .c`, g)), `(.a, (.b | .c))`) // , binds looser than | in yq
	wantEqual(t, ast.Print(mustParse(t, `2 * 3 + 1`, g)), `(2 * (3 + 1))`)
	wantEqual(t, ast.Print(mustParse(t, `.a == 1 and .b`, g)), `((.a == 1) and .b)`)
	wantEqual(t, ast.Print(mustParse(t, `.a = 1 + 2`, g)), `(.a = (1 + 2))`)
	wantYQ(t, `2 * 3 + 1`, "null\n", "8\n")
}

func TestYQKeywordForms(t *testing.T) {
	g := parser.YQ()
	wantEqual(t, ast.Print(mustParse(t, `.[] as $x ireduce (0; . + $x)`, g)), `reduce .[] as $x (0; (. + $x))`)
	wantEqual(t, ast.Print(mustParse(t, `.[] as $x ireduce (0; . + $x) | . * 2`, g)), `(reduce .[] as $x (0; (. + $x)) | (. * 2))`)
	wantEqual(t, ast.Print(mustParse(t, `...`, g)), `...`)
	wantEqual(t, ast.Print(mustParse(t, `if . then 1 else 2 end`, g)), `if . then 1 else 2 end`)
	for _, src := range []string{`def f: 1; f`, `reduce .[] as $x (0; 1)`, `try .a`, `label $f | 1`} {
		if _, err := Parse(src); err == nil {
			t.Fatalf("Parse(%q) succeeded, want an error", src)
		}
	}
	wantYQ(t, `.b[] as $i ireduce (0; . + $i)`, "b: [1, 2]\n", "3\n")
	wantYQ(t, `.a | . as $x | path`, "a: 1\n", "- a\n")
}

func TestYQMergeFlags(t *testing.T) {
	g := parser.YQ()
	wantEqual(t, ast.Print(mustParse(t, `.a *+ .b`, g)), `(.a *+ .b)`)
	wantEqual(t, ast.Print(mustParse(t, `.a *=d .b`, g)), `(.a *=d .b)`)
	wantEqual(t, ast.Print(mustParse(t, `.a *?n .b * 2`, g)), `(.a *?n (.b * 2))`)
}

func TestYQParseErrors(t *testing.T) {
	cases := map[string]string{
		`.a +`:             `'+' expects 2 args but there is 1`,
		`.a |`:             `'|' expects 2 args but there is 1`,
		`| .a`:             `'|' expects 2 args but there is 1`,
		`.a =`:             `'=' expects 2 args but there is 1`,
		`.a,`:              `',' expects 2 args but there is 1`,
		`.a and`:           `'and' expects 2 args but there is 1`,
		`(.a`:              `bad expression - probably missing close bracket on (`,
		`.a)`:              `bad expression, got close brackets without matching opening bracket`,
		`.[`:               "bad expression, could not find matching `]`",
		`.[] as [$a] | $a`: `RHS of 'as' operator must be a variable name e.g. $foo`,
	}
	for src, want := range cases {
		_, err := Parse(src)
		wantErrContains(t, err, want)
		wantEqual(t, err.Error(), want)
	}
	n, err := Parse("  ")
	if err != nil {
		t.Fatal(err)
	}
	wantEqual(t, ast.Print(n), `.`)
}

func TestYQRefusesNamesItsLexerLacks(t *testing.T) {
	cases := map[string]string{
		`empty`:                   `1:1: lexer: invalid input text "empty"`,
		`.a | empty`:              `1:6: lexer: invalid input text "empty"`,
		"[splat]":                 `1:2: lexer: invalid input text "splat]"`,
		`paths`:                   `1:5: lexer: invalid input text "s"`,
		`keysort`:                 `1:7: lexer: invalid input text "t"`,
		`ascii_downcase`:          `1:3: lexer: invalid input text "cii_downcase"`,
		`reduce .[] as $x (0; .)`: `1:1: lexer: invalid input text "reduce .[] as $x..."`,
		`def f: .; f`:             `1:1: lexer: invalid input text "def f: .; f"`,
		`try .a`:                  `1:1: lexer: invalid input text "try .a"`,
		`{a: 1}`:                  `1:2: lexer: invalid input text "a: 1}"`,
		`env`:                     `1:1: lexer: invalid input text "env"`,
		`comments`:                `1:1: lexer: invalid input text "comments"`,
		`(empty`:                  `1:2: lexer: invalid input text "empty"`,
		".a |\n  empty":           `2:3: lexer: invalid input text "empty"`,
		`.a | @html`:              `1:6: lexer: invalid input text "@html"`,
		`"x\(.a | empty)"`:        `1:6: lexer: invalid input text "empty"`,
		"\"a\\(.a |\n empty)\"":   `2:2: lexer: invalid input text "empty"`,
	}
	for src, want := range cases {
		_, err := Parse(src)
		if err == nil {
			t.Fatalf("Parse(%q) succeeded, want %q", src, want)
		}
		wantEqual(t, err.Error(), want)
	}
	for _, src := range []string{
		`parent(1)`, `flatten(1)`, `to_yaml(2)`, `.[] as $x ireduce (0; .)`,
		`line_comment`, `@base64d`, `di`, `fi`, `type`, `true`, `not`,
	} {
		if _, err := Parse(src); err != nil {
			t.Fatalf("Parse(%q): %v", src, err)
		}
	}
}

// Every name the yq preludes define in place of jq's is one yq's lexer
// reads, and so is an internal _yq_ name, which those definitions call.
func TestYQAdmitsItsPreludeNames(t *testing.T) {
	names := []string{"_yq_internal"}
	for _, prog := range registry().Prelude()[1:] {
		for def, ok := prog.(*ast.FuncDef); ok; def, ok = def.Rest.(*ast.FuncDef) {
			names = append(names, def.Name)
		}
	}
	for _, name := range names {
		if _, err := Parse(name); err != nil {
			t.Fatalf("Parse(%q): %v", name, err)
		}
	}
}
