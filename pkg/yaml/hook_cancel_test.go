package yaml_test

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

var gapHookCalls int

type gapHook struct{ Val string }

func (h *gapHook) UnmarshalYAML(n *yaml.Node) error {
	gapHookCalls++
	h.Val = "hook:" + n.Value
	return nil
}

func TestLaterKeyCancelsEarlierHooks(t *testing.T) {
	var nulled map[int]gapHook
	if err := yaml.Unmarshal([]byte("1: a\n0x1: ~\n"), &nulled); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(nulled, map[int]gapHook{1: {}}) {
		t.Errorf("null over a hook: %v", nulled)
	}

	gapHookCalls = 0
	var nested map[int]map[string]gapHook
	if err := yaml.Unmarshal([]byte("1: {a: x}\n0x1: {b: y}\n"), &nested); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(nested, map[int]map[string]gapHook{1: {"b": {Val: "hook:y"}}}) {
		t.Errorf("nested: %v", nested)
	}
	if gapHookCalls != 1 {
		t.Errorf("the hook ran %d times, want 1", gapHookCalls)
	}

	var nestedNull map[int]map[string]gapHook
	if err := yaml.Unmarshal([]byte("1: {a: x}\n0x1: ~\n"), &nestedNull); err != nil {
		t.Fatal(err)
	}
	if len(nestedNull[1]) != 0 {
		t.Errorf("null over a nested map: %v", nestedNull)
	}
}

func TestCancelStopsAtAStepBoundary(t *testing.T) {
	var m map[int]gapHook
	if err := yaml.Unmarshal([]byte("12: b\n1: a\n0x1: ~\n"), &m); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m, map[int]gapHook{12: {Val: "hook:b"}, 1: {}}) {
		t.Errorf("got %v", m)
	}
}

// cancelCase decodes in into a fresh value from target and expects want,
// with the hook run calls times.
type cancelCase struct {
	name   string
	in     string
	target func() any
	want   any
	calls  int
}

func runCancelCases(t *testing.T, cases []cancelCase) {
	t.Helper()
	for _, tc := range cases {
		gapHookCalls = 0
		got := tc.target()
		if err := yaml.Unmarshal([]byte(tc.in), got); err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if g := reflect.ValueOf(got).Elem().Interface(); !reflect.DeepEqual(g, tc.want) {
			t.Errorf("%s: got %+v, want %+v", tc.name, g, tc.want)
		}
		if gapHookCalls != tc.calls {
			t.Errorf("%s: the hook ran %d times, want %d", tc.name, gapHookCalls, tc.calls)
		}
	}
}

type cancelShared struct {
	V map[string]gapHook         `yaml:"v"`
	M map[int]map[string]gapHook `yaml:"m"`
}

type cancelFields struct {
	A gapHook `yaml:"a"`
	B struct {
		C gapHook `yaml:"c"`
	} `yaml:"b"`
}

