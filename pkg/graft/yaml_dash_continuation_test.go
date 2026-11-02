package graft

import (
	"strings"
	"testing"
)

// TestMergeReadsDashContinuation pins what graft merge reads for a plain
// scalar continued on a line that starts with "-". spruce v1.35.17 reads
// a: "v - x" for each document, and refuses the ones with text after a
// comment or a mapping value in the scalar, as graft now does.
func TestMergeReadsDashContinuation(t *testing.T) {
	for _, doc := range []string{"a: v\n  - x #\tc\n", "a: v\n  - x\t\n", "a: v\n  - x #c\n"} {
		t.Run(doc, func(t *testing.T) {
			parsed, err := NewDefaultEngine().ParseYAML([]byte(doc))
			if err != nil {
				t.Fatalf("ParseYAML: %v", err)
			}
			out, err := parsed.ToYAML()
			if err != nil {
				t.Fatalf("ToYAML: %v", err)
			}
			if want := "a: v - x\n"; string(out) != want {
				t.Errorf("merge printed %q, want %q", out, want)
			}
		})
	}
	for _, c := range []struct{ doc, msg string }{
		{"a: s3cr3t\n  - x #c\n  y\n", "[3:3] a plain scalar cannot continue after a comment"},
		{"a: s3cr3t\n  - x: y\n", "[2:6] mapping values are not allowed in this context"},
	} {
		t.Run(c.doc, func(t *testing.T) {
			_, err := NewDefaultEngine().ParseYAML([]byte(c.doc))
			if err == nil {
				t.Fatalf("ParseYAML accepted the document")
			}
			// The message names the place and never quotes the input. The
			// source lines graft prints after it, as for any parse error,
			// are not part of the message.
			first, _, _ := strings.Cut(err.Error(), "\n")
			if !strings.HasSuffix(first, c.msg) || strings.Contains(first, "s3cr3t") {
				t.Errorf("ParseYAML message = %q, want it to end in %q", first, c.msg)
			}
		})
	}
}
