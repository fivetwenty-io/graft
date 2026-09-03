package main

import (
	"fmt"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/histdiff"
	"github.com/fivetwenty-io/graft/internal/utils/ansi"
)

func TestRenderChangeListFormatsHeaderAndGroups(t *testing.T) {
	ansi.Color(false)
	changes := []histdiff.Change{
		{Path: "database.host", Kind: histdiff.Modified, Old: "localhost", New: "db.prod.example.com"},
		{Path: "database.timeout", Kind: histdiff.Modified, Old: 30, New: 60},
		{Path: "database.ssl", Kind: histdiff.Added, New: true},
		{Path: "meta.version", Kind: histdiff.Removed, Old: "1.0"},
	}

	out := renderChangeList(changes)

	if !strings.HasPrefix(out, "Changes (2 modified, 1 added, 1 removed):\n") {
		t.Fatalf("unexpected header, got:\n%s", out)
	}

	// Modified entries must appear before Added, which must appear before
	// Removed (docs/user-guide/cli/diff.md's example ordering).
	modifiedIdx := strings.Index(out, "MODIFIED  database.host")
	addedIdx := strings.Index(out, "ADDED     database.ssl")
	removedIdx := strings.Index(out, "REMOVED   meta.version")
	if modifiedIdx < 0 || addedIdx < 0 || removedIdx < 0 {
		t.Fatalf("missing expected entries in output:\n%s", out)
	}
	if modifiedIdx >= addedIdx || addedIdx >= removedIdx {
		t.Fatalf("entries out of order (modified=%d added=%d removed=%d):\n%s", modifiedIdx, addedIdx, removedIdx, out)
	}

	if !strings.Contains(out, "- localhost") || !strings.Contains(out, "+ db.prod.example.com") {
		t.Fatalf("missing old/new values for modified entry:\n%s", out)
	}
	if !strings.Contains(out, "+ true") {
		t.Fatalf("missing new value for added entry:\n%s", out)
	}
	if !strings.Contains(out, `- "1.0"`) {
		t.Fatalf("missing old value for removed entry:\n%s", out)
	}
}

func TestRenderChangeListEmptyChanges(t *testing.T) {
	ansi.Color(false)
	out := renderChangeList(nil)
	if out != "Changes (0 modified, 0 added, 0 removed):\n" {
		t.Fatalf("unexpected output for no changes: %q", out)
	}
}

func TestYamlValueLinesNil(t *testing.T) {
	lines := yamlValueLines(nil)
	if len(lines) != 1 || lines[0] != "~" {
		t.Fatalf("yamlValueLines(nil) = %v, want [~]", lines)
	}
}

func TestRenderUnifiedDiffGroupsByTopLevelKey(t *testing.T) {
	ansi.Color(false)
	from := map[string]interface{}{
		"database": map[string]interface{}{"host": "localhost", "port": 5432, "timeout": 30},
		"meta":     map[string]interface{}{"version": "1.0"},
	}
	to := map[string]interface{}{
		"database": map[string]interface{}{"host": "db.prod.example.com", "port": 5432, "timeout": 60, "ssl": true},
	}

	out, err := renderUnifiedDiff("base.yml", from, "modified.yml", to, 3)
	if err != nil {
		t.Fatalf("renderUnifiedDiff error: %v", err)
	}

	if !strings.HasPrefix(out, "--- base.yml\n+++ modified.yml\n") {
		t.Fatalf("missing file header, got:\n%s", out)
	}
	if !strings.Contains(out, "@@ database @@") {
		t.Fatalf("missing database hunk header:\n%s", out)
	}
	if !strings.Contains(out, "@@ meta @@") {
		t.Fatalf("missing meta hunk header:\n%s", out)
	}
	if !strings.Contains(out, `-  host: localhost`) {
		t.Fatalf("missing removed host line:\n%s", out)
	}
	if !strings.Contains(out, `+  host: db.prod.example.com`) {
		t.Fatalf("missing added host line:\n%s", out)
	}
	if !strings.Contains(out, "   port: 5432") {
		t.Fatalf("missing unchanged context line for port:\n%s", out)
	}
	if !strings.Contains(out, `+  ssl: true`) {
		t.Fatalf("missing added ssl line:\n%s", out)
	}
	if !strings.Contains(out, `-  version: "1.0"`) {
		t.Fatalf("missing removed meta line:\n%s", out)
	}

	// database appears before meta (topKeys order is honored).
	if strings.Index(out, "@@ database @@") > strings.Index(out, "@@ meta @@") {
		t.Fatalf("expected database hunk before meta hunk:\n%s", out)
	}
}

