package load

import (
	"bufio"
	"strings"
	"testing"
)

func TestAutoDetectCommitsOnce(t *testing.T) {
	for in, want := range map[string]Format{
		`{"a":1}`: FormatJSON, `[1]`: FormatJSON, `"s"`: FormatJSON, `12`: FormatJSON, `true`: FormatJSON, `null`: FormatJSON,
		"a: 1\n": FormatYAML, "- 1\n": FormatYAML, "# c\na: 1\n": FormatYAML, "%YAML 1.2\n---\na: 1\n": FormatYAML, "&x 1\n": FormatYAML, "!!str 1\n": FormatYAML, "|\n  x\n": FormatYAML, "'q'\n": FormatYAML, "plain\n": FormatYAML, "12 apples\n": FormatYAML, "yes\n": FormatYAML, "--- 1\n": FormatYAML,
	} {
		if got := detect(bufio.NewReader(strings.NewReader(in))); got != want {
			t.Errorf("detect(%q) = %d, want %d", in, got, want)
		}
	}
	docs := mustReadAll(t, "a: 1\n---\nb: 2\n", LoadOptions{})
	wantEqual(t, len(docs), 2)
	wantEqual(t, docs[1].DocIndex, 1)
	_, err := Open(Source{"f", strings.NewReader("{a: 1}")}, 0, LoadOptions{}).Next()
	wantErrContains(t, err, "line 1, column 2") // flow YAML reads as JSON and fails there, no retry
	docs = mustReadAll(t, "{a: 1}", LoadOptions{Format: FormatYAML})
	wantEqual(t, mustGet(docs[0].Value, "a").Float(), 1.0)
}

func TestAutoDetectOtherStarts(t *testing.T) {
	for in, want := range map[string]Format{
		"\n\n  {\"a\":1}": FormatJSON, "\xEF\xBB\xBF[1]": FormatJSON, "1 2 3\n": FormatJSON, "1 true\n": FormatJSON,
		"-1\n": FormatJSON, "key:\n  b: 1\n": FormatYAML, "12: x\n": FormatYAML, "\xFF\xFEa\x00": FormatYAML, "": FormatYAML,
	} {
		if got := detect(bufio.NewReader(strings.NewReader(in))); got != want {
			t.Errorf("detect(%q) = %d, want %d", in, got, want)
		}
	}
}
