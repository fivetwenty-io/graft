package yamlprep

import (
	"testing"

	"github.com/goccy/go-yaml/token"
)

func mergeKeyToken(tks token.Tokens) *token.Token {
	for _, tk := range tks {
		if tk.Value == "<<" {
			return tk
		}
	}
	return nil
}

// TestTaggedMergeKeys pins the type of a tagged << and the tag left in
// front of it. A !!merge or !!str tag always stays.
func TestTaggedMergeKeys(t *testing.T) {
	cases := []struct {
		src     string
		want    token.Type
		tagLeft string
	}{
		{"m:\n  !!merge <<: {a: 1}\n", token.MergeKeyType, "!!merge"},
		{"m:\n  ? !!merge <<\n  : {a: 1}\n", token.MergeKeyType, "!!merge"},
		{"m:\n  !!str <<: {a: 1}\n", token.StringType, "!!str"},
		{"m:\n  <<: {a: 1}\n", token.MergeKeyType, ""},
		{"m: {!!merge <<: {a: 1}, c: 2}\n", token.MergeKeyType, "!!merge"},
		{"m: {!!str <<: {a: 1}}\n", token.StringType, "!!str"},
		{"m:\n  ? !!str <<\n  : {a: 1}\n", token.StringType, "!!str"},
		{"!!merge <<: {a: 1}\n", token.MergeKeyType, "!!merge"},
		{"? !!merge <<\n: {a: 1}\n", token.MergeKeyType, "!!merge"},
	}
	for _, c := range cases {
		tks := Tokenize(c.src)
		tk := mergeKeyToken(tks)
		if tk == nil || tk.Type != c.want {
			t.Errorf("%q: << token = %+v, want type %v", c.src, tk, c.want)
			continue
		}
		var tag string
		for _, other := range tks {
			if other.Type == token.TagType {
				tag = other.Value
			}
		}
		if tag != c.tagLeft {
			t.Errorf("%q: tag token %q left, want %q", c.src, tag, c.tagLeft)
		}
	}
}

// TestTaggedMergeKeysMoveNoToken pins that the pass changes the type of
// a << and nothing else. Every token keeps its value, its source text,
// its position, and its neighbors.
func TestTaggedMergeKeysMoveNoToken(t *testing.T) {
	for _, src := range []string{
		"m:\n  ? !!merge <<\n  : {a: 1}\n  c: [x, y]\n",
		"m:\n  !!str <<: {a: 1}\n  c: 2\n",
		"m: {!!str <<: {a: 1}, c: 2}\n",
	} {
		before, after := tokenize(src), Tokenize(src)
		if len(after) != len(before) {
			t.Errorf("%q: %d tokens, want %d", src, len(after), len(before))
			continue
		}
		changed := 0
		for i := range before {
			b, a := before[i], after[i]
			if a.Value != b.Value || a.Origin != b.Origin || *a.Position != *b.Position {
				t.Errorf("%q: token %d = %q %q at %v, want %q %q at %v", src, i, a.Value, a.Origin, a.Position, b.Value, b.Origin, b.Position)
			}
			if a.Type != b.Type {
				changed++
			}
			if i > 0 && (a.Prev != after[i-1] || after[i-1].Next != a) {
				t.Errorf("%q: tokens %d and %d are not linked", src, i-1, i)
			}
		}
		if changed != 1 {
			t.Errorf("%q: %d token types changed, want 1", src, changed)
		}
	}
}

// TestTaggedMergeKeysLeaveOtherTags pins the << keys taggedMergeKeys
// leaves as the scanner reads them: a !!merge key in a block or flow
// mapping, which goccy reads already, an anchor between "?" and the tag,
// a colon on the next line, another tag, a tag on a line of its own, a
// quoted key, and a tagged << value.
func TestTaggedMergeKeysLeaveOtherTags(t *testing.T) {
	for _, src := range []string{
		"m:\n  !!merge <<: {a: 1}\n",
		"m: {!!merge <<: {a: 1}, c: 2}\n",
		"m:\n  &a !!merge <<: {a: 1}\n  c: 2\n",
		"m:\n  ? &k !!merge <<\n  : {a: 1}\n",
		"m:\n  !!merge <<\n  : {a: 1}\n",
		"m:\n  !!int <<: {a: 1}\n",
		"m:\n  !merge <<: {a: 1}\n",
		"m:\n  !!merge\n  <<: {a: 1}\n",
		"m:\n  !!merge \"<<\": {a: 1}\n",
		"a: !!merge <<\n",
		"a: !!str <<\n",
	} {
		before, after := tokenize(src), Tokenize(src)
		if len(after) != len(before) {
			t.Errorf("%q: %d tokens, want %d", src, len(after), len(before))
			continue
		}
		for i := range before {
			if after[i].Type != before[i].Type || after[i].Value != before[i].Value {
				t.Errorf("%q: token %d = %v %q, want %v %q", src, i, after[i].Type, after[i].Value, before[i].Type, before[i].Value)
			}
		}
	}
}
