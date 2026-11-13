package yaml_test

import (
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

type inlineRest struct {
	A    int            `yaml:"a"`
	Rest map[string]int `yaml:",inline"`
}

type inlineOuter struct {
	Name  string     `yaml:"name"`
	Inner inlineRest `yaml:"inner"`
}

func TestInlineMapErrorNamesTheFirstBadValue(t *testing.T) {
	cases := []struct {
		src  string
		v    func() any
		line int
	}{
		{"a: 1\nb: x\nc: y\nd: z\ne: w\n", func() any { return &inlineRest{} }, 2},
		{"name: n\ninner:\n  a: 1\n  b: 2\n  c: x\n  d: y\n  e: z\n", func() any { return &inlineOuter{} }, 5},
	}
	for _, c := range cases {
		for i := 0; i < 200; i++ {
			err := yaml.Unmarshal([]byte(c.src), c.v())
			var de *yaml.DecodeError
			if !errors.As(err, &de) || de.Kind != yaml.ErrorKindType || de.Line != c.line {
				t.Fatalf("run %d of %q: %v, want a type error at line %d", i, c.src, err, c.line)
			}
			for _, bad := range []string{"x", "y", "z", "w"} {
				if strings.Contains(de.Msg, ": "+bad) || strings.HasSuffix(de.Msg, " "+bad) {
					t.Fatalf("the message quotes the input: %q", de.Msg)
				}
			}
		}
	}
}

func TestInlineMapStillDecodes(t *testing.T) {
	var v inlineRest
	if err := yaml.Unmarshal([]byte("a: 1\nb: 2\nc: 3\n"), &v); err != nil || v.A != 1 || v.Rest["b"] != 2 || v.Rest["c"] != 3 {
		t.Errorf("got %+v, %v", v, err)
	}
}

type inlineLists struct {
	Rest map[string][]int `yaml:",inline"`
}

type inlineBools struct {
	Rest map[string]bool `yaml:",inline"`
}

type inlineDurations struct {
	Rest map[string]time.Duration `yaml:",inline"`
}

type inlineIntKeys struct {
	Rest map[string]map[int]string `yaml:",inline"`
}

type inlineDefs struct {
	Defs  map[string]string `yaml:"defs"`
	Inner inlineRest        `yaml:"inner"`
}

type inlinePointer struct {
	Rest *map[string]int `yaml:",inline"`
}

// TestInlineMapErrorIsTheSameEveryRun decodes each input many times and
// checks that the error, its text included, never changes.
func TestInlineMapErrorIsTheSameEveryRun(t *testing.T) {
	cases := []struct {
		name string
		src  string
		v    func() any
		line int
	}{
		{"nested value", "b: [1]\nc: [x]\nd: [y]\ne: [z]\n", func() any { return &inlineLists{} }, 2},
		{"mapping values", "b: 1\nc:\n  k: 1\nd:\n  k: 2\ne: {k: 3}\n", func() any { return &inlineRest{} }, 3},
		{"yaml 1.1 bools", "b: true\nc: maybe\nd: nope\ne: never\n", func() any { return &inlineBools{} }, 2},
		{"durations", "b: 1s\nc: x\nd: y\ne: z\n", func() any { return &inlineDurations{} }, 2},
		{"pointer to map", "b: 1\nc: x\nd: y\ne: z\n", func() any { return &inlinePointer{} }, 2},
		{"in a sequence", "- a: 1\n  b: 2\n- a: 1\n  b: x\n  c: y\n  d: z\n", func() any { return &[]inlineRest{} }, 4},
		{"in a map value", "k:\n  a: 1\n  b: x\n  c: y\n  d: z\n", func() any { return &map[string]inlineRest{} }, 3},
		{"alias after the bad value", "name: &x hello\ninner:\n  a: 1\n  b: x\n  c: *x\n", func() any { return &inlineOuter{} }, 1},
		{"alias after several bad values", "name: &x hello\ninner:\n  a: 1\n  b: x\n  c: y\n  d: z\n  e: *x\n", func() any { return &inlineOuter{} }, 1},
		{"alias to a mapping", "name: n\ninner:\n  a: 1\n  b: x\n  c: &m {k: 1}\n  d: *m\n", func() any { return &inlineOuter{} }, 4},
		{"merge from an anchor", "defs: &d\n  k: zz\n  j: ww\ninner:\n  a: 1\n  b: x\n  <<: *d\n", func() any { return &inlineDefs{} }, 2},
		{"merge with an override", "defs: &d\n  k: zz\n  j: ww\ninner:\n  a: 1\n  b: x\n  <<: *d\n  k: 5\n", func() any { return &inlineDefs{} }, 3},
		{"flow merge", "inner:\n  a: 1\n  <<: {m: q, n: r}\n  b: x\n", func() any { return &inlineOuter{} }, 3},
		{"bad keys", "k:\n  1: a\n  x: b\nj:\n  y: c\nm:\n  z: d\n", func() any { return &inlineIntKeys{} }, 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var want string
			for i := 0; i < 200; i++ {
				err := yaml.Unmarshal([]byte(c.src), c.v())
				var de *yaml.DecodeError
				if !errors.As(err, &de) || de.Kind != yaml.ErrorKindType || de.Line != c.line {
					t.Fatalf("run %d: %v, want a type error at line %d", i, err, c.line)
				}
				if i == 0 {
					want = err.Error()
				} else if err.Error() != want {
					t.Fatalf("run %d: %q, run 0: %q", i, err.Error(), want)
				}
			}
		})
	}
}

