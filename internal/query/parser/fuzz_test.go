package parser

import "testing"

func FuzzParse(f *testing.F) {
	for _, s := range []string{`.a[0] | {b: .c}`, `reduce .[] as $x (0; . + $x)`, `try error("x") catch .`, `[.[] | select(. > 1)] | length`, `"a\(.b)c"`, `.. | .a?`, `label $out | 1, break $out`, `{(.a): 1, "b": 2, $c, @base64: 3}`, `if . then 1 elif 2 then 3 end`, "1 \x00"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		_, _ = Parse(src, JQ())
	})
}