func TestRenderUnifiedDiffMissingKeyOnOneSide(t *testing.T) {
	ansi.Color(false)
	from := map[string]interface{}{"api": map[string]interface{}{"key": "abc"}}
	to := map[string]interface{}{}

	out, err := renderUnifiedDiff("base.yml", from, "modified.yml", to, 3)
	if err != nil {
		t.Fatalf("renderUnifiedDiff error: %v", err)
	}
	if !strings.Contains(out, `-  key: abc`) {
		t.Fatalf("expected removed line for wholly-removed key, got:\n%s", out)
	}
	if strings.Contains(out, "\n+ ") || strings.HasPrefix(out, "+ ") {
		t.Fatalf("did not expect any added hunk lines, got:\n%s", out)
	}
}

// TestRenderUnifiedDiffSequenceRoot locks F7's fix: a sequence-root document
// (ytbx/dyff's TopLevelPaths would yield a bracket index like "[0]", never a
// real map key) must still produce a real diff, not a header with an empty
// body.
func TestRenderUnifiedDiffSequenceRoot(t *testing.T) {
	ansi.Color(false)
	from := []interface{}{"a", "b"}
	to := []interface{}{"a", "c"}

	out, err := renderUnifiedDiff("l1.yml", from, "l2.yml", to, 3)
	if err != nil {
		t.Fatalf("renderUnifiedDiff error: %v", err)
	}
	if !strings.Contains(out, "@@ (root) @@") {
		t.Fatalf("expected a whole-document hunk header for a non-map root, got:\n%s", out)
	}
	if !strings.Contains(out, "-  - b") {
		t.Fatalf("expected a removed line for 'b', got:\n%s", out)
	}
	if !strings.Contains(out, "+  - c") {
		t.Fatalf("expected an added line for 'c', got:\n%s", out)
	}
}

// TestRenderUnifiedDiffLiteralDottedKey locks F7's fix: a literal top-level
// map key containing a dot (e.g. "a.b") must diff as that one key, not
// collide with a "a" prefix derived by splitting the key string.
func TestRenderUnifiedDiffLiteralDottedKey(t *testing.T) {
	ansi.Color(false)
	from := map[string]interface{}{"a.b": 1, "plain": "x"}
	to := map[string]interface{}{"a.b": 2, "plain": "x"}

	out, err := renderUnifiedDiff("d1.yml", from, "d2.yml", to, 3)
	if err != nil {
		t.Fatalf("renderUnifiedDiff error: %v", err)
	}
	if !strings.Contains(out, "@@ a.b @@") {
		t.Fatalf("expected a hunk header for the literal key 'a.b', got:\n%s", out)
	}
	if strings.Contains(out, "@@ a @@") {
		t.Fatalf("did not expect a hunk header for a split-off 'a' prefix, got:\n%s", out)
	}
	if !strings.Contains(out, "-  1") {
		t.Fatalf("expected removed value 1 for a.b, got:\n%s", out)
	}
	if !strings.Contains(out, "+  2") {
		t.Fatalf("expected added value 2 for a.b, got:\n%s", out)
	}
	if strings.Contains(out, "@@ plain @@") {
		t.Fatalf("did not expect a hunk for the unchanged 'plain' key, got:\n%s", out)
	}
}

