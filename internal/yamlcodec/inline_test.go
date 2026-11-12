package yamlcodec

import (
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/yamlnode"
)

func TestInlineScalarHonorsTheStyleItCan(t *testing.T) {
	cases := []struct {
		in     string
		prefer InlineStyle
		want   string
	}{
		{"web", InlinePlain, "web"},
		{"web", InlineDouble, `"web"`},
		{"web", InlineSingle, `'web'`},
		{"it's", InlineSingle, `'it''s'`},
		{"a\nb", InlinePlain, `"a\nb"`},
		{"a\nb", InlineSingle, `"a\nb"`},
		{"", InlineSingle, `''`},
		{"", InlineDouble, `""`},
	}
	for _, c := range cases {
		if got := InlineScalar(c.in, c.prefer); got != c.want {
			t.Errorf("InlineScalar(%q, %d) = %s, want %s", c.in, c.prefer, got, c.want)
		}
	}
}

func TestInlineScalarReadsBack(t *testing.T) {
	tricky := []string{
		"", " ", "yes", "No", "on", "~", "null", "1", "0x1F", "1e3", "1.0", ".inf",
		"- x", "a: b", "#x", "x #y", "@x", "`x", "{x}", "[x]", "*x", "&x", "!x",
		"%x", "'x", "\"x", "a\tb", "trailing ", " leading", "a\nb", "a\n", "é",
		"((grab x))", "---", "...", "a\\b",
	}
	for _, s := range tricky {
		for _, p := range []InlineStyle{InlinePlain, InlineSingle, InlineDouble} {
			out := InlineScalar(s, p)
			if strings.Contains(out, "\n") {
				t.Errorf("InlineScalar(%q, %d) = %q spans lines", s, p, out)
				continue
			}
			docs, err := yamlnode.Parse([]byte("k: " + out + "\n"))
			if err != nil || len(docs) != 1 {
				t.Errorf("InlineScalar(%q, %d) = %s does not parse: %v", s, p, out, err)
				continue
			}
			v := docs[0].Content[0].Content[1]
			if v.Value != s || v.Tag != "!!str" {
				t.Errorf("InlineScalar(%q, %d) = %s reads back %q %s", s, p, out, v.Value, v.Tag)
			}
		}
	}
}

func TestInlinePlainMatchesTheEncoder(t *testing.T) {
	for _, s := range []string{"web", "yes", "1.0", "a: b", "#x", "", "x #y", "((grab x))"} {
		out, err := MarshalYAML(map[string]interface{}{"k": s})
		if err != nil {
			t.Fatal(err)
		}
		want := strings.TrimSuffix(strings.TrimPrefix(string(out), "k: "), "\n")
		if got := InlineScalar(s, InlinePlain); got != want {
			t.Errorf("InlineScalar(%q, InlinePlain) = %s, the encoder writes %s", s, got, want)
		}
	}
}

// TestInlineScalarNeverFolds checks that a string longer than the
// encoder's fold width stays on one line in every style.
func TestInlineScalarNeverFolds(t *testing.T) {
	long := strings.Repeat("word ", 40) + "end"
	for _, p := range []InlineStyle{InlinePlain, InlineSingle, InlineDouble} {
		if out := InlineScalar(long, p); strings.Contains(out, "\n") {
			t.Errorf("InlineScalar(long, %d) folds: %q", p, out)
		}
	}
}

// TestInlinePlainQuotesWhatTheParserMisreads checks strings the encoder
// writes plain but the parser reads as a merge key or a timestamp, which
// InlineScalar double-quotes instead.
func TestInlinePlainQuotesWhatTheParserMisreads(t *testing.T) {
	for s, want := range map[string]string{"<<": `"<<"`, "2001-01-01": `"2001-01-01"`} {
		if got := InlineScalar(s, InlinePlain); got != want {
			t.Errorf("InlineScalar(%q, InlinePlain) = %s, want %s", s, got, want)
		}
	}
}
