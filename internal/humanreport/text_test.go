package humanreport

import (
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/yamlgolden"
)

func TestPluralMatchesText(t *testing.T) {
	var vectors []struct {
		N     int      `json:"n"`
		Words []string `json:"words"`
		Out   string   `json:"out"`
	}
	yamlgolden.ReadJSON(t, "testdata/golden/plural.json", &vectors)
	for _, v := range vectors {
		if got := plural(v.N, v.Words...); got != v.Out {
			t.Errorf("plural(%d, %v) = %q, want %q", v.N, v.Words, got, v.Out)
		}
	}
}

func TestTableMatchesDyff(t *testing.T) {
	var vectors []struct {
		Separator string   `json:"separator"`
		Indent    int      `json:"indent"`
		Columns   []string `json:"columns"`
		Out       string   `json:"out"`
	}
	yamlgolden.ReadJSON(t, "testdata/golden/table.json", &vectors)
	for _, v := range vectors {
		if got := createTableStyleString(v.Separator, v.Indent, v.Columns...); got != v.Out {
			t.Errorf("createTableStyleString(%q, %d, %q) = %q, want %q", v.Separator, v.Indent, v.Columns, got, v.Out)
		}
	}
}

func TestIsMinorChangeMatchesLevenshtein(t *testing.T) {
	var vectors []struct {
		A        string `json:"a"`
		B        string `json:"b"`
		Distance int    `json:"distance"`
	}
	yamlgolden.ReadJSON(t, "testdata/golden/minor.json", &vectors)
	for _, v := range vectors {
		if got := isMinorChange(v.A, v.B); got != (v.Distance <= 4) {
			t.Errorf("isMinorChange(%q, %q) = %v, but the distance is %d", v.A, v.B, got, v.Distance)
		}
	}
	long := strings.Repeat("ab", 100000)
	if !isMinorChange(long, long+"x") || isMinorChange(long, "xyz"+long+"ab") {
		t.Fatal("long single-line values must be measured exactly and quickly")
	}
}

func TestWriteTextBlocksTrimsOnlyInTableMode(t *testing.T) {
	render := func(width int) string {
		var b strings.Builder
		writeTextBlocks(&b, 0, width, "empty: \n", "k: v\n")
		return b.String()
	}
	if got := render(80); got != "empty:    k: v\n" {
		t.Errorf("table layout = %q", got)
	}
	if got := render(14); got != "empty:    k: v\n" {
		t.Errorf("a total equal to the width must still be a table, got %q", got)
	}
	if got := render(13); got != "empty: \n\nk: v\n\n" {
		t.Errorf("stacked layout = %q", got)
	}
	if got := render(0); got != "empty: \n\nk: v\n\n" {
		t.Errorf("width 0 must stack, got %q", got)
	}
}

func TestPrefixHelpers(t *testing.T) {
	if got := createStringWithPrefix("\x1b[31m- \x1b[0m", "a\nb", 0); got != "\x1b[31m- \x1b[0ma\n  b\n" {
		t.Errorf("createStringWithPrefix = %q", got)
	}
	if got := createStringWithContinuousPrefix("+ ", "a\nb\n", 0); got != "+ a\n+ b\n" {
		t.Errorf("createStringWithContinuousPrefix = %q", got)
	}
}

func TestStringArrayLenCountsPlainText(t *testing.T) {
	if got := stringArrayLen([]string{"ab", "\x1b[31mcd\x1b[0m", "日本"}); got != 6 {
		t.Errorf("stringArrayLen = %d, want 6", got)
	}
}
