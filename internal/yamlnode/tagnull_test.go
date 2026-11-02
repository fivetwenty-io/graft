package yamlnode

import (
	"fmt"
	"testing"

	"github.com/goccy/go-yaml/token"

	"github.com/fivetwenty-io/graft/internal/yamlprep"
)

// TestEmptyTagValues checks which tags get an implicit null, which tags
// it reports as collection tags, and that the tokens already there keep
// their places and their links in the text.
func TestEmptyTagValues(t *testing.T) {
	for _, c := range []struct {
		in, want    string
		collections int
	}{
		// A tag with no value before a sibling key or item.
		{"a: !custom\nb: 1\n", "a MappingValue !custom ImplicitNull b MappingValue Integer", 0},
		{"a: !custom &n # c\nb: 1\n", "a MappingValue !custom Anchor n ImplicitNull Comment b MappingValue Integer", 0},
		{"a: &n !custom\n\nb: 1\n", "a MappingValue Anchor n !custom ImplicitNull b MappingValue Integer", 0},
		{"a: !!map &n\nb: 1\n", "a MappingValue !!map Anchor n ImplicitNull b MappingValue Integer", 1},
		{"l:\n- !custom\n- 2\n", "l MappingValue SequenceEntry !custom ImplicitNull SequenceEntry Integer", 0},
		{"k: # c\n  !!str\nj: 1\n", "k MappingValue Comment !!str ImplicitNull j MappingValue Integer", 0},
		{"&k a: !custom\nb: 1\n", "Anchor k a MappingValue !custom ImplicitNull b MappingValue Integer", 0},
		{"a: !custom &n\n", "a MappingValue !custom Anchor n ImplicitNull", 0},
		// The scanner reads the token after a tag that isn't a core one as
		// a string, so the key on the next line gets back its own type.
		{"a: !custom\n~: 1\n", "a MappingValue !custom ImplicitNull Null MappingValue Integer", 0},
		{"a: &n !custom\nNull: 1\n", "a MappingValue Anchor n !custom ImplicitNull Null MappingValue Integer", 0},
		{"a: !\n0x1F: 1\n", "a MappingValue ! ImplicitNull HexInteger MappingValue Integer", 0},
		{"a: !custom\ntrue: 1\n", "a MappingValue !custom ImplicitNull Bool MappingValue Integer", 0},
		{"a: !custom\n\"~\": 1\n", "a MappingValue !custom ImplicitNull DoubleQuote MappingValue Integer", 0},
		{"a: !custom &n\n~: 1\n", "a MappingValue !custom Anchor n ImplicitNull Null MappingValue Integer", 0},
		// A node indented under the tag's parent is the tag's value.
		{"a: !custom\n  x: 1\n", "a MappingValue !custom x MappingValue Integer", 0},
		{"a: !custom\n- 1\n", "a MappingValue !custom SequenceEntry Integer", 0},
		{"l:\n- !custom\n  x: 1\n", "l MappingValue SequenceEntry !custom x MappingValue Integer", 0},
		{"a:\n  !custom\n  b: 1\n", "a MappingValue !custom b MappingValue Integer", 0},
		// In a flow collection, a tag before a comma or the end, with a
		// blank or an anchor after it, but not a core tag, nor a tag with
		// the comma or the bracket right after it.
		{"a: [!custom , z]\n", "a MappingValue SequenceStart !custom ImplicitNull CollectEntry z SequenceEnd", 0},
		{"a: {k: !custom &n}\n", "a MappingValue MappingStart k MappingValue !custom Anchor n ImplicitNull MappingEnd", 0},
		{"a: [!custom, z]\n", "a MappingValue SequenceStart !custom CollectEntry z SequenceEnd", 0},
		{"a: [!, z]\n", "a MappingValue SequenceStart ! CollectEntry z SequenceEnd", 0},
		{"a: {k: !!set &n, w: z}\n", "a MappingValue MappingStart k MappingValue !!set Anchor n CollectEntry w MappingValue z MappingEnd", 0},
		{"a: [!!str , z]\n", "a MappingValue SequenceStart !!str CollectEntry z SequenceEnd", 0},
		// A value on the tag's line, a root tag, two tags, a !!merge
		// tag, and a %TAG directive are left alone.
		{"a: !custom x\nb: 1\n", "a MappingValue !custom x b MappingValue Integer", 0},
		{"!custom\nb: 1\n", "!custom b MappingValue Integer", 0},
		{"a: !x !y\nb: 1\n", "a MappingValue !x !y b MappingValue Integer", 0},
		{"a: !custom &n &m\nb: 1\n", "a MappingValue !custom Anchor n Anchor m b MappingValue Integer", 0},
		{"a: !!merge\nb: 1\n", "a MappingValue !!merge b MappingValue Integer", 0},
		{"%TAG !! tag:x.y,2020:\n---\na: !!str\nb: 1\n", "Directive TAG !! tag:x.y,2020: DocumentHeader a MappingValue !!str b MappingValue Integer", 0},
	} {
		text := c.in
		toks := yamlprep.Tokenize(text)
		before := make(map[*token.Token][3]any, len(toks))
		for _, tk := range toks {
			before[tk] = [3]any{*tk.Position, tk.Prev, tk.Next}
		}
		got, collections := emptyTagValues(text, toks)
		if s := tokenTypes(got); s != c.want {
			t.Errorf("%q: tokens %s, want %s", c.in, s, c.want)
		}
		if len(collections) != c.collections {
			t.Errorf("%q: %d collection tags, want %d", c.in, len(collections), c.collections)
		}
		for _, tk := range toks {
			if was := before[tk]; was != [3]any{*tk.Position, tk.Prev, tk.Next} {
				t.Errorf("%q: token %q moved from %v to %v:%v", c.in, tk.Value, was[0], tk.Position.Line, tk.Position.Column)
			}
		}
		for _, tk := range got {
			if tk.Type == token.ImplicitNullType && (tk.Prev != nil || tk.Next != nil) {
				t.Errorf("%q: the added null is linked into the text", c.in)
			}
		}
	}
}

