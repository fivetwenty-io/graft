package parser

import (
	"sort"
	"testing"

	"github.com/fivetwenty-io/graft/internal/query/ast"
)

func TestPrecedenceRowsRoundTrip(t *testing.T) {
	cases := map[string]string{
		`1, 2 | 3`:                             `((1, 2) | 3)`,
		`1 | 2 | 3`:                            `(1 | (2 | 3))`,
		`1, 2, 3`:                              `((1, 2), 3)`,
		`.a // .b // .c`:                       `(.a // (.b // .c))`,
		`.a = 1 | .b`:                          `((.a = 1) | .b)`,
		`.a |= . + 1`:                          `(.a |= (. + 1))`,
		`.a = .b // 1`:                         `((.a = .b) // 1)`,
		`1 + 2 * 3 - 4`:                        `((1 + (2 * 3)) - 4)`,
		`1 - 2 - 3`:                            `((1 - 2) - 3)`,
		`8 / 4 % 3`:                            `((8 / 4) % 3)`,
		`.a and .b or .c`:                      `((.a and .b) or .c)`,
		`.a or .b and .c`:                      `(.a or (.b and .c))`,
		`1 == 2 and 3 < 4`:                     `((1 == 2) and (3 < 4))`,
		`.a? // 1`:                             `((.a)? // 1)`,
		`-1 - -2`:                              `((-1) - (-2))`,
		`.a[0].b[]?`:                           `(((.a[0]).b)[]?)`,
		`.[1:2]`:                               `.[1:2]`,
		`.a[:2]`:                               `(.a[:2])`,
		`.[1:]`:                                `.[1:]`,
		`.a."b"`:                               `(.a["b"])`,
		`."a"`:                                 `.["a"]`,
		`.a.[0]`:                               `(.a[0])`,
		`try error catch .`:                    `try error catch .`,
		`try error("x")`:                       `try error("x")`,
		`reduce .[] as $x (0; . + $x)`:         `reduce .[] as $x (0; (. + $x))`,
		`foreach .[] as $x (0; . + $x)`:        `foreach .[] as $x (0; (. + $x))`,
		`foreach .[] as [$a] (0; 1; 2)`:        `foreach .[] as [$a] (0; 1; 2)`,
		`def f(g; $x): g + $x; f(.; 1)`:        `def f(g; $x): (g + $x); f(.; 1)`,
		`def f: 1; def g: 2; f + g`:            `def f: 1; def g: 2; (f + g)`,
		`def f: 1;`:                            `def f: 1;`,
		`. as [$a, {b: $c}] ?// $d | $a`:       `. as [$a, {b: $c}] ?// $d | $a`,
		`. as {$a, "b": $c, (.d): [$e]} | $a`:  `. as {$a, "b": $c, (.d): [$e]} | $a`,
		`1 + . as $x | $x`:                     `(1 + (. as $x | $x))`,
		`label $out | 1, break $out`:           `label $out | (1, break $out)`,
		`"a\(1)b"`:                             `"a\(1)b"`,
		`@base64 "x\(.)"`:                      `@base64 "x\(.)"`,
		`@base64`:                              `@base64`,
		`if . then 1 end`:                      `if . then 1 end`,
		`if . then 1 elif 2 then 3 else 4 end`: `if . then 1 elif 2 then 3 else 4 end`,
		`if . then {} else [] end.a`:           `(if . then {} else [] end.a)`,
		`[.[] | . * 2]`:                        `[(.[] | (. * 2))]`,
		`[]`:                                   `[]`,
		`null, true, false, 1.5e3`:             `(((null, true), false), 1.5e3)`,
		`a::f(1)`:                              `a::f(1)`,
		`$x`:                                   `$x`,
		`{(.a, .b): 1 | 2}`:                    `{((.a, .b)): (1 | 2)}`,
		``:                                     `.`,
	}
	keys := make([]string, 0, len(cases))
	for k := range cases {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, src := range keys {
		wantEqual(t, ast.Print(mustParse(t, src, JQ())), cases[src])
	}
}

func TestNonAssociativeRowsRejectChains(t *testing.T) {
	for _, src := range []string{`1 < 2 < 3`, `1 == 2 != 3`, `.a = .b = 1`, `.a |= 1 += 2`} {
		_, err := Parse(src, JQ())
		wantErrContains(t, err, "non-associative")
	}
}

func TestToyGrammarProvesTableDriven(t *testing.T) {
	g := JQ()
	g.Ops = OperatorTable{{"+", 5, Right, Binary}, {"*", 10, Left, Binary}}
	wantEqual(t, ast.Print(mustParse(t, `1 * 2 + 3 + 4`, g)), `((1 * 2) + (3 + 4))`)
}

func TestFormsThatJQRejects(t *testing.T) {
	for _, src := range []string{
		`1 | def f: 1;`, `1, def f: 1;`, `. as $x | def f: 1;`, `1 + def f: 1;`,
		`1 + def f: 1; f`, `1 // label $x | 2`, `1 and def f: 1; f`,
		`{a: def f: 1; f}`, `{a: . as $x | 1}`, `{a: label $x | 1}`,
		`def if: 1; 2`, `def f(if): 1; 2`,
		`try 1 as $x | error("e") catch 5`, `try def f: 1; f`, `try label $x | 1`,
	} {
		if _, err := Parse(src, JQ()); err == nil {
			t.Errorf("Parse(%q) succeeded, want an error", src)
		}
	}
}
