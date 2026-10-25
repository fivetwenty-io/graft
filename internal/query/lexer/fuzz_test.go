package lexer

import "testing"

func FuzzTokenize(f *testing.F) {
	for _, s := range []string{`.a[0] | {b: .c}`, `"a\(1 + 2)b"`, `def f(x): x; f(.)`, `1e999999999 # c`, ".a\x00", `"\ud800"`, `@base64 "x\(.)"`, `?// . as [$a] ?// $a | $a`} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		for _, set := range []TokenSet{JQ(), YQ()} {
			_, _ = Tokenize(src, set)
		}
	})
}
