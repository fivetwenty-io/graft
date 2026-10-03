package operators

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// TestLoadRejectsCyclicAndOverDeepFiles loads files that spruce refuses.
// A self-containing anchor fails with "anchor 'a' value contains
// itself", as it does in a merge input. Nesting past 10,000 levels fails
// with the recursion text a merge gives for any tree nested past 4,096
// levels, before goccy's parser sees the file.
func TestLoadRejectsCyclicAndOverDeepFiles(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct{ name, content, want string }{
		{"cyclic", "a: &a\n  b: *a\n", "anchor 'a' value contains itself"},
		{"cyclic after a directive", "%YAML 1.1\n---\na: &a\n  b: *a\n", "anchor 'a' value contains itself"},
		{"deep", "a: " + strings.Repeat("[", 10001) + strings.Repeat("]", 10001) + "\n", "Hit max recursion depth. You seem to have a self-referencing dataset"},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(dir, c.name+".yml")
			if err := os.WriteFile(path, []byte(c.content), 0o600); err != nil {
				t.Fatal(err)
			}
			engine, err := graft.NewEngine()
			if err != nil {
				t.Fatal(err)
			}
			doc, err := engine.ParseYAML([]byte("result: (( load \"" + path + "\" ))\n"))
			if err != nil {
				t.Fatal(err)
			}
			_, err = engine.Evaluate(context.TODO(), doc)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("load of %s = %v, want an error containing %q", c.name, err, c.want)
			}
		})
	}
}

// TestLoadReadsOnlyTheFirstDocument loads a file whose second document
// has a syntax error. spruce's load reads only the first document, so
// the load succeeds.
func TestLoadReadsOnlyTheFirstDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "two.yml")
	if err := os.WriteFile(path, []byte("x: 1\n---\ny: [\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	doc, err := engine.ParseYAML([]byte("result: (( load \"" + path + "\" ))\n"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := engine.Evaluate(context.TODO(), doc)
	if err != nil {
		t.Fatalf("load = %v, want success", err)
	}
	if got, _ := out.GetInt("result.x"); got != 1 {
		t.Errorf("result.x = %v, want 1", got)
	}
}

// TestLoadKeepsDirectivesWithFirstDocument loads a file that opens with
// a directive and holds a second document. The directive belongs to the
// first document, so spruce's load gives that document's content.
func TestLoadKeepsDirectivesWithFirstDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "directive.yml")
	if err := os.WriteFile(path, []byte("%YAML 1.1\n---\nx: 1\n---\ny: 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine, err := graft.NewEngine()
	if err != nil {
		t.Fatal(err)
	}
	doc, err := engine.ParseYAML([]byte("result: (( load \"" + path + "\" ))\n"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := engine.Evaluate(context.TODO(), doc)
	if err != nil {
		t.Fatalf("load = %v, want success", err)
	}
	if got := out.RawData(); !reflect.DeepEqual(got, map[string]interface{}{"result": map[string]interface{}{"x": 1}}) {
		t.Errorf("load = %#v, want result.x: 1 alone", got)
	}
}
