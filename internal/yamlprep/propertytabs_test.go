package yamlprep

import (
	"strings"
	"testing"

	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/token"
)

// TestTokenizePropertyTabs checks that a tag or an anchor with a tab
// after it gives every token the type, the value, and the line goccy
// gives it when the tabs are spaces, which it reads right, and the column
// it gives it there less one for each tab before that column on its line
// outside a double-quoted scalar, since goccy counts no tab between
// tokens. The column a token takes is the one the text with spaces gives
// it, even where goccy places it at a blank before its first character,
// as it does after a tag. The
// tab must stay in the source text of the tag, or of the token after an
// anchor's name. A tab inside a plain scalar is left out here, since
// goccy drops it from the value and the parse puts it back.
func TestTokenizePropertyTabs(t *testing.T) {
	for _, in := range []string{
		"a: !!str\tx\nb: 1\n",
		"a: !!int\t12\nb: 1\n",
		"a: !custom\tbar\nb: 1\n",
		"a: !<tag:yaml.org,2002:str>\tx\nb: 1\n",
		"a: !\tx\nb: 1\n",
		"a: !!str\t\tx\nb: 1\n",
		"a: !!str\t# c\nb: 1\n",
		"a: !!str\t|\n  x\nb: 1\n",
		"a: !!map\t{k: v}\nb: 1\n",
		"!!str\tk: v\nb: 1\n",
		"a:\n- !!str\tx\n- z\n",
		"a: [!!str\tx, z]\nb: 1\n",
		"a: {k: !!str\tx, m: z}\nb: 1\n",
		"a: {!!str\tk: v}\nb: 1\n",
		"a: &n\tx\nb: *n\n",
		"a: &n\t{k: v}\nb: *n\n",
		"&n\tk: v\nb: 1\n",
		"a: [&n\tx, *n]\n",
		"a: &n\t!!str\tx\nb: *n\n",
		"a: !!str\t&n\tx\nb: *n\n",
		"a: [&n\t!!str\tyes, *n]\n",
		"a: !!str\t\"p\tq\"\nb: 1\n",
		"a: &n\t\"p\tq\"\nb: *n\n",
		"a: !!str\tx",
		"a: &n\tx",
		"a: !!str\t \t# c\nb: 1\n",
		"a:\t!!str\tx\nb: 1\n",
		"a: [!!int\t3, !!str\t\tz, &n\tq]\n",
		"!\t",
	} {
		toks := Tokenize(in)
		got := rowsOf(toks)
		want := rowsOf(lexer.Tokenize(strings.ReplaceAll(in, "\t", " ")))
		if len(got) != len(want) {
			t.Errorf("Tokenize(%q) gave %d tokens, want %d: %+v", in, len(got), len(want), got)
			continue
		}
		for i := range got {
			g, w := got[i], want[i]
			// An anchor's name moves as its "&" does.
			at := w
			if i > 0 && want[i-1].typ == token.AnchorType {
				at = want[i-1]
			}
			if g.typ != w.typ || g.line != w.line || g.column != w.column-uncountedTabs(in, at.line, at.column) ||
				strings.ReplaceAll(g.value, "\t", " ") != w.value || strings.ReplaceAll(g.origin, "\t", " ") != w.origin {
				t.Errorf("Tokenize(%q) token %d = %+v, want %+v with its tabs", in, i, g, w)
			}
		}
		for i, tk := range toks {
			if !isPropertyName(toks, i) {
				continue
			}
			text := tk.Origin
			if i+1 < len(toks) {
				text += toks[i+1].Origin
			}
			if !strings.Contains(text, tk.Value+"\t") {
				t.Errorf("Tokenize(%q) token %d = %+v lost the tab after it", in, i, rowsOf(toks[i : i+1])[0])
			}
		}
	}
}

// uncountedTabs returns how many tabs outside double-quoted scalars the
// line of src holds before the character at column, counting a tab as
// one column, as the text with spaces for tabs does.
func uncountedTabs(src string, line, column int) int {
	text := []rune(strings.Split(src, "\n")[line-1])
	n, quoted := 0, false
	for i := 0; i < column-1 && i < len(text); i++ {
		switch text[i] {
		case '"':
			quoted = !quoted
		case '\t':
			if !quoted {
				n++
			}
		}
	}
	return n
}

