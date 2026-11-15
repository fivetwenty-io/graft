package yamlcodec

import "testing"

// TestMarshalYAMLQuotesLiteralMergeKeyName checks the untyped encoder,
// which graft merge uses, quotes a key that is the string "<<".
func TestMarshalYAMLQuotesLiteralMergeKeyName(t *testing.T) {
	out, err := MarshalYAML(map[string]interface{}{"a": map[string]interface{}{"<<": "s", "b": 1}})
	if err != nil {
		t.Fatal(err)
	}
	want := "a:\n  \"<<\": s\n  b: 1\n"
	if string(out) != want {
		t.Errorf("MarshalYAML = %q, want %q", out, want)
	}
}