// TestImplicitNullAfterPlace checks that the added null sits one column
// after the last of the tag and its anchor, on their line.
func TestImplicitNullAfterPlace(t *testing.T) {
	for _, c := range []struct {
		in           string
		line, column int
	}{
		{"a: !custom\nb: 1\n", 1, 5},
		{"a: !custom &n\nb: 1\n", 1, 13},
		{"a: &n !custom\nb: 1\n", 1, 8},
		{"l:\n- !custom\n- 2\n", 2, 4},
	} {
		toks, _ := emptyTagValues(c.in, yamlprep.Tokenize(c.in))
		for _, tk := range toks {
			if tk.Type == token.ImplicitNullType && (tk.Position.Line != c.line || tk.Position.Column != c.column) {
				t.Errorf("%q: null at %d:%d, want %d:%d", c.in, tk.Position.Line, tk.Position.Column, c.line, c.column)
			}
		}
	}
}

// TestHideCollectionTags checks that a collection tag reads as "!" until
// the returned function puts it back.
func TestHideCollectionTags(t *testing.T) {
	tag := &token.Token{Type: token.TagType, Value: "!!map"}
	restore := hideCollectionTags([]*token.Token{tag})
	if tag.Value != "!" {
		t.Errorf("hidden tag = %q, want \"!\"", tag.Value)
	}
	restore()
	if tag.Value != "!!map" {
		t.Errorf("restored tag = %q, want \"!!map\"", tag.Value)
	}
}

// TestTagAbuts checks which tags have a character other than a blank
// right after them on their line, read from the text, and that a tag
// whose text is not at its token's column counts as one.
func TestTagAbuts(t *testing.T) {
	for _, c := range []struct {
		in   string
		want bool
	}{
		{"a: [!custom, z]\n", true},
		{"a: [!custom , z]\n", false},
		{"a: [!custom\t, z]\n", false},
		{"a: {k: !custom,w: z}\n", true},
		{"a: [!custom\n, z]\n", false},
		{"a: [!, z]\n", true},
		{"a: [!<tag:x.y,2020:z> , z]\n", true},
	} {
		lines := lineIndex{text: c.in, after: map[int]int{}}
		for _, tk := range yamlprep.Tokenize(c.in) {
			if tk.Type == token.TagType {
				if got := lines.tagAbuts(tk); got != c.want {
					t.Errorf("%q: tagAbuts = %v, want %v", c.in, got, c.want)
				}
				break
			}
		}
	}

	// Each tag after the first on a line sits one column further from
	// the place the parser gives it than the tag before it.
	for _, c := range []struct {
		in   string
		want []bool
	}{
		{"a: [! , ! , ! , ! , !, z]\n", []bool{false, false, false, false, true}},
		{"a: [!a , !a, z]\n", []bool{false, true}},
		{"a: !x [!y , z]\n", []bool{false, false}},
	} {
		lines := lineIndex{text: c.in, after: map[int]int{}}
		var got []bool
		for _, tk := range yamlprep.Tokenize(c.in) {
			if tk.Type == token.TagType {
				got = append(got, lines.tagAbuts(tk))
			}
		}
		if fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("%q: tagAbuts = %v, want %v", c.in, got, c.want)
		}
	}
}
