package graft

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
)

const diffStringGolden = "testdata/diff_string_golden.txt"

func goldenFixtures() (a, b map[string]interface{}) {
	a = map[string]interface{}{
		"keep":    "same",
		"gone":    map[string]interface{}{"inner": "old", "@R{mk}": 1},
		"scalar":  "before @R{x} text",
		"@G{k}":   "v",
		"zeta":    "z",
		"type":    "text",
		"list":    []interface{}{"a", "@C{b}", "z"},
		"keyed":   []interface{}{map[string]interface{}{"name": "x", "v": 1}, map[string]interface{}{"name": "@R{gone}", "v": 3}},
		"nested":  map[string]interface{}{"deep": map[string]interface{}{"s": "one\ntwo", "n": 1}},
		"number":  1,
		"percent": "100% %s %d",
	}
	b = map[string]interface{}{
		"keep":    "same",
		"added":   map[string]interface{}{"k": "v @R{y}", "@B{ak}": []interface{}{1, 2}},
		"scalar":  "after @G{x} text",
		"@G{k}":   "w",
		"zeta":    "zz",
		"type":    []interface{}{"text"},
		"list":    []interface{}{"a", "c", "d", "e"},
		"keyed":   []interface{}{map[string]interface{}{"name": "x", "v": 2}, map[string]interface{}{"name": "y"}, map[string]interface{}{"name": "@B{new}"}},
		"nested":  map[string]interface{}{"deep": map[string]interface{}{"s": "one\nthree", "n": 2}},
		"number":  2.5,
		"percent": "100% %s %d!",
	}
	return a, b
}

// goldenDump renders every Diffable type with String, for the given toggle
// state, building the diff while that state is in effect.
func goldenDump(t *testing.T, on bool) string {
	t.Helper()
	prev := ansi.IsColorEnabled()
	ansi.Color(on)
	defer ansi.Color(prev)

	a, b := goldenFixtures()
	d, err := Diff(a, b)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	var sb strings.Builder
	emit := func(label, s string) { fmt.Fprintf(&sb, "== %s\n%q\n", label, s) }
	for _, key := range []string{"$", "@R{root}", ""} {
		emit("full "+key, d.String(key))
	}
	emit("none", DiffNone{Orig: map[string]interface{}{"a": "@R{x}"}}.String("$"))
	emit("type", DiffType{Old: "s", New: []interface{}{1}}.String("$.t"))
	emit("scalar", DiffScalar{Old: "a\nb", New: "c"}.String("$.s"))
	sd, _ := Diff("old @R{x}", "new")
	emit("scalar diff", sd.String("$"))
	ld, _ := Diff([]interface{}{"a", "@R{b}"}, []interface{}{"a", "c"})
	emit("list diff", ld.String("@C{l}"))
	return sb.String()
}

func TestDiffStringGolden(t *testing.T) {
	got := "# toggle on\n" + goldenDump(t, true) + "# toggle off\n" + goldenDump(t, false)
	if os.Getenv("UPDATE_DIFF_GOLDEN") != "" {
		if err := os.WriteFile(diffStringGolden, []byte(got), 0o600); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(diffStringGolden)
	if err != nil {
		t.Fatal(err)
	}
	if string(want) != got {
		t.Errorf("String output changed from the recorded golden output\n got: %s\nwant: %s", got, want)
	}
}
