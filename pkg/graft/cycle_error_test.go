package graft

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
	"github.com/fivetwenty-io/graft/pkg/graft/interfaces"
)

func TestSanitizeDisplayEscapesNewlines(t *testing.T) {
	// The payload proved in the spec: a value or key carrying a literal
	// newline followed by text matching genesis's stderr scrape regex.
	got := sanitizeDisplay("(( concat \"a\n - $.evil: boom\" meta.b ))")

	if strings.Contains(got, "\n") {
		t.Errorf("sanitizeDisplay() = %q; a literal newline must not survive", got)
	}
	if !strings.Contains(got, `\n`) {
		t.Errorf("sanitizeDisplay() = %q; the newline must render as the two-character sequence \\n", got)
	}
}

func TestSanitizeDisplayStripsEscapeBytes(t *testing.T) {
	got := sanitizeDisplay("(( grab \033[31mred\033[0m ))")

	if strings.Contains(got, "\033") {
		t.Errorf("sanitizeDisplay() = %q; no escape byte may survive", got)
	}
	if !strings.Contains(got, "red") {
		t.Errorf("sanitizeDisplay() = %q; the readable text must survive", got)
	}
}

func TestSanitizeDisplayPreservesColorDirectivesVerbatim(t *testing.T) {
	// An "@r{...}" sequence is not an escape; it is document text. It
	// must reach output exactly as written, which is what the MultiError
	// change makes possible.
	const in = "(( grab @r{secret} ))"

	if got := sanitizeDisplay(in); got != in {
		t.Errorf("sanitizeDisplay(%q) = %q, want it unchanged", in, got)
	}
}

func TestSanitizeDisplayEscapesCarriageReturnAndControls(t *testing.T) {
	got := sanitizeDisplay("a\rb\x01c")

	if got != `a\rb\x01c` {
		t.Errorf("sanitizeDisplay() = %q, want %q", got, `a\rb\x01c`)
	}
}

func TestSanitizeDisplayTruncatesLongStrings(t *testing.T) {
	in := strings.Repeat("x", cycleExprMaxRunes+50)

	got := sanitizeDisplay(in)

	if len([]rune(got)) != cycleExprMaxRunes+3 {
		t.Errorf("sanitizeDisplay() produced %d runes, want %d", len([]rune(got)), cycleExprMaxRunes+3)
	}
	if !strings.HasSuffix(got, "...") {
		t.Errorf("sanitizeDisplay() = %q, want a trailing ellipsis", got)
	}
}

func TestSanitizeDisplayHandlesHostilePath(t *testing.T) {
	// The key.yml payload proved in the spec: a mapping key carrying a
	// newline and a forged genesis error line, reaching output through
	// op.canonical.String().
	got := sanitizeDisplay("meta.e\n - $.evil: boom")

	if strings.Contains(got, "\n") {
		t.Errorf("sanitizeDisplay() = %q; a hostile path must not break the line", got)
	}
}

func TestSanitizeDisplayEscapesLineBreakingUnicode(t *testing.T) {
	// NEL, LINE SEPARATOR, and PARAGRAPH SEPARATOR break a line in
	// terminals and editors just as \n and \r do. The spec states the
	// sanitized result "always occupies exactly one output line"; leaving
	// these three unescaped would violate that invariant.
	in := "a\u0085b\u2028c\u2029d"

	got := sanitizeDisplay(in)

	for _, r := range []rune{'\u0085', '\u2028', '\u2029'} {
		if strings.ContainsRune(got, r) {
			t.Errorf("sanitizeDisplay() = %q; must not contain literal %U", got, r)
		}
	}
	want := `a\u0085b\u2028c\u2029d`
	if got != want {
		t.Errorf("sanitizeDisplay() = %q, want %q", got, want)
	}
}

func TestSanitizeDisplayAtExactLimitIsNotTruncated(t *testing.T) {
	in := strings.Repeat("x", cycleExprMaxRunes)

	got := sanitizeDisplay(in)

	if got != in {
		t.Errorf("sanitizeDisplay() = %q, want unchanged %q", got, in)
	}
	if strings.HasSuffix(got, "...") {
		t.Errorf("sanitizeDisplay() = %q; must not truncate exactly at the limit", got)
	}
}

func TestSanitizeDisplayOneUnderLimitIsNotTruncated(t *testing.T) {
	in := strings.Repeat("x", cycleExprMaxRunes-1)

	got := sanitizeDisplay(in)

	if got != in {
		t.Errorf("sanitizeDisplay() = %q, want unchanged %q", got, in)
	}
}

