package yaml_test

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

// scalarsDoc holds every scalar form the generic route converts. The
// expected values below were recorded from graft's merge parse path
// (DefaultEngine.ParseYAML) on this document, with YAML 1.1 booleans on
// and off, so a change here means Unmarshal no longer reads a document
// the way a merge does.
const scalarsDoc = `p_yes: yes
p_Yes: Yes
p_YES: YES
p_no: no
p_No: No
p_NO: NO
p_on: on
p_On: On
p_ON: ON
p_off: off
p_Off: Off
p_OFF: OFF
q_yes: "yes"
q_Yes: 'Yes'
q_YES: "YES"
q_no: 'no'
q_No: "No"
q_NO: 'NO'
q_on: "on"
q_On: 'On'
q_ON: "ON"
q_off: 'off'
q_Off: "Off"
q_OFF: 'OFF'
y_lower: y
y_upper: Y
n_lower: n
n_upper: N
exp: 1e3
date: 2026-01-02
stamp: 2026-01-02T03:04:05Z
int: 42
neg: -7
huge: 18446744073709551615
float: 1.5
null_value: ~
bool_true: true
seq:
  - yes
  - "on"
  - 3
  - n
nested:
  inner: Off
`

// scalarsWant returns what scalarsDoc decodes to. The quoted forms, y,
// n, 1e3, and timestamps are strings in both modes, and only the plain
// yes, no, on, and off words depend on the mode. Integers are int in
// both modes, while graft's engine leaves them uint64 and int64 when its
// own YAML 1.1 conversion is off, because DecodeOptions promises int.
func scalarsWant(yaml11 bool) map[string]any {
	word := func(s string, b bool) any {
		if yaml11 {
			return b
		}
		return s
	}
	return map[string]any{
		"p_yes": word("yes", true), "p_Yes": word("Yes", true), "p_YES": word("YES", true),
		"p_no": word("no", false), "p_No": word("No", false), "p_NO": word("NO", false),
		"p_on": word("on", true), "p_On": word("On", true), "p_ON": word("ON", true),
		"p_off": word("off", false), "p_Off": word("Off", false), "p_OFF": word("OFF", false),
		"q_yes": "yes", "q_Yes": "Yes", "q_YES": "YES",
		"q_no": "no", "q_No": "No", "q_NO": "NO",
		"q_on": "on", "q_On": "On", "q_ON": "ON",
		"q_off": "off", "q_Off": "Off", "q_OFF": "OFF",
		"y_lower": "y", "y_upper": "Y",
		"n_lower": "n", "n_upper": "N",
		"exp":        "1e3",
		"date":       "2026-01-02",
		"stamp":      "2026-01-02T03:04:05Z",
		"int":        42,
		"neg":        -7,
		"huge":       uint64(18446744073709551615),
		"float":      1.5,
		"null_value": nil,
		"bool_true":  true,
		"seq":        []any{word("yes", true), "on", 3, "n"},
		"nested":     map[string]any{"inner": word("Off", false)},
	}
}

// modes runs a case once in each decode mode.
var modes = []struct {
	name   string
	opts   yaml.DecodeOptions
	yaml11 bool
}{
	{"yaml11", yaml.DecodeOptions{}, true},
	{"yaml12", yaml.DecodeOptions{YAML12: true}, false},
}

func wantDeepEqual(t *testing.T, got, want any) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %s\nwant %s", describe(got), describe(want))
	}
}