func TestInlineMapErrorKeepsWhatTheDecodeWrote(t *testing.T) {
	var v inlineOuter
	if err := yaml.Unmarshal([]byte("name: n\ninner:\n  a: 1\n  b: x\n  c: y\n"), &v); err == nil {
		t.Fatal("decoded a string into an int")
	}
	if v.Name != "n" {
		t.Errorf("got %+v, want the name the decode wrote", v)
	}
}

// keyRing is a caller's type whose method panics unless its mapping
// holds every key from a to h, as a method that trusts its input can.
type keyRing struct {
	keys map[string]int
}

// keyRingPanics counts the panics keyRing's method raises.
var keyRingPanics atomic.Int64

func (k *keyRing) UnmarshalYAML(unmarshal func(any) error) error {
	var m map[string]int
	if err := unmarshal(&m); err != nil {
		return err
	}
	for _, name := range strings.Split("abcdefgh", "") {
		if _, ok := m[name]; !ok {
			keyRingPanics.Add(1)
			panic("keyRing has no key " + name)
		}
	}
	k.keys = m
	return nil
}

// ringBetween puts a keyRing between two inline maps, and declares the
// later one first so that goccy reports its bad value first.
type ringBetween struct {
	Second inlineRest `yaml:"second"`
	Keys   keyRing    `yaml:"keys"`
	First  inlineRest `yaml:"first"`
}

// ringSource gives a ringBetween document with a bad value in first and
// in second, and pad good pairs in second ahead of its bad value.
func ringSource(pad int) []byte {
	var b strings.Builder
	b.WriteString("first:\n  a: 1\n  b: x\nkeys:\n")
	for i, name := range strings.Split("abcdefgh", "") {
		b.WriteString("  " + name + ": " + string(rune('1'+i)) + "\n")
	}
	b.WriteString("second:\n  a: 1\n")
	for i := range pad {
		b.WriteString("  f" + string(rune('a'+i)) + ": 1\n")
	}
	b.WriteString("  b: y\n")
	return []byte(b.String())
}

func TestInlineMapErrorSurvivesAPanickingMethod(t *testing.T) {
	cases := []struct {
		name string
		pad  int
		line int
	}{
		// Good pairs after the ring let the search narrow to the first
		// bad value before it cuts the ring short.
		{name: "padded", pad: 20, line: 3},
		// Without them the first probe cuts the ring short, and the
		// search keeps the error of the longest prefix. That names
		// second's bad value, and the answer is the same every run only
		// because each inline map holds a single bad value.
		{name: "unpadded", pad: 0, line: 15},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := ringSource(tc.pad)
			keyRingPanics.Store(0)
			var want string
			for i := 0; i < 200; i++ {
				var v ringBetween
				err := yaml.Unmarshal(src, &v)
				var de *yaml.DecodeError
				if !errors.As(err, &de) || de.Kind != yaml.ErrorKindType ||
					de.Line != tc.line {
					t.Fatalf("run %d: %v, want a type error at line %d",
						i, err, tc.line)
				}
				if i == 0 {
					want = err.Error()
				} else if err.Error() != want {
					t.Fatalf("run %d: %q, run 0: %q", i, err.Error(), want)
				}
			}
			if keyRingPanics.Load() == 0 {
				t.Fatal("the search never handed keyRing a mapping " +
					"without all its keys")
			}
		})
	}
}

// boolShy is a caller's type whose method panics when a value in its
// mapping is a bool.
type boolShy struct {
	values map[string]any
}

func (b *boolShy) UnmarshalYAML(unmarshal func(any) error) error {
	var m map[string]any
	if err := unmarshal(&m); err != nil {
		return err
	}
	for k, v := range m {
		if _, ok := v.(bool); ok {
			panic("boolShy got a bool at " + k)
		}
	}
	b.values = m
	return nil
}

// shyAndFlag holds a boolShy beside a bool that YAML 1.1 refuses a
// quoted "yes" for. Placing that refusal decodes the document again
// with each plain yes read as a bool, which boolShy panics on.
type shyAndFlag struct {
	Second inlineRest `yaml:"second"`
	Flag   bool       `yaml:"flag"`
	Ring   boolShy    `yaml:"ring"`
}

