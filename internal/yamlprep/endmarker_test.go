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
		{"key with a nested value", "a: 1\n...x:\n  b: 2\n", "a: 1\n'...x':\n  b: 2\n"},
		{"CRLF line breaks", "a: 1\r\n...x: 2\r\nb: 3\r\n", "a: 1\r\n'...x': 2\r\nb: 3\r\n"},
		{"scalar document", "...x\n", "'...x'\n"},
		{"scalar document with comments", "...x # c\n\n# d\n", "'...x' # c\n\n# d\n"},
		{"scalar document before a marker", "...x\n---\nb: 1\n", "'...x'\n---\nb: 1\n"},
		{"two keys", "...x: 1\n...y: 2\n", "'...x': 1\n'...y': 2\n"},
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
// a line break, a "#", or the end of the input, four dots, a "..." line
// inside a quoted string, a block scalar, or a flow collection, and a
// plain scalar that runs on to the next line.
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
		"[a,\n...x]\n",
		"...x\ny\n",
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
