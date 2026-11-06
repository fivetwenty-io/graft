package operators_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// envDecodeRow holds the output spruce 1.35.17 gives for one value of
// $P5_ENV under grab, concat, and grab with a fallback. Each output was
// taken from the real spruce binary.
type envDecodeRow struct {
	value, grab, concat, or string
}

// envDecodeSpruce is the table of values from the env value research,
// each with spruce's output for the three shapes.
var envDecodeSpruce = []envDecodeRow{
	{"yes", "v: true", "v: x-true", "v: true"},
	{"Yes", "v: true", "v: x-true", "v: true"},
	{"YES", "v: true", "v: x-true", "v: true"},
	{"no", "v: false", "v: x-false", "v: false"},
	{"No", "v: false", "v: x-false", "v: false"},
	{"NO", "v: false", "v: x-false", "v: false"},
	{"on", "v: true", "v: x-true", "v: true"},
	{"On", "v: true", "v: x-true", "v: true"},
	{"ON", "v: true", "v: x-true", "v: true"},
	{"off", "v: false", "v: x-false", "v: false"},
	{"Off", "v: false", "v: x-false", "v: false"},
	{"OFF", "v: false", "v: x-false", "v: false"},
	{"True", "v: true", "v: x-true", "v: true"},
	{"TRUE", "v: true", "v: x-true", "v: true"},
	{"False", "v: false", "v: x-false", "v: false"},
	{"FALSE", "v: false", "v: x-false", "v: false"},
	{"y", "v: true", "v: x-true", "v: true"},
	{"Y", "v: true", "v: x-true", "v: true"},
	{"n", "v: false", "v: x-false", "v: false"},
	{"N", "v: false", "v: x-false", "v: false"},
	{"~", "v: null", "v: x-<nil>", "v: null"},
	{"Null", "v: null", "v: x-<nil>", "v: null"},
	{"NULL", "v: null", "v: x-<nil>", "v: null"},
	{"# c", "v: null", "v: x-<nil>", "v: null"},
	{"   ", "v: null", "v: x-<nil>", "v: null"},
	{"12", "v: 12", "v: x-12", "v: 12"},
	{"0", "v: 0", "v: x-0", "v: 0"},
	{"00", "v: 0", "v: x-0", "v: 0"},
	{"07", "v: 7", "v: x-7", "v: 7"},
	{"+1", "v: 1", "v: x-1", "v: 1"},
	{"-1", "v: -1", "v: x--1", "v: -1"},
	{"0x10", "v: 16", "v: x-16", "v: 16"},
	{"+0x10", "v: 16", "v: x-16", "v: 16"},
	{"010", "v: 8", "v: x-8", "v: 8"},
	{"0o7", "v: 7", "v: x-7", "v: 7"},
	{"0b11", "v: 3", "v: x-3", "v: 3"},
	{"1_000", "v: 1000", "v: x-1000", "v: 1000"},
	{"1__0", "v: 10", "v: x-10", "v: 10"},
	{"1_", "v: 1", "v: x-1", "v: 1"},
	{"0x_1", "v: 1", "v: x-1", "v: 1"},
	{"8080", "v: 8080", "v: x-8080", "v: 8080"},
	{"01234", "v: 668", "v: x-668", "v: 668"},
	{"0X10", "v: 16", "v: x-16", "v: 16"},
	{"08", "v: 8", "v: x-8", "v: 8"},
	{"9223372036854775808", "v: 9223372036854775808", "v: x-9223372036854775808", "v: 9223372036854775808"},
	{"18446744073709551616", "v: 1.8446744e+19", "v: x-1.8446744073709552e+19", "v: 1.8446744e+19"},
	{"-9223372036854775809", "v: -9.223372e+18", "v: x--9.223372036854776e+18", "v: -9.223372e+18"},
	{"0.1", "v: 0.1", "v: x-0.1", "v: 0.1"},
	{"3.14", "v: 3.14", "v: x-3.14", "v: 3.14"},
	{".5", "v: 0.5", "v: x-0.5", "v: 0.5"},
	{"+.5", "v: 0.5", "v: x-0.5", "v: 0.5"},
	{".1e3", "v: 100", "v: x-100", "v: 100"},
	{"1e3", "v: 1000", "v: x-1000", "v: 1000"},
	{"1E3", "v: 1000", "v: x-1000", "v: 1000"},
	{"1e+3", "v: 1000", "v: x-1000", "v: 1000"},
	{"1e-3", "v: 0.001", "v: x-0.001", "v: 0.001"},
	{"1.0", "v: 1", "v: x-1", "v: 1"},
	{"1.", "v: 1", "v: x-1", "v: 1"},
	{"100.0", "v: 100", "v: x-100", "v: 100"},
	{".inf", "v: .inf", "v: x-+Inf", "v: .inf"},
	{".nan", "v: .nan", "v: x-NaN", "v: .nan"},
	{".NaN", "v: .nan", "v: x-NaN", "v: .nan"},
	{"a: b", "v:\n  a: b", "v: x-map[a:b]", "v:\n  a: b"},
	{"x: yes", "v:\n  x: true", "v: x-map[x:true]", "v:\n  x: true"},
	{"{a: yes}", "v:\n  a: true", "v: x-map[a:true]", "v:\n  a: true"},
	{"[yes, no]", "v:\n- true\n- false", "v: x-[true false]", "v:\n- true\n- false"},
	{"[a, b]", "v:\n- a\n- b", "v: x-[a b]", "v:\n- a\n- b"},
	{"- a", "v:\n- a", "v: x-[a]", "v:\n- a"},
	{"- yes", "v:\n- true", "v: x-[true]", "v:\n- true"},
	{"- ", "v:\n- null", "v: x-[<nil>]", "v:\n- null"},
	{"? a", "v:\n  a: null", "v: x-map[a:<nil>]", "v:\n  a: null"},
	{"!!int 5", "v: 5", "v: x-5", "v: 5"},
	{"{a: [1, {b: yes}]}", "v:\n  a:\n  - 1\n  - b: true", "v: x-map[a:[1 map[b:true]]]", "v:\n  a:\n  - 1\n  - b: true"},
	{"a: &x [1, 2]\nb: *x", "v:\n  a:\n  - 1\n  - 2\n  b:\n  - 1\n  - 2", "v: x-map[a:[1 2] b:[1 2]]", "v:\n  a:\n  - 1\n  - 2\n  b:\n  - 1\n  - 2"},
	{"yes ", "v: true", "v: x-true", "v: true"},
	{"  yes", "v: true", "v: x-true", "v: true"},
	{"hello", "v: hello", "v: x-hello", "v: hello"},
	{"hello world", "v: hello world", "v: x-hello world", "v: hello world"},
	{"\"q\"", "v: '\"q\"'", "v: x-\"q\"", "v: '\"q\"'"},
	{"'yes'", "v: '''yes'''", "v: x-'yes'", "v: '''yes'''"},
	{"!!str yes", "v: '!!str yes'", "v: x-!!str yes", "v: '!!str yes'"},
	{"Yes!", "v: Yes!", "v: x-Yes!", "v: Yes!"},
	{"[", "v: '['", "v: x-[", "v: '['"},
	{"[a", "v: '[a'", "v: x-[a", "v: '[a'"},
	{"\t", "v: \"\\t\"", "v: \"x-\\t\"", "v: \"\\t\""},
	{"1\n---\n2", "v: 1", "v: x-1", "v: 1"},
	{"1\n---\n[", "v: 1", "v: x-1", "v: 1"},
	{"!!float 3", "v: '!!float 3'", "v: x-!!float 3", "v: '!!float 3'"},
	{"x: 1\nx: 2", "v:\n  x: 2", "v: x-map[x:2]", "v:\n  x: 2"},
}

