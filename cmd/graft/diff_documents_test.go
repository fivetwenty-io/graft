package main

import (
	"os"
	"strings"
	"testing"
)

// writeDiffInputs writes the two YAML texts to from.yml and to.yml in a
// temporary directory, makes that directory the working directory for the
// test, and returns the two file names.
func writeDiffInputs(t *testing.T, from, to string) (fromPath, toPath string) {
	t.Helper()
	dir := t.TempDir()
	t.Chdir(dir)
	fromPath, toPath = "from.yml", "to.yml"
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

// sideBySide builds the output of --side-by-side --width 40 from rows of
// left and right text. Each side is 18 columns wide.
func sideBySide(rows ...[2]string) string {
	var out strings.Builder
	for _, row := range rows {
		out.WriteString(row[0] + strings.Repeat(" ", 18-len([]rune(row[0]))) + " │ " + row[1] + "\n")
	}
	return out.String()
}

const sideBySideRule = "───────────────────┼───────────────────\n"

func TestDiffSideBySideComparesEveryDocument(t *testing.T) {
	from, to := writeDiffInputs(t, twoDocsFrom, twoDocsTo)
	stdout, stderr, rc := runMainCaptured(t, "diff", "--side-by-side", "--width", "40", from, to)
	want := "(document #2)\n" +
		sideBySide([2]string{"from.yml", "to.yml"}) + sideBySideRule +
		sideBySide([2]string{"b: 1", "b: 2"})
	if rc != 1 || stderr != "" || stdout != want {
		t.Fatalf("rc=%d stderr=%q stdout=%q, want rc 1 and %q", rc, stderr, stdout, want)
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

const (
	resourceA = "apiVersion: v1\nkind: A\nmetadata:\n  name: a\nvalue: 1\n"
	resourceB = "apiVersion: v1\nkind: B\nmetadata:\n  name: b\nvalue: 2\n"
	resourceC = "apiVersion: v1\nkind: C\nmetadata:\n  name: c\n"
)

func TestDiffGraftOnlyModesMatchKubernetesDocumentsByName(t *testing.T) {
	changedB := strings.Replace(resourceB, "value: 2", "value: 3", 1)
	from, to := writeDiffInputs(t, resourceA+"---\n"+resourceB, changedB+"---\n"+resourceA)

	stdout, stderr, rc := runMainCaptured(t, "diff", "--unified", from, to)
	want := "--- from.yml\n+++ to.yml\n" +
		"(document #2)\n@@ value @@\n-  2\n+  3\n" +
		"(file level)\n@@ (root) @@\n+  - v1/B/b\n   - v1/A/a\n-  - v1/B/b\n"
	if rc != 1 || stderr != "" || stdout != want {
		t.Fatalf("changed resource: rc=%d stderr=%q stdout=%q, want rc 1 and %q", rc, stderr, stdout, want)
	}
}

func TestDiffGraftOnlyModesReportDocumentOrderChange(t *testing.T) {
	from, to := writeDiffInputs(t, resourceA+"---\n"+resourceB, resourceB+"---\n"+resourceA)

	stdout, stderr, rc := runMainCaptured(t, "diff", "--changes", from, to)
	want := "Changes (1 modified, 0 added, 0 removed):\n\n" +
		"  MODIFIED  (file level)\n" +
		"            - - v1/A/a\n" +
		"            - - v1/B/b\n" +
		"            + - v1/B/b\n" +
		"            + - v1/A/a\n"
	if rc != 1 || stderr != "" || stdout != want {
		t.Fatalf("--changes: rc=%d stderr=%q stdout=%q, want rc 1 and %q", rc, stderr, stdout, want)
	}

	stdout, stderr, rc = runMainCaptured(t, "diff", "--unified", from, to)
	want = "--- from.yml\n+++ to.yml\n(file level)\n@@ (root) @@\n+  - v1/B/b\n   - v1/A/a\n-  - v1/B/b\n"
	if rc != 1 || stderr != "" || stdout != want {
		t.Fatalf("--unified: rc=%d stderr=%q stdout=%q, want rc 1 and %q", rc, stderr, stdout, want)
	}

	stdout, stderr, rc = runMainCaptured(t, "diff", "--side-by-side", "--width", "40", from, to)
	want = "(file level)\n" +
		sideBySide([2]string{"from.yml", "to.yml"}) + sideBySideRule +
		sideBySide([2]string{"", "- v1/B/b"}, [2]string{"- v1/A/a", "- v1/A/a"}, [2]string{"- v1/B/b", ""})
	if rc != 1 || stderr != "" || stdout != want {
		t.Fatalf("--side-by-side: rc=%d stderr=%q stdout=%q, want rc 1 and %q", rc, stderr, stdout, want)
	}
}

func TestDiffGraftOnlyModesShowADocumentOnlyOneSideHas(t *testing.T) {
	from, to := writeDiffInputs(t, resourceA+"---\n"+resourceB, resourceA+"---\n"+resourceC)

	stdout, stderr, rc := runMainCaptured(t, "diff", "--changes", from, to)
	want := "Changes (1 modified, 1 added, 1 removed):\n\n" +
		"  ADDED     (document #2)\n" +
		"            + apiVersion: v1\n" +
		"            + kind: C\n" +
		"            + metadata:\n" +
		"            +   name: c\n\n" +
		"  REMOVED   (document #2)\n" +
		"            - apiVersion: v1\n" +
		"            - kind: B\n" +
		"            - metadata:\n" +
		"            -   name: b\n" +
		"            - value: 2\n\n" +
		"  MODIFIED  (file level)\n" +
		"            - - v1/A/a\n" +
		"            - - v1/B/b\n" +
		"            + - v1/A/a\n" +
		"            + - v1/C/c\n"
	if rc != 1 || stderr != "" || stdout != want {
		t.Fatalf("--changes: rc=%d stderr=%q stdout=%q, want rc 1 and %q", rc, stderr, stdout, want)
	}

	stdout, stderr, rc = runMainCaptured(t, "diff", "--unified", from, to)
	want = "--- from.yml\n+++ to.yml\n" +
		"(document #2)\n@@ (root) @@\n" +
		"-  apiVersion: v1\n-  kind: B\n-  metadata:\n-    name: b\n-  value: 2\n" +
		"(document #2)\n@@ (root) @@\n" +
		"+  apiVersion: v1\n+  kind: C\n+  metadata:\n+    name: c\n" +
		"(file level)\n@@ (root) @@\n   - v1/A/a\n-  - v1/B/b\n+  - v1/C/c\n"
	if rc != 1 || stderr != "" || stdout != want {
		t.Fatalf("--unified: rc=%d stderr=%q stdout=%q, want rc 1 and %q", rc, stderr, stdout, want)
	}

	stdout, stderr, rc = runMainCaptured(t, "diff", "--side-by-side", "--width", "40", from, to)
	header := sideBySide([2]string{"from.yml", "to.yml"}) + sideBySideRule
	want = "(document #2)\n" + header +
		sideBySide([2]string{"apiVersion: v1", ""}, [2]string{"kind: B", ""}, [2]string{"metadata:", ""}, [2]string{"  name: b", ""}, [2]string{"value: 2", ""}) +
		"\n(document #2)\n" + header +
		sideBySide([2]string{"", "apiVersion: v1"}, [2]string{"", "kind: C"}, [2]string{"", "metadata:"}, [2]string{"", "  name: c"}) +
		"\n(file level)\n" + header +
		sideBySide([2]string{"- v1/A/a", "- v1/A/a"}, [2]string{"- v1/B/b", "- v1/C/c"})
	if rc != 1 || stderr != "" || stdout != want {
		t.Fatalf("--side-by-side: rc=%d stderr=%q stdout=%q, want rc 1 and %q", rc, stderr, stdout, want)
	}
}

func TestDiffChangesLabelsWholeDocumentsOfSingleDocumentInputs(t *testing.T) {
	from, to := writeDiffInputs(t, "apiVersion: v1\nkind: A\nmetadata:\n  name: a\n", "apiVersion: v1\nkind: A\nmetadata:\n  name: b\n")
	stdout, stderr, rc := runMainCaptured(t, "diff", "--changes", from, to)
	if rc != 1 || stderr != "" {
		t.Fatalf("rc=%d stderr=%q, want rc 1", rc, stderr)
	}
	for _, line := range strings.Split(stdout, "\n") {
		if strings.HasSuffix(line, " ") {
			t.Errorf("line %q has trailing spaces", line)
		}
	}
	for _, entry := range []string{"  ADDED     (document #1)\n", "  REMOVED   (document #1)\n"} {
		if !strings.Contains(stdout, entry) {
			t.Errorf("output lacks %q:\n%s", entry, stdout)
		}
	}
}
