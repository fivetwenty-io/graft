package yaml_test

import (
	"errors"
	"strings"
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
