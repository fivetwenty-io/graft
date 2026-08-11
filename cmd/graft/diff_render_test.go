package main

import (
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
	if !(modifiedIdx < addedIdx && addedIdx < removedIdx) {
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
