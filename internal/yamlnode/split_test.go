package yamlnode

import (
	"reflect"
	"testing"
)

// TestParseRejectsDocumentAfterEndMarker checks that content after a "..."
// needs a "---" first, as yaml.v3 requires. Its error carries the line
// before the first token, which is how libyaml marks it, so a token on
// line 3 fails on line 2.
func TestParseRejectsDocumentAfterEndMarker(t *testing.T) {
	for in, want := range map[string]string{
		"a\n... # c0\nk: 1\n":      "yaml: line 2: did not find expected <document start>",
		"a\n...\nk: 1\n":           "yaml: line 2: did not find expected <document start>",
		"a\n...\nb\n":              "yaml: line 2: did not find expected <document start>",
		"a\n...\n- x\n":            "yaml: line 2: did not find expected <document start>",
		"a\n...\n[1]\n":            "yaml: line 2: did not find expected <document start>",
		"a\n...\n\n\nk: 1\n":       "yaml: line 4: did not find expected <document start>",
		"a\n...\n# c\nk: 1\n":      "yaml: line 3: did not find expected <document start>",
		"a\n... # c\n\nk: 1\n":     "yaml: line 3: did not find expected <document start>",
		"a\n...\n...\nk: 1\n":      "yaml: line 3: did not find expected <document start>",
		"x\n---\nb\n...\n- y\n":    "yaml: line 4: did not find expected <document start>",
		"a\n---\n...\nq\n":         "yaml: line 3: did not find expected <document start>",
		"a\n...\n---\nb\n...\nq\n": "yaml: line 5: did not find expected <document start>",
	} {
		docs, err := Parse([]byte(in))
		if err == nil || err.Error() != want {
			t.Errorf("Parse(%q) = %d documents, %v; want the error %q", in, len(docs), err, want)
		}
	}
	for _, in := range []string{"a\n...\n", "a\n... # c\n", "a\n...\n# c\n", "a\n...\n---\nk: 1\n", "a\n...\n# c\n---\nk: 1\n", "a\n...\n...\n---\nk: 1\n"} {
		if _, err := Parse([]byte(in)); err != nil {
			t.Errorf("Parse(%q): %v", in, err)
		}
	}
}

func TestParseSplitsDocumentsLikeYAMLv3(t *testing.T) {
	for _, c := range []struct {
		in   string
		docs int
		tags []string
	}{
		{"", 0, nil},
		{"# c\n", 0, nil},
		{"---\n", 1, []string{"!!null"}},
		{"---\n---\n", 2, []string{"!!null", "!!null"}},
		{"a: 1\n---\n", 2, []string{"!!map", "!!null"}},
		{"a: 1\n---\n# c\n", 2, []string{"!!map", "!!null"}},
		{"# c\n---\na: 1\n", 1, []string{"!!map"}},
		{"a: 1\n...\n", 1, []string{"!!map"}},
		{"a: 1\n...\n---\n", 2, []string{"!!map", "!!null"}},
		{"---\na: 1\n---\n\n---\nb: 2\n", 3, []string{"!!map", "!!null", "!!map"}},
		{"--- |\n  x\n", 1, []string{"!!str"}},
		{"a: 1\n--- \n", 2, []string{"!!map", "!!null"}},
		{"%YAML 1.1\n---\na: 1\n", 1, []string{"!!map"}},
		{"--- []\n# c\n---\nd: 1\n", 2, []string{"!!seq", "!!map"}},
		{"a: |\n  x\n  ---\n---\nb: 1\n", 2, []string{"!!map", "!!map"}},
		{"...\n", 0, nil},
	} {
		docs, err := Parse([]byte(c.in))
		if err != nil {
			t.Errorf("Parse(%q): %v", c.in, err)
			continue
		}
		if len(docs) != c.docs {
			t.Errorf("Parse(%q) returned %d documents, want %d", c.in, len(docs), c.docs)
			continue
		}
		for i, tag := range c.tags {
			if got := docs[i].Content[0].Tag; got != tag {
				t.Errorf("Parse(%q) document %d root tag %q, want %q", c.in, i, got, tag)
			}
		}
	}
}

func TestParseKeepsTrueLineNumbersAcrossDocuments(t *testing.T) {
	docs, err := Parse([]byte("---\na: 1\n---\n\n---\nb: 2\n"))
	if err != nil {
		t.Fatal(err)
	}
	if key := docs[2].Content[0].Content[0]; key.Value != "b" || key.Line != 6 {
		t.Fatalf("key b is at line %d, want 6", key.Line)
	}
}