func TestSanitizeDisplayOneOverLimitIsTruncated(t *testing.T) {
	in := strings.Repeat("x", cycleExprMaxRunes+1)

	got := sanitizeDisplay(in)

	want := strings.Repeat("x", cycleExprMaxRunes) + "..."
	if got != want {
		t.Errorf("sanitizeDisplay() = %q, want %q", got, want)
	}
}

func TestSanitizeDisplayTruncationNeverSplitsReplacement(t *testing.T) {
	// 119 plain runes leave exactly 1 rune of budget, but the next
	// character maps to a 2-rune replacement ("\t"). The replacement must
	// not be split across the boundary (a dangling "\" with its "t"
	// sliced off, for example) - it is either written whole or dropped
	// whole, with truncation deciding before any partial write.
	in := strings.Repeat("x", cycleExprMaxRunes-1) + "\t" + "more text that will not fit"

	got := sanitizeDisplay(in)

	want := strings.Repeat("x", cycleExprMaxRunes-1) + "..."
	if got != want {
		t.Errorf("sanitizeDisplay() = %q, want %q", got, want)
	}
}

func TestSanitizeDisplayEscSequenceAdjacentToAtSign(t *testing.T) {
	// Accepted behavior, not a defect. ansi.StripEscapes deletes only the
	// lone ESC byte from an unrecognized two-byte escape (ESC followed by
	// a lowercase letter is not a recognized introducer), so
	// "@" + ESC + "r{whoami}" sanitizes to the literal text "@r{whoami}"
	// - a directive the raw bytes never contained as one complete
	// sequence. This is accepted rather than prevented: on a non-tty,
	// ansi.Color is already false (resolved once in the root command's
	// PersistentPreRunE before any subcommand runs), so ansi.Errorf's
	// processColorCodes strips "@r{...}" instead of emitting escape
	// bytes - the no-escape-bytes-on-a-non-tty contract holds regardless
	// of this composition. An attacker gains nothing from it that writing
	// "@r{" literally does not already grant, and the spec
	// (plans/cycle-detection-provenance.md, "Closing the color-directive
	// hole") accepts in writing that any "@X{...}" sequence in an error
	// message is stripped when color is off and colorized when it is on.
	got := sanitizeDisplay("@\033r{whoami}")

	want := "@r{whoami}"
	if got != want {
		t.Errorf("sanitizeDisplay() = %q, want %q", got, want)
	}
}

func TestCycleErrorRendersTwoNodeBlock(t *testing.T) {
	err := &CycleError{
		Inputs: []string{"base.yml", "a.yml", "b.yml"},
		Nodes: []CycleNode{
			{Path: "meta.bar", Expr: "(( grab meta.foo ))", Pos: interfaces.Position{File: "b.yml", Line: 2}},
			{Path: "meta.foo", Expr: "(( grab meta.bar ))", Pos: interfaces.Position{File: "a.yml", Line: 3}},
		},
	}

	want := strings.Join([]string{
		"cycle detected in operator data-flow graph",
		"   inputs:",
		"     [1] base.yml",
		"     [2] a.yml",
		"     [3] b.yml",
		"   cycle (2 nodes): meta.bar -> meta.foo -> meta.bar",
		"     b.yml:2  meta.bar: (( grab meta.foo ))",
		"     a.yml:3  meta.foo: (( grab meta.bar ))",
	}, "\n")

	if got := err.Error(); got != want {
		t.Errorf("Error() =\n%s\n\nwant\n%s", got, want)
	}
}

func TestCycleErrorThreeNodeWrapsToClosingEdge(t *testing.T) {
	err := &CycleError{
		Nodes: []CycleNode{
			{Path: "meta.a", Expr: "(( grab meta.b ))", Pos: interfaces.Position{File: "a.yml", Line: 3}},
			{Path: "meta.b", Expr: "(( grab meta.c ))", Pos: interfaces.Position{File: "b.yml", Line: 2}},
			{Path: "meta.c", Expr: "(( grab meta.a ))", Pos: interfaces.Position{File: "c.yml", Line: 5}},
		},
	}

	lines := strings.Split(err.Error(), "\n")

	if lines[1] != "   cycle (3 nodes): meta.a -> meta.b -> meta.c -> meta.a" {
		t.Errorf("chain line = %q", lines[1])
	}
	// The last two lines name the closing edge: meta.c references
	// meta.a, and meta.a is repeated so both ends are visible.
	last := lines[len(lines)-2:]
	if last[0] != "     c.yml:5  meta.c: (( grab meta.a ))" {
		t.Errorf("second-to-last line = %q", last[0])
	}
	if last[1] != "     a.yml:3  meta.a: (( grab meta.b ))" {
		t.Errorf("last line = %q", last[1])
	}
}

