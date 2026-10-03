package yamlprep

import (
	"testing"
)

// TestQuoteEndMarkerScalars checks the column-1 plain scalars that start
// with "..." and a character YAML does not read as the end of a document
// marker. goccy v1.19.2 lexes the "..." as a document end anyway, which
// drops a key such as "...x" from the document, so the rewrite quotes
// each one. A quote that YAML needs inside the key is doubled, and the
// line breaks stay where they were.
func TestQuoteEndMarkerScalars(t *testing.T) {
	for _, c := range []struct{ name, in, want string }{
		{"key after a key", "a: 1\n...x: 2\n", "a: 1\n'...x': 2\n"},
		{"key on the first line", "...x: 2\na: 1\n", "'...x': 2\na: 1\n"},
		{"key after a literal block scalar", "a: |\n  t\n...x: 2\n", "a: |\n  t\n'...x': 2\n"},
		{"three dots as the key", "a:\n- 1\n...: 2\n", "a:\n- 1\n'...': 2\n"},
		{"three dots as a key with no value", "a: 1\n...:\n", "a: 1\n'...':\n"},
		{"key with a space and a comment", "a: 1\n...x y: 2 # c\n", "a: 1\n'...x y': 2 # c\n"},
		{"key with a quote", "a: 1\n...x'y: 2\n", "a: 1\n'...x''y': 2\n"},
		{"key with a comment sign", "a: 1\n...x#y: 2\n", "a: 1\n'...x#y': 2\n"},
		{"key with a colon", "a: 1\n...x:y: 2\n", "a: 1\n'...x:y': 2\n"},
		{"space before the colon", "a: 1\n...x  : 2\n", "a: 1\n'...x'  : 2\n"},
		{"tab before the colon", "a: 1\n...x\t: 2\n", "a: 1\n'...x' : 2\n"},
		{"space and tab before the colon", "a: 1\n...x \t: 2\n", "a: 1\n'...x'  : 2\n"},
		{"key with a nested value", "a: 1\n...x:\n  b: 2\n", "a: 1\n'...x':\n  b: 2\n"},
		{"CRLF line breaks", "a: 1\r\n...x: 2\r\nb: 3\r\n", "a: 1\r\n'...x': 2\r\nb: 3\r\n"},
		{"scalar document", "...x\n", "'...x'\n"},
		{"scalar document with comments", "...x # c\n\n# d\n", "'...x' # c\n\n# d\n"},
		{"scalar document before a marker", "...x\n---\nb: 1\n", "'...x'\n---\nb: 1\n"},
		{"two keys", "...x: 1\n...y: 2\n", "'...x': 1\n'...y': 2\n"},
		{"scalar document after a tag", "!!str\n...x\n", "!!str\n'...x'\n"},
		{"scalar document after an anchor and a comment", "&a # c\n...x\n", "&a # c\n'...x'\n"},
		{"scalar document after a tagged header", "--- !!str\n...x\n", "--- !!str\n'...x'\n"},
		{"scalar document after a directive", "%YAML 1.1\n---\n...x\n", "%YAML 1.1\n---\n'...x'\n"},
		{"scalar document after comments", "# c\n\n...x\n", "# c\n\n'...x'\n"},
		{"scalar document in a second document", "a: 1\n---\n...x\n", "a: 1\n---\n'...x'\n"},
		{"scalar document before an end marker", "...x\n---\n...\n", "'...x'\n---\n...\n"},
		{"key that starts with a comment sign", "a: 1\n...#c: 2\n", "a: 1\n'...#c': 2\n"},
		{"comment sign alone as the key", "a: 1\n...#: 2\n", "a: 1\n'...#': 2\n"},
		{"scalar document that starts with a comment sign", "...#c\n", "'...#c'\n"},
		{"flow sequence entry", "a: [1,\n...x]\n", "a: [1,\n'...x']\n"},
		{"flow sequence entry before another", "a: [1,\n...x, 2]\n", "a: [1,\n'...x', 2]\n"},
		{"flow sequence entry with a space", "a: [1,\n...x y]\n", "a: [1,\n'...x y']\n"},
		{"flow sequence entry with a quote", "a: [1,\n...x'y]\n", "a: [1,\n'...x''y']\n"},
		{"flow sequence entry with a comment sign", "a: [1,\n...#c]\n", "a: [1,\n'...#c']\n"},
		{"flow sequence entry and a comment", "a: [1,\n...x #c\n]\n", "a: [1,\n'...x' #c\n]\n"},
		{"flow sequence entry at the end of its line", "a: [1,\n...x\n]\n", "a: [1,\n'...x'\n]\n"},
		{"flow sequence entry before a comment line", "a: [1,\n...x\n\n  # c\n]\n", "a: [1,\n'...x'\n\n  # c\n]\n"},
		{"flow sequence entry with a trailing tab", "a: [1,\n...x\t]\n", "a: [1,\n'...x'\t]\n"},
		{"flow mapping key", "a: {b: 1,\n...x: 2}\n", "a: {b: 1,\n'...x': 2}\n"},
		{"flow mapping key with a tab before the colon", "a: {b: 1,\n...x\t: 2}\n", "a: {b: 1,\n'...x' : 2}\n"},
		{"flow mapping key with no value", "a: {b: 1,\n...x}\n", "a: {b: 1,\n'...x'}\n"},
		{"flow pair in a sequence", "a: [1,\n...x: 2]\n", "a: [1,\n'...x': 2]\n"},
		{"flow entry with CRLF line breaks", "a: [1,\r\n...x]\r\n", "a: [1,\r\n'...x']\r\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := QuoteEndMarkerScalars([]byte(c.in)); string(got) != c.want {
				t.Errorf("QuoteEndMarkerScalars(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

// TestQuoteEndMarkerScalarsLeavesOtherInput checks that every real end
// of a document marker, and every "..." goccy does not misread, comes
// back as the original slice. That covers a "..." followed by a blank,
// a line break, or the end of the input, four dots, a "..." line
// inside a quoted string or a block scalar, a flow entry that runs on
// to a later line or holds a character spruce and the diff read
// differently, a flow entry whose comment the next content line does
// not follow with a ",", a "]", or a "}", a
// scalar that is no key and continues an earlier line, and a scalar
// that is no key and runs on to a later line. The last two stay as
// goccy reads them, which fails some and silently drops text from
// others, such as "...x\ny", which spruce reads as "...x y".
func TestQuoteEndMarkerScalarsLeavesOtherInput(t *testing.T) {
	for _, in := range []string{
		"a: 1\n",
		"a: 1\n...\n",
		"a: 1\n... # c\n",
		"a: 1\n...\t\n",
		"a: 1\n...#c\n",
		"a: 1\n...",
		"a: 1\r\n...\r\nb: 2\r\n",
		"...\n---\na: 1\n",
		"a: 1\n....x: 2\n",
		"a:\n  ...x: 1\n",
		"a: 1\n---x: 2\n",
		"a: \"foo\n...x bar\"\n",
		"a: 'foo\n...x: bar'\n",
		"--- |\n...x\n",
		"a: [1,\n...x\ny]\n",
		"a: [1,\n...x\n  y]\n",
		"a: [1,\n...x\n...\n]\n",
		"a: [1,\n...x?y]\n",
		"a: [1,\n...x:y]\n",
		"a: [1,\n...[x]]\n",
		"a: [1,\n...{x]]\n",
		"a: [1,\n...x",
		"a: {b: 1,\n...x\n# c\n: 2}\n",
		"a: {b: 1,\n...x # c\n: 2}\n",
		"a: [1,\n...x\n# c\n",
		"a: [1,\n....x]\n",
		"...x\ny\n",
		"...x # c\nfoo\n",
		"foo\n...x\n",
		"--- foo\n...x\n",
		"foo\n  bar\n...x\n",
		"foo\n\n...x\n",
		"foo\n...x\n...\n",
		"...x\n...y\n",
		"a:\n...x\n",
		"a:\n...x\n...\n",
		"- a\n-\n...x\n",
		"a: |\n  t\n...x\n",
	} {
		data := []byte(in)
		out := QuoteEndMarkerScalars(data)
		if string(out) != in || &out[0] != &data[0] {
			t.Errorf("QuoteEndMarkerScalars(%q) = %q, want the original slice", in, out)
		}
	}
}

// TestPrepareQuotesEndMarkerKeys checks that Prepare applies the
// end-marker rewrite with the others.
func TestPrepareQuotesEndMarkerKeys(t *testing.T) {
	in := "b: {{x}}\nl:\n- a\n-\n...x: 1\n"
	out, lines := Prepare([]byte(in))
	if want := "b: '{{x}}'\nl:\n- a\n- ~\n'...x': 1\n"; string(out) != want || len(lines) != 1 || lines[0] != 4 {
		t.Errorf("Prepare(%q) = %q, %v; want %q, [4]", in, out, lines, want)
	}
}
