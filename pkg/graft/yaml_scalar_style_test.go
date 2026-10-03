package graft

import (
	"bytes"
	"strings"
	"testing"

	"github.com/goccy/go-yaml/token"
)

// spruceScalarStyleCases pairs a string with the exact text spruce's
// merge writes for it, first as the value of a key "k" and then as a
// key whose value is 1. Every expected value was captured from spruce
// v1.35.17 (its stdout minus the trailing blank line it adds after the
// document).
var spruceScalarStyleCases = []struct {
	in, asValue, asKey string
}{
	// single-quoted
	{" ", "k: ' '\n", "' ': 1\n"},
	{"-", "k: '-'\n", "'-': 1\n"},
	{"#", "k: '#'\n", "'#': 1\n"},
	{"!", "k: '!'\n", "'!': 1\n"},
	{"}", "k: '}'\n", "'}': 1\n"},
	{"|", "k: '|'\n", "'|': 1\n"},
	{"'a", "k: '''a'\n", "'''a': 1\n"},
	{"*.uaa.((system_domain))", "k: '*.uaa.((system_domain))'\n", "'*.uaa.((system_domain))': 1\n"},
	{"a: b", "k: 'a: b'\n", "'a: b': 1\n"},
	// plain
	{"\\", "k: \\\n", "\\: 1\n"},
	{"a#", "k: a#\n", "a#: 1\n"},
	{"-#", "k: -#\n", "-#: 1\n"},
	{"a- b", "k: a- b\n", "a- b: 1\n"},
	{"q --- s", "k: q --- s\n", "q --- s: 1\n"},
	{"uaa.((system_domain))", "k: uaa.((system_domain))\n", "uaa.((system_domain)): 1\n"},
	{"<<", "k: <<\n", "<<: 1\n"},
	{"2001-01-01", "k: 2001-01-01\n", "2001-01-01: 1\n"},
	// tab-escaped
	{"\t", "k: \"\\t\"\n", "\"\\t\": 1\n"},
	{"a\t", "k: \"a\\t\"\n", "\"a\\t\": 1\n"},
	{"\t\"", "k: \"\\t\\\"\"\n", "\"\\t\\\"\": 1\n"},
	{"\t'", "k: \"\\t'\"\n", "\"\\t'\": 1\n"},
	{"a\tb", "k: \"a\\tb\"\n", "\"a\\tb\": 1\n"},
	// question-mark
	{"?", "k: '?'\n", "'?': 1\n"},
	{"? a", "k: '? a'\n", "'? a': 1\n"},
	{"?a", "k: ?a\n", "?a: 1\n"},
	// document-markers
	{"---", "k: '---'\n", "'---': 1\n"},
	{"...", "k: '...'\n", "'...': 1\n"},
	{"...x", "k: '...x'\n", "'...x': 1\n"},
	{"--- a", "k: '--- a'\n", "'--- a': 1\n"},
	// lookalikes
	{"1.0", "k: \"1.0\"\n", "\"1.0\": 1\n"},
	{"yes", "k: \"yes\"\n", "\"yes\": 1\n"},
	{"null", "k: \"null\"\n", "\"null\": 1\n"},
	{"+.inf", "k: \"+.inf\"\n", "\"+.inf\": 1\n"},
	{".nan", "k: \".nan\"\n", "\".nan\": 1\n"},
	{"1e3", "k: \"1e3\"\n", "\"1e3\": 1\n"},
	{"0o17", "k: \"0o17\"\n", "\"0o17\": 1\n"},
	{"1:20", "k: \"1:20\"\n", "\"1:20\": 1\n"},
	{"", "k: \"\"\n", "\"\": 1\n"},
	// multi-line
	{"a\nb", "k: |-\n  a\n  b\n", "? |-\n  a\n  b\n: 1\n"},
	{"a\nb\n", "k: |\n  a\n  b\n", "? |\n  a\n  b\n: 1\n"},
	{"a\nb\n\n", "k: |+\n  a\n  b\n\n", "? |+\n  a\n  b\n\n: 1\n"},
	{" a\nb", "k: |2-\n   a\n  b\n", "? |2-\n   a\n  b\n: 1\n"},
	{"\na", "k: |2-\n\n  a\n", "? |2-\n\n  a\n: 1\n"},
	{"a \nb", "k: \"a \\nb\"\n", "? \"a \\nb\"\n: 1\n"},
	{"a\n\nb", "k: |-\n  a\n\n  b\n", "? |-\n  a\n\n  b\n: 1\n"},
	// long
	{"word word word word word word word word word word word word word word word word word word word word end", "k: word word word word word word word word word word word word word word word word\n  word word word word end\n", "word word word word word word word word word word word word word word word word word word word word end: 1\n"},
	{"a: b b b b b b b b b b b b b b b b b b b b b b b b b b b b b b b b b b b b b b b b c", "k: 'a: b b b b b b b b b b b b b b b b b b b b b b b b b b b b b b b b b b b b b b\n  b b c'\n", "'a: b b b b b b b b b b b b b b b b b b b b b b b b b b b b b b b b b b b b b b b b c': 1\n"},
	{"xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx", "k: xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx\n", "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx: 1\n"},
	{"kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk", "k: kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk\n", "? kkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkkk\n: 1\n"},
	{"word word word word word word word word word word word word word word word word word word word word word word word word word word word word word word", "k: word word word word word word word word word word word word word word word word\n  word word word word word word word word word word word word word word\n", "? word word word word word word word word word word word word word word word word\n  word word word word word word word word word word word word word word\n: 1\n"},
	// unicode
	{"é", "k: é\n", "é: 1\n"},
	{"emoji 😀", "k: \"emoji \\U0001F600\"\n", "\"emoji \\U0001F600\": 1\n"},
	{"a\u2028b", "k: 'a\u2028  b'\n", "? 'a\u2028  b'\n: 1\n"},
	{"a\u00a0b", "k: a\u00a0b\n", "a\u00a0b: 1\n"},
	{"\u00a0", "k: \u00a0\n", "\u00a0: 1\n"},
}