// TestLaterKeyCancelsEveryForm checks that whichever way the later key
// is written or supplied, only the value goccy keeps reaches a hook.
func TestLaterKeyCancelsEveryForm(t *testing.T) {
	ints := func() any { return new(map[int]gapHook) }
	nested := func() any { return new(map[int]map[string]gapHook) }
	deep := func() any { return new(map[int]map[int]map[int][]map[string]gapHook) }
	runCancelCases(t, []cancelCase{
		{"later value replaces", "1: a\n0x1: b\n", ints, map[int]gapHook{1: {Val: "hook:b"}}, 1},
		{"third key wins", "1: a\n0x1: ~\n+1: c\n", ints, map[int]gapHook{1: {Val: "hook:c"}}, 1},
		{"a null after a later value", "1: a\n0x1: b\n+1: ~\n", ints, map[int]gapHook{1: {}}, 0},
		{"bool keys", "true: a\nyes: ~\n", func() any { return new(map[bool]gapHook) }, map[bool]gapHook{true: {}}, 0},
		{"bool keys, later value", "yes: a\ntrue: b\n", func() any { return new(map[bool]gapHook) }, map[bool]gapHook{true: {Val: "hook:b"}}, 1},
		{"merge supplies the earlier key", "<<: {1: a}\n0x1: ~\n", ints, map[int]gapHook{1: {}}, 0},
		{"merge supplies the later key", "0x1: b\n<<: {1: a}\n", ints, map[int]gapHook{1: {Val: "hook:a"}}, 1},
		{"merge supplies an earlier nested value", "<<: {1: {a: x}}\n0x1: {b: y}\n", nested, map[int]map[string]gapHook{1: {"b": {Val: "hook:y"}}}, 1},
		{"alias supplies the earlier key", "&k 1: a\n*k : ~\n", ints, map[int]gapHook{1: {}}, 0},
		{"alias supplies the earlier value", "v: &v {a: x}\nm:\n  1: *v\n  0x1: {b: y}\n", func() any { return new(cancelShared) },
			cancelShared{V: map[string]gapHook{"a": {Val: "hook:x"}}, M: map[int]map[string]gapHook{1: {"b": {Val: "hook:y"}}}}, 2},
		{"alias supplies the later value", "v: &v {a: x}\nm:\n  0x1: {b: y}\n  1: *v\n", func() any { return new(cancelShared) },
			cancelShared{V: map[string]gapHook{"a": {Val: "hook:x"}}, M: map[int]map[string]gapHook{1: {"a": {Val: "hook:x"}}}}, 2},
		{"sites several levels down", "1: {2: {3: [{a: x}, {b: y}]}}\n0x1: {2: {3: [{c: z}]}}\n", deep,
			map[int]map[int]map[int][]map[string]gapHook{1: {2: {3: {{"c": {Val: "hook:z"}}}}}}, 1},
		{"an empty later value", "1: {2: {3: [{a: x}]}}\n0x1: {}\n", deep, map[int]map[int]map[int][]map[string]gapHook{1: {}}, 0},
		{"a pointer value", "1: a\n0x1: ~\n", func() any { return new(map[int]*gapHook) }, map[int]*gapHook{1: nil}, 0},
		{"a pointer to a nested map", "1: {a: x}\n0x1: ~\n", func() any { return new(map[int]*map[string]gapHook) }, map[int]*map[string]gapHook{1: nil}, 0},
		{"zero and negative zero", "-0.0: a\n0.0: ~\n", func() any { return new(map[float64]gapHook) }, map[float64]gapHook{0: {}}, 0},
		{"interface keys", "1: a\n0x1: ~\n", func() any { return new(map[any]gapHook) }, map[any]gapHook{uint64(1): {}}, 0},
	})
}

// TestDistinctKeysKeepTheirHooks checks that keys goccy keeps apart each
// keep their hook, however alike they look.
func TestDistinctKeysKeepTheirHooks(t *testing.T) {
	runCancelCases(t, []cancelCase{
		{"different numbers", "1: a\n2: b\n0x3: c\n", func() any { return new(map[int]gapHook) },
			map[int]gapHook{1: {Val: "hook:a"}, 2: {Val: "hook:b"}, 3: {Val: "hook:c"}}, 3},
		{"quoted and unquoted strings", "1: a\n'0x1': b\n", func() any { return new(map[string]gapHook) },
			map[string]gapHook{"1": {Val: "hook:a"}, "0x1": {Val: "hook:b"}}, 2},
		{"an int and a float", "1: a\n1.0: b\n", func() any { return new(map[any]gapHook) },
			map[any]gapHook{uint64(1): {Val: "hook:a"}, 1.0: {Val: "hook:b"}}, 2},
		{"a negative int and a negative float", "-1: a\n-1.0: b\n", func() any { return new(map[any]gapHook) },
			map[any]gapHook{int64(-1): {Val: "hook:a"}, -1.0: {Val: "hook:b"}}, 2},
		{"a null in another map", "a: {b: 1}\n0x1: ~\n", func() any { return new(map[any]map[string]gapHook) },
			map[any]map[string]gapHook{"a": {"b": {Val: "hook:1"}}, uint64(1): nil}, 1},
	})
}

