package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMergeAndJSONRejectSelfContainingAnchor runs `graft merge` and
// `graft json` on a file whose alias sits inside the mapping its anchor
// names. spruce fails both with "anchor 'a' value contains itself" and
// exits 2. goccy decodes the alias as null, so graft used to print
// "b: null" and exit 0.
func TestMergeAndJSONRejectSelfContainingAnchor(t *testing.T) {
	cyclic := filepath.Join(t.TempDir(), "cyclic.yml")
	if err := os.WriteFile(cyclic, []byte("a: &a\n  b: *a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{"merge", "json"} {
		t.Run(cmd, func(t *testing.T) {
			stderr, rc := runGraftCapturingOutput(t, []string{cmd, cyclic})
			if rc != 2 || !strings.Contains(stderr, "anchor 'a' value contains itself") {
				t.Errorf("graft %s: rc=%d stderr=%q, want rc=2 and anchor 'a' value contains itself", cmd, rc, stderr)
			}
		})
	}
}

// TestMergeOverDeepPrintsOneMessage merges files nested 4,096 and 10,001
// levels deep. spruce prints the same bare recursion sentence for both
// and exits 2. graft printed it bare at 4,096 levels but prefixed the
// file name and "parse_error:" from 10,001 levels on, where the depth
// guard rather than the cycle check rejects the input.
func TestMergeOverDeepPrintsOneMessage(t *testing.T) {
	const want = "Hit max recursion depth. You seem to have a self-referencing dataset\n"
	dir := t.TempDir()
	for _, n := range []int{4096, 10001} {
		path := filepath.Join(dir, "deep.yml")
		text := "a: " + strings.Repeat("[", n) + strings.Repeat("]", n) + "\n"
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
		stderr, rc := runGraftCapturingOutput(t, []string{"merge", path})
		if rc != 2 || stderr != want {
			t.Errorf("graft merge of %d levels: rc=%d stderr=%q, want rc=2 and %q", n, rc, stderr, want)
		}
	}
}

// TestMergeIgnoresSyntaxErrorInLaterDocument merges a file whose second
// document has a syntax error. spruce reads only the first document and
// exits 0. graft parsed every document and exited 2.
func TestMergeIgnoresSyntaxErrorInLaterDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "two.yml")
	if err := os.WriteFile(path, []byte("x: 1\n---\ny: [\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stderr, rc := runGraftCapturingOutput(t, []string{"merge", path})
	if rc != 0 || stderr != "" {
		t.Errorf("graft merge: rc=%d stderr=%q, want rc=0 and no stderr", rc, stderr)
	}
}

// TestMergeAndJSONRejectContentAfterADocumentEnd runs `graft merge` and
// `graft json` on a file with content after a "..." and no "---" before
// it. spruce fails both with "yaml: line 2: did not find expected
// <document start>" and exits 2. graft printed x: 1, dropped q, and
// exited 0. A "---" after the "..." starts a document neither reads,
// and a directive after the "..." needs one.
func TestMergeAndJSONRejectContentAfterADocumentEnd(t *testing.T) {
	dir := t.TempDir()
	bare := filepath.Join(dir, "bare.yml")
	if err := os.WriteFile(bare, []byte("x: 1\n...\nq: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{"merge", "json"} {
		t.Run(cmd, func(t *testing.T) {
			stderr, rc := runGraftCapturingOutput(t, []string{cmd, bare})
			if rc != 2 || !strings.Contains(stderr, "yaml: line 2: did not find expected <document start>") {
				t.Errorf("graft %s: rc=%d stderr=%q, want rc=2 and yaml: line 2: did not find expected <document start>", cmd, rc, stderr)
			}
		})
	}
	// json reads each part between "\n---\n" lines on its own, so the
	// directive here ends its first part, with no "---" after it.
	directive := filepath.Join(dir, "directive.yml")
	if err := os.WriteFile(directive, []byte("x: 1\n...\n%TAG !e! tag:e.com,2000:\n---\nq: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if stderr, rc := runGraftCapturingOutput(t, []string{"json", directive}); rc != 2 || !strings.Contains(stderr, "yaml: line 3: did not find expected <document start>") {
		t.Errorf("graft json: rc=%d stderr=%q, want rc=2 and yaml: line 3: did not find expected <document start>", rc, stderr)
	}
	headed := filepath.Join(dir, "headed.yml")
	if err := os.WriteFile(headed, []byte("x: 1\n...\n---\ny: 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, rc := runGraftCommand(t, []string{"merge", headed})
	if rc != 0 || stdout != "---\nx: 1\n\n" || stderr != "" {
		t.Errorf("graft merge: rc=%d stdout=%q stderr=%q, want rc=0 and stdout %q", rc, stdout, stderr, "---\nx: 1\n\n")
	}
}

// TestMergeKeepsDirectivesWithFirstDocument merges a file that opens
// with a %YAML directive. spruce prints x: 1 and exits 0. graft gave the
// directive a document of its own and printed {}.
func TestMergeKeepsDirectivesWithFirstDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "directive.yml")
	if err := os.WriteFile(path, []byte("%YAML 1.1\n---\nx: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, rc := runGraftCommand(t, []string{"merge", path})
	if rc != 0 || stdout != "---\nx: 1\n\n" || stderr != "" {
		t.Errorf("graft merge: rc=%d stdout=%q stderr=%q, want rc=0 and stdout %q", rc, stdout, stderr, "---\nx: 1\n\n")
	}
}