func TestCycleErrorSelfCycleHasNoWrapDuplicate(t *testing.T) {
	err := &CycleError{
		Nodes: []CycleNode{
			{Path: "meta.a", Expr: "(( grab meta.a ))", Pos: interfaces.Position{File: "a.yml", Line: 1}},
		},
	}

	lines := strings.Split(err.Error(), "\n")

	if len(lines) != 3 {
		t.Fatalf("got %d lines, want 3 (header, chain, one detail):\n%s", len(lines), err.Error())
	}
	if lines[1] != "   cycle (1 node): meta.a -> meta.a" {
		t.Errorf("chain line = %q", lines[1])
	}
}

func TestCycleErrorOmitsEmptyInputsBlock(t *testing.T) {
	err := &CycleError{
		Nodes: []CycleNode{
			{Path: "meta.a", Expr: "(( grab meta.a ))"},
		},
	}

	if strings.Contains(err.Error(), "inputs:") {
		t.Errorf("Error() = %q; an empty Inputs must omit the block entirely", err.Error())
	}
}

func TestCycleErrorDegradesWithoutInventingALine(t *testing.T) {
	err := &CycleError{
		Nodes: []CycleNode{
			{Path: "meta.a", Expr: "(( grab meta.b ))", Pos: interfaces.Position{File: "only.yml"}},
			{Path: "meta.b", Expr: "(( grab meta.a ))"},
		},
	}

	got := err.Error()

	if !strings.Contains(got, "     only.yml  meta.a: (( grab meta.b ))") {
		t.Errorf("Error() = %q; a file without a line must render as the bare filename", got)
	}
	if !strings.Contains(got, "     <unknown>  meta.b: (( grab meta.a ))") {
		t.Errorf("Error() = %q; an unattributed node must render as <unknown>", got)
	}
	if strings.Contains(got, ":0") {
		t.Errorf("Error() = %q; a zero line number must never be printed", got)
	}
}

func TestCycleErrorClassifiesAsCircularReference(t *testing.T) {
	err := &CycleError{Nodes: []CycleNode{{Path: "meta.a", Expr: "(( grab meta.a ))"}}}

	if !strings.HasPrefix(err.Error(), "cycle detected") {
		t.Fatalf("Error() must start with %q for error-code classification; got %q",
			"cycle detected", err.Error())
	}
	if !errors.Is(err, ErrDependencyCycle) {
		t.Errorf("errors.Is(err, ErrDependencyCycle) = false, want true")
	}
}

func TestCycleErrorDetailLinesNeverStartWithGenesisPrefix(t *testing.T) {
	// The hostile shapes proved in the spec, in a path and in an
	// expression at once.
	err := &CycleError{
		Inputs: []string{"a\n - $.evil: boom.yml"},
		Nodes: []CycleNode{
			{Path: "meta.e\n - $.evil: boom", Expr: "(( concat \"x\n - $.evil: boom\" meta.b ))"},
			{Path: "meta.b", Expr: "(( grab meta.e ))"},
		},
	}

	// Render the way the CLI does: inside a MultiError, with color
	// disabled the way main.go resolves it whenever stderr isn't a tty -
	// the state genesis always observes, since it captures graft's
	// stderr to a file. MultiError.Error() colorizes only its leading
	// count (see errors.go), so this is the only way to observe the
	// no-escape-bytes contract this test checks without also asserting
	// on ansi's own color-toggle behavior.
	previousColor := ansi.IsColorEnabled()
	ansi.Color(false)
	defer ansi.Color(previousColor)

	out := MultiError{Errors: []error{err}}.Error()

	var prefixed int
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, " - ") {
			prefixed++
		}
	}
	if prefixed != 1 {
		t.Errorf("got %d lines starting with %q, want exactly 1:\n%s", prefixed, " - ", out)
	}
	if strings.Contains(out, "\033") {
		t.Errorf("output contains an escape byte:\n%q", out)
	}
}

func TestSanitizeFilenameKeepsShortPathsUnchanged(t *testing.T) {
	const in = "/tmp/deploy/manifest.yml"

	if got := sanitizeFilename(in); got != in {
		t.Errorf("sanitizeFilename(%q) = %q, want it unchanged", in, got)
	}
}

