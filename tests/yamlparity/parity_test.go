// Package yamlparity checks that pkg/yaml reads a document the way graft's
// merge engine does. It lives outside pkg/yaml so that pkg/yaml never has
// to import pkg/graft.
package yamlparity

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
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
