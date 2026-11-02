package yamlnode

import (
	"strings"
	"testing"

	"github.com/goccy/go-yaml/token"

	"github.com/fivetwenty-io/graft/internal/yamlprep"
)

// tokenTypes lists the types of toks, with each tag's and name's text.
func tokenTypes(toks token.Tokens) string {
	var parts []string
	for _, tk := range toks {
		switch tk.Type {
		case token.TagType, token.StringType:
			parts = append(parts, tk.Value)
		default:
			parts = append(parts, tk.Type.String())
		}
	}
	return strings.Join(parts, " ")
}

// TestAnchorBeforeScalarTag checks which anchors move before the scalar
// core tag in front of them, and that the move leaves every token's
// position and its links in the text alone.
func TestAnchorBeforeScalarTag(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		// The anchor takes no scalar on its line, so it moves.
		{"a: !!str &n {k: v}\n", "a MappingValue Anchor n !!str MappingStart k MappingValue v MappingEnd"},
		{"a: !!str &n\n  x\n", "a MappingValue Anchor n !!str x"},
		{"a: !!int\n  &n\n  1\n", "a MappingValue Anchor n !!int Integer"},
		{"a: !!str # c\n  &n\n  x\n", "a MappingValue Anchor n !!str Comment x"},
		// The anchor takes the scalar on its line, which parses right.
		{"a: !!str &n x\n", "a MappingValue !!str Anchor n x"},
		{"a: !!str &n |\n  x\n", "a MappingValue !!str Anchor n Literal x\n"},
		{"a: !!str &n *m\n", "a MappingValue !!str Anchor n Alias m"},
		// A tag that is no scalar core tag, an anchor first, and an
		// anchor with nothing after it stay as they are.
		{"a: !custom &n {k: v}\n", "a MappingValue !custom Anchor n MappingStart k MappingValue v MappingEnd"},
		{"a: !!map &n {k: v}\n", "a MappingValue !!map Anchor n MappingStart k MappingValue v MappingEnd"},
		{"a: &n !!str {k: v}\n", "a MappingValue Anchor n !!str MappingStart k MappingValue v MappingEnd"},
		{"a: !!str &n", "a MappingValue !!str Anchor n"},
	} {
		toks := yamlprep.Tokenize(c.in)
		before := make(map[*token.Token][3]any, len(toks))
		for _, tk := range toks {
			before[tk] = [3]any{*tk.Position, tk.Prev, tk.Next}
		}
		anchorBeforeScalarTag(toks)
		if got := tokenTypes(toks); got != c.want {
			t.Errorf("%q: tokens %s, want %s", c.in, got, c.want)
		}
		for _, tk := range toks {
			if was := before[tk]; was != [3]any{*tk.Position, tk.Prev, tk.Next} {
				t.Errorf("%q: token %q moved from %v to %v:%v", c.in, tk.Value, was[0], tk.Position.Line, tk.Position.Column)
			}
		}
	}
}

// TestKindTagRetryAfterAnAnchor checks that a scalar core tag over a
// collection with an anchor between them is blanked for the retry, as it
// is with no anchor, and that a tag found after a tab is blanked where
// it sits.
func TestKindTagRetryAfterAnAnchor(t *testing.T) {
	for _, c := range []struct{ in, want string }{
		{"a: !!str &n {k: v}\n", "a: !xxxx &n {k: v}\n"},
		{"a: !!str\t&n\t[x]\n", "a: !xxxx\t&n\t[x]\n"},
		{"a: &n\t!!str\t[x]\n", "a: &n\t!xxxx\t[x]\n"},
		{"a: !!str &n\n  k: v\n", "a: !xxxx &n\n  k: v\n"},
		// goccy blames the anchor here, not the sequence.
		{"!!map &a\n- x\n", "!xxxx &a\n- x\n"},
	} {
		_, err := ParseBytes([]byte(c.in), 0)
		if err == nil {
			t.Fatalf("ParseBytes(%q) accepted it", c.in)
		}
		text, tags, ok := neutralizeKindTags(c.in, err)
		if !ok || text != c.want || len(tags) != 1 {
			t.Errorf("neutralizeKindTags(%q) = %q, %v, %v, want %q", c.in, text, tags, ok, c.want)
		}
	}
}

// TestTagStart checks where blankTags finds a tag from its token's
// column, which counts no tab between tokens.
func TestTagStart(t *testing.T) {
	for _, c := range []struct {
		line   string
		col    int
		want   int
		wantOK bool
	}{
		{"a: !!str x", 4, 3, true},
		{"a: &n\t!!str\t[x]", 6, 6, true},
		{"a:\t\t!!str x", 3, 4, true},
		{"a: \"p\tq\" !!str x", 10, 9, true},
		{"a: x", 4, 0, false},
		{"a: !!str x", 5, 0, false},
	} {
		got, ok := tagStart([]rune(c.line), c.col, "!!str")
		if got != c.want || ok != c.wantOK {
			t.Errorf("tagStart(%q, %d) = %d, %v, want %d, %v", c.line, c.col, got, ok, c.want, c.wantOK)
		}
	}
}

// TestFirstToken checks that a node's properties start at whichever of
// its tag and anchor comes first in the text.
func TestFirstToken(t *testing.T) {
	at := func(line, col int) *token.Token {
		return &token.Token{Position: &token.Position{Line: line, Column: col}}
	}
	tag, anchor, below := at(1, 4), at(1, 10), at(2, 3)
	for _, c := range []struct{ a, b, want *token.Token }{
		{nil, tag, tag},
		{tag, nil, tag},
		{tag, anchor, tag},
		{anchor, tag, tag},
		{below, tag, tag},
		{tag, below, tag},
	} {
		if got := firstToken(c.a, c.b); got != c.want {
			t.Errorf("firstToken(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