// TestRepeatedFieldKeyCancelsEarlierHooks checks a struct key that an
// alias repeats, where goccy fills the field from the later pair alone.
func TestRepeatedFieldKeyCancelsEarlierHooks(t *testing.T) {
	fields := func() any { return new(cancelFields) }
	var later cancelFields
	later.A.Val = "hook:y"
	runCancelCases(t, []cancelCase{
		{"null over a hooked field", "&k a: x\n*k : ~\n", fields, cancelFields{}, 0},
		{"empty over a nested hook", "&k b: {c: x}\n*k : {}\n", fields, cancelFields{}, 0},
		{"value over a hooked field", "&k a: x\n*k : y\n", fields, later, 1},
	})
}

// renderEntries prints v with every pointer followed, a nil pointer as
// nil, map entries sorted, and a gapHook as the text its hook received,
// so a hooked result and the result goccy keeps for the same input with
// string in place of gapHook print alike exactly when they agree.
func renderEntries(v reflect.Value) string {
	switch v.Kind() {
	case reflect.Pointer:
		if v.IsNil() {
			return "nil"
		}
		return "&" + renderEntries(v.Elem())
	case reflect.Map:
		if v.IsNil() {
			return "nilmap"
		}
		var out []string
		for it := v.MapRange(); it.Next(); {
			out = append(out, renderEntries(it.Key())+": "+renderEntries(it.Value()))
		}
		sort.Strings(out)
		return "{" + strings.Join(out, ", ") + "}"
	case reflect.Struct:
		if h, ok := v.Interface().(gapHook); ok {
			return strings.TrimPrefix(h.Val, "hook:")
		}
		return fmt.Sprintf("%+v", v.Interface())
	default:
		return fmt.Sprintf("%v", v.Interface())
	}
}

// ptrKeyCase decodes in into the hooked target and into the plain one,
// which holds string where the hooked one holds gapHook.
type ptrKeyCase struct {
	name          string
	in            string
	hooked, plain func() any
	calls         int
}

// TestPointerKeysPlaceEachHook checks that a map with pointer keys gets
// each hook on the entry goccy made for its pair.
func TestPointerKeysPlaceEachHook(t *testing.T) {
	ints := func() any { return new(map[*int]gapHook) }
	intsPlain := func() any { return new(map[*int]string) }
	for _, tc := range []ptrKeyCase{
		{"distinct keys", "1: a\n2: b\n", ints, intsPlain, 2},
		{"a nil key alone", "~: a\n", ints, intsPlain, 1},
		{"a nil key beside another", "~: a\n1: b\n", ints, intsPlain, 2},
		{"an alias to a nil key", "&n ~: a\n*n : b\n", ints, intsPlain, 1},
		{"a null under a nil key", "~: a\nnull: ~\n", ints, intsPlain, 0},
		{"nested maps", "x: {1: a, 2: b}\ny: {1: c}\n",
			func() any { return new(map[*string]map[*int]gapHook) },
			func() any { return new(map[*string]map[*int]string) }, 3},
		{"equal keys with no hook site", "1: ~\n0x1: ~\n", ints, intsPlain, 0},
		{"equal keys over empty maps", "1: {}\n0x1: {}\n",
			func() any { return new(map[*int]map[string]gapHook) },
			func() any { return new(map[*int]map[string]string) }, 0},
		{"a pointer to a struct as a nil key", "~: a\n",
			func() any { return new(map[*cancelFields]gapHook) },
			func() any { return new(map[*cancelFields]string) }, 1},
	} {
		gapHookCalls = 0
		hooked, plain := tc.hooked(), tc.plain()
		if err := yaml.Unmarshal([]byte(tc.in), hooked); err != nil {
			t.Errorf("%s: %v", tc.name, err)
			continue
		}
		if err := yaml.Unmarshal([]byte(tc.in), plain); err != nil {
			t.Fatalf("%s: plain: %v", tc.name, err)
		}
		got, want := renderEntries(reflect.ValueOf(hooked)), renderEntries(reflect.ValueOf(plain))
		if got != want {
			t.Errorf("%s: got %s, want %s", tc.name, got, want)
		}
		if gapHookCalls != tc.calls {
			t.Errorf("%s: the hook ran %d times, want %d", tc.name, gapHookCalls, tc.calls)
		}
	}
}

