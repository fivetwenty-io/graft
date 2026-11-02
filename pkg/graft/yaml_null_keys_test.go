package graft

import (
	"strings"
	"testing"
)

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

// TestMergeRefusesNullKeyBesideNullText pins a null key and a string key
// whose text is null in one mapping. spruce v1.35.17 prints both, as
// null: 1 and "null": 2, and graft's string-keyed maps can hold only
// one, so graft refuses the document rather than drop a value.
func TestMergeRefusesNullKeyBesideNullText(t *testing.T) {
	for _, doc := range []string{
		"~: 1\n!!str null: 2\n",
		"!!str null: 1\n~: 2\n",
		"\"null\": 1\n~: 2\n",
	} {
		t.Run(doc, func(t *testing.T) {
			if _, err := NewDefaultEngine().ParseYAML([]byte(doc)); err == nil ||
				!strings.Contains(err.Error(), "mapping key already defined at [1:1]") {
				t.Fatalf("ParseYAML err = %v, want the repeated key refused", err)
			}
		})
	}
}

// TestMergeKeepsNullKeyBesideTildeText pins a null key beside a string
// key whose text is ~, which spruce v1.35.17 prints as null: keep-me and
// "~": 2. graft prints the null key as "null", as it prints any null
// key, and sorts it after "~", where spruce writes a null key first.
func TestMergeKeepsNullKeyBesideTildeText(t *testing.T) {
	for _, doc := range []string{"~: keep-me\n!!str ~: 2\n", "~: keep-me\n\"~\": 2\n"} {
		t.Run(doc, func(t *testing.T) {
			parsed, err := NewDefaultEngine().ParseYAML([]byte(doc))
			if err != nil {
				t.Fatalf("ParseYAML: %v", err)
			}
			out, err := parsed.ToYAML()
			if err != nil {
				t.Fatalf("ToYAML: %v", err)
			}
			if want := "\"~\": 2\n\"null\": keep-me\n"; string(out) != want {
				t.Errorf("merge printed %q, want %q", out, want)
			}
		})
	}
}