// TestMarshalYAML_ScalarStylesMatchSpruce writes every string in the
// table as a value, and every string spruce writes as a simple key as a
// key, and compares the output with spruce's.
func TestMarshalYAML_ScalarStylesMatchSpruce(t *testing.T) {
	for _, tc := range spruceScalarStyleCases {
		got, err := MarshalYAML(map[string]interface{}{"k": tc.in})
		if err != nil {
			t.Fatalf("MarshalYAML(value %q): %v", tc.in, err)
		}
		if string(got) != tc.asValue {
			t.Errorf("value %q:\n got %q\nwant %q", tc.in, got, tc.asValue)
		}

		if strings.HasPrefix(tc.asKey, "? ") {
			continue
		}
		got, err = MarshalYAML(map[string]interface{}{tc.in: 1})
		if err != nil {
			t.Fatalf("MarshalYAML(key %q): %v", tc.in, err)
		}
		if string(got) != tc.asKey {
			t.Errorf("key %q:\n got %q\nwant %q", tc.in, got, tc.asKey)
		}
	}
}

// TestMarshalYAML_ScalarStylesInNestedBlocksMatchSpruce covers the
// places where a scalar's column and indent come from the structure
// around it: sequence items, items of nested sequences, and folding
// under nested keys. Expected output was captured from spruce v1.35.17.
func TestMarshalYAML_ScalarStylesInNestedBlocksMatchSpruce(t *testing.T) {
	words := strings.Repeat("word ", 20) + "end"
	cases := []struct {
		name string
		in   map[string]interface{}
		want string
	}{
		{
			name: "sequence items under a nested key",
			in: map[string]interface{}{"a": map[string]interface{}{
				"b": []interface{}{"\t", " a\nb", words, "?"},
			}},
			want: "a:\n  b:\n  - \"\\t\"\n  - |2-\n     a\n    b\n  - word word word word word word word word word word word word word word word word\n    word word word word end\n  - '?'\n",
		},
		{
			name: "long key over a folded nested value",
			in: map[string]interface{}{strings.Repeat("a b ", 30): map[string]interface{}{
				"c": strings.Repeat("word ", 30),
			}},
			want: "'" + strings.Repeat("a b ", 30) + "':\n  c: 'word word word word word word word word word word word word word word word word\n    word word word word word word word word word word word word word word '\n",
		},
		{
			name: "items of a nested sequence",
			in:   map[string]interface{}{"s": []interface{}{[]interface{}{"a\nb\n", "q --- s"}}},
			want: "s:\n- - |\n    a\n    b\n  - q --- s\n",
		},
	}
	runMarshalCases(t, cases)
}

