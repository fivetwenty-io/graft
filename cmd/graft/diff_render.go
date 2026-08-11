package main

import (
	"fmt"
	"sort"
	"strings"

	"github.com/pmezard/go-difflib/difflib"

	"github.com/fivetwenty-io/graft/internal/histdiff"
	"github.com/fivetwenty-io/graft/internal/utils/ansi"
	"github.com/fivetwenty-io/graft/pkg/graft"
)

// defaultSideBySideWidth is the total terminal width `graft diff
// --side-by-side` targets when --width isn't given, matching the two
// ~38-column panes shown in docs/user-guide/cli/diff.md's example.
const defaultSideBySideWidth = 80

// defaultUnifiedContext is the number of unchanged context lines shown
// around each hunk in `graft diff --unified` when --context isn't given,
// matching the git/diff -u convention.
const defaultUnifiedContext = 3

// renderChangeList renders changes in the "Changes (N modified, M added, K
// removed):" format used by `graft diff --changes` (and reused by
// `graft debug`'s `diff` REPL command). Entries are grouped by kind
// (Modified, then Added, then Removed) and sorted by path within each
// group, matching docs/user-guide/cli/diff.md's example ordering.
func renderChangeList(changes []histdiff.Change) string {
	counts := histdiff.CountChanges(changes)

	var buf strings.Builder
	fmt.Fprintf(&buf, "Changes (%d modified, %d added, %d removed):\n",
		counts.Modified, counts.Added, counts.Removed)

	for _, kind := range []histdiff.Kind{histdiff.Modified, histdiff.Added, histdiff.Removed} {
		for _, c := range changes {
			if c.Kind != kind {
				continue
			}
			buf.WriteString("\n")
			fmt.Fprintf(&buf, "  %-9s %s\n", kind.String(), c.Path)
			switch kind {
			case histdiff.Added:
				writeValueLines(&buf, "+", ansi.Green, c.New)
			case histdiff.Removed:
				writeValueLines(&buf, "-", ansi.Red, c.Old)
			case histdiff.Modified:
				writeValueLines(&buf, "-", ansi.Red, c.Old)
				writeValueLines(&buf, "+", ansi.Green, c.New)
			}
		}
	}

	return buf.String()
}

// writeValueLines writes value (marshaled to YAML) under a "+ "/"- "
// gutter, each line colored by colorFn, indented to align under the
// change-list entries renderChangeList produces.
func writeValueLines(buf *strings.Builder, gutter string, colorFn func(string) string, value interface{}) {
	for _, line := range yamlValueLines(value) {
		fmt.Fprintf(buf, "            %s\n", colorFn(gutter+" "+line))
	}
}

// yamlValueLines renders value as YAML and returns its non-empty lines
// (trailing newline stripped). A nil value renders as "~", matching
// graft's own YAML null convention rather than an empty line.
func yamlValueLines(value interface{}) []string {
	if value == nil {
		return []string{"~"}
	}
	raw, err := graft.MarshalYAML(value)
	if err != nil {
		return []string{fmt.Sprintf("<error rendering value: %s>", err.Error())}
	}
	text := strings.TrimRight(string(raw), "\n")
	if text == "" {
		return []string{"~"}
	}
	return strings.Split(text, "\n")
}

