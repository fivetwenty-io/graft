package main

import (
	"os"
	"strings"
	"testing"
)

func writeStrictInput(t *testing.T, name, body string) string {
	t.Helper()
	path := t.TempDir() + "/" + name
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestJSONStrictFailsOnNonStringKeys runs the command the way a user does
// and checks the exit code and the text spruce gives, which names the file
// and the index of the document that holds the key.
func TestJSONStrictFailsOnNonStringKeys(t *testing.T) {
	path := writeStrictInput(t, "k.yml", "a: 1\n---\nb:\n  2: x\n")

	stdout, stderr, rc := runMainCaptured(t, "json", "--strict", path)
	if rc != 2 || stdout != "" {
		t.Fatalf("--strict: rc=%d stdout=%q, want rc 2 and no output", rc, stdout)
	}
	if want := path + "[1]: non-string keys found during strict JSON conversion\n"; stderr != want {
		t.Errorf("--strict stderr = %q, want %q", stderr, want)
	}

	stdout, stderr, rc = runMainCaptured(t, "json", path)
	if rc != 0 || stderr != "" || stdout != "{\"a\":1}\n{\"b\":{\"2\":\"x\"}}\n" {
		t.Errorf("without --strict: rc=%d stdout=%q stderr=%q, want both documents", rc, stdout, stderr)
	}
}

func TestJSONStrictAcceptsStringKeys(t *testing.T) {
	path := writeStrictInput(t, "ok.yml", "a: 1\n\"2\": two\nb:\n  c: [1, 2]\n")

	stdout, stderr, rc := runMainCaptured(t, "json", "--strict", path)
	if rc != 0 || stderr != "" || !strings.Contains(stdout, `"2":"two"`) {
		t.Errorf("rc=%d stdout=%q stderr=%q, want the JSON and rc 0", rc, stdout, stderr)
	}
}