// TestTokenizeKeepsPropertyLookalikes checks inputs whose tokens
// Tokenize leaves as goccy gives them: a tab follows a "!" or a "&" that
// starts no tag or anchor, or follows an alias, or a space comes between
// a tag or an anchor and the tab.
func TestTokenizeKeepsPropertyLookalikes(t *testing.T) {
	for _, in := range []string{
		"a: x!y\tz\n",
		"a: x&y\tz\n",
		"# !!str\tx\nb: 1\n",
		"a: |\n  !!str\tx\nb: 1\n",
		"a: '!!str\tx'\nb: 1\n",
		"a: &n x\nb: *n\t# c\nc: 1\n",
		"a: !!str \tx\nb: 1\n",
		"a: &n \tx\nb: *n\n",
		"&\t",
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

// TestMayHoldPropertyTab checks the cheap test Tokenize runs before it
// looks for a tab after a tag or an anchor.
func TestMayHoldPropertyTab(t *testing.T) {
	for in, want := range map[string]bool{
		"a: !!str\tx":  true,
		"a: [&n\tx]":   true,
		"a: x!y\tz":    true,
		"a: !!str x":   false,
		"a: x\ty":      false,
		"a: ! x\ty":    false,
		"a: !\n\tx":    false,
		"\t!!str":      false,
		"":             false,
		"a: &n\r\tx":   false,
		"a: !!str \tx": false,
	} {
		if got := mayHoldPropertyTab(in); got != want {
			t.Errorf("mayHoldPropertyTab(%q) = %v, want %v", in, got, want)
		}
	}
}

// TestGoccyColumn checks goccyColumn against the column Tokenize gives
// a scalar that follows the prefix on its line.
func TestGoccyColumn(t *testing.T) {
	for _, c := range []struct{ prefix, close string }{
		{"a: [", "]"},
		{"a: [x,\t", "]"},
		{"a: ['p\tq', ", "]"},
		{"a: ['it''s\tq', ", "]"},
		{"a: [\"p\tq\", ", "]"},
		{"a: [\"p\\\"\tq\", ", "]"},
		{"a: [\"p\\\\\", \t", "]"},
		{"a: [x'y,\t", "]"},
		// A quote right after a ":" that ends a key in a flow mapping
		// opens a quoted scalar, and one after any other ":" does not.
		{"a: {\"k\":\"p\tq\", m: ", "}"},
		{"a: {'k':'p\tq', m: ", "}"},
		{"a: {\"k\" :\"p\tq\", m: ", "}"},
		{"a: {k:\"p\tq\", m: ", "}"},
		{"a: {? k:\"p\tq\", m: ", "}"},
		{"a: {\"k\":\"p\\\"\tq\", m: ", "}"},
		{"a: {b: [x:\"p\tq\", ", "]}"},
		{"a: [{k:\"p\tq\", m: ", "}]"},
		{"a: {b: {k:\"p\tq\"}, m: ", "}"},
		{"a: {k: [x], m:\"p\tq\", n: ", "}"},
		{"a: {k: [\"x\":\"p\tq\"], m: ", "}"},
		{"a: [k:\"p\tq\", ", "]"},
		{"a: [\"k\":\"p\tq\", ", "]"},
		{"a: [[x], k:\"p\tq\", ", "]"},
		{"a: {k: x:\"p\tq\", m: ", "}"},
		{"a: {k::\"p\tq\", m: ", "}"},
		{"a: {k:x\"p\tq\", m: ", "}"},
	} {
		toks := Tokenize(c.prefix + "zz" + c.close + "\n")
		var col int
		for _, tk := range toks {
			if tk.Value == "zz" {
				col = tk.Position.Column
			}
		}
		line := c.prefix[strings.LastIndexByte(c.prefix, '\n')+1:]
		if got := goccyColumn(line); got != col {
			t.Errorf("goccyColumn(%q) = %d, want %d", line, got, col)
		}
	}
}
