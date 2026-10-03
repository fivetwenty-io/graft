package main

import (
	"strings"
	"testing"
)

// TestMergeReadsNELAsALineBreak runs `graft merge` and `graft json` on
// files that hold a next-line character (U+0085). libyaml reads it as a
// line break, so spruce v1.35.17 splits `a: 1` and `b: 2` into two
// entries, rejects a plain scalar that holds one, and folds it inside a
// double-quoted scalar the way it folds an LF. graft used to keep the
// character as text in all three.
func TestMergeReadsNELAsALineBreak(t *testing.T) {
	for _, c := range []struct {
		name, in string
		args     []string
		want     string
	}{
		{"break between entries", "a: 1\u0085b: 2\n", []string{"merge"}, "---\na: 1\nb: 2\n\n"},
		{"fold in a double-quoted scalar", "a: \"q\u0085  r\"\n", []string{"merge"}, "---\na: q r\n\n"},
		{"fold in a double-quoted scalar, json", "a: \"q\u0085  r\"\n", []string{"json"}, "{\"a\":\"q r\"}\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			path := writeCRLFFile(t, "nel.yml", c.in)
			stdout, stderr, rc := runGraftCommand(t, append(c.args, path))
			if rc != 0 || stdout != c.want || stderr != "" {
				t.Errorf("graft %s: rc=%d stdout=%q stderr=%q, want rc=0 and stdout %q", c.args[0], rc, stdout, stderr, c.want)
			}
		})
	}
}

// TestMergeRejectsNELInAPlainScalar runs `graft merge` on `a: x` and
// U+0085 and `y`. The character ends the line, so `y` has no colon, and
// spruce v1.35.17 exits 2 with "could not find expected ':'". graft exits
// 2 as well, with its own wording for the same file with an LF there.
// It used to merge the file with the character inside the value.
func TestMergeRejectsNELInAPlainScalar(t *testing.T) {
	path := writeCRLFFile(t, "nel.yml", "a: x\u0085y\n")
	stdout, stderr, rc := runGraftCommand(t, []string{"merge", path})
	if rc != 2 || stdout != "" || !strings.Contains(stderr, "[2:1]") {
		t.Errorf("graft merge: rc=%d stdout=%q stderr=%q, want rc=2 and an error at 2:1", rc, stdout, stderr)
	}
}

// TestDiffRejectsNELInAPlainScalar runs `graft diff` on `a: x` and
// U+0085 and `y` against `a: 1`. spruce v1.35.17 exits 2 on the file.
// graft used to report a type change and exit 1.
func TestDiffRejectsNELInAPlainScalar(t *testing.T) {
	from, to := writeDiffInputs(t, "a: x\u0085y\n", "a: 1\n")
	stdout, stderr, rc := runMainCaptured(t, "diff", from, to)
	if rc != 2 || stdout != "" || !strings.Contains(stderr, "yaml: line 2:") {
		t.Errorf("graft diff: rc=%d stdout=%q stderr=%q, want rc=2 and an error on line 2", rc, stdout, stderr)
	}
}

// TestMergeStillRejectsLSBetweenEntries pins a known gap. spruce
// v1.35.17 reads a line-separator character (U+2028) as a line break
// and merges `a: 1`, the character, and `b: 2` into two entries. graft
// leaves it as text, so the file fails with exit 2. The docs list this
// as a difference.
func TestMergeStillRejectsLSBetweenEntries(t *testing.T) {
	path := writeCRLFFile(t, "ls.yml", "a: 1\u2028b: 2\n")
	stdout, stderr, rc := runGraftCommand(t, []string{"merge", path})
	if rc != 2 || stdout != "" || !strings.Contains(stderr, "mapping value is not allowed in this context") {
		t.Errorf("graft merge: rc=%d stdout=%q stderr=%q, want rc=2 and mapping value is not allowed", rc, stdout, stderr)
	}
}