func TestRenderSideBySideAlignsUnchangedAndChangedLines(t *testing.T) {
	ansi.Color(false)
	from := map[string]interface{}{"database": map[string]interface{}{"host": "localhost", "port": 5432}}
	to := map[string]interface{}{"database": map[string]interface{}{"host": "db.prod.example.com", "port": 5432}}

	out, err := renderSideBySide("base.yml", from, "modified.yml", to, 80)
	if err != nil {
		t.Fatalf("renderSideBySide error: %v", err)
	}
	if !strings.Contains(out, "base.yml") || !strings.Contains(out, "modified.yml") {
		t.Fatalf("missing file labels in header:\n%s", out)
	}
	if !strings.Contains(out, "┼") {
		t.Fatalf("missing separator row:\n%s", out)
	}
	if !strings.Contains(out, "port: 5432") {
		t.Fatalf("missing unchanged port row:\n%s", out)
	}
	if !strings.Contains(out, "localhost") || !strings.Contains(out, "db.prod.example.com") {
		t.Fatalf("missing both sides of the changed host row:\n%s", out)
	}
}

// TestRenderSideBySideSeparatorAlignsWithColumnDivider locks F10's fix: the
// "┼" in the separator row between the header and the data rows must sit at
// the same rune column as every "│" in the header/data rows below it, not
// one column to the left.
func TestRenderSideBySideSeparatorAlignsWithColumnDivider(t *testing.T) {
	ansi.Color(false)
	from := map[string]interface{}{"database": map[string]interface{}{"host": "localhost"}}
	to := map[string]interface{}{"database": map[string]interface{}{"host": "db.prod.example.com"}}

	out, err := renderSideBySide("base.yml", from, "modified.yml", to, 80)
	if err != nil {
		t.Fatalf("renderSideBySide error: %v", err)
	}

	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) < 3 {
		t.Fatalf("expected at least a header, separator, and one data row, got %d lines:\n%s", len(lines), out)
	}
	headerBarCol := runeIndex(lines[0], '│')
	sepCrossCol := runeIndex(lines[1], '┼')
	if headerBarCol < 0 {
		t.Fatalf("header row missing '│': %q", lines[0])
	}
	if sepCrossCol < 0 {
		t.Fatalf("separator row missing '┼': %q", lines[1])
	}
	if headerBarCol != sepCrossCol {
		t.Fatalf("separator '┼' at column %d, header '│' at column %d - want equal\nheader:    %q\nseparator: %q", sepCrossCol, headerBarCol, lines[0], lines[1])
	}

	for i, line := range lines[2:] {
		col := runeIndex(line, '│')
		if col < 0 {
			continue // a wrapped/empty side of a changed row may omit the bar mid-block; not this test's concern
		}
		if col != headerBarCol {
			t.Fatalf("data row %d '│' at column %d, want %d (header's column)\nrow: %q", i, col, headerBarCol, line)
		}
	}
}

func runeIndex(s string, target rune) int {
	for i, r := range s {
		if r == target {
			return len([]rune(s[:i]))
		}
	}
	return -1
}

func TestPadTruncPadsAndTruncates(t *testing.T) {
	if got := padTrunc("abc", 5); got != "abc  " {
		t.Errorf("padTrunc short string = %q, want %q", got, "abc  ")
	}
	if got := padTrunc("abcdefgh", 5); got != "abcde" {
		t.Errorf("padTrunc long string = %q, want %q", got, "abcde")
	}
}

// buildJobsSequence returns a 60-element sequence-rooted document (a YAML
// list at the top level, not a map), matching the shape of a BOSH-style
// jobs manifest: each block has a "name" that's the same on both from/to
// sides, an "enabled: true" line repeated identically across all 60
// blocks (the popular/common line difflib's autojunk heuristic refuses to
// anchor on above 200 lines), and "version"/"instances" values unique per
// block and per side (suffix), so a correct diff can't shrink the true
// edit count by matching a value from a different block.
func buildJobsSequence(suffix string) []interface{} {
	blocks := make([]interface{}, 0, 60)
	for i := 0; i < 60; i++ {
		blocks = append(blocks, map[string]interface{}{
			"name":      fmt.Sprintf("job%d", i),
			"enabled":   true,
			"version":   fmt.Sprintf("ver-%d-%s", i, suffix),
			"instances": fmt.Sprintf("inst-%d-%s", i, suffix),
		})
	}
	return blocks
}

