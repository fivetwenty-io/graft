package graft

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cppforlife/go-patch/patch"
)

// crlfScalarCases are quoted scalars that break across lines, with the
// value spruce v1.35.17 reads from each one. goccy folds a quoted scalar
// correctly only across LF breaks, so graft used to read the CRLF form of
// each into a different value than its LF copy, and exit 0.
var crlfScalarCases = []struct {
	name, in, want string
}{
	{"double-quoted fold", "k: \"q\r\n  r s\"\r\n", "q r s"},
	{"single-quoted fold", "k: 'q\r\n  r s'\r\n", "q r s"},
	{"double-quoted blank line", "k: \"a\r\n\r\n  b\"\r\n", "a\nb"},
	{"double-quoted two blank lines", "k: \"a\r\n\r\n\r\n  b\"\r\n", "a\n\nb"},
	{"single-quoted blank line", "k: 'a\r\n\r\n  b'\r\n", "a\nb"},
	{"escaped line break", "k: \"a\\\r\n  b\"\r\n", "ab"},
	{"escaped line break after a space", "k: \"a \\\r\n  b\"\r\n", "a b"},
}

// TestMergeReadsCRLFQuotedScalarsAsTheirLFCopy merges each CRLF scalar
// and checks that it reads as spruce reads it, and as its LF copy.
func TestMergeReadsCRLFQuotedScalarsAsTheirLFCopy(t *testing.T) {
	for _, c := range crlfScalarCases {
		t.Run(c.name, func(t *testing.T) {
			for _, in := range []string{c.in, strings.ReplaceAll(c.in, "\r\n", "\n")} {
				out, err := guardedMerge(t, in)
				if err != nil {
					t.Fatalf("merge of %q = %v, want success", in, err)
				}
				if got, want := out.RawData(), map[string]interface{}{"k": c.want}; !reflect.DeepEqual(got, want) {
					t.Errorf("merge of %q = %#v, want %#v", in, got, want)
				}
			}
		})
	}
}

// TestJSONReadsCRLFQuotedScalarsAsTheirLFCopy converts each CRLF scalar
// and checks that it reads as spruce's json reads it, and as its LF copy.
func TestJSONReadsCRLFQuotedScalarsAsTheirLFCopy(t *testing.T) {
	for _, c := range crlfScalarCases {
		t.Run(c.name, func(t *testing.T) {
			want := `{"k":"` + strings.ReplaceAll(c.want, "\n", `\n`) + `"}`
			for _, in := range []string{c.in, strings.ReplaceAll(c.in, "\r\n", "\n")} {
				got, err := jsonifyData([]byte(in), false)
				if err != nil || got != want {
					t.Errorf("jsonifyData of %q = %q, %v; want %q", in, got, err, want)
				}
			}
		})
	}
}

// TestJSONifyFilesReadsOnlyTheFirstDocumentOfACRLFFile converts a CRLF
// file with two documents. The file splits only at a "---" line between
// LF breaks, so the part keeps both documents, and spruce's json reads
// only the first of them.
func TestJSONifyFilesReadsOnlyTheFirstDocumentOfACRLFFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "two.yml")
	if err := os.WriteFile(path, []byte("a: \"q\r\n  r s\"\r\n---\r\nb: 2\r\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := JSONifyFiles([]string{path}, false)
	if want := []string{`{"a":"q r s"}`}; err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("JSONifyFiles = %q, %v; want %q", got, err, want)
	}
}

// TestGoPatchReadsCRLFQuotedScalarsAsTheirLFCopy parses a CRLF go-patch
// operation list whose value is a folded quoted scalar.
func TestGoPatchReadsCRLFQuotedScalarsAsTheirLFCopy(t *testing.T) {
	src := "- type: replace\r\n  path: /k\r\n  value: \"q\r\n    r s\"\r\n"
	for _, in := range []string{src, strings.ReplaceAll(src, "\r\n", "\n")} {
		ops, err := ParseGoPatch([]byte(in))
		if err != nil || len(ops) != 1 {
			t.Fatalf("ParseGoPatch of %q = %d operations, %v; want 1 operation", in, len(ops), err)
		}
		replace, ok := ops[0].(patch.ReplaceOp)
		if !ok || replace.Value != "q r s" {
			t.Errorf("ParseGoPatch of %q = %#v, want a replace of the value %q", in, ops[0], "q r s")
		}
	}
}

// TestLoneCRInsideAValue pins what a lone CR inside a value does against
// spruce v1.35.17. spruce reads the CR inside a quoted value as a line
// break that folds to a space, and fails a plain value that holds one.
func TestLoneCRInsideAValue(t *testing.T) {
	out, err := guardedMerge(t, "k: \"a\rb\"\n")
	if err != nil {
		t.Fatalf("merge of a lone CR in a quoted value = %v, want success", err)
	}
	if got, want := out.RawData(), map[string]interface{}{"k": "a b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("merge of a lone CR in a quoted value = %#v, want %#v", got, want)
	}
	if out, err := guardedMerge(t, "k: a\rb\n"); err == nil {
		t.Errorf("merge of a lone CR in a plain value = %#v, want the parse error spruce gives", out.RawData())
	}
}
