package yamlcodec_test

import (
	"strings"
	"testing"
)

// TestSecondTagErrorNamesItsLine pins the line of the error for a node
// with two tags, which is the line of the second tag, counted from 1,
// wherever the node sits. The error quotes nothing of the document.
func TestSecondTagErrorNamesItsLine(t *testing.T) {
	cases := []struct {
		doc  string
		line string
	}{
		{"!x !y\n", "1"},
		{"a: !x !y\nb: 1\n", "1"},
		{"a:\n  b: !x !y\n", "2"},
		{"a:\n  b: !x !y\n  c: d\n", "2"},
		{"k: v\nl:\n  - !x !y\n", "3"},
		{"a:\n  b: !!str !!int S3KRIT\n", "2"},
	}
	for _, c := range cases {
		t.Run(c.doc, func(t *testing.T) {
			_, err := decodeMerge(t, c.doc)
			want := "yaml: line " + c.line + ": found a second tag on one node"
			if err == nil || err.Error() != want {
				t.Fatalf("error = %v, want %q", err, want)
			}
			if strings.Contains(err.Error(), "S3KRIT") {
				t.Fatal("error quotes the value")
			}
		})
	}
}
