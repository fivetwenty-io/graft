package graft_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// Every Context method treats a nil ctx as context.Background, as
// Evaluate does, in both modes.
func TestContextMethodsTreatNilAsBackground(t *testing.T) {
	var nilCtx context.Context
	src := "a: 1\nb: (( grab a ))\n"
	path := filepath.Join(t.TempDir(), "in.yml")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	type call func(graft.Engine, graft.Document) (string, error)
	encoded := func(out []byte, err error) (string, error) {
		return string(out), err
	}
	parsed := func(doc graft.Document, err error) (string, error) {
		if err != nil {
			return "", err
		}
		return fmt.Sprint(doc.RawData()), nil
	}
	calls := map[string]call{
		"ParseYAMLContext": func(e graft.Engine, _ graft.Document) (string, error) {
			return parsed(e.ParseYAMLContext(nilCtx, []byte(src)))
		},
		"ParseJSONContext": func(e graft.Engine, _ graft.Document) (string, error) {
			return parsed(e.ParseJSONContext(nilCtx, []byte(`{"a": 1}`)))
		},
		"ParseFileContext": func(e graft.Engine, _ graft.Document) (string, error) {
			return parsed(e.ParseFileContext(nilCtx, path))
		},
		"ParseReaderContext": func(e graft.Engine, _ graft.Document) (string, error) {
			return parsed(e.ParseReaderContext(nilCtx, strings.NewReader(src)))
		},
		"DiffContext": func(e graft.Engine, doc graft.Document) (string, error) {
			r, err := e.DiffContext(nilCtx, doc, doc.Clone(), nil)
			if err != nil {
				return "", err
			}
			return fmt.Sprint(r.HasChanges()), nil
		},
		"ToYAMLContext": func(e graft.Engine, doc graft.Document) (string, error) {
			return encoded(e.ToYAMLContext(nilCtx, doc))
		},
		"ToJSONContext": func(e graft.Engine, doc graft.Document) (string, error) {
			return encoded(e.ToJSONContext(nilCtx, doc))
		},
		"ToJSONIndentContext": func(e graft.Engine, doc graft.Document) (string, error) {
			return encoded(e.ToJSONIndentContext(nilCtx, doc, " "))
		},
	}
	want := map[string]string{
		"ParseYAMLContext":    "map[a:1 b:(( grab a ))]",
		"ParseJSONContext":    "map[a:1]",
		"ParseFileContext":    "map[a:1 b:(( grab a ))]",
		"ParseReaderContext":  "map[a:1 b:(( grab a ))]",
		"DiffContext":         "false",
		"ToYAMLContext":       "a: 1\nb: 1\n",
		"ToJSONContext":       `{"a":1,"b":1}`,
		"ToJSONIndentContext": "{\n \"a\": 1,\n \"b\": 1\n}",
	}
	for name, run := range calls {
		for _, parallel := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/parallel=%v", name, parallel), func(t *testing.T) {
				e := markerEngine(t, parallel)
				doc := markerParse(t, e, src)
				var got string
				var err error
				func() {
					defer func() {
						if r := recover(); r != nil {
							t.Fatalf("panic on a nil ctx: %v", r)
						}
					}()
					got, err = run(e, doc)
				}()
				if err != nil {
					t.Fatalf("err = %v", err)
				}
				if strings.TrimSpace(got) != strings.TrimSpace(want[name]) {
					t.Fatalf("got %q, want %q", got, want[name])
				}
			})
		}
	}
}
