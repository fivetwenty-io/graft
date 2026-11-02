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

// loadFile writes content to a file, loads it into key result of a
// merge, and returns the evaluated document or the evaluation's error.
func loadFile(t *testing.T, content string) (graft.Document, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "loaded.yml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
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
	return engine.Evaluate(context.TODO(), doc)
}

// TestLoadResolvesExplicitTags loads files whose scalars carry explicit
// tags. The loaded file reads its tags the way a merge input does, and
// every expectation is what real spruce v1.35.17 printed.
func TestLoadResolvesExplicitTags(t *testing.T) {
	out, err := loadFile(t, "d: !!str yes\ne: !!int 0x1f\nf: !!binary aGk=\ng: !foo off\nh: !!timestamp 2001-12-14\ni: !!bool on\nj: &x !!str on\nk: *x\n")
	if err != nil {
		t.Fatalf("load = %v", err)
	}
	want := map[string]any{"d": "yes", "e": 31, "f": "hi", "g": "off", "h": "2001-12-14", "i": true, "j": "on", "k": "on"}
	for key, w := range want {
		got, err := out.Get("result." + key)
		if err != nil {
			t.Errorf("result.%s: %v", key, err)
			continue
		}
		if n, ok := got.(int64); ok {
			got = int(n)
		}
		if !reflect.DeepEqual(got, w) {
			t.Errorf("result.%s = %#v, want %#v", key, got, w)
		}
	}

	out, err = loadFile(t, "- !!str on\n- !!bool on\n")
	if err != nil {
		t.Fatalf("load of a list = %v", err)
	}
	got, err := out.Get("result")
	if err != nil {
		t.Fatal(err)
	}
	if want := []any{"on", true}; !reflect.DeepEqual(got, want) {
		t.Errorf("result = %#v, want %#v", got, want)
	}
}

// TestLoadRefusesTagsThatCannotHoldTheirText loads files spruce refuses,
// because a tag cannot hold its text. The error names the tag but never
// quotes the text.
func TestLoadRefusesTagsThatCannotHoldTheirText(t *testing.T) {
	for _, c := range []struct{ content, tag string }{
		{"b: !!int S3KRIT\n", "!!int"},
		{"b: !!bool S3KRIT\n", "!!bool"},
		{"- !!float S3KRIT\n", "!!float"},
	} {
		t.Run(c.content, func(t *testing.T) {
			_, err := loadFile(t, c.content)
			if err == nil {
				t.Fatal("load succeeded, want an error")
			}
			if msg := err.Error(); !strings.Contains(msg, c.tag) || strings.Contains(msg, "S3KRIT") {
				t.Errorf("load error %q, want one naming %s without the text", msg, c.tag)
			}
		})
	}
}

// TestLoadReadsBoolWordsAsMergeDoes loads a file of untagged YAML 1.1
// boolean words. A loaded file reads them the way a merge input does, so
// a plain yes, No, ON, or off is a boolean, while a quoted word, a !!str
// word, and a block scalar stay strings. Every expectation is what real
// spruce v1.35.17 printed, apart from y and n, which spruce reads as
// booleans and graft merge keeps as words, and which a load reads as
// merge does.
func TestLoadReadsBoolWordsAsMergeDoes(t *testing.T) {
	out, err := loadFile(t, "a: yes\nb: No\nc: ON\nd: off\ne: y\nf: n\nh: \"yes\"\ni: !!str on\nj: |-\n  off\nk: [yes, \"no\"]\nl: &x On\nm: *x\nn: true\n")
	if err != nil {
		t.Fatalf("load = %v", err)
	}
	want := map[string]any{
		"a": true, "b": false, "c": true, "d": false, "e": "y", "f": "n",
		"h": "yes", "i": "on", "j": "off", "k": []any{true, "no"}, "l": true, "m": true, "n": true,
	}
	for key, w := range want {
		got, err := out.Get("result." + key)
		if err != nil {
			t.Errorf("result.%s: %v", key, err)
			continue
		}
		if !reflect.DeepEqual(got, w) {
			t.Errorf("result.%s = %#v, want %#v", key, got, w)
		}
	}

	out, err = loadFile(t, "- yes\n- \"on\"\n- Off\n")
	if err != nil {
		t.Fatalf("load of a list = %v", err)
	}
	got, err := out.Get("result")
	if err != nil {
		t.Fatal(err)
	}
	if want := []any{true, "on", false}; !reflect.DeepEqual(got, want) {
		t.Errorf("result = %#v, want %#v", got, want)
	}
}
