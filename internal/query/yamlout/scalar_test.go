package yamlout

import (
	"testing"

	"github.com/fivetwenty-io/graft/pkg/query/value"
)

func TestScalarStyleChoices(t *testing.T) {
	cases := []struct {
		text string
		sc   scalarContext
		want Style
	}{
		{"plain", scalarContext{}, StylePlain},
		{"", scalarContext{}, StyleDoubleQuoted},
		{"a\nb", scalarContext{}, StyleLiteral},
		{"a\nb", scalarContext{Key: true}, StyleDoubleQuoted},
		{"a \nb", scalarContext{}, StyleDoubleQuoted},
		{"a\r\nb", scalarContext{}, StyleDoubleQuoted},
		{"\x01", scalarContext{}, StyleDoubleQuoted},
		{" lead", scalarContext{}, StyleSingleQuoted},
		{"[x]", scalarContext{Flow: true}, StyleDoubleQuoted},
		{"a,b", scalarContext{Flow: true}, StyleDoubleQuoted},
		{"10", scalarContext{Key: true}, StyleDoubleQuoted},
		{"1:30", scalarContext{}, StyleDoubleQuoted},
		{"0b101", scalarContext{}, StyleDoubleQuoted},
		{"1.2.3", scalarContext{}, StylePlain},
		{"y", scalarContext{}, StylePlain},
	}
	for _, c := range cases {
		if got := scalarStyle(c.text, c.sc); got != c.want {
			t.Errorf("scalarStyle(%q, %+v) = %d, want %d", c.text, c.sc, got, c.want)
		}
	}
}

func TestDoubleQuotedEscapes(t *testing.T) {
	wantEqual(t, doubleQuoted("a\"b\\c\t\n\x01\u0085  é"), `"a\"b\\c\t\n\x01\N\_\Lé"`)
	wantEqual(t, doubleQuoted("\U0001F600"+string(rune(0xfeff))), "\"\U0001F600\\uFEFF\"")
	wantEqual(t, doubleQuoted("\xff"), `"\xFF"`)
}

func TestLiteralChomping(t *testing.T) {
	v := obj(pair("a", value.Str("x\ny")), pair("b", value.Str("x\ny\n")), pair("c", value.Str("x\ny\n\n")), pair("d", value.Str("x\n\ny")))
	wantEqual(t, emitOne(t, v, Options{}), "a: |-\n  x\n  y\nb: |\n  x\n  y\nc: |+\n  x\n  y\n\nd: |-\n  x\n\n  y\n")
}

func TestYAMLNumber(t *testing.T) {
	wantEqual(t, yamlNumber(lit("1.50"), false), "1.50")
	wantEqual(t, yamlNumber(dec("0.1"), true), "0.1")
	wantEqual(t, yamlNumber(value.Num(-1), false), "-1")
}