// countUnifiedChangeLines counts the removed ("-") and added ("+") lines in
// a renderUnifiedDiff/writeUnifiedHunkLines-style unified diff body,
// skipping the "--- "/"+++ " file header lines (which also start with "-"
// and "+" respectively) and "@@ ... @@" hunk headers.
func countUnifiedChangeLines(out string) (removed, added int) {
	for _, line := range strings.Split(out, "\n") {
		switch {
		case strings.HasPrefix(line, "--- "), strings.HasPrefix(line, "+++ "), strings.HasPrefix(line, "@@"):
			continue
		case strings.HasPrefix(line, "-"):
			removed++
		case strings.HasPrefix(line, "+"):
			added++
		}
	}
	return removed, added
}

// countSideBySideDifferingRows counts renderSideBySide data rows (skipping
// the header and "┼" separator rows) whose left and right columns hold
// different text once padding is trimmed. A correctly aligned diff shows a
// differing row only where the source document actually changed; an
// over-reporting diff (autojunk refusing to anchor on a popular line)
// widens the mismatched region well past the true edit count.
func countSideBySideDifferingRows(out string) int {
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	differing := 0
	for i, line := range lines {
		if i < 2 { // header row, then the "┼" separator row
			continue
		}
		parts := strings.SplitN(line, " │ ", 2)
		if len(parts) != 2 {
			continue
		}
		left := strings.TrimSpace(parts[0])
		right := strings.TrimSpace(parts[1])
		if left != right {
			differing++
		}
	}
	return differing
}

// TestRenderUnifiedDiffLargeDocDoesNotOverReport locks the fix for
// difflib's autojunk heuristic over-reporting changes on documents of 200
// or more lines that repeat a common line often (real YAML manifests do
// this constantly, e.g. "enabled: true"). Autojunk refuses to anchor a
// match on any line occurring more than 1% of the time, which on this
// 240-line sequence (60 four-line blocks, "enabled: true" repeated in
// every block) makes the matcher report the whole document as changed
// instead of just the 2 lines per block ("version", "instances") that
// actually differ.
func TestRenderUnifiedDiffLargeDocDoesNotOverReport(t *testing.T) {
	ansi.Color(false)
	from := buildJobsSequence("a")
	to := buildJobsSequence("b")

	out, err := renderUnifiedDiff("from.yml", from, "to.yml", to, 300)
	if err != nil {
		t.Fatalf("renderUnifiedDiff error: %v", err)
	}
	if !strings.Contains(out, "@@ (root) @@") {
		t.Fatalf("expected a whole-document hunk header for a sequence root, got:\n%s", out)
	}

	removed, added := countUnifiedChangeLines(out)
	const wantChanged = 120 // 60 blocks * 2 changed fields (version, instances)
	if removed != wantChanged || added != wantChanged {
		t.Fatalf("renderUnifiedDiff over-reported changes: got %d removed / %d added, want %d/%d\n%s",
			removed, added, wantChanged, wantChanged, out)
	}

	// Cheap side-by-side regression check: the same matcher powers
	// renderSideBySide's row alignment, so an over-reporting matcher would
	// also misalign well past the true 120 changed lines.
	sideOut, err := renderSideBySide("from.yml", from, "to.yml", to, 120)
	if err != nil {
		t.Fatalf("renderSideBySide error: %v", err)
	}
	const maxDiffering = 150 // true edit count (120) plus slack for row grouping, well under a full over-report (~240)
	if got := countSideBySideDifferingRows(sideOut); got > maxDiffering {
		t.Fatalf("renderSideBySide over-reported changes: %d differing rows, want <= %d\n%s", got, maxDiffering, sideOut)
	}
}

func TestSplitTrimmedLinesDropsTrailingNewlineArtifact(t *testing.T) {
	lines := splitTrimmedLines("a: 1\nb: 2\n")
	want := []string{"a: 1", "b: 2"}
	if len(lines) != len(want) {
		t.Fatalf("splitTrimmedLines() = %v, want %v", lines, want)
	}
	for i := range want {
		if lines[i] != want[i] {
			t.Fatalf("splitTrimmedLines() = %v, want %v", lines, want)
		}
	}
}
