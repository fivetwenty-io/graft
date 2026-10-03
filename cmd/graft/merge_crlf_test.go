package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeCRLFFile writes text to name in a fresh directory and returns its
// path.
func writeCRLFFile(t *testing.T, name, text string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestMergeReadsCRLFQuotedScalarAsSpruceDoes runs `graft merge` on a
// file with CRLF line breaks whose quoted scalar folds across two lines.
// spruce v1.35.17 reads the value "q r s", and so does the LF copy of the
// file. graft folded the CRLF into "q\nr s" and exited 0.
func TestMergeReadsCRLFQuotedScalarAsSpruceDoes(t *testing.T) {
	path := writeCRLFFile(t, "q.yml", "k: \"q\r\n  r s\"\r\n")
	stdout, stderr, rc := runGraftCommand(t, []string{"merge", path})
	if rc != 0 || stdout != "---\nk: q r s\n\n" || stderr != "" {
		t.Errorf("graft merge: rc=%d stdout=%q stderr=%q, want rc=0 and stdout %q", rc, stdout, stderr, "---\nk: q r s\n\n")
	}
}

// TestMergeReadsCRLFFileThroughLoadAndGoPatch runs `graft merge` on a
// file that loads a CRLF file with (( load )), and on a base file with a
// CRLF go-patch operation file. Each gives what the LF copy of the CRLF
// file gives.
func TestMergeReadsCRLFFileThroughLoadAndGoPatch(t *testing.T) {
	dir := t.TempDir()
	loadee := filepath.Join(dir, "loadee.yml")
	loader := filepath.Join(dir, "loader.yml")
	base := filepath.Join(dir, "base.yml")
	ops := filepath.Join(dir, "ops.yml")
	for path, text := range map[string]string{
		loadee: "k: \"q\r\n  r s\"\r\nl: |\r\n  x\r\n  y\r\n",
		loader: "v: (( load \"" + loadee + "\" ))\n",
		base:   "k: old\nm: old\n",
		ops:    "- type: replace\r\n  path: /k\r\n  value: \"q\r\n    r s\"\r\n- type: replace\r\n  path: /m\r\n  value: |\r\n    x\r\n    y\r\n",
	} {
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct {
		name string
		args []string
		want string
	}{
		{"load", []string{"merge", loader}, "---\nv:\n  k: q r s\n  l: |\n    x\n    y\n\n"},
		{"go-patch", []string{"merge", "--go-patch", base, ops}, "---\nk: q r s\nm: |\n  x\n  y\n\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			stdout, stderr, rc := runGraftCommand(t, c.args)
			if rc != 0 || stdout != c.want || stderr != "" {
				t.Errorf("graft %s: rc=%d stdout=%q stderr=%q, want rc=0 and stdout %q", strings.Join(c.args, " "), rc, stdout, stderr, c.want)
			}
		})
	}
}

// TestMergeParseErrorSnippetOfCRLFFileHasNoCR runs `graft merge` on a
// CRLF file with an unclosed flow sequence. The snippet under the error
// printed each line's CR, which a terminal shows as ^M, and must match
// the snippet of the LF copy.
func TestMergeParseErrorSnippetOfCRLFFileHasNoCR(t *testing.T) {
	crlf := "a: 1\r\nb: 2\r\nc: [1, 2\r\nd: 3\r\n"
	stderr := map[bool]string{}
	for _, withCR := range []bool{true, false} {
		text := crlf
		if !withCR {
			text = strings.ReplaceAll(crlf, "\r\n", "\n")
		}
		path := writeCRLFFile(t, "bad.yml", text)
		errText, rc := runGraftCapturingOutput(t, []string{"merge", path})
		if rc != 2 {
			t.Fatalf("graft merge of %q: rc=%d, want rc=2", text, rc)
		}
		stderr[withCR] = strings.ReplaceAll(errText, path, "FILE")
	}
	if strings.Contains(stderr[true], "\r") {
		t.Errorf("stderr for the CRLF file holds a CR: %q", stderr[true])
	}
	if stderr[true] != stderr[false] {
		t.Errorf("stderr for the CRLF file = %q, want the LF copy's %q", stderr[true], stderr[false])
	}
}