// checkPointerKeyRefusal checks that err refuses a map of Go type typ at
// line, and that its message quotes no scalar of in.
func checkPointerKeyRefusal(t *testing.T, name, in string, err error, typ string, line int) {
	t.Helper()
	checkRefusal(t, name, in, err, "two keys of a map of Go type "+typ+" point to equal values, and a value under one of them has an UnmarshalYAML method", line)
}

// checkRefusal checks that err is a type error at line with the message
// want, and that the message quotes no scalar of in.
func checkRefusal(t *testing.T, name, in string, err error, want string, line int) {
	t.Helper()
	var de *yaml.DecodeError
	if !errors.As(err, &de) {
		t.Errorf("%s: got %v, want a *DecodeError", name, err)
		return
	}
	if de.Kind != yaml.ErrorKindType || de.Msg != want || de.Line != line {
		t.Errorf("%s: got kind %v, line %d, %q", name, de.Kind, de.Line, de.Msg)
	}
	// A word the message holds in any case, such as the line number or a
	// letter of the type, says nothing about quoting.
	fixed := "yaml: line " + strconv.Itoa(line) + ": " + want
	for _, word := range strings.FieldsFunc(in, func(r rune) bool { return strings.ContainsRune(" :\n{},&*<?", r) }) {
		if !strings.Contains(fixed, word) && strings.Contains(err.Error(), word) {
			t.Errorf("%s: the message quotes %q: %v", name, word, err)
		}
	}
}

// TestPointerKeysWithEqualValuesAreRefused checks the maps with pointer
// keys whose hooks no entry can be matched to, since two keys point to
// equal values and only document order tells their entries apart.
func TestPointerKeysWithEqualValuesAreRefused(t *testing.T) {
	ints := func() any { return new(map[*int]gapHook) }
	for _, tc := range []struct {
		name, in string
		target   func() any
		typ      string
		line     int
		opts     yaml.DecodeOptions
	}{
		{"two spellings", "1: alpha\n0x1: bravo\n", ints, "map[*int]yaml_test.gapHook", 2, yaml.DecodeOptions{}},
		{"a hook on the earlier key only", "1: a\n0x1: ~\n", ints, "map[*int]yaml_test.gapHook", 2, yaml.DecodeOptions{}},
		{"a hook on the later key only", "1: ~\n0x1: b\n", ints, "map[*int]yaml_test.gapHook", 2, yaml.DecodeOptions{}},
		{"bool keys", "true: alpha\nTrue: bravo\n", func() any { return new(map[*bool]gapHook) }, "map[*bool]yaml_test.gapHook", 2, yaml.DecodeOptions{YAML12: true}},
		// An alias expands to its anchor's own node, so the refusal names
		// the line of the anchor's key.
		{"an alias key", "&k 1: a\n*k : b\n", ints, "map[*int]yaml_test.gapHook", 1, yaml.DecodeOptions{}},
		{"zero and negative zero", "-0.0: a\n0.0: b\n", func() any { return new(map[*float64]gapHook) }, "map[*float64]yaml_test.gapHook", 2, yaml.DecodeOptions{}},
		{"an inner map", "x:\n  1: a\n  0x1: b\n", func() any { return new(map[*string]map[*int]gapHook) }, "map[*int]yaml_test.gapHook", 3, yaml.DecodeOptions{}},
		{"a key a merge supplies", "<<: {1: a}\n0x1: b\n", ints, "map[*int]yaml_test.gapHook", 2, yaml.DecodeOptions{}},
	} {
		gapHookCalls = 0
		err := yaml.UnmarshalWith([]byte(tc.in), tc.target(), tc.opts)
		checkPointerKeyRefusal(t, tc.name, tc.in, err, tc.typ, tc.line)
		if gapHookCalls != 0 {
			t.Errorf("%s: the hook ran %d times", tc.name, gapHookCalls)
		}
	}
}

