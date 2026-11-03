// Package yamlparity checks that pkg/yaml reads a document the way graft's
// merge engine does. It lives outside pkg/yaml so that pkg/yaml never has
// to import pkg/graft.
package yamlparity

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
	"github.com/fivetwenty-io/graft/pkg/yaml"
)

const fixtureGlob = "../spruce-compat/fixtures/*"

// notComparable names the fixtures that cannot be compared as a map, with
// the reason for each. Every other fixture must decode to the same value
// through both routes.
var notComparable = map[string]string{
	"gopatch-ops.yml": "the root is a sequence, which the engine refuses and Unmarshal refuses for a map target",
}

// refusedByBoth names the fixtures that both routes must refuse, as
// spruce does, each with the text the error must never quote.
var refusedByBoth = map[string]string{
	"explicit-tags-int-word.yml":   "S3KRIT",
	"explicit-tags-bad-base64.yml": "S3KRIT",
}

func TestUnmarshalMatchesEngineParseYAML(t *testing.T) {
	t.Parallel()
	files, err := filepath.Glob(fixtureGlob)
	if err != nil || len(files) == 0 {
		t.Fatalf("no spruce-compat fixtures found: %v", err)
	}
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	for _, file := range files {
		name := filepath.Base(file)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			data, err := os.ReadFile(file)
			if err != nil {
				t.Fatal(err)
			}
			if reason, ok := notComparable[name]; ok {
				assertAgreedOutcome(t, engine, data, reason)
				return
			}
			if secret, ok := refusedByBoth[name]; ok {
				assertBothRefuse(t, engine, data, secret)
				return
			}
			doc, err := engine.ParseYAML(data)
			if err != nil {
				t.Fatalf("engine.ParseYAML: %v", err)
			}
			var got map[string]any
			if err := yaml.Unmarshal(data, &got); err != nil {
				t.Fatalf("yaml.Unmarshal: %v", err)
			}
			want, ok := doc.GetData().(map[string]any)
			if !ok {
				t.Fatalf("engine data is %T, want map[string]any", doc.GetData())
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Unmarshal and ParseYAML disagree\nUnmarshal: %#v\nParseYAML: %#v", got, want)
			}
		})
	}
}

// assertBothRefuse checks that the engine and Unmarshal both refuse
// data, and that neither error quotes secret.
func assertBothRefuse(t *testing.T, engine graft.Engine, data []byte, secret string) {
	t.Helper()
	_, engineErr := engine.ParseYAML(data)
	var got any
	unmarshalErr := yaml.Unmarshal(data, &got)
	for route, err := range map[string]error{"engine.ParseYAML": engineErr, "yaml.Unmarshal": unmarshalErr} {
		switch {
		case err == nil:
			t.Errorf("%s read the document", route)
		case strings.Contains(err.Error(), secret):
			t.Errorf("%s error %q quotes the scalar", route, err)
		}
	}
}

// assertAgreedOutcome pins what happens to a fixture that cannot be
// compared as a map. The engine refuses it, Unmarshal into a map refuses
// it with a type error, and Unmarshal into any reads the sequence.
func assertAgreedOutcome(t *testing.T, engine graft.Engine, data []byte, reason string) {
	t.Helper()
	t.Logf("not compared as a map: %s", reason)
	if _, err := engine.ParseYAML(data); err == nil {
		t.Error("engine.ParseYAML accepted the document, want a refusal")
	}
	var m map[string]any
	err := yaml.Unmarshal(data, &m)
	var de *yaml.DecodeError
	if !errors.As(err, &de) || de.Kind != yaml.ErrorKindType {
		t.Errorf("Unmarshal into a map gave %v, want an ErrorKindType DecodeError", err)
	}
	var generic any
	if err := yaml.Unmarshal(data, &generic); err != nil {
		t.Fatalf("Unmarshal into any: %v", err)
	}
	if _, ok := generic.([]any); !ok {
		t.Errorf("Unmarshal into any gave %T, want []any", generic)
	}
}

// TestEveryFixtureIsAccountedFor fails when notComparable names a fixture
// that no longer exists, so a rename cannot turn an exclusion into a
// silent gap.
func TestEveryFixtureIsAccountedFor(t *testing.T) {
	t.Parallel()
	for name := range notComparable {
		if _, err := os.Stat(filepath.Join("..", "spruce-compat", "fixtures", name)); err != nil {
			t.Errorf("notComparable names %s: %v", name, err)
		}
	}
}

// yaml11Documents hold the YAML 1.1 spellings that the spruce-compat
// fixtures barely use. Each must decode alike through both routes.
var yaml11Documents = map[string]string{
	"booleans":                    "a: yes\nb: No\nc: ON\nd: off\ne: 'yes'\nf: \"on\"\ng: y\nh: oFF\ni: true\n",
	"nulls":                       "a: ~\nb: null\nc: Null\nd:\ne: ''\n",
	"integers":                    "octal: 0755\nhex: 0x1F\nunderscore: 1_000\nplain: 42\nneg: -7\nbig: 9223372036854775807\n",
	"floats":                      "a: 1.5\nb: 1e3\nc: 1.10\nd: .inf\ne: 3.0\n",
	"timestamps":                  "a: 2001-12-14\nb: 2001-12-14T21:59:43.10-05:00\nc: 2001-12-14 21:59:43.10 -5\nd: '2001-12-14'\n",
	"merge keys":                  "base: &b {x: 1, y: 2}\nuse:\n  <<: *b\n  y: 3\nlate:\n  y: 4\n  <<: *b\n",
	"merge list":                  "a: &a {n: a, p: 1}\nb: &b {n: b, p: 2}\nx:\n  <<: [*a, *b]\n",
	"anchors":                     "list: &l [1, 2]\ncopy: *l\nstr: &s hello\nagain: *s\n",
	"tagged scalars":              "a: !!str 123\nb: !!int '42'\nc: !!float 1.0\nd: !!null ''\ne: !!str yes\nf: !!bool on\ng: !!int 0x1F\nh: !!binary aGk=\n",
	"float tag over integer text": "a: !!float 3\nb: !!float -7\nc: !!float 0x1F\nd: !!float '3'\ne: !!float 1_000\n",
	"block scalars":               "lit: |\n  one\n  two\nfold: >-\n  one\n  two\n",
	"quoted escapes":              "a: \"tab\\there\"\nb: 'it''s'\nc: \"\\u00e9\"\n",
	"non-string keys":             "1: one\ntrue: yes\n~: nil\n1.5: float\n",
}

func TestUnmarshalMatchesEngineOnYAML11Spellings(t *testing.T) {
	t.Parallel()
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	for name, src := range yaml11Documents {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			doc, err := engine.ParseYAML([]byte(src))
			if err != nil {
				t.Fatalf("engine.ParseYAML: %v", err)
			}
			var got map[string]any
			if err := yaml.Unmarshal([]byte(src), &got); err != nil {
				t.Fatalf("yaml.Unmarshal: %v", err)
			}
			want, ok := doc.GetData().(map[string]any)
			if !ok {
				t.Fatalf("engine data is %T, want map[string]any", doc.GetData())
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("Unmarshal and ParseYAML disagree\nUnmarshal: %#v\nParseYAML: %#v", got, want)
			}
		})
	}
}
