package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeDiffInputs writes the two YAML texts to files in a temporary
// directory and returns their paths.
func writeDiffInputs(t *testing.T, from, to string) (fromPath, toPath string) {
	t.Helper()
	dir := t.TempDir()
	fromPath, toPath = filepath.Join(dir, "from.yml"), filepath.Join(dir, "to.yml")
	if err := os.WriteFile(fromPath, []byte(from), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(toPath, []byte(to), 0o600); err != nil {
		t.Fatal(err)
	}
	return fromPath, toPath
}

// TestDiffChangesSingleDocumentValueSemantics pins that the graft-only
// modes compare decoded values, so spellings of one value are no change.
func TestDiffChangesSingleDocumentValueSemantics(t *testing.T) {
	cases := map[string][2]string{
		"1.0 against 1":              {"a: 1.0\n", "a: 1\n"},
		"merge key against expanded": {"base: &a {x: 1}\nb:\n  <<: *a\n", "base: {x: 1}\nb: {x: 1}\n"},
		"0x10 against 16":            {"a: 0x10\n", "a: 16\n"},
	}
	for name, inputs := range cases {
		t.Run(name, func(t *testing.T) {
			from, to := writeDiffInputs(t, inputs[0], inputs[1])
			stdout, stderr, rc := runMainCaptured(t, "diff", "--changes", from, to)
			if rc != 0 || stderr != "" || stdout != "Changes (0 modified, 0 added, 0 removed):\n" {
				t.Fatalf("rc=%d stderr=%q stdout=%q, want no change", rc, stderr, stdout)
			}
		})
	}
}

const (
	twoDocsFrom = "a: 1\n---\nb: 1\n"
	twoDocsTo   = "a: 1\n---\nb: 2\n"
)

func TestDiffChangesComparesEveryDocument(t *testing.T) {
	from, to := writeDiffInputs(t, twoDocsFrom, twoDocsTo)
	stdout, stderr, rc := runMainCaptured(t, "diff", "--changes", from, to)
	want := "Changes (1 modified, 0 added, 0 removed):\n\n" +
		"  MODIFIED  b  (document #2)\n" +
		"            - 1\n" +
		"            + 2\n"
	if rc != 1 || stderr != "" || stdout != want {
		t.Fatalf("rc=%d stderr=%q stdout=%q, want rc 1 and %q", rc, stderr, stdout, want)
	}
}

func TestDiffUnifiedComparesEveryDocument(t *testing.T) {
	from, to := writeDiffInputs(t, twoDocsFrom, twoDocsTo)
	stdout, stderr, rc := runMainCaptured(t, "diff", "--unified", from, to)
	want := "--- " + from + "\n+++ " + to + "\n" +
		"(document #2)\n" +
		"@@ b @@\n" +
		"-  1\n" +
		"+  2\n"
	if rc != 1 || stderr != "" || stdout != want {
		t.Fatalf("rc=%d stderr=%q stdout=%q, want rc 1 and %q", rc, stderr, stdout, want)
	}
}

func TestDiffSideBySideComparesEveryDocument(t *testing.T) {
	from, to := writeDiffInputs(t, twoDocsFrom, twoDocsTo)
	stdout, stderr, rc := runMainCaptured(t, "diff", "--side-by-side", "--width", "40", from, to)
	if rc != 1 || stderr != "" {
		t.Fatalf("rc=%d stderr=%q, want rc 1 and no stderr", rc, stderr)
	}
	if !strings.Contains(stdout, "(document #2)\n") || strings.Contains(stdout, "(document #1)") {
		t.Fatalf("output must label only the changed document #2:\n%s", stdout)
	}
	if !strings.Contains(stdout, "b: 1") || !strings.Contains(stdout, "b: 2") {
		t.Fatalf("output must show document #2's change:\n%s", stdout)
	}
}

func TestDiffGraftOnlyModesReportDocumentCountMismatch(t *testing.T) {
	from, to := writeDiffInputs(t, twoDocsFrom, "a: 1\n")
	for _, mode := range []string{"--changes", "--unified", "--side-by-side"} {
		t.Run(mode, func(t *testing.T) {
			stdout, stderr, rc := runMainCaptured(t, "diff", mode, from, to)
			if rc != 2 || stdout != "" ||
				!strings.Contains(stderr, "comparing YAMLs with a different number of documents is currently not supported") {
				t.Fatalf("rc=%d stdout=%q stderr=%q, want rc 2 and the default mode's error", rc, stdout, stderr)
			}
		})
	}
}

func TestDiffGraftOnlyModesMatchKubernetesDocumentsByName(t *testing.T) {
	const (
		a = "apiVersion: v1\nkind: A\nmetadata:\n  name: a\nvalue: 1\n"
		b = "apiVersion: v1\nkind: B\nmetadata:\n  name: b\nvalue: 2\n"
	)
	from, to := writeDiffInputs(t, a+"---\n"+b, b+"---\n"+a)
	stdout, stderr, rc := runMainCaptured(t, "diff", "--changes", from, to)
	if rc != 0 || stderr != "" || stdout != "Changes (0 modified, 0 added, 0 removed):\n" {
		t.Fatalf("swapped resources: rc=%d stderr=%q stdout=%q, want no change", rc, stderr, stdout)
	}

	from, to = writeDiffInputs(t, a+"---\n"+b, strings.Replace(b, "value: 2", "value: 3", 1)+"---\n"+a)
	stdout, stderr, rc = runMainCaptured(t, "diff", "--unified", from, to)
	want := "--- " + from + "\n+++ " + to + "\n(document #2)\n@@ value @@\n-  2\n+  3\n"
	if rc != 1 || stderr != "" || stdout != want {
		t.Fatalf("changed resource: rc=%d stderr=%q stdout=%q, want rc 1 and %q", rc, stderr, stdout, want)
	}
}