// envDecodeGap is graft's output for a row where graft knowingly
// differs from spruce, with the known-gaps entry that records why.
type envDecodeGap struct {
	gap, grab, concat, or string
}

// envDecodeGaps lists the rows that differ from spruce on purpose or as
// a recorded open gap. Every other row must match spruce exactly.
var envDecodeGaps = map[string]envDecodeGap{
	"!!float 3":            {"float-tag-over-integer-text in genesis-compat-contract.md", "v: 3.0", "v: x-3", "v: 3.0"},
	"y":                    {"y-n-boolean-values-not-coerced", "v: \"y\"", "v: x-y", "v: \"y\""},
	"Y":                    {"y-n-boolean-values-not-coerced", "v: \"Y\"", "v: x-Y", "v: \"Y\""},
	"n":                    {"y-n-boolean-values-not-coerced", "v: \"n\"", "v: x-n", "v: \"n\""},
	"N":                    {"y-n-boolean-values-not-coerced", "v: \"N\"", "v: x-N", "v: \"N\""},
	"0X10":                 {"numbers-libyaml-types-and-graft-reads-as-strings", "v: \"0X10\"", "v: x-0X10", "v: \"0X10\""},
	"08":                   {"numbers-libyaml-types-and-graft-reads-as-strings", "v: \"08\"", "v: x-08", "v: \"08\""},
	"1e3":                  {"numbers-libyaml-types-and-graft-reads-as-strings", "v: \"1e3\"", "v: x-1e3", "v: \"1e3\""},
	"1E3":                  {"numbers-libyaml-types-and-graft-reads-as-strings", "v: \"1E3\"", "v: x-1E3", "v: \"1E3\""},
	"1e+3":                 {"numbers-libyaml-types-and-graft-reads-as-strings", "v: \"1e+3\"", "v: x-1e+3", "v: \"1e+3\""},
	"1e-3":                 {"numbers-libyaml-types-and-graft-reads-as-strings", "v: \"1e-3\"", "v: x-1e-3", "v: \"1e-3\""},
	"18446744073709551616": {"numbers-libyaml-types-and-graft-reads-as-strings", "v: \"18446744073709551616\"", "v: x-18446744073709551616", "v: \"18446744073709551616\""},
	"-9223372036854775809": {"numbers-libyaml-types-and-graft-reads-as-strings", "v: \"-9223372036854775809\"", "v: x--9223372036854775809", "v: \"-9223372036854775809\""},
	".1e3":                 {"whole-floats-keep-their-decimal-point", "v: 100.0", "v: x-100", "v: 100.0"},
	"1.0":                  {"whole-floats-keep-their-decimal-point", "v: 1.0", "v: x-1", "v: 1.0"},
	"1.":                   {"whole-floats-keep-their-decimal-point", "v: 1.0", "v: x-1", "v: 1.0"},
	"100.0":                {"whole-floats-keep-their-decimal-point", "v: 100.0", "v: x-100", "v: 100.0"},
	"x: 1\nx: 2":           {"duplicate-mapping-keys-are-an-error", "v: |-\n  x: 1\n  x: 2", "v: |-\n  x-x: 1\n  x: 2", "v: |-\n  x: 1\n  x: 2"},
}

