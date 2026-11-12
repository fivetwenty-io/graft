package yaml_test

import (
	"reflect"
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
