package load_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/query/load"
	"github.com/fivetwenty-io/graft/pkg/yq"
)

// readAll renders every document of text as the reader yields them, and
// the error that ended the read.
func readAll(text string, incremental bool, noHeader bool) string {
	opts := load.LoadOptions{Format: load.FormatYAML, KeepMeta: true, NodeComments: noHeader, Incremental: incremental}
	r := load.Open(load.Source{Name: "in", Reader: strings.NewReader(text)}, 0, opts)
	var out strings.Builder
	for {
		d, err := r.Next()
		if errors.Is(err, io.EOF) {
			return out.String()
		}
		if err != nil {
			fmt.Fprintf(&out, "error: %v\n", err)
			return out.String()
		}
		var buf bytes.Buffer
		enc := yq.NewEncoder(&buf, yq.EncodeOptions{NoDocSeparators: true})
		if err := enc.Encode(d.Value); err == nil {
			_ = enc.Flush()
		}
		fmt.Fprintf(&out, "doc %d line %d lead %q\n%s", d.DocIndex, d.Line, d.LeadingContent, buf.String())
	}
}

var incrementalInputs = []string{
	"",
	"\n",
	"a: 1\n",
	"a: 1",
	"a: 1\n---\nb: 2\n",
	"---\na: 1\n---\nb: 2\n",
	"# head\n\n---\na: 1\n---\nb: 2\n",
	"# head\na: 1 # line\n# foot\n---\n# lead\nb: 2\n",
	"# only a comment\n",
	"# only a comment\n---\n",
	"---\n---\n---\na: 1\n",
	"a: 1\n---\n---\nb: 2\n",
	"a: 1\n---\n",
	"a: 1\n...\n---\nb: 2\n",
	"a: 1\n...\nb: 2\n",
	"%YAML 1.2\n---\na: 1\n...\n%YAML 1.2\n---\nb: 2\n",
	"--- text\n--- other\n",
	"--- |\n  block\n  ---\n  more\n---\nb: 2\n",
	"a: |\n  one\n  two\n---\nb: >\n  folded\n",
	"a: &x 1\nb: *x\n---\nc: &x 2\nd: *x\n",
	"a: [1,\n  2]\n---\nb: {x: 1,\n  y: 2}\n",
	"a: 'it''s'\n---\nb: \"q\\n\"\n",
	"a: 1\r\n---\r\nb: 2\r\n",
	"\ufeffa: 1\n---\nb: 2\n",
	"a: 1\n---\nb: [\n",
	"a: 1\n---\nb: 2\n---\n: bad\n  x\n",
	"- 1\n- 2\n---\n- 3\n",
	"a: 1\n\n\n---\n\n# c\nb: 2\n\n",
	"a: 1\n--- # trailing\nb: 2\n",
	"a: 1\n---   \nb: 2\n",
	"a: 1\n----\nb: 2\n",
	"a: 1\n---x\n",
	"# c1\n# c2\n\n--- a\n--- b\n",
	"scalar\n---\nscalar2\n",
	"a: 1\n# between\n---\n# lead2\nb: 2\n# end\n",
	"k: !!str 5\n---\nt: !tag v\n",
	"? a\n: 1\n---\n? b\n: 2\n",
	"a: 1\n---\n\n\n\n",
	"a: 1\n... # c\n---\nx: 1\n",
	"b: 2\n... # c\n--- # tail\n...\n",
	"'q'\n# comment\n--- \n  # indented\n\nd: |\n  lit\n  # indented\n--- v\n'q'\n",
}

// same reports whether an incremental read matches the whole one. A source
// with a document that does not parse fails whole, where an incremental
// read yields the documents before it first and then fails the same way.
func same(whole, inc string) bool {
	if whole == inc {
		return true
	}
	if !strings.HasPrefix(whole, "error: ") {
		return false
	}
	// Both fail. The two parsers may word the failure differently when the
	// text they see differs in what came before it.
	last := inc[strings.LastIndex(strings.TrimSuffix(inc, "\n"), "\n")+1:]
	return strings.HasPrefix(last, "error: ")
}

func TestIncrementalMatchesWhole(t *testing.T) {
	for _, text := range incrementalInputs {
		for _, noHeader := range []bool{false, true} {
			whole := readAll(text, false, noHeader)
			inc := readAll(text, true, noHeader)
			if !same(whole, inc) {
				t.Errorf("input %q (no header %v)\nwhole:\n%s\nincremental:\n%s", text, noHeader, whole, inc)
			}
		}
	}
}

func TestIncrementalMatchesWholeOnFixtures(t *testing.T) {
	var files []string
	for _, pattern := range []string{
		"../../../tests/query-parity/yq/cases/*/in.yaml",
		"../../../tests/query-parity/yq/cases/*/*.yaml",
		"../../../tests/query-parity/jq/cases/*/in.yaml",
		"../../../examples/*/*.yml",
		"../../../examples/*/*.yaml",
	} {
		m, _ := filepath.Glob(pattern)
		files = append(files, m...)
	}
	if len(files) == 0 {
		t.Skip("no fixtures")
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil || len(data) > 1<<20 {
			continue
		}
		for _, noHeader := range []bool{false, true} {
			if whole, inc := readAll(string(data), false, noHeader), readAll(string(data), true, noHeader); !same(whole, inc) {
				t.Errorf("%s (no header %v)\nwhole:\n%s\nincremental:\n%s", f, noHeader, whole, inc)
			}
		}
	}
}

// TestIncrementalMatchesWholeOnShuffledLines builds sources from the lines
// that make documents hard to cut, in an order a seeded generator picks.
func TestIncrementalMatchesWholeOnShuffledLines(t *testing.T) {
	lines := []string{
		"a: 1\n", "b: [1, 2]\n", "c:\n  - x\n  - y\n", "---\n", "--- \n", "--- v\n", "...\n",
		"# comment\n", "\n", "  # indented\n", "%YAML 1.2\n", "d: |\n  lit\n", "e: &k 1\n", "f: *k\n",
		"plain\n", "- 1\n", "'q'\n", "g: {x: 1}\n", "h: 'multi\n  line'\n",
	}
	seed := uint64(7)
	next := func() int {
		seed = seed*6364136223846793005 + 1442695040888963407
		return int(seed >> 33)
	}
	for range 4000 {
		var b strings.Builder
		for range 1 + next()%9 {
			b.WriteString(lines[next()%len(lines)])
		}
		text := b.String()
		for _, noHeader := range []bool{false, true} {
			// Only the yq decoder reads incrementally, and it never keeps
			// node comments, so a directive that the node-comment parse
			// refuses is out of scope there.
			if noHeader && strings.HasPrefix(text, "%") {
				continue
			}
			// A directive the whole-stream parse refuses, with no "---"
			// to follow it, is one the chunked parse reads leniently.
			if strings.Contains(text, "%YAML") && strings.Contains(readAll(text, false, noHeader), "document not started") {
				continue
			}
			whole := readAll(text, false, noHeader)
			inc := readAll(text, true, noHeader)
			if !same(whole, inc) {
				t.Fatalf("input %q (no header %v)\nwhole:\n%s\nincremental:\n%s", text, noHeader, whole, inc)
			}
		}
	}
}