func TestInlineMapErrorSurvivesAPanicWhilePlacingIt(t *testing.T) {
	src := []byte("ring: {v: yes}\nflag: \"yes\"\nsecond:\n  a: 1\n  b: x\n")
	var want string
	for i := 0; i < 200; i++ {
		var v shyAndFlag
		err := yaml.Unmarshal(src, &v)
		var de *yaml.DecodeError
		if !errors.As(err, &de) || de.Kind != yaml.ErrorKindType || de.Line != 5 {
			t.Fatalf("run %d: %v, want a type error at line 5", i, err)
		}
		if i == 0 {
			want = err.Error()
		} else if err.Error() != want {
			t.Fatalf("run %d: %q, run 0: %q", i, err.Error(), want)
		}
	}
}

// aliasedValues writes n inline values that each hold an alias to the
// anchor on line 1. Every one of them is a sequence where an int
// belongs, and every one starts where the anchor does.
func aliasedValues(n int) []byte {
	var b strings.Builder
	b.WriteString("name: &x hello\ninner:\n  a: 1\n")
	for i := range n {
		fmt.Fprintf(&b, "  c%04d: [*x]\n", i)
	}
	return []byte(b.String())
}

func TestInlineMapErrorWithManyAliasedValues(t *testing.T) {
	cases := []struct {
		n, runs int
	}{
		{200, 100},
		{1000, 20},
	}
	for _, c := range cases {
		t.Run(fmt.Sprint(c.n), func(t *testing.T) {
			if c.n > 200 && testing.Short() {
				t.Skip("decodes a 1000-value document")
			}
			src := aliasedValues(c.n)
			var want string
			for i := 0; i < c.runs; i++ {
				err := yaml.Unmarshal(src, &inlineOuter{})
				var de *yaml.DecodeError
				if !errors.As(err, &de) || de.Kind != yaml.ErrorKindType || de.Line != 4 {
					t.Fatalf("run %d: %v, want a type error at line 4", i, err)
				}
				if i == 0 {
					want = err.Error()
				} else if err.Error() != want {
					t.Fatalf("run %d: %q, run 0: %q", i, err.Error(), want)
				}
			}
		})
	}
}

// nextFirst and xFirst are recursive types whose inline maps let every
// level hold a bad value. goccy fills a struct's fields in the order they
// are declared, so it reports the deepest x of a nextFirst document
// first and the shallowest x of an xFirst document first.
type nextFirst struct {
	Next *nextFirst     `yaml:"next"`
	X    int            `yaml:"x"`
	Rest map[string]any `yaml:",inline"`
}

type xFirst struct {
	X    int            `yaml:"x"`
	Next *xFirst        `yaml:"next"`
	Rest map[string]any `yaml:",inline"`
}

// xThenNext nests depth levels, each with x set to value and written
// before next, so the shallowest x comes first.
func xThenNext(depth int, value string) []byte {
	var b strings.Builder
	for i := range depth {
		indent := strings.Repeat("  ", i)
		b.WriteString(indent + "x: " + value + "\n" + indent + "next:\n")
	}
	b.WriteString(strings.Repeat("  ", depth) + "x: " + value + "\n")
	return []byte(b.String())
}

// nextThenX nests depth levels, each with x written after next, so the
// deepest x comes first.
func nextThenX(depth int, value string) []byte {
	var b strings.Builder
	for i := range depth {
		b.WriteString(strings.Repeat("  ", i) + "next:\n")
	}
	for i := depth; i >= 0; i-- {
		b.WriteString(strings.Repeat("  ", i) + "x: " + value + "\n")
	}
	return []byte(b.String())
}

// TestInlineMapErrorCostIsBounded checks that finding the earliest bad
// value in a deep document costs a bounded number of decodes, measured
// against a decode of the same document with every value good. When
// goccy meets the bad values in reverse document order, each round of
// the search halves the prefixes it tries, and every prefix holds the
// whole chain of next keys, so that case costs about one decode for
// each bit of the unit count, and its bound is looser.
func TestInlineMapErrorCostIsBounded(t *testing.T) {
	if testing.Short() {
		t.Skip("decodes 1000-level documents")
	}
	const depth = 1000
	cases := []struct {
		name  string
		doc   func(int, string) []byte
		v     func() any
		line  int
		times time.Duration
	}{
		{"errors in reverse order, x first", xThenNext, func() any { return &nextFirst{} }, 1, 8},
		{"errors in reverse order, next first", nextThenX, func() any { return &xFirst{} }, depth + 1, 32},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			start := time.Now()
			if err := yaml.Unmarshal(c.doc(depth, "1"), c.v()); err != nil {
				t.Fatal(err)
			}
			once := time.Since(start)
			start = time.Now()
			err := yaml.Unmarshal(c.doc(depth, "bad"), c.v())
			took := time.Since(start)
			var de *yaml.DecodeError
			if !errors.As(err, &de) || de.Kind != yaml.ErrorKindType || de.Line != c.line {
				t.Fatalf("%v, want a type error at line %d", err, c.line)
			}
			if took > c.times*once {
				t.Errorf("the error took %v, more than %d times the %v a good decode takes", took, c.times, once)
			}
		})
	}
}