func runMarshalCases(t *testing.T, cases []struct {
	name string
	in   map[string]interface{}
	want string
}) {
	t.Helper()
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := MarshalYAML(tc.in)
			if err != nil {
				t.Fatalf("MarshalYAML: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("\n got %q\nwant %q", got, tc.want)
			}
		})
	}
}

// TestMarshalYAML_TabStringsRoundTrip checks that strings holding a tab
// survive a second merge: the merged output parses back to the same
// strings and marshals to the same bytes. Before spruce's escape rules
// were ported, a lone tab was written raw and read back as null, and a
// value opening with a tab was not valid YAML at all.
func TestMarshalYAML_TabStringsRoundTrip(t *testing.T) {
	engine := NewDefaultEngine()
	for _, s := range []string{"\t", "a\t", "\ta", "\t\"", "\t'", "a\tb", " \t "} {
		for _, data := range []map[string]interface{}{
			{"k": s},
			{s: "v"},
			{"l": []interface{}{s}},
		} {
			first, err := MarshalYAML(data)
			if err != nil {
				t.Fatalf("MarshalYAML(%q): %v", s, err)
			}
			doc, err := engine.ParseYAML(first)
			if err != nil {
				t.Fatalf("re-parsing %q failed: %v\noutput:\n%s", s, err, first)
			}
			second, err := MarshalYAML(doc.RawData())
			if err != nil {
				t.Fatalf("second MarshalYAML(%q): %v", s, err)
			}
			if !bytes.Equal(second, first) {
				t.Errorf("%q did not round-trip:\nfirst  %q\nsecond %q", s, first, second)
			}
		}
	}
}

// TestGoccyWritesVerbatimMatchesGoccy checks the inline shortcut in
// goccyWritesVerbatim against goccy's own token.IsNeedQuoted for every
// string spruce writes plain, over all strings of up to three
// characters drawn from letters, digits, and YAML's indicator
// characters, plus some longer shapes.
func TestGoccyWritesVerbatimMatchesGoccy(t *testing.T) {
	alphabet := []string{"a", "Z", "1", "0", " ", "-", "+", ".", "_", "#", ":", "\\", "'", "\"", "!", "&", "*", "[", "{", "|", ">", "%", "@", "`", ",", "?", "~", "x", "é"}
	strs := []string{"2001-01-01", "2001-01-01 10:00:00", "15:4", "1:20", "10.0.0.1", "a #b", "a: b", "a- b", "-a", "+1", ".5", "0x1F", "0o17", "1_000"}
	for _, a := range alphabet {
		strs = append(strs, a)
		for _, b := range alphabet {
			strs = append(strs, a+b)
			for _, c := range alphabet {
				strs = append(strs, a+b+c)
			}
		}
	}
	for _, s := range strs {
		if renderValueScalar(s, 3, 2, false) != s {
			continue
		}
		want := !token.IsNeedQuoted(s)
		if got := goccyWritesVerbatim(s); got != want {
			t.Errorf("goccyWritesVerbatim(%q) = %v, token.IsNeedQuoted says %v", s, got, want)
		}
	}
}

// TestMarshalYAMLWithComments_FindsQuotedKeys checks that a head
// comment still lands above its node when the node's key, or a key on
// the path to it, is one graft writes quoted.
func TestMarshalYAMLWithComments_FindsQuotedKeys(t *testing.T) {
	data := map[string]interface{}{
		"*a":     map[string]interface{}{"\t": "v", "z": 1},
		"plain":  1,
		"y\tz":   2,
		"a #b":   3,
		"l":      []interface{}{"x", map[string]interface{}{"?": 2}},
		"normal": map[string]interface{}{"d": "e"},
	}
	out, err := MarshalYAMLWithComments(data, []YAMLHeadComment{
		{Path: "*a.\t", Lines: []string{" on a tab key"}},
		{Path: "a #b", Lines: []string{" on a key with a hash"}},
	})
	if err != nil {
		t.Fatalf("MarshalYAMLWithComments: %v", err)
	}
	for _, want := range []string{
		"'*a':\n  # on a tab key\n  \"\\t\": v\n",
		"# on a key with a hash\n'a #b': 3\n",
	} {
		if !strings.Contains(string(out), want) {
			t.Errorf("output lacks %q:\n%s", want, out)
		}
	}
}