// TestReportedRepeatOfAPointerKeyIsRefused checks that a key written
// twice in a map with pointer keys, which OnDuplicateKey folds into one
// pair before the decode sees it, is refused when the map has a hook
// under it, since goccy keeps an entry for each pair of such a map.
func TestReportedRepeatOfAPointerKeyIsRefused(t *testing.T) {
	report := yaml.DecodeOptions{OnDuplicateKey: func(yaml.DuplicateKey) {}}
	ints := func() any { return new(map[*int]gapHook) }
	for _, tc := range []struct {
		name, in string
		target   func() any
		typ      string
		line     int
	}{
		{"a repeat", "1: alpha\n1: bravo\n", ints, "map[*int]yaml_test.gapHook", 2},
		{"a repeat with no hook site", "1: ~\n1: ~\n", ints, "map[*int]yaml_test.gapHook", 2},
		{"a third repeat", "1: alpha\n1: bravo\n1: charlie\n", ints, "map[*int]yaml_test.gapHook", 3},
		{"explicit keys", "? 1\n: alpha\n? 1\n: bravo\n", ints, "map[*int]yaml_test.gapHook", 3},
		{"an inner map", "x:\n  1: alpha\n  1: bravo\n", func() any { return new(map[*string]map[*int]gapHook) }, "map[*int]yaml_test.gapHook", 3},
	} {
		gapHookCalls = 0
		err := yaml.UnmarshalWith([]byte(tc.in), tc.target(), report)
		checkRefusal(t, tc.name, tc.in, err, "a key repeats in a map of Go type "+tc.typ+", which has an UnmarshalYAML method under it", tc.line)
		if gapHookCalls != 0 {
			t.Errorf("%s: the hook ran %d times", tc.name, gapHookCalls)
		}
	}
}

// TestReportedRepeatsElsewhereAreUnchanged checks that OnDuplicateKey
// still folds a repeat of a nil key, a repeat in a map whose keys are no
// pointers, and a repeat in a map with no hook under it, as it did.
func TestReportedRepeatsElsewhereAreUnchanged(t *testing.T) {
	for _, tc := range []struct {
		name, in      string
		hooked, plain func() any
		calls, report int
	}{
		{"a nil key", "~: a\n~: b\n", func() any { return new(map[*int]gapHook) }, func() any { return new(map[*int]string) }, 1, 1},
		{"keys that are no pointers", "1: a\n1: b\n", func() any { return new(map[int]gapHook) }, func() any { return new(map[int]string) }, 1, 1},
		{"no hook", "1: a\n1: b\n", func() any { return new(map[*int]string) }, func() any { return new(map[*int]string) }, 0, 1},
	} {
		gapHookCalls = 0
		reports := 0
		opts := yaml.DecodeOptions{OnDuplicateKey: func(yaml.DuplicateKey) { reports++ }}
		hooked, plain := tc.hooked(), tc.plain()
		if err := yaml.UnmarshalWith([]byte(tc.in), hooked, opts); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if reports != tc.report {
			t.Errorf("%s: %d reports, want %d", tc.name, reports, tc.report)
		}
		if err := yaml.UnmarshalWith([]byte(tc.in), plain, opts); err != nil {
			t.Fatalf("%s: plain: %v", tc.name, err)
		}
		got, want := renderEntries(reflect.ValueOf(hooked)), renderEntries(reflect.ValueOf(plain))
		if got != want {
			t.Errorf("%s: got %s, want %s", tc.name, got, want)
		}
		if gapHookCalls != tc.calls {
			t.Errorf("%s: the hook ran %d times, want %d", tc.name, gapHookCalls, tc.calls)
		}
	}
}

