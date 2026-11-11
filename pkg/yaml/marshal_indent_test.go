package yaml_test

import (
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

// layoutDoc is the value the goccy v1.19.2 probe encoded. Each want below
// is goccy's output for it at the same options, which graft matches,
// since none of its scalars needs graft's own quoting rules.
func layoutDoc() map[string]any {
	return map[string]any{
		"a":    1,
		"list": []any{"x", map[string]any{"k": "v", "m": map[string]any{"n": 1}}},
		"m":    map[string]any{"b": []any{1, 2}, "lit": "l1\nl2\n"},
	}
}

func layoutNested() map[string]any {
	return map[string]any{"s": []any{[]any{1, 2}, map[string]any{"a": []any{3}, "b": "x\ny\n"}}}
}

func TestMarshalWithIndentMatchesGoccyLayout(t *testing.T) {
	t.Parallel()
	const two = "a: 1\nlist:\n- x\n- k: v\n  m:\n    \"n\": 1\nm:\n  b:\n  - 1\n  - 2\n  lit: |\n    l1\n    l2\n"
	cases := []struct {
		name string
		v    any
		opts yaml.EncodeOptions
		want string
	}{
		{"zero means two", layoutDoc(), yaml.EncodeOptions{}, two},
		{"two", layoutDoc(), yaml.EncodeOptions{Indent: 2}, two},
		{"two, sequences indented", layoutDoc(), yaml.EncodeOptions{Indent: 2, IndentSequence: true},
			"a: 1\nlist:\n  - x\n  - k: v\n    m:\n      \"n\": 1\nm:\n  b:\n    - 1\n    - 2\n  lit: |\n    l1\n    l2\n"},
		{"three", layoutDoc(), yaml.EncodeOptions{Indent: 3},
			"a: 1\nlist:\n- x\n- k: v\n  m:\n     \"n\": 1\nm:\n   b:\n   - 1\n   - 2\n   lit: |\n      l1\n      l2\n"},
		{"three, sequences indented", layoutDoc(), yaml.EncodeOptions{Indent: 3, IndentSequence: true},
			"a: 1\nlist:\n   - x\n   - k: v\n     m:\n        \"n\": 1\nm:\n   b:\n      - 1\n      - 2\n   lit: |\n      l1\n      l2\n"},
		{"four", layoutDoc(), yaml.EncodeOptions{Indent: 4},
			"a: 1\nlist:\n- x\n- k: v\n  m:\n      \"n\": 1\nm:\n    b:\n    - 1\n    - 2\n    lit: |\n        l1\n        l2\n"},
		{"four, sequences indented", layoutDoc(), yaml.EncodeOptions{Indent: 4, IndentSequence: true},
			"a: 1\nlist:\n    - x\n    - k: v\n      m:\n          \"n\": 1\nm:\n    b:\n        - 1\n        - 2\n    lit: |\n        l1\n        l2\n"},
		{"nested, four", layoutNested(), yaml.EncodeOptions{Indent: 4},
			"s:\n- - 1\n  - 2\n- a:\n  - 3\n  b: |\n      x\n      y\n"},
		{"nested, four, sequences indented", layoutNested(), yaml.EncodeOptions{Indent: 4, IndentSequence: true},
			"s:\n    - - 1\n      - 2\n    - a:\n          - 3\n      b: |\n          x\n          y\n"},
		{"root sequence, four, sequences indented", []any{1, map[string]any{"a": 1, "b": 2}}, yaml.EncodeOptions{Indent: 4, IndentSequence: true},
			"    - 1\n    - a: 1\n      b: 2\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			got, err := yaml.MarshalWith(c.v, c.opts)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != c.want {
				t.Errorf("got  %q\nwant %q", got, c.want)
			}
		})
	}
}

// TestMarshalWithIndentPlacesGraftScalars covers the scalars graft writes
// itself and substitutes into goccy's output by position.
func TestMarshalWithIndentPlacesGraftScalars(t *testing.T) {
	t.Parallel()
	v := map[string]any{
		"m": map[string]any{
			"long": strings.TrimSpace(strings.Repeat("word ", 40)),
			"q":    "yes",
			"e":    "",
			"lead": "  lead\nx\n",
			"keep": "a\nb\n\n",
			"s":    []any{"on", "x: y", map[string]any{"deep": "l1\nl2"}},
		},
		strings.Repeat("k", 1100): map[string]any{"z": "zv", "y": []any{"a"}},
	}
	for _, opts := range []yaml.EncodeOptions{{Indent: 3}, {Indent: 4}, {Indent: 4, IndentSequence: true}, {Indent: 9, IndentSequence: true}} {
		t.Run(strconv.Itoa(opts.Indent)+"/"+strconv.FormatBool(opts.IndentSequence), func(t *testing.T) {
			t.Parallel()
			out, err := yaml.MarshalWith(v, opts)
			if err != nil {
				t.Fatal(err)
			}
			var back map[string]any
			if err := yaml.Unmarshal(out, &back); err != nil {
				t.Fatalf("%v, reading back:\n%s", err, out)
			}
			if !reflect.DeepEqual(back, v) {
				t.Fatalf("the value changed on the way back:\n%s", out)
			}
			lines := strings.Split(string(out), "\n")
			matched := 0
			for i, l := range lines {
				if strings.HasPrefix(l, strings.Repeat(" ", opts.Indent)+"long: ") {
					matched++
					// A folded line continues at the key's column plus the indent.
					if want := strings.Repeat(" ", 2*opts.Indent) + "word"; !strings.HasPrefix(lines[i+1], want) {
						t.Errorf("continuation %q, want prefix %q", lines[i+1], want)
					}
				}
			}
			if matched == 0 {
				t.Errorf("no line starts with the long key at the indent:\n%s", out)
			}
		})
	}
}

func TestMarshalWithRefusesIndentOutOfRange(t *testing.T) {
	t.Parallel()
	for _, n := range []int{-1, 1, 10, 100} {
		out, err := yaml.MarshalWith(map[string]any{"a": 1}, yaml.EncodeOptions{Indent: n})
		if err == nil || out != nil {
			t.Fatalf("indent %d: got %q, %v, want no output and an error", n, out, err)
		}
		want := "yaml: MarshalWith does not support indent " + strconv.Itoa(n) + "; use 0, which means 2, or 2 through 9"
		if err.Error() != want {
			t.Errorf("indent %d: got %q, want %q", n, err.Error(), want)
		}
	}
}