// TestSplitDocumentsDoesNotPadChunks checks that a chunk holds only its
// own lines and records the line its text starts on. Padding each chunk
// with the lines before it made parsing a stream quadratic in its length.
func TestSplitDocumentsDoesNotPadChunks(t *testing.T) {
	chunks := splitDocuments("a: 1\n---\nb: 2\n")
	if len(chunks) != 2 || chunks[1].startLine != 2 || chunks[1].textLine != 2 || chunks[1].text != "---\nb: 2\n" {
		t.Fatalf("chunks = %+v, want a second chunk holding only its own text, starting on line 2", chunks)
	}
	chunks = splitDocuments("a: 1\n...\n# c\n---\nb: 2\n")
	if len(chunks) != 2 || chunks[1].startLine != 4 || chunks[1].textLine != 3 || chunks[1].text != "# c\n---\nb: 2\n" {
		t.Fatalf("chunks = %+v, want a second chunk that carries the comment on line 3", chunks)
	}
}

// TestParseIndentIndicatorBeforeDocumentMarker checks the block-scalar
// workaround on a document that is not the last. goccy sees each chunk
// as a whole stream, so an indentation indicator followed by blank lines
// before "---" needs the same fix as one at the end of the stream.
func TestParseIndentIndicatorBeforeDocumentMarker(t *testing.T) {
	for _, c := range []struct {
		in       string
		value    string
		next     string
		nextLine int
	}{
		{"a: |2\n   x\n\n---\nb: 1\n", " x\n", "b", 5},
		{"- |2\n   x\n\n---\n- y\n", " x\n", "y", 5},
		{"a: >2-\n   x\n\n\n---\nb: 1\n", " x", "b", 6},
	} {
		docs, err := Parse([]byte(c.in))
		if err != nil || len(docs) != 2 {
			t.Errorf("Parse(%q) = %d documents, %v; want 2", c.in, len(docs), err)
			continue
		}
		first, second := docs[0].Content[0], docs[1].Content[0]
		if got := first.Content[len(first.Content)-1].Value; got != c.value {
			t.Errorf("Parse(%q) block scalar = %q, want %q", c.in, got, c.value)
		}
		if n := second.Content[0]; n.Value != c.next || n.Line != c.nextLine {
			t.Errorf("Parse(%q) second document starts with %q on line %d, want %q on line %d", c.in, n.Value, n.Line, c.next, c.nextLine)
		}
	}
}

// TestSplitDocumentsCarriesLeadingComments checks that comments before
// the first "---", or between "..." and the next "---", travel with the
// document that follows, where yaml.v3 gives them to its first node, and
// that comments after the last "..." come back as a tail chunk.
func TestSplitDocumentsCarriesLeadingComments(t *testing.T) {
	for in, want := range map[string][]chunk{
		"#!/usr/bin/env genesis\n---\nkit: a\n": {{text: "#!/usr/bin/env genesis\n---\nkit: a\n", startLine: 2, textLine: 1}},
		"x: 1\n...\n# c\n---\na: 1\n": {
			{text: "x: 1\n...\n", startLine: 1, textLine: 1, marker: true},
			{text: "# c\n---\na: 1\n", startLine: 4, textLine: 3, index: 1, afterEnd: true},
		},
		"x: 1\n...\n# c\n": {
			{text: "x: 1\n...\n", startLine: 1, textLine: 1, marker: true},
			{text: "# c\n", startLine: 3, textLine: 3, index: 1, afterEnd: true, tail: true},
		},
		"x: 1\n---\ny: 2\n...\n---\nz: 3\n": {
			{text: "x: 1\n", startLine: 1, textLine: 1, marker: true},
			{text: "---\ny: 2\n...\n", startLine: 2, textLine: 2, index: 1, marker: true},
			{text: "---\nz: 3\n", startLine: 5, textLine: 5, index: 2, afterEnd: true},
		},
	} {
		if got := splitDocuments(in); !reflect.DeepEqual(got, want) {
			t.Errorf("splitDocuments(%q) = %+v, want %+v", in, got, want)
		}
	}
}

// TestSplitDocumentsSurvivesUncountedLineBreaks checks the splitter's
// backstop. Parse turns every CR into LF first, but if goccy's lexer ever
// counts a line break the splitter does not, a marker line can lie past
// the last line, and the splitter must not slice past it.
func TestSplitDocumentsSurvivesUncountedLineBreaks(t *testing.T) {
	for _, in := range []string{"a: 1\r\r\r---\r\rb: 2\n", "0\r\r...\n", "a: 1\r---\rb: 2\r\n"} {
		for _, c := range splitDocuments(in) {
			if c.text == "" {
				t.Errorf("splitDocuments(%q) produced an empty chunk", in)
			}
		}
	}
}