func TestSanitizeFilenameTruncatesFromTheLeft(t *testing.T) {
	const tail = "/59c9d5dd/scratchpad/self.yml"
	in := "/private/tmp/" + strings.Repeat("deep-workspace-root/", 12) + tail[1:]

	got := sanitizeFilename(in)

	if !strings.HasPrefix(got, "...") {
		t.Errorf("sanitizeFilename() = %q; a shortened path must be marked with a leading ...", got)
	}
	if !strings.HasSuffix(got, "self.yml") {
		t.Errorf("sanitizeFilename() = %q; the distinguishing tail must survive", got)
	}
	if n := utf8.RuneCountInString(got); n > cycleExprMaxRunes {
		t.Errorf("sanitizeFilename() is %d runes, want at most %d", n, cycleExprMaxRunes)
	}
}

func TestSanitizeFilenameDistinguishesSiblingsUnderALongRoot(t *testing.T) {
	root := "/private/tmp/" + strings.Repeat("deep-workspace-root/", 12)

	a := sanitizeFilename(root + "alpha.yml")
	b := sanitizeFilename(root + "beta.yml")

	if a == b {
		t.Errorf("two inputs under a long root both render as %q; the numbered inputs list must distinguish them", a)
	}
}

func TestSanitizeFilenameKeepsTheEscapeGuarantees(t *testing.T) {
	// Same contract as sanitizeDisplay: no escape byte reaches stderr and
	// the result occupies exactly one line, whether or not it is shortened.
	hostile := "/tmp/\033[31m" + strings.Repeat("padding-segment/", 12) + "a\nb\r\tc.yml"

	got := sanitizeFilename(hostile)

	if strings.Contains(got, "\033") {
		t.Errorf("sanitizeFilename() = %q; no escape byte may survive", got)
	}
	for _, br := range []string{"\n", "\r", "\t"} {
		if strings.Contains(got, br) {
			t.Errorf("sanitizeFilename() = %q; a raw %q must not survive", got, br)
		}
	}
	if n := utf8.RuneCountInString(got); n > cycleExprMaxRunes {
		t.Errorf("sanitizeFilename() is %d runes, want at most %d", n, cycleExprMaxRunes)
	}
}

func TestSanitizeFilenameTruncationNeverSplitsReplacement(t *testing.T) {
	// U+2028 renders as the six characters \u2028. The tail is kept one
	// whole replacement at a time, so the cut can never land inside one.
	in := strings.Repeat("x", 200) + strings.Repeat("\u2028", 4) + "end.yml"

	got := sanitizeFilename(in)

	if strings.Contains(got, "\u2028") {
		t.Errorf("sanitizeFilename() = %q; a raw line separator must not survive", got)
	}
	rest := strings.TrimPrefix(got, "...")
	for _, frag := range []string{`u2028`, `2028`, `028`, `28`, `8`} {
		if strings.HasPrefix(rest, frag) {
			t.Errorf("sanitizeFilename() = %q; the cut landed inside a \\u2028 replacement", got)
		}
	}
}

func TestCycleErrorShortensLongFilenamesFromTheLeft(t *testing.T) {
	long := "/private/tmp/" + strings.Repeat("deep-workspace-root/", 12) + "self.yml"
	e := &CycleError{
		Inputs: []string{long},
		Nodes: []CycleNode{{
			Path: "meta.self",
			Expr: "(( grab meta.self ))",
			Pos:  interfaces.Position{File: long, Line: 2},
		}},
	}

	out := e.Error()

	if !strings.Contains(out, "[1] ...") || !strings.Contains(out, "root/self.yml\n") {
		t.Errorf("the inputs list does not keep the path's tail:\n%s", out)
	}
	if !strings.Contains(out, "self.yml:2  meta.self:") {
		t.Errorf("the detail line does not keep the path's tail:\n%s", out)
	}
}

func TestSanitizeFilenameCutsBetweenWholeReplacements(t *testing.T) {
	// The budget boundary falls in the MIDDLE of a run of six-rune
	// U+2028 replacements, which is where a scan that stops as soon as
	// the budget is spent is most likely to differ from one that expands
	// the whole input first. The budget is 117 runes once the leading
	// marker is paid for, so 19 whole replacements fit (114 runes) and a
	// 20th does not.
	in := strings.Repeat("a", 100) + strings.Repeat("\u2028", 20)

	got := sanitizeFilename(in)

	want := "..." + strings.Repeat(`\u2028`, 19)
	if got != want {
		t.Errorf("sanitizeFilename() = %q, want %q", got, want)
	}
	if n := utf8.RuneCountInString(got); n != cycleExprMaxRunes-3 {
		t.Errorf("sanitizeFilename() is %d runes, want %d", n, cycleExprMaxRunes-3)
	}
}
