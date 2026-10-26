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
