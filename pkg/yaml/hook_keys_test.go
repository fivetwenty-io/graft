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

// TestReplacedNaNKeysMoveNothing checks that a pair whose value held a
// NaN key, and that a later pair replaces, moves no entry of the later
// pair's value, even when that value holds a key equal to the stand-in
// the NaN key went to goccy under.
func TestReplacedNaNKeysMoveNothing(t *testing.T) {
	const top = "1.7976931348623157e308"
	inner := func() any { return new(map[string]map[float64]gapHook) }
	innerPlain := func() any { return new(map[string]map[float64]string) }
	runKeyCases(t, true, []keyCase{
		{"a repeated key", "x:\n  .NaN: a\nx:\n  " + top + ": b\n", inner, innerPlain, 1},
		{"a repeated key in flow", "x: {.nan: a, 1: c}\nx: {" + top + ": b}\n", inner, innerPlain, 1},
		{"a repeated key over a float32", "x: {.nan: a}\nx: {3.4028234663852886e+38: b}\n",
			func() any { return new(map[string]map[float32]gapHook) },
			func() any { return new(map[string]map[float32]string) }, 1},
		{"a repeated key two maps down", "x: {y: {.nan: a}}\nx: {y: {" + top + ": b}}\n",
			func() any { return new(map[string]map[string]map[float64]gapHook) },
			func() any { return new(map[string]map[string]map[float64]string) }, 1},
		{"a repeated field", "m: {.nan: a}\nm: {" + top + ": b}\n",
			func() any { return new(nanField) }, func() any { return new(nanFieldPlain) }, 1},
		{"a repeated key over a merge", "<<: {x: {.nan: a}}\nx: {" + top + ": b}\n", inner, innerPlain, 1},
	})
	runKeyCases(t, false, []keyCase{
		{"integer keys that fold alike", "+1:\n  .nan: a\n0x1:\n  " + top + ": b\n",
			func() any { return new(map[int]map[float64]gapHook) },
			func() any { return new(map[int]map[float64]string) }, 1},
		{"null keys that fold alike", "~:\n  .nan: a\nnull:\n  " + top + ": b\n",
			func() any { return new(map[any]map[any]gapHook) },
			func() any { return new(map[any]map[any]string) }, 1},
		{"a NaN kept beside a replaced one", "+1: {.nan: a}\n0x1: {.nan: c, " + top + ": b}\n",
			func() any { return new(map[int]map[float64]gapHook) },
			func() any { return new(map[int]map[float64]string) }, 2},
	})

	// Node.Decode walks the same sites.
	var n yaml.Node
	if err := yaml.Unmarshal([]byte("+1: {.nan: a}\n0x1: {"+top+": b}\n"), &n); err != nil {
		t.Fatal(err)
	}
	var m map[int]map[float64]gapHook
	if err := n.Decode(&m); err != nil {
		t.Fatal(err)
	}
	if got, want := renderEntries(reflect.ValueOf(m)), "{1: {1.7976931348623157e+308: b}}"; got != want {
		t.Errorf("Node.Decode: got %s, want %s", got, want)
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

// TestNullKeysOfIntegerMaps checks that a null key of a hooked map with
// integer keys is the zero key, as goccy reads it, so it keeps or
// replaces the entry of a key written 0, with the hook only on the value
// goccy keeps.
func TestNullKeysOfIntegerMaps(t *testing.T) {
	ints := func() any { return new(map[int]gapHook) }
	intsPlain := func() any { return new(map[int]string) }
	runKeyCases(t, false, []keyCase{
		{"a null key alone", "~: a\n", ints, intsPlain, 1},
		{"a null value over 0", "0: a\n~: ~\n", ints, intsPlain, 0},
		{"a null key over 0", "0: a\nnull: b\n", ints, intsPlain, 1},
		{"0 over a null key", "~: a\n0: b\n", ints, intsPlain, 1},
		{"a tagged null key", "0: a\n!!null ~: ~\n", ints, intsPlain, 0},
		{"a null key beside others", "1: a\n~: b\n2: c\n", ints, intsPlain, 3},
		{"a null key of a small int", "0: a\n~: ~\n",
			func() any { return new(map[uint8]gapHook) }, func() any { return new(map[uint8]string) }, 0},
		{"a null key over an inner map", "0: {a: x}\n~: {b: y}\n",
			func() any { return new(map[int]map[string]gapHook) }, func() any { return new(map[int]map[string]string) }, 1},
	})
}

// TestKeysReadAsWrittenBesideRepeats checks that a hooked decode reads
// each key of a document that repeats a key as the plain decode reads
// it, such as -0.0, which the plain decode keeps as a string, in a
// nested mapping, in a merge, and as the repeated key itself.
func TestKeysReadAsWrittenBesideRepeats(t *testing.T) {
	nested := func() any { return new(map[string]map[any]gapHook) }
	nestedPlain := func() any { return new(map[string]map[any]string) }
	anys := func() any { return new(map[any]gapHook) }
	anysPlain := func() any { return new(map[any]string) }
	runKeyCases(t, true, []keyCase{
		{"a nested key", "x:\n  -0.0: q\n  a: u\n  a: v\n", nested, nestedPlain, 2},
		{"a nested key beside a repeat above", "x:\n  -1.5: q\ny: {}\ny: {a: u}\n",
			nested, nestedPlain, 2},
		{"a merged key", "<<: {-0.0: m}\na: u\na: v\n", anys, anysPlain, 2},
		{"a repeated key", "x:\n  -0.0: q\n  -0.0: ~\n", nested, nestedPlain, 1},
		{"a key repeated with a value", "x:\n  -0.0: p\n  -0.0: 'yes'\n", nested, nestedPlain, 2},
	})
}