// TestReportedRepeatKeepsTheLastAlikeKey checks that when OnDuplicateKey
// folds a key written twice, a pair between the two whose key a map reads
// as the same Go key still loses to the later repeat, as it does when
// the text decodes straight into the map, and that the repeats reported
// are the same on both routes.
func TestReportedRepeatKeepsTheLastAlikeKey(t *testing.T) {
	for _, tc := range []struct {
		name, in      string
		hooked, plain func() any
	}{
		{"a spelling between", "1: a\n+1: b\n1: c\n", func() any { return new(map[int]gapHook) }, func() any { return new(map[int]string) }},
		{"two spellings between", "1: a\n+1: b\n0x1: c\n1: d\n", func() any { return new(map[int]gapHook) }, func() any { return new(map[int]string) }},
		{"a repeat of the middle spelling", "+1: a\n1: b\n+1: c\n", func() any { return new(map[int]gapHook) }, func() any { return new(map[int]string) }},
		{"keys of any type", "1: a\n1.0: b\n1: c\n", func() any { return new(map[float64]gapHook) }, func() any { return new(map[float64]string) }},
		{"an inner map", "x:\n  1: a\n  +1: b\n  1: c\n", func() any { return new(map[string]map[int]gapHook) }, func() any { return new(map[string]map[int]string) }},
		{"two spellings of null", "null: a\n~: b\nnull: c\n", func() any { return new(map[any]gapHook) }, func() any { return new(map[any]string) }},
		{"a merge between", "1: a\n<<: {+1: m}\n1: c\n", func() any { return new(map[int]gapHook) }, func() any { return new(map[int]string) }},
		{"a merge after", "+1: a\n1: b\n<<: {1: m}\n+1: c\n", func() any { return new(map[int]gapHook) }, func() any { return new(map[int]string) }},
	} {
		var hookedReports, plainReports []yaml.DuplicateKey
		hooked, plain := tc.hooked(), tc.plain()
		err := yaml.UnmarshalWith([]byte(tc.in), hooked, yaml.DecodeOptions{OnDuplicateKey: func(d yaml.DuplicateKey) { hookedReports = append(hookedReports, d) }})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		err = yaml.UnmarshalWith([]byte(tc.in), plain, yaml.DecodeOptions{OnDuplicateKey: func(d yaml.DuplicateKey) { plainReports = append(plainReports, d) }})
		if err != nil {
			t.Fatalf("%s: plain: %v", tc.name, err)
		}
		if got, want := renderEntries(reflect.ValueOf(hooked)), renderEntries(reflect.ValueOf(plain)); got != want {
			t.Errorf("%s: got %s, want %s", tc.name, got, want)
		}
		if !reflect.DeepEqual(hookedReports, plainReports) {
			t.Errorf("%s: reports %+v, want %+v", tc.name, hookedReports, plainReports)
		}
	}
}

// TestReportedRepeatLeavesTheNodeOrder checks that a Node read with
// OnDuplicateKey still keeps a repeated key at its first place, holding
// its last value, and that a plain map still takes the last value.
func TestReportedRepeatLeavesTheNodeOrder(t *testing.T) {
	in := []byte("1: a\n+1: b\n1: c\n")
	reports := 0
	opts := yaml.DecodeOptions{OnDuplicateKey: func(yaml.DuplicateKey) { reports++ }}
	var n yaml.Node
	if err := yaml.UnmarshalWith(in, &n, opts); err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, c := range n.Content[0].Content {
		got = append(got, c.Value)
	}
	if want := []string{"1", "c", "+1", "b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("Node pairs %q, want %q", got, want)
	}
	var plain map[int]string
	if err := yaml.UnmarshalWith(in, &plain, opts); err != nil {
		t.Fatal(err)
	}
	if plain[1] != "c" || len(plain) != 1 {
		t.Errorf("plain map %v, want 1: c", plain)
	}
	if reports != 2 {
		t.Errorf("%d reports, want 2", reports)
	}
}
