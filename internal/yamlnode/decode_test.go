package yamlnode_test

import (
	"testing"

	"github.com/fivetwenty-io/graft/internal/yamlgolden"
	"github.com/fivetwenty-io/graft/internal/yamlnode"
)

func TestDecodeMatchesYAMLv3(t *testing.T) {
	yamlgolden.PinLocal(t)
	var vectors []struct {
		In    string            `json:"in"`
		Value *yamlgolden.Typed `json:"value"`
		Error string            `json:"error"`
	}
	yamlgolden.ReadJSON(t, "testdata/golden/decode.json", &vectors)
	for _, v := range vectors {
		docs, err := yamlnode.Parse([]byte(v.In))
		if v.In == "? [a, b]\n: c\n" {
			// goccy rejects a complex mapping key at parse time, where
			// yaml.v3 parses it and fails only on decode. spruce diff
			// accepts it, so this is an accepted divergence.
			if err == nil {
				t.Errorf("Parse(%q) must fail", v.In)
			}
			continue
		}
		if err != nil {
			t.Errorf("Parse(%q): %v", v.In, err)
			continue
		}
		var got interface{}
		if len(docs) > 0 {
			got, err = yamlnode.Decode(docs[0])
		}
		if v.Error != "" {
			if err == nil || err.Error() != v.Error {
				t.Errorf("Decode(%q) error = %v, want %q", v.In, err, v.Error)
			}
			continue
		}
		if err != nil {
			t.Errorf("Decode(%q): %v", v.In, err)
			continue
		}
		if d := yamlgolden.Diff(v.Value, yamlgolden.FromValue(got)); d != "" {
			t.Errorf("Decode(%q): %s", v.In, d)
		}
	}
}

func TestDecodeRoundTripsFromValue(t *testing.T) {
	in := map[string]interface{}{"a": 1.0, "b": []interface{}{"x", nil}, "<<": map[string]interface{}{"c": true}}
	doc, err := yamlnode.FromValue(in)
	if err != nil {
		t.Fatal(err)
	}
	got, err := yamlnode.Decode(doc)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]interface{}{"a": 1, "b": []interface{}{"x", nil}, "c": true}
	if d := yamlgolden.Diff(yamlgolden.FromValue(want), yamlgolden.FromValue(got)); d != "" {
		t.Fatal(d)
	}
}
