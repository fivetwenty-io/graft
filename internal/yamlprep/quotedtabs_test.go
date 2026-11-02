package yamlprep

import (
	"slices"
	"strings"
	"testing"

	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/token"
)

// tokenRow is a token's type, value, source text, and place.
type tokenRow struct {
	typ          token.Type
	value        string
	origin       string
	line, column int
}

func rowsOf(toks token.Tokens) []tokenRow {
	rows := make([]tokenRow, len(toks))
	for i, tk := range toks {
		rows[i] = tokenRow{tk.Type, tk.Value, tk.Origin, tk.Position.Line, tk.Position.Column}
	}
	return rows
}

// TestTokenizeQuotedTabs checks that each double-quoted scalar holding a
// tab comes back with its tab, and that every token has the type and the
// place goccy gives it when the tabs are spaces, which it reads right.
// goccy counts a tab inside a quoted scalar as one column.
func TestTokenizeQuotedTabs(t *testing.T) {
	for _, in := range []string{
		"a: \"p\tq\"\nb: 1\n",
		"a: \"p\tq\" # c\nb: 1\n",
		"a: [\"p\tq\", \"r\t\ts\"]\nb: 1\n",
		"a: {k: \"p\tq\", m: x}\n",
		"\"p\tq\": 1\nb: 2\n",
		"a: \"p\tq\n  r\ts\"\nb: 1\n",
		"a: !!str \"p\tq\"\nb: 1\n",
		"a: &n \"p\tq\"\nb: *n\n",
		"- \"é\tü\"\n- x\n",
		"a: \"x\\\"p\tq\\\"\"\nb: 1\n",
		"a: \"p\tq\"",
	} {
		got := rowsOf(Tokenize(in))
		want := rowsOf(lexer.Tokenize(strings.ReplaceAll(in, "\t", " ")))
		if len(got) != len(want) {
			t.Errorf("Tokenize(%q) gave %d tokens, want %d: %+v", in, len(got), len(want), got)
			continue
		}
		for i := range got {
			g, w := got[i], want[i]
			if g.typ != w.typ || g.line != w.line || g.column != w.column ||
				strings.ReplaceAll(g.value, "\t", " ") != w.value || strings.ReplaceAll(g.origin, "\t", " ") != w.origin {
				t.Errorf("Tokenize(%q) token %d = %+v, want %+v with its tabs", in, i, g, w)
			}
			if g.typ == token.DoubleQuoteType && !strings.Contains(g.value, "\t") {
				t.Errorf("Tokenize(%q) token %d = %+v lost its tab", in, i, g)
			}
		}
	}
}

// TestTokenizeKeepsTabsGoccyReads checks inputs whose tokens Tokenize
// leaves as goccy gives them: no double-quoted scalar holds a tab that
// goccy counts twice.
func TestTokenizeKeepsTabsGoccyReads(t *testing.T) {
	for _, in := range []string{
		"a: x\ty\n",
		"a: 'p\tq'\nb: 1\n",
		"# \"p\tq\"\nb: 1\n",
		"a: |\n  \"p\tq\"\nb: 1\n",
		"a: x\"p\tq\"\nb: 1\n",
		"a: \"p\t\"\nb: 1\n",
		"a: \"p\t\n  q\"\nb: 1\n",
		"a: \"p\\\n  \tq\"\nb: 1\n",
		"a: \"p\\tq\"\nb: 1\n",
		"a: \"p\tq\nb: 1\n",
		"a: \"unclosed\t",
		"\"\t",
		"\t\"",
		"",
	} {
		got, want := rowsOf(Tokenize(in)), rowsOf(lexer.Tokenize(in))
		if len(got) != len(want) {
			t.Errorf("Tokenize(%q) gave %d tokens, want %d", in, len(got), len(want))
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("Tokenize(%q) token %d = %+v, want %+v", in, i, got[i], want[i])
			}
		}
	}
}

// TestTokenizeMarkerNotInInput checks that the marker for a tab is a
// character the input does not hold, and never reaches a token.
func TestTokenizeMarkerNotInInput(t *testing.T) {
	in := "a: \"p\tq\"\nb: \"\ue000\ue001\"\nc: 1\n"
	toks := Tokenize(in)
	rows := rowsOf(toks)
	if len(rows) != 9 || rows[2].value != "p\tq" || rows[5].value != "\ue000\ue001" || rows[6].line != 3 {
		t.Fatalf("Tokenize(%q) = %+v", in, rows)
	}
	if r, ok := unusedRune(in); !ok || r != '\ue002' {
		t.Errorf("unusedRune(%q) = %U, %v; want U+E002", in, r, ok)
	}
}

func TestQuotedTabs(t *testing.T) {
	cases := []struct {
		src  string
		want []int
	}{
		{`"p` + "\t" + `q"`, []int{2}},
		{`"p` + "\t\t" + `q"`, []int{2, 3}},
		{`"p` + "\t" + `"`, []int{2}},
		{`"p` + "\t\n" + `q"`, nil},
		{`"p` + "\n\t" + `q"`, nil},
		{`"p\` + "\t" + `q"`, nil},
		{`"\"` + "\t" + `q"`, []int{3}},
		{`"\x41` + "\t" + `q"`, []int{5}},
		{`"\` + "u0041\tq\"", []int{7}},
		{`"p\` + "\r\n\t" + `q"`, nil},
		{`"p` + "\t" + `q`, nil},
		{`"p\`, nil},
	}
	for _, c := range cases {
		got := quotedTabs(c.src, 0)
		if !slices.Equal(got, c.want) {
			t.Errorf("quotedTabs(%q) = %v, want %v", c.src, got, c.want)
		}
	}
}
