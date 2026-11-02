package graft

import "testing"

// TestMergeReadsKeyAfterEmptyTag pins what graft merge prints for a
// null key on the line after a tag with no value. spruce v1.35.17 prints
// null: 1 and a: "" for each document. graft's string-keyed maps print
// that key as "null", and graft sorts it among the other keys, where
// spruce writes a null key first.
func TestMergeReadsKeyAfterEmptyTag(t *testing.T) {
	for _, doc := range []string{
		"a: !foo\n~: 1\n",
		"a: !foo\nNull: 1\n",
		"a: &n !foo\n~: 1\n",
	} {
		t.Run(doc, func(t *testing.T) {
			parsed, err := NewDefaultEngine().ParseYAML([]byte(doc))
			if err != nil {
				t.Fatalf("ParseYAML: %v", err)
			}
			out, err := parsed.ToYAML()
			if err != nil {
				t.Fatalf("ToYAML: %v", err)
			}
			if want := "a: \"\"\n\"null\": 1\n"; string(out) != want {
				t.Errorf("merge printed %q, want %q", out, want)
			}
		})
	}
}
