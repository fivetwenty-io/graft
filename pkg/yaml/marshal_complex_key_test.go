package yaml_test

import (
	goflag "flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/pkg/yaml"
)

var updateComplexKey = goflag.Bool("update-complex-key", false, "rewrite the complex-key goldens")

// TestComplexKeyPadding pins the "? key" form, which only a key longer
// than 1024 bytes reaches, across every indent, both sequence styles,
// and map, sequence, and nested values.
func TestComplexKeyPadding(t *testing.T) {
	key := strings.Repeat("k", 1100)
	values := map[string]any{
		"map":    map[string]any{"z": "zv", "a": 1},
		"seq":    []any{"a", map[string]any{"b": 1}},
		"nested": map[string]any{"y": []any{"a", []any{"b"}}},
	}
	for name, val := range values {
		for indent := 2; indent <= 9; indent++ {
			for _, seq := range []bool{false, true} {
				opts := yaml.EncodeOptions{Indent: indent, IndentSequence: seq}
				out, err := yaml.MarshalWith(map[string]any{key: val}, opts)
				if err != nil {
					t.Fatal(err)
				}
				var back map[string]any
				if err := yaml.Unmarshal(out, &back); err != nil {
					t.Fatalf("%s/%d/%v does not read back: %v", name, indent, seq, err)
				}
				golden := filepath.Join("testdata", "complex-key", fmt.Sprintf("%s-%d-%v.golden", name, indent, seq))
				shown := strings.ReplaceAll(string(out), key, "<KEY>")
				if *updateComplexKey {
					if err := os.MkdirAll(filepath.Dir(golden), 0o750); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(golden, []byte(shown), 0o600); err != nil {
						t.Fatal(err)
					}
					continue
				}
				want, err := os.ReadFile(golden)
				if err != nil {
					t.Fatal(err)
				}
				if shown != string(want) {
					t.Errorf("%s:\n got %q\nwant %q", golden, shown, want)
				}
			}
		}
	}
}
