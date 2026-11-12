package yaml_test

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

const (
	corpusFloor   = 1070
	sentinel      = "graft-document-sentinel"
	maxSetsInFile = 20
	setSweepFloor = 3380

	// The short sweep visits every shortStride-th file and makes at
	// most shortSetsInFile edits in each, which keeps make test-race
	// quick while still covering every kind of file.
	shortStride     = 8
	shortSetsInFile = 4
	shortSweepFloor = 165
)

// corpusFiles returns every YAML file under tests and examples, leaving
// out any directory named testdata, whose files may be invalid on purpose.
func corpusFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	for _, root := range []string{"../../tests", "../../examples"} {
		err := filepath.WalkDir(root, func(p string, e fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if e.IsDir() && e.Name() == "testdata" {
				return filepath.SkipDir
			}
			if !e.IsDir() && (strings.HasSuffix(p, ".yml") || strings.HasSuffix(p, ".yaml")) {
				files = append(files, p)
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	return files
}

func TestDocumentCorpusRoundTrips(t *testing.T) {
	parsed, skipped := 0, 0
	for _, f := range corpusFiles(t) {
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		d, err := yaml.ParseDocument(data)
		if err != nil {
			skipped++
			continue
		}
		parsed++
		if !bytes.Equal(d.Bytes(), data) {
			t.Errorf("%s: Bytes() differs from the input", f)
		}
	}
	if parsed < corpusFloor {
		t.Fatalf("parsed %d corpus files, want at least %d", parsed, corpusFloor)
	}
	t.Logf("%d corpus files round-trip, %d the parser refuses were skipped", parsed, skipped)
}

// stringPaths returns up to limit cursor paths of one-line string
// scalars under n whose keys need no quoting.
func stringPaths(n *yaml.Node, prefix string, limit int, out *[]string) {
	if len(*out) >= limit || n == nil {
		return
	}
	join := func(seg string) string {
		if prefix == "" {
			return seg
		}
		return prefix + "." + seg
	}
	switch n.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(n.Content); i += 2 {
			k := n.Content[i]
			if k.Kind != yaml.ScalarNode || k.Value == "" || strings.ContainsAny(k.Value, ".[]=$") {
				continue
			}
			stringPaths(n.Content[i+1], join(k.Value), limit, out)
		}
	case yaml.SequenceNode:
		for i, c := range n.Content {
			stringPaths(c, join(strconv.Itoa(i)), limit, out)
		}
	case yaml.ScalarNode:
		if n.Tag == "!!str" && !strings.Contains(n.Value, "\n") && prefix != "" {
			*out = append(*out, prefix)
		}
	case yaml.DocumentNode, yaml.AliasNode:
		// The caller passes the document's root, and the sweep never
		// edits through an alias.
	}
}

// indentOf counts the leading spaces and tabs of s.
func indentOf(s string) int {
	return len(s) - len(strings.TrimLeft(s, " \t"))
}

// nodeEnd returns the index one past the last input line the node that
// starts on the 1-based line occupies. A block scalar owns the lines that
// follow its header while they are blank or indented deeper than it, and
// any other node owns its own line.
func nodeEnd(lines []string, line int) int {
	end := line
	header := lines[line-1]
	base := indentOf(header)
	for end < len(lines) {
		l := lines[end]
		if strings.TrimSpace(l) != "" && indentOf(l) <= base {
			break
		}
		end++
	}
	if end == line {
		return end
	}
	// Only a block scalar header may claim the lines after it.
	h := strings.TrimRight(header, " \t\r")
	if i := strings.LastIndexAny(h, "|>"); i >= 0 && strings.Trim(h[i:], "|>+-0123456789") == "" {
		return end
	}
	return line
}

// editLeak checks that an edit changed only the node that started on the
// 1-based line before. Everything above the node and everything below it
// must be unchanged, and a block scalar edit may change the line count,
// which shifts the lines below it. It returns "" when the edit stayed
// local, or a description of the stray change.
func editLeak(oldLines, newLines []string, line int) string {
	pre := 0
	for pre < len(oldLines) && pre < len(newLines) && oldLines[pre] == newLines[pre] {
		pre++
	}
	suf := 0
	for suf < len(oldLines)-pre && suf < len(newLines)-pre &&
		oldLines[len(oldLines)-1-suf] == newLines[len(newLines)-1-suf] {
		suf++
	}
	oldEnd := nodeEnd(oldLines, line)
	// The changed block is oldLines[pre:len-suf] and must sit inside the
	// node's old range, which is lines line-1 up to oldEnd.
	if pre < line-1 || len(oldLines)-suf > oldEnd {
		return fmt.Sprintf("changed lines %d to %d, outside the node at lines %d to %d",
			pre+1, len(oldLines)-suf, line, oldEnd)
	}
	return ""
}

// TestEditLeakCatchesStrayEdits feeds editLeak edits that touch a line
// outside the node, alone and next to a legitimate edit, and checks that
// it reports both while accepting the legitimate edit by itself.
func TestEditLeakCatchesStrayEdits(t *testing.T) {
	old := strings.Split("a: 1\nb: 2\nc: 3\nd: 4", "\n")
	cases := map[string][]string{
		"stray line before":       {"a: X", "b: SENTINEL", "c: 3", "d: 4"},
		"stray line after":        {"a: 1", "b: SENTINEL", "c: 3", "d: X"},
		"stray beside the edit":   {"a: 1", "b: SENTINEL", "c: X", "d: 4"},
		"stray both sides":        {"a: X", "b: SENTINEL", "c: 3", "d: X"},
		"stray with a line added": {"a: 1", "b: SENTINEL", "c: X", "d: 4", "e: 5"},
	}
	for name, next := range cases {
		if editLeak(old, next, 2) == "" {
			t.Errorf("%s: editLeak did not report the stray edit", name)
		}
	}
	if got := editLeak(old, []string{"a: 1", "b: SENTINEL", "c: 3", "d: 4"}, 2); got != "" {
		t.Errorf("legitimate edit reported: %s", got)
	}
	block := strings.Split("k: |\n  one\n  two\nz: 1", "\n")
	grown := strings.Split("k: |\n  one\n  two\n  three\nz: 1", "\n")
	if got := editLeak(block, grown, 1); got != "" {
		t.Errorf("block edit that adds a line reported: %s", got)
	}
	grown[4] = "z: 2"
	if editLeak(block, grown, 1) == "" {
		t.Error("stray edit beside a block edit not reported")
	}
}

func TestDocumentCorpusSetSweep(t *testing.T) {
	stride, perFile, floor := 1, maxSetsInFile, setSweepFloor
	if testing.Short() {
		stride, perFile, floor = shortStride, shortSetsInFile, shortSweepFloor
	}
	start := time.Now()
	sets, skipped, refused := 0, 0, 0
	for i, f := range corpusFiles(t) {
		if i%stride != 0 {
			continue
		}
		data, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		probe, err := yaml.ParseDocument(data)
		if err != nil {
			skipped++
			continue
		}
		root, err := probe.Get("")
		if err != nil {
			continue
		}
		var paths []string
		stringPaths(root, "", perFile, &paths)
		oldLines := strings.Split(string(data), "\n")
		for _, p := range paths {
			d, _ := yaml.ParseDocument(data)
			if d.Editable(p) != nil {
				refused++
				continue
			}
			before, _ := d.Get(p)
			if err := d.Set(p, sentinel); err != nil {
				t.Errorf("%s: Editable(%s) was nil but Set failed: %v", f, p, err)
				continue
			}
			after, err := d.Get(p)
			if err != nil || after.Value != sentinel || after.Line != before.Line {
				t.Errorf("%s: Get(%s) after Set = %+v, %v", f, p, after, err)
				continue
			}
			newLines := strings.Split(string(d.Bytes()), "\n")
			if leak := editLeak(oldLines, newLines, before.Line); leak != "" {
				t.Errorf("%s: Set(%s) %s", f, p, leak)
			}
			sets++
		}
	}
	if sets < floor {
		t.Fatalf("the sweep made %d edits, want at least %d", sets, floor)
	}
	t.Logf("%d edits verified in %s, %d Editable refusals, %d unparsable files skipped", sets, time.Since(start).Round(time.Millisecond), refused, skipped)
}
