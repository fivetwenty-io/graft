package yaml_test

import (
	"math"
	"reflect"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

// keyCase decodes in into the hooked target and into the plain one,
// which holds string where the hooked one holds gapHook, and expects
// the hook to run calls times.
type keyCase struct {
	name          string
	in            string
	hooked, plain func() any
	calls         int
}

// runKeyCases checks that each hooked decode keeps the entries the
// plain decode keeps, with each hook on its own entry's value, both
// with and without OnDuplicateKey. With dupsOnly set, a case runs only
// with OnDuplicateKey, since its input repeats a key.
func runKeyCases(t *testing.T, dupsOnly bool, cases []keyCase) {
	t.Helper()
	for _, tc := range cases {
		for _, dups := range []bool{false, true} {
			if dupsOnly && !dups {
				continue
			}
			var opts yaml.DecodeOptions
			if dups {
				opts.OnDuplicateKey = func(yaml.DuplicateKey) {}
			}
			gapHookCalls = 0
			hooked, plain := tc.hooked(), tc.plain()
			if err := yaml.UnmarshalWith([]byte(tc.in), hooked, opts); err != nil {
				t.Errorf("%s (dups %v): %v", tc.name, dups, err)
				continue
			}
			calls := gapHookCalls
			if err := yaml.UnmarshalWith([]byte(tc.in), plain, opts); err != nil {
				t.Fatalf("%s (dups %v): plain: %v", tc.name, dups, err)
			}
			if got, want := renderEntries(reflect.ValueOf(hooked)), renderEntries(reflect.ValueOf(plain)); got != want {
				t.Errorf("%s (dups %v): got %s, want %s", tc.name, dups, got, want)
			}
			if calls != tc.calls {
				t.Errorf("%s (dups %v): the hook ran %d times, want %d", tc.name, dups, calls, tc.calls)
			}
		}
	}
}

// TestNaNKeysKeepOneEntryEach checks that each NaN key of a hooked map
// keeps the one entry goccy makes for it, with its own hook, though no
// NaN key ever finds another, and that a failed decode leaves no other
// key in its place.
func TestNaNKeysKeepOneEntryEach(t *testing.T) {
	anys := func() any { return new(map[any]gapHook) }
	anysPlain := func() any { return new(map[any]string) }
	floats := func() any { return new(map[float64]gapHook) }
	floatsPlain := func() any { return new(map[float64]string) }
	runKeyCases(t, false, []keyCase{
		{"one NaN", ".nan: u\n", anys, anysPlain, 1},
		{"one NaN as a float", ".nan: u\n", floats, floatsPlain, 1},
		{"two NaNs", ".nan: u\n.NaN: v\n", anys, anysPlain, 2},
		{"two NaNs as floats", ".nan: u\n.NaN: v\n", floats, floatsPlain, 2},
		{"a NaN among other keys", "a: x\n.nan: u\n1: y\n", anys, anysPlain, 3},
		{"a NaN among other floats", "1: x\n.nan: u\n2.5: y\n", floats, floatsPlain, 3},
		{"a NaN with a null", ".nan: ~\n.NaN: u\n", floats, floatsPlain, 1},
		{"a NaN beside the largest float", "1.7976931348623157e+308: a\n.nan: u\n", floats, floatsPlain, 2},
		{"a NaN beside the largest float32", "3.4028234663852886e+38: a\n.nan: u\n",
			func() any { return new(map[float32]gapHook) }, func() any { return new(map[float32]string) }, 2},
		{"NaNs in an inner map", "x: {.nan: u, .NaN: v}\n.nan: {.nan: w}\n",
			func() any { return new(map[any]map[any]gapHook) }, func() any { return new(map[any]map[any]string) }, 3},
		{"a NaN over an inner map", ".nan: {a: u, b: v}\n.NaN: {a: w}\n",
			func() any { return new(map[float64]map[string]gapHook) }, func() any { return new(map[float64]map[string]string) }, 3},
		{"a NaN by a merge", "<<: {.nan: m}\n.NaN: u\n", floats, floatsPlain, 2},
		{"a NaN under a pointer key", "x: {.nan: u}\ny: {.nan: v, 1: w}\n",
			func() any { return new(map[*string]map[float64]gapHook) }, func() any { return new(map[*string]map[float64]string) }, 3},
		{"a NaN in a list", "l: [{.nan: u}, {.nan: v}]\n",
			func() any { return new(map[string][]map[float64]gapHook) }, func() any { return new(map[string][]map[float64]string) }, 2},
		{"a NaN in a field", "m: {.nan: u}\nn: v\n",
			func() any { return new(nanField) }, func() any { return new(nanFieldPlain) }, 2},
	})
	runKeyCases(t, true, []keyCase{
		{"a NaN written twice", ".nan: u\n.nan: v\n", anys, anysPlain, 2},
		{"a NaN written twice as a float", ".nan: u\n.nan: v\n", floats, floatsPlain, 2},
	})

	// A decode that fails after a pair with a NaN key leaves that
	// pair's entry under a NaN key.
	var m map[float64]gapHook
	if err := yaml.Unmarshal([]byte(".nan: u\nfoo: v\n"), &m); err == nil {
		t.Fatal("decoded a key that is no float")
	}
	for k := range m {
		if !math.IsNaN(k) {
			t.Errorf("got key %v, want only NaN keys", k)
		}
	}
}

// nanField has a map with float keys beside a hooked field.
type nanField struct {
	M map[float64]gapHook `yaml:"m"`
	N gapHook             `yaml:"n"`
}

// nanFieldPlain is nanField with string in place of gapHook.
type nanFieldPlain struct {
	M map[float64]string `yaml:"m"`
	N string             `yaml:"n"`
}
