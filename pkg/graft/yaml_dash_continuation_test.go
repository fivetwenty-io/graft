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

// TestMergeReadsDashScalarOnItsOwnLine pins what graft merge prints for
// a plain scalar that starts with "-" on a line of its own, under a key,
// a dash, or a flow opener. spruce v1.35.17 reads each as the number it
// reads on the key's line, and prints it bare, as graft now does. A key
// such as -0.0 on its own line reads as it does on the line before.
func TestMergeReadsDashScalarOnItsOwnLine(t *testing.T) {
	toYAML := func(t *testing.T, doc string) string {
		t.Helper()
		parsed, err := NewDefaultEngine().ParseYAML([]byte(doc))
		if err != nil {
			t.Fatalf("ParseYAML(%q): %v", doc, err)
		}
		out, err := parsed.ToYAML()
		if err != nil {
			t.Fatalf("ToYAML: %v", err)
		}
		return string(out)
	}
	for _, c := range []struct{ doc, want string }{
		{"a:\n  -1\n", "a: -1\n"},
		{"a:\n  -1.5\n", "a: -1.5\n"},
		{"a:\n  -.inf\n", "a: -.inf\n"},
		{"a:\n  -0x1\n", "a: -1\n"},
		{"a:\n\n  -1 #c\n", "a: -1\n"},
		{"l:\n- \n  -1\n", "l:\n- -1\n"},
		{"a: [\n  -1]\n", "a:\n- -1\n"},
		{"a: &x\n  -1\nb: *x\n", "a: -1\nb: -1\n"},
		{"a:\n  -1\n  -2\n", "a: -1 -2\n"},
	} {
		t.Run(c.doc, func(t *testing.T) {
			if got := toYAML(t, c.doc); got != c.want {
				t.Errorf("merge printed %q, want %q", got, c.want)
			}
		})
	}
	for _, c := range []struct{ own, inline string }{
		{"x:\n  -0.0: q\n", "x: {-0.0: q}\n"},
		{"x:\n  -1: q\n  -1.5: r\n", "x: {-1: q, -1.5: r}\n"},
		{"x:\n  -.inf: q\n", "x: {-.inf: q}\n"},
	} {
		t.Run(c.own, func(t *testing.T) {
			if got, want := toYAML(t, c.own), toYAML(t, c.inline); got != want {
				t.Errorf("merge printed %q, want %q", got, want)
			}
		})
	}
}