func mergeEnvShape(t *testing.T, src string) string {
	t.Helper()
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	doc, err := engine.ParseYAML([]byte(src))
	if err != nil {
		t.Fatalf("ParseYAML: %v", err)
	}
	out, err := engine.Evaluate(context.Background(), doc)
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	b, err := out.ToYAML()
	if err != nil {
		t.Fatalf("ToYAML: %v", err)
	}
	return strings.TrimSpace(string(b))
}

// TestEnvValueDecodesLikeSpruce runs every row under grab, concat, and
// grab with a fallback, and compares graft's output with spruce's.
func TestEnvValueDecodesLikeSpruce(t *testing.T) {
	shapes := []struct {
		name, src string
		want      func(envDecodeRow) string
		gap       func(envDecodeGap) string
	}{
		{"grab", "v: (( grab $P5_ENV ))\n",
			func(r envDecodeRow) string { return r.grab }, func(g envDecodeGap) string { return g.grab }},
		{"concat", "v: (( concat \"x-\" $P5_ENV ))\n",
			func(r envDecodeRow) string { return r.concat }, func(g envDecodeGap) string { return g.concat }},
		{"or", "v: (( grab $P5_ENV || \"dflt\" ))\n",
			func(r envDecodeRow) string { return r.or }, func(g envDecodeGap) string { return g.or }},
	}
	for _, row := range envDecodeSpruce {
		for _, sh := range shapes {
			t.Run(sh.name+"/"+row.value, func(t *testing.T) {
				t.Setenv("P5_ENV", row.value)
				want := sh.want(row)
				if g, ok := envDecodeGaps[row.value]; ok {
					want = sh.gap(g)
				}
				if got := mergeEnvShape(t, sh.src); got != want {
					t.Errorf("FOO=%q under %s:\n got: %q\nwant: %q", row.value, sh.name, got, want)
				}
			})
		}
	}
}

// TestEnvValueGapsAreRecorded checks that every gap row is a row of the
// table, that it really differs from spruce, and that each gap it names
// has its entry in docs/spruce/known-gaps.md.
func TestEnvValueGapsAreRecorded(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "..", "docs", "spruce", "known-gaps.md"))
	if err != nil {
		t.Fatalf("reading known-gaps.md: %v", err)
	}
	rows := make(map[string]envDecodeRow, len(envDecodeSpruce))
	for _, r := range envDecodeSpruce {
		rows[r.value] = r
	}
	for value, g := range envDecodeGaps {
		r, ok := rows[value]
		if !ok {
			t.Errorf("gap row %q is not in the table", value)
			continue
		}
		if r.grab == g.grab && r.concat == g.concat && r.or == g.or {
			t.Errorf("gap row %q matches spruce, so it is no longer a gap", value)
		}
		if strings.HasSuffix(g.gap, ".md") {
			continue
		}
		if !strings.Contains(string(doc), "\n### "+g.gap+"\n") {
			t.Errorf("gap %q for row %q has no entry in known-gaps.md", g.gap, value)
		}
	}
}