// describe prints v with the dynamic type of every leaf, since int,
// uint64, and float64 print alike under %v.
func describe(v any) string {
	switch x := v.(type) {
	case map[string]any:
		parts := make([]string, 0, len(x))
		for k, item := range x {
			parts = append(parts, k+": "+describe(item))
		}
		return "{" + strings.Join(parts, ", ") + "}"
	case []any:
		parts := make([]string, 0, len(x))
		for _, item := range x {
			parts = append(parts, describe(item))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	default:
		return fmt.Sprintf("%#v(%T)", v, v)
	}
}

func TestUnmarshalGenericMapping(t *testing.T) {
	for _, m := range modes {
		t.Run(m.name+"/map", func(t *testing.T) {
			var got map[string]any
			if err := yaml.UnmarshalWith([]byte(scalarsDoc), &got, m.opts); err != nil {
				t.Fatalf("UnmarshalWith: %v", err)
			}
			wantDeepEqual(t, got, scalarsWant(m.yaml11))
		})
		t.Run(m.name+"/any", func(t *testing.T) {
			var got any
			if err := yaml.UnmarshalWith([]byte(scalarsDoc), &got, m.opts); err != nil {
				t.Fatalf("UnmarshalWith: %v", err)
			}
			wantDeepEqual(t, got, any(scalarsWant(m.yaml11)))
		})
	}
}

func TestUnmarshalDefaultIsYAML11(t *testing.T) {
	var got map[string]any
	if err := yaml.Unmarshal([]byte(scalarsDoc), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	wantDeepEqual(t, got, scalarsWant(true))
}

func TestUnmarshalGenericSequence(t *testing.T) {
	const doc = "- yes\n- \"on\"\n- 3\n- n\n- 1e3\n- 2026-01-02\n- {a: Off}\n"
	for _, m := range modes {
		word := func(s string, b bool) any {
			if m.yaml11 {
				return b
			}
			return s
		}
		want := []any{word("yes", true), "on", 3, "n", "1e3", "2026-01-02", map[string]any{"a": word("Off", false)}}
		t.Run(m.name+"/slice", func(t *testing.T) {
			var got []any
			if err := yaml.UnmarshalWith([]byte(doc), &got, m.opts); err != nil {
				t.Fatalf("UnmarshalWith: %v", err)
			}
			wantDeepEqual(t, got, want)
		})
		t.Run(m.name+"/any", func(t *testing.T) {
			var got any
			if err := yaml.UnmarshalWith([]byte(doc), &got, m.opts); err != nil {
				t.Fatalf("UnmarshalWith: %v", err)
			}
			wantDeepEqual(t, got, any(want))
		})
	}
}

func TestUnmarshalGenericScalarRoot(t *testing.T) {
	cases := []struct {
		doc            string
		yaml11, yaml12 any
	}{
		{"yes\n", true, "yes"},
		{"OFF\n", false, "OFF"},
		{"\"yes\"\n", "yes", "yes"},
		{"'On'\n", "On", "On"},
		{"y\n", "y", "y"},
		{"N\n", "N", "N"},
		{"1e3\n", "1e3", "1e3"},
		{"2026-01-02\n", "2026-01-02", "2026-01-02"},
		{"42\n", 42, 42},
		{"-7\n", -7, -7},
		{"1.5\n", 1.5, 1.5},
		{"text\n", "text", "text"},
	}
	for _, tc := range cases {
		for _, m := range modes {
			t.Run(m.name+"/"+strings.TrimSpace(tc.doc), func(t *testing.T) {
				var got any
				if err := yaml.UnmarshalWith([]byte(tc.doc), &got, m.opts); err != nil {
					t.Fatalf("UnmarshalWith: %v", err)
				}
				want := tc.yaml12
				if m.yaml11 {
					want = tc.yaml11
				}
				wantDeepEqual(t, got, want)
			})
		}
	}
}

// TestUnmarshalEmptyLeavesTargetUnchanged covers empty input and input
// that holds only comments, which leave the target as it was.
func TestUnmarshalEmptyLeavesTargetUnchanged(t *testing.T) {
	for _, doc := range []string{"", "# only a comment\n", "# one\n\n# two\n", "---\n"} {
		for _, m := range modes {
			t.Run(fmt.Sprintf("%s/%q", m.name, doc), func(t *testing.T) {
				gotAny := any("before")
				gotMap := map[string]any{"before": 1}
				gotSlice := []any{"before"}
				for _, target := range []any{&gotAny, &gotMap, &gotSlice} {
					if err := yaml.UnmarshalWith([]byte(doc), target, m.opts); err != nil {
						t.Fatalf("UnmarshalWith(%T): %v", target, err)
					}
				}
				wantDeepEqual(t, gotAny, any("before"))
				wantDeepEqual(t, gotMap, map[string]any{"before": 1})
				wantDeepEqual(t, gotSlice, []any{"before"})
			})
		}
	}
}

// TestUnmarshalDecodesFirstDocumentOnly checks that Unmarshal reads only
// the first document of a stream, even when a later one is broken.
func TestUnmarshalDecodesFirstDocumentOnly(t *testing.T) {
	cases := []struct {
		name string
		doc  string
		want map[string]any
	}{
		{"second document valid", "a: 1\n---\nb: 2\n", map[string]any{"a": 1}},
		{"second document broken", "a: 1\n---\nb: [\n", map[string]any{"a": 1}},
		{"leading separator", "---\na: yes\n---\nb: 2\n", map[string]any{"a": true}},
		{"end marker then separator", "a: 1\n...\n---\nb: 2\n", map[string]any{"a": 1}},
		{"byte order mark", "\xef\xbb\xbfa: 1\n---\nb: 2\n", map[string]any{"a": 1}},
		{"crlf", "a: 1\r\n---\r\nb: 2\r\n", map[string]any{"a": 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got map[string]any
			if err := yaml.Unmarshal([]byte(tc.doc), &got); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			wantDeepEqual(t, got, tc.want)
		})
	}
}

// TestUnmarshalReplacesMapAndSlice checks that a decoded mapping or
// sequence replaces what the target held, as the struct route's decoder
// does, rather than adding to it.
func TestUnmarshalReplacesMapAndSlice(t *testing.T) {
	gotMap := map[string]any{"old": 1}
	if err := yaml.Unmarshal([]byte("new: 2\n"), &gotMap); err != nil {
		t.Fatalf("Unmarshal map: %v", err)
	}
	wantDeepEqual(t, gotMap, map[string]any{"new": 2})

	gotSlice := []any{"old"}
	if err := yaml.Unmarshal([]byte("- new\n"), &gotSlice); err != nil {
		t.Fatalf("Unmarshal slice: %v", err)
	}
	wantDeepEqual(t, gotSlice, []any{"new"})
}

// TestUnmarshalAliasesAndMergeKeys checks that aliases and << merge keys
// resolve as they do in a merge.
func TestUnmarshalAliasesAndMergeKeys(t *testing.T) {
	const doc = "base: &b\n  x: yes\n  n: 1\nchild:\n  <<: *b\n  n: 2\nref: *b\n"
	var got map[string]any
	if err := yaml.Unmarshal([]byte(doc), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	wantDeepEqual(t, got, map[string]any{
		"base":  map[string]any{"x": true, "n": 1},
		"child": map[string]any{"x": true, "n": 2},
		"ref":   map[string]any{"x": true, "n": 1},
	})
}

// node is a recursive type, through a pointer and through a slice.
type node struct {
	Name string
	Next *node
	Kids []*node
}

// badNode is recursive and holds a refused map type below the cycle.
type badNode struct {
	Next  *badNode
	Kids  []*badNode
	Leaf  *badLeaf
	Count int
}

type badLeaf struct {
	Back  *badNode
	Extra map[interface{}]interface{}
}

// TestUnmarshalTargetCheckTerminatesOnRecursiveTypes checks that the
// target check walks a recursive type once, both when it finds nothing
// to refuse and when the refused type sits below the cycle.
func TestUnmarshalTargetCheckTerminatesOnRecursiveTypes(t *testing.T) {
	var ok node
	if err := yaml.Unmarshal([]byte("name: a\n"), &ok); err != nil && strings.Contains(err.Error(), "map[interface {}]interface {}") {
		t.Fatalf("recursive type refused for a map it does not hold: %v", err)
	}

	var bad badNode
	err := yaml.Unmarshal([]byte("count: 1\n"), &bad)
	de := wantDecodeError(t, err, yaml.ErrorKindType, 0)
	if !strings.Contains(de.Msg, "map[interface {}]interface {}") || !strings.Contains(de.Msg, "Extra") {
		t.Errorf("Msg %q does not name the refused type and field", de.Msg)
	}
}
