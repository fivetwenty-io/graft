package yamlprep

import (
	"strings"
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

func TestTaggedMergeKeys(t *testing.T) {
	cases := []struct {
		src     string
		want    token.Type
		tagLeft string
	}{
		{"m:\n  !!merge <<: {a: 1}\n", token.MergeKeyType, ""},
		{"m:\n  ? !!merge <<\n  : {a: 1}\n", token.MergeKeyType, ""},
		{"m:\n  !!str <<: {a: 1}\n", token.StringType, "!!str"},
		{"m:\n  <<: {a: 1}\n", token.MergeKeyType, ""},
		{"m: {!!merge <<: {a: 1}, c: 2}\n", token.MergeKeyType, ""},
		{"m: {!!str <<: {a: 1}}\n", token.StringType, "!!str"},
		{"m:\n  ? !!str <<\n  : {a: 1}\n", token.StringType, "!!str"},
		{"!!merge <<: {a: 1}\n", token.MergeKeyType, ""},
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
		for i := 1; i < len(tks); i++ {
			if tks[i].Prev != tks[i-1] || tks[i-1].Next != tks[i] {
				t.Errorf("%q: tokens %d and %d are not linked", c.src, i-1, i)
			}
		}
	}
}

// tokenText joins the source text of tks, which is the text they print.
func tokenText(tks token.Tokens) string {
	var b strings.Builder
	for _, tk := range tks {
		b.WriteString(tk.Origin)
	}
	return b.String()
}

// TestTaggedMergeKeysKeepTheText pins where the rewritten tokens sit. A
// !!merge key takes its tag's place, and every other token keeps the
// position the scanner gave it, and the tokens print the same text as
// before.
func TestTaggedMergeKeysKeepTheText(t *testing.T) {
	for _, src := range []string{
		"m:\n  !!merge <<: {a: 1}\n  c: [x, y]\n",
		"m:\n  ? !!merge <<\n  : {a: 1}\n  c: 2\n",
		"m: {!!merge <<: {a: 1}, c: 2}\n",
		"m:\n  !!str <<: {a: 1}\n",
	} {
		before, after := tokenize(src), Tokenize(src)
		if tokenText(after) != tokenText(before) {
			t.Errorf("%q: tokens print %q, want %q", src, tokenText(after), tokenText(before))
		}
		var tag *token.Token
		j := 0
		for _, tk := range before {
			if tk.Type == token.TagType && tk.Value == "!!merge" {
				tag = tk
				continue
			}
			got := after[j]
			j++
			want := *tk.Position
			if tk.Value == "<<" && tag != nil {
				want = *tag.Position
			}
			if *got.Position != want {
				t.Errorf("%q: token %q at %v, want %v", src, got.Value, got.Position, want)
			}
		}
		if j != len(after) {
			t.Errorf("%q: %d tokens, want %d", src, len(after), j)
		}
	}
}

// TestTaggedMergeKeysLeaveOtherTags pins the << keys taggedMergeKeys
// leaves as the scanner reads them: another tag, a tag on a line of its
// own, a quoted key, and a tagged << value.
func TestTaggedMergeKeysLeaveOtherTags(t *testing.T) {
	for _, src := range []string{
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