// renderUnifiedDiff renders a git-style unified diff of fromDoc/toDoc.
//
// When both sides are map-rooted documents (the common case), hunks are
// grouped by top-level key (docs/user-guide/cli/diff.md's "@@ <key> @@"
// headers) rather than by raw document line number: each top-level key
// present in either side gets its own header and its own line-level
// unified diff of that key's value, with contextLines unchanged lines of
// surrounding context (git's -u default is 3) - but only if that key's
// value actually differs; an unchanged key produces no hunk at all.
//
// Grouping keys come directly from fromMap/toMap's own real top-level
// keys (sorted, de-duplicated), not from splitting a dotted change path
// string (as an earlier version did via histdiff.TopLevelPaths). That
// matters for two cases the path-splitting approach silently produced an
// empty hunk for (F7): a literal top-level key containing a dot (e.g.
// "a.b: 1") is diffed as that one key, not confused with a "a" prefix;
// and a document whose root is not a map at all (most commonly a YAML
// sequence) falls through to the second branch below - a single
// whole-document hunk under a "@@ (root) @@" header - since "top-level
// key" has no meaning there.
func renderUnifiedDiff(fromLabel string, fromDoc interface{}, toLabel string, toDoc interface{}, contextLines int) (string, error) {
	if contextLines < 0 {
		contextLines = defaultUnifiedContext
	}

	var buf strings.Builder
	fmt.Fprintf(&buf, "--- %s\n+++ %s\n", fromLabel, toLabel)

	fromMap, fromIsMap := fromDoc.(map[string]interface{})
	toMap, toIsMap := toDoc.(map[string]interface{})

	if !fromIsMap || !toIsMap {
		fromLines, err := yamlBlockLines(fromDoc)
		if err != nil {
			return "", fmt.Errorf("rendering %s: %w", fromLabel, err)
		}
		toLines, err := yamlBlockLines(toDoc)
		if err != nil {
			return "", fmt.Errorf("rendering %s: %w", toLabel, err)
		}
		if !equalLines(fromLines, toLines) {
			buf.WriteString("@@ (root) @@\n")
			writeUnifiedHunkLines(&buf, fromLines, toLines, contextLines)
		}
		return buf.String(), nil
	}

	for _, key := range unionSortedKeys(fromMap, toMap) {
		fromLines, err := yamlBlockLines(fromMap[key])
		if err != nil {
			return "", fmt.Errorf("rendering %q from %s: %w", key, fromLabel, err)
		}
		toLines, err := yamlBlockLines(toMap[key])
		if err != nil {
			return "", fmt.Errorf("rendering %q from %s: %w", key, toLabel, err)
		}
		if equalLines(fromLines, toLines) {
			continue
		}

		fmt.Fprintf(&buf, "@@ %s @@\n", key)
		writeUnifiedHunkLines(&buf, fromLines, toLines, contextLines)
	}

	return buf.String(), nil
}

