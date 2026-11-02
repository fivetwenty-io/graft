package yaml_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

// fakeSecret stands in for a credential. It is a scalar in every document
// the leak fuzzer builds, and no error may print it. It is never an
// anchor name, because a DecodeError does not promise to hide those.
const fakeSecret = "hunter2-TOP-SECRET-9f3a"

// leakShapes wrap the secret in valid and broken documents.
var leakShapes = []string{
	"key: %s\n",
	"- %s\n",
	"key: [%s, other]\n",
	"key: {inner: %s}\n",
	"key: \"%s\"\n",
	"key: '%s'\n",
	"%s: value\n",
	"\"%s\": value\n",
	"key: |\n  %s\n",
	"key: !!int %s\n",
	"key: !!timestamp %s\n",
	"key: !!binary %s\n",
	"key: %s\n  bad: indent\n",
	"key: [%s\n",
	"key: \"%s\n",
	"key: {inner: %s\n",
	"key: %s: nested\n",
	"? [%s]\n: value\n",
	"a: &a [1]\nb: *a\nc: %s\n\t- tab\n",
	"base: &b {x: 1}\nkey:\n  <<: *b\n  y: %s\n",
	"a: %s\na: %s\n",
	"key: %s\n---\nsecond: %s\n",
	"%s\n",
	"- - - %s\n  - [\n",
}

// leakTargets are decoded into for every document, so the type errors of
// each route are tried as well as the syntax errors.
func leakTargets() []any {
	type inner struct {
		Key  int      `yaml:"key"`
		List []int    `yaml:"list"`
		Flag bool     `yaml:"flag"`
		Name []string `yaml:"name"`
	}
	return []any{
		new(any),
		new(map[string]any),
		new(map[string]int),
		new([]any),
		new([]int),
		new(string),
		new(inner),
		new(yaml.Node),
	}
}

func FuzzUnmarshalNoLeak(f *testing.F) {
	for i := range leakShapes {
		f.Add(uint8(i), "", "", false)
	}
	f.Add(uint8(0), "# c\n", "x: [\n", true)
	f.Add(uint8(3), "---\n", "\n...\n", false)
	f.Add(uint8(10), "a: 1\n", "b: 2", true)

	f.Fuzz(func(t *testing.T, shape uint8, prefix, suffix string, yaml12 bool) {
		if strings.Contains(prefix+suffix, fakeSecret) {
			t.Skip("the fuzzer wrote the secret itself")
		}
		body := strings.ReplaceAll(leakShapes[int(shape)%len(leakShapes)], "%s", fakeSecret)
		doc := []byte(prefix + body + suffix)
		for _, target := range leakTargets() {
			err := yaml.UnmarshalWith(doc, target, yaml.DecodeOptions{YAML12: yaml12})
			checkNoLeak(t, doc, err)
		}
		var node yaml.Node
		if err := yaml.Unmarshal(doc, &node); err == nil {
			for _, target := range leakTargets() {
				checkNoLeak(t, doc, node.Decode(target))
			}
		}
	})
}

func checkNoLeak(t *testing.T, doc []byte, err error) {
	t.Helper()
	if err == nil {
		return
	}
	var de *yaml.DecodeError
	if !errors.As(err, &de) {
		t.Fatalf("error is %T, want *DecodeError, for %q", err, doc)
	}
	if strings.Contains(err.Error(), fakeSecret) || strings.Contains(de.Msg, fakeSecret) {
		t.Fatalf("error quotes the secret: %q\ndocument: %q", err.Error(), doc)
	}
}

// FuzzMarshalRoundTrip turns the fuzzer's seed into a value with the
// generator the round-trip tests use, writes it, reads it back, and
// expects an equal value.
func FuzzMarshalRoundTrip(f *testing.F) {
	for _, seed := range []uint64{0, 1, 2, 3, 7, 42, 1 << 40, 1<<64 - 1} {
		f.Add(seed, false)
		f.Add(seed, true)
	}
	f.Fuzz(func(t *testing.T, seed uint64, lexical bool) {
		v := genValue(newPRNG(seed), 3)
		opts := yaml.EncodeOptions{}
		if lexical {
			opts.KeyOrder = "lexical"
		}
		out, err := yaml.MarshalWith(v, opts)
		if err != nil {
			t.Fatalf("MarshalWith(%#v): %v", v, err)
		}
		var got any
		if err := yaml.Unmarshal(out, &got); err != nil {
			t.Fatalf("Unmarshal(%q): %v", out, err)
		}
		if !sameValue(got, want(v)) {
			t.Fatalf("%#v\nwrote %q\nread  %#v", v, out, got)
		}
	})
}
