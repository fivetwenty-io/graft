package main

import (
	"os"
	"path/filepath"
	"strconv"
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

// TestMergeAndJSONRejectAScalarGoccyReadsAsADocumentEnd runs `graft
// merge` and `graft json` on a file whose "...#c" line yaml.v3 reads as
// a plain scalar that cannot follow a mapping. spruce fails both with
// "yaml: line 2: could not find expected ':'" and exits 2. goccy ended
// the document at the three dots, so graft printed a: 1, dropped q, and
// exited 0.
func TestMergeAndJSONRejectAScalarGoccyReadsAsADocumentEnd(t *testing.T) {
	path := filepath.Join(t.TempDir(), "false-end.yml")
	if err := os.WriteFile(path, []byte("a: 1\n...#c\nq: 1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{"merge", "json"} {
		t.Run(cmd, func(t *testing.T) {
			stderr, rc := runGraftCapturingOutput(t, []string{cmd, path})
			if rc != 2 || !strings.Contains(stderr, "yaml: line 2: could not find expected ':'") {
				t.Errorf("graft %s: rc=%d stderr=%q, want rc=2 and yaml: line 2: could not find expected ':'", cmd, rc, stderr)
			}
		})
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

// TestMergeExpandsAliasesInsideAnchoredValues runs `graft merge` on
// files with aliases to anchors defined inside an anchored collection.
// Each expected output is spruce v1.35.17's, after the "---" graft
// prints first. goccy decoded those aliases as null, so graft printed
// null in their place and exited 0.
func TestMergeExpandsAliasesInsideAnchoredValues(t *testing.T) {
	dir := t.TempDir()
	for _, c := range []struct{ name, in, want string }{
		{"redefined in a list", "a: &x [&x 1, *x]\n", "---\na:\n- 1\n- 1\n\n"},
		{"redefined in a mapping", "a: &x {b: &x 1, c: *x}\n", "---\na:\n  b: 1\n  c: 1\n\n"},
		{"nested anchor", "x: &o\n  p: &p v\n  q: *p\nr: *o\n", "---\nr:\n  p: v\n  q: v\nx:\n  p: v\n  q: v\n\n"},
		{"aliased list", "a: &x [&y 1, *y]\nb: *x\n", "---\na:\n- 1\n- 1\nb:\n- 1\n- 1\n\n"},
		{"grab inside an aliased map", "meta: {v: hello}\nbase: &b\n  g: (( grab meta.v ))\n  k: &k 1\n  j: *k\nuse: *b\n",
			"---\nbase:\n  g: hello\n  j: 1\n  k: 1\nmeta:\n  v: hello\nuse:\n  g: hello\n  j: 1\n  k: 1\n\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(dir, "aliases.yml")
			if err := os.WriteFile(path, []byte(c.in), 0o600); err != nil {
				t.Fatal(err)
			}
			stdout, stderr, rc := runGraftCommand(t, []string{"merge", path})
			if rc != 0 || stdout != c.want || stderr != "" {
				t.Errorf("graft merge: rc=%d stdout=%q stderr=%q, want rc=0 and stdout %q", rc, stdout, stderr, c.want)
			}
		})
	}
}

// TestMergeRejectsAliasBomb runs `graft merge` and `graft json` on a
// file whose aliases expand to over ten million scalars. Both fail it
// with the recursion text and exit 2, before they copy any aliased
// value. json must not call the root anything but a map, since it is one.
func TestMergeRejectsAliasBomb(t *testing.T) {
	var b strings.Builder
	b.WriteString("a0: &a0 [x, x, x, x, x, x, x, x, x, x]\n")
	for i := 1; i < 7; i++ {
		alias := "*a" + strconv.Itoa(i-1)
		b.WriteString("a" + strconv.Itoa(i) + ": &a" + strconv.Itoa(i) + " [" + strings.TrimSuffix(strings.Repeat(alias+", ", 10), ", ") + "]\n")
	}
	path := filepath.Join(t.TempDir(), "bomb.yml")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, cmd := range []string{"merge", "json"} {
		t.Run(cmd, func(t *testing.T) {
			stderr, rc := runGraftCapturingOutput(t, []string{cmd, path})
			if rc != 2 || !strings.Contains(stderr, "Hit max recursion depth. You seem to have a self-referencing dataset") || strings.Contains(stderr, "not a hash/map") {
				t.Errorf("graft %s: rc=%d stderr=%q, want rc=2 and the max recursion error", cmd, rc, stderr)
			}
		})
	}
}