// equalLines reports whether a and b hold the same lines in the same
// order.
func equalLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// unionSortedKeys returns the sorted, de-duplicated union of a's and b's
// top-level keys.
func unionSortedKeys(a, b map[string]interface{}) []string {
	seen := make(map[string]bool, len(a)+len(b))
	keys := make([]string, 0, len(a)+len(b))
	for k := range a {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	for k := range b {
		if !seen[k] {
			seen[k] = true
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	return keys
}

// yamlBlockLines renders v (typically a top-level key's value) as an
// indented YAML block: MarshalYAML's output with every line prefixed by two
// spaces, so a nested map's fields line up the way
// docs/user-guide/cli/diff.md's unified example shows them ("  host: ...").
// A nil v (key absent on this side) renders as no lines at all, so a
// wholly added/removed top-level key diffs as a pure insertion/deletion
// rather than a "~" placeholder line.
func yamlBlockLines(v interface{}) ([]string, error) {
	if v == nil {
		return nil, nil
	}
	raw, err := graft.MarshalYAML(v)
	if err != nil {
		return nil, err
	}
	text := strings.TrimRight(string(raw), "\n")
	if text == "" {
		return nil, nil
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = "  " + line
	}
	return lines, nil
}

// writeUnifiedHunkLines writes fromLines/toLines as unified-diff hunks
// (grouped opcodes with contextLines of surrounding context), using
// difflib's Myers-diff-derived opcodes directly rather than difflib's own
// WriteUnifiedDiff so the numeric "@@ -l,c +l,c @@" range header can be
// omitted in favor of renderUnifiedDiff's per-key "@@ <key> @@" header.
func writeUnifiedHunkLines(buf *strings.Builder, fromLines, toLines []string, contextLines int) {
	matcher := difflib.NewMatcher(fromLines, toLines)
	for _, group := range matcher.GetGroupedOpCodes(contextLines) {
		for _, op := range group {
			switch op.Tag {
			case 'e':
				for _, line := range fromLines[op.I1:op.I2] {
					fmt.Fprintf(buf, " %s\n", line)
				}
			case 'r':
				for _, line := range fromLines[op.I1:op.I2] {
					fmt.Fprintf(buf, "%s\n", ansi.Red("-"+line))
				}
				for _, line := range toLines[op.J1:op.J2] {
					fmt.Fprintf(buf, "%s\n", ansi.Green("+"+line))
				}
			case 'd':
				for _, line := range fromLines[op.I1:op.I2] {
					fmt.Fprintf(buf, "%s\n", ansi.Red("-"+line))
				}
			case 'i':
				for _, line := range toLines[op.J1:op.J2] {
					fmt.Fprintf(buf, "%s\n", ansi.Green("+"+line))
				}
			}
		}
	}
}

// renderSideBySide renders a two-column side-by-side view of fromDoc's and
// toDoc's full YAML text, aligned by difflib's line-level Myers diff
// (matching docs/user-guide/cli/diff.md's example). width is the total
// output width (both columns plus the " │ " separator); columns are
// truncated to fit and padded to stay aligned.
func renderSideBySide(fromLabel string, fromDoc interface{}, toLabel string, toDoc interface{}, width int) (string, error) {
	if width <= 0 {
		width = defaultSideBySideWidth
	}
	const sepWidth = 3 // " │ "
	colWidth := (width - sepWidth) / 2
	if colWidth < 1 {
		colWidth = 1
	}

	fromRaw, err := graft.MarshalYAML(fromDoc)
	if err != nil {
		return "", fmt.Errorf("rendering %s: %w", fromLabel, err)
	}
	toRaw, err := graft.MarshalYAML(toDoc)
	if err != nil {
		return "", fmt.Errorf("rendering %s: %w", toLabel, err)
	}
	fromLines := splitTrimmedLines(string(fromRaw))
	toLines := splitTrimmedLines(string(toRaw))

	var buf strings.Builder
	fmt.Fprintf(&buf, "%s │ %s\n", padTrunc(fromLabel, colWidth), truncate(toLabel, colWidth))
	// Every header/data row is "<colWidth chars> │ <colWidth chars>": the
	// "│" sits at rune column colWidth+1 (colWidth chars, then the one
	// space before "│"). The separator's "┼" must sit at that same
	// column, so each side gets colWidth+1 dashes (one more than the data
	// columns themselves) - not colWidth, which puts "┼" one column short
	// of every "│" below it.
	buf.WriteString(strings.Repeat("─", colWidth+1))
	buf.WriteString("┼")
	buf.WriteString(strings.Repeat("─", colWidth+1))
	buf.WriteString("\n")

	matcher := difflib.NewMatcher(fromLines, toLines)
	for _, op := range matcher.GetOpCodes() {
		switch op.Tag {
		case 'e':
			for i := op.I1; i < op.I2; i++ {
				writeSideBySideRow(&buf, fromLines[i], toLines[op.J1+(i-op.I1)], colWidth, nil)
			}
		case 'r', 'd', 'i':
			left := fromLines[op.I1:op.I2]
			right := toLines[op.J1:op.J2]
			rows := len(left)
			if len(right) > rows {
				rows = len(right)
			}
			colorFn := ansi.Yellow
			if op.Tag == 'd' {
				colorFn = ansi.Red
			} else if op.Tag == 'i' {
				colorFn = ansi.Green
			}
			for i := 0; i < rows; i++ {
				var l, r string
				if i < len(left) {
					l = left[i]
				}
				if i < len(right) {
					r = right[i]
				}
				writeSideBySideRow(&buf, l, r, colWidth, colorFn)
			}
		}
	}

	return buf.String(), nil
}

// writeSideBySideRow writes one row of a side-by-side view: left/right
// truncated or padded to colWidth and separated by " │ ". colorFn, if
// non-nil, colors both non-empty sides (used for added/removed/modified
// rows); nil leaves unchanged rows uncolored.
func writeSideBySideRow(buf *strings.Builder, left, right string, colWidth int, colorFn func(string) string) {
	l := padTrunc(left, colWidth)
	r := truncate(right, colWidth)
	if colorFn != nil {
		if left != "" {
			l = colorFn(l)
		}
		if right != "" {
			r = colorFn(r)
		}
	}
	fmt.Fprintf(buf, "%s │ %s\n", l, r)
}

// padTrunc truncates s to width runes (appending nothing to signal
// truncation, matching a plain terminal column) or right-pads it with
// spaces to width.
func padTrunc(s string, width int) string {
	runes := []rune(s)
	if len(runes) > width {
		return string(runes[:width])
	}
	return s + strings.Repeat(" ", width-len(runes))
}

// truncate shortens s to at most width runes, leaving it unpadded. Used for
// the rightmost column of a side-by-side row/header, where padding would
// only add trailing whitespace with nothing after it to align.
func truncate(s string, width int) string {
	runes := []rune(s)
	if len(runes) > width {
		return string(runes[:width])
	}
	return s
}

// splitTrimmedLines splits s on newlines and drops a single trailing empty
// element produced by a trailing newline (MarshalYAML always ends with
// one), so side-by-side rendering doesn't show a spurious blank final row.
func splitTrimmedLines(s string) []string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
