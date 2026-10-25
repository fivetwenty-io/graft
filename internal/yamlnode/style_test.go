package yamlnode_test

import (
	"testing"

	"github.com/fivetwenty-io/graft/internal/yamlnode"
)

// rootMapping parses text and returns its first document's root mapping.
func rootMapping(t *testing.T, text string) *yamlnode.Node {
	t.Helper()
	docs, err := yamlnode.Parse([]byte(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(docs) == 0 || len(docs[0].Content) == 0 || docs[0].Content[0].Kind != yamlnode.MappingNode {
		t.Fatalf("%q does not hold a mapping", text)
	}
	return docs[0].Content[0]
}

// wantStyles checks the style of the value under each key of m.
func wantStyles(t *testing.T, m *yamlnode.Node, want map[string]yamlnode.Style) {
	t.Helper()
	for key, style := range want {
		found := false
		for i := 0; i+1 < len(m.Content); i += 2 {
			if m.Content[i].Value != key {
				continue
			}
			found = true
			wantStyle(t, "value of "+key, m.Content[i+1].Style, style)
		}
		if !found {
			t.Errorf("key %q missing", key)
		}
	}
}

func wantStyle(t *testing.T, what string, got, want yamlnode.Style) {
	t.Helper()
	if got != want {
		t.Errorf("%s has style %06b, want %06b", what, got, want)
	}
}

func TestStyleCaptured(t *testing.T) {
	m := rootMapping(t, "a: 'x'\nb: \"y\"\nc: |\n  z\nd: >\n  w\ne: [1]\nf: !!str 1\ng: {k: v}\nh: plain\ni:\n  - 1\nj:\n  k: 1\n")
	wantStyles(t, m, map[string]yamlnode.Style{
		"a": yamlnode.StyleSingle,
		"b": yamlnode.StyleDouble,
		"c": yamlnode.StyleLiteral,
		"d": yamlnode.StyleFolded,
		"e": yamlnode.StyleFlow,
		"f": yamlnode.StyleTagged,
		"g": yamlnode.StyleFlow,
		"h": 0,
		"i": 0,
		"j": 0,
	})
}

func TestStyleFlagsCombine(t *testing.T) {
	m := rootMapping(t, "a: !!str 'yes'\nb: !foo \"x\"\nc: !!str |-\n  z\nd: !!seq [1]\ne: &x !!int 1\nf: *x\n")
	wantStyles(t, m, map[string]yamlnode.Style{
		"a": yamlnode.StyleTagged | yamlnode.StyleSingle,
		"b": yamlnode.StyleTagged | yamlnode.StyleDouble,
		"c": yamlnode.StyleTagged | yamlnode.StyleLiteral,
		"d": yamlnode.StyleTagged | yamlnode.StyleFlow,
		"e": yamlnode.StyleTagged,
		"f": 0,
	})
}

func TestStyleNonSpecificTagIsNotTagged(t *testing.T) {
	// yaml.v3 ignores a bare "!" and records no tagged style for it.
	wantStyles(t, rootMapping(t, "a: ! x\n"), map[string]yamlnode.Style{"a": 0})
}

func TestStyleOnKeysAndFlowContent(t *testing.T) {
	m := rootMapping(t, "'a': 1\n\"b\": 2\n!!str c: 3\nd: [x: 1, 'y']\n")
	wantStyle(t, "key a", m.Content[0].Style, yamlnode.StyleSingle)
	wantStyle(t, "key b", m.Content[2].Style, yamlnode.StyleDouble)
	wantStyle(t, "key c", m.Content[4].Style, yamlnode.StyleTagged)
	wantStyle(t, "key d", m.Content[6].Style, 0)
	seq := m.Content[7]
	wantStyle(t, "pair in flow sequence", seq.Content[0].Style, yamlnode.StyleFlow)
	wantStyle(t, "quoted flow item", seq.Content[1].Style, yamlnode.StyleSingle)
}

func TestStyleIgnoresQuotesAddedBeforeParsing(t *testing.T) {
	// graft quotes "<<<" keys, "{{...}}" placeholders, and keys that start
	// with "..." before goccy reads them, and those scalars were plain.
	m := rootMapping(t, "...x: 0\nmeta:\n  <<<: a\nb: {{x}}\nc: '{{y}}'\nd: [{{z}}, \"w\"]\n")
	wantStyle(t, "key ...x", m.Content[0].Style, 0)
	wantStyle(t, "key <<<", m.Content[3].Content[0].Style, 0)
	wantStyle(t, "value of b", m.Content[5].Style, 0)
	wantStyle(t, "value of c", m.Content[7].Style, yamlnode.StyleSingle)
	seq := m.Content[9]
	wantStyle(t, "placeholder in flow sequence", seq.Content[0].Style, 0)
	wantStyle(t, "quoted item after a placeholder", seq.Content[1].Style, yamlnode.StyleDouble)
}
