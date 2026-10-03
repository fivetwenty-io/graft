package main

import "testing"

// lineSeparatorInput holds values and a key with a LINE SEPARATOR
// (U+2028) or PARAGRAPH SEPARATOR (U+2029), written as escapes, plus two
// single-quoted values that put spaces or a tab next to the separator
// and one that follows it with a line break.
const lineSeparatorInput = "k: \"a\\Lb\"\np: \"x\\Py\"\nm: \"a\\Lb\\nc\"\n\"a\\Lb\": 1\n" +
	"r: 'a   \tb'\ns: 'a \n  b'\n"

// TestMergeRereadsLineSeparators merges values that hold a LINE
// SEPARATOR or PARAGRAPH SEPARATOR and then merges graft's own output
// again. spruce v1.35.17 writes such a value in single quotes or in a
// literal block, with the separator followed by a line's indentation,
// and reads the separator as a line break: the spaces and tabs around
// it in a quoted scalar, and the indentation after it in a block
// scalar, are not part of the value. Both merges match spruce's output
// byte for byte, and `graft json` reads the values as spruce does. graft
// used to keep those spaces in the value, so each merge added more.
func TestMergeRereadsLineSeparators(t *testing.T) {
	first := "---\n? 'a   b'\n: 1\nk: 'a   b'\nm: |-\n  a   b\n  c\np: 'x   y'\n" +
		"r: 'a   b'\ns: |-\n  a \n  b\n\n"
	path := writeCRLFFile(t, "in.yml", lineSeparatorInput)
	stdout, stderr, rc := runGraftCommand(t, []string{"merge", path})
	if rc != 0 || stdout != first || stderr != "" {
		t.Fatalf("first merge: rc=%d stdout=%q stderr=%q, want rc=0 and stdout %q", rc, stdout, stderr, first)
	}
	again := writeCRLFFile(t, "out.yml", stdout)
	stdout, stderr, rc = runGraftCommand(t, []string{"merge", again})
	if rc != 0 || stdout != first || stderr != "" {
		t.Errorf("second merge: rc=%d stdout=%q stderr=%q, want rc=0 and stdout %q", rc, stdout, stderr, first)
	}
	want := "{\"a\\u2028b\":1,\"k\":\"a\\u2028b\",\"m\":\"a\\u2028b\\nc\",\"p\":\"x\\u2029y\",\"r\":\"a\\u2028b\",\"s\":\"a\\u2028\\nb\"}\n"
	for name, p := range map[string]string{"input": path, "merge output": again} {
		stdout, stderr, rc = runGraftCommand(t, []string{"json", p})
		if rc != 0 || stdout != want || stderr != "" {
			t.Errorf("graft json of the %s: rc=%d stdout=%q stderr=%q, want rc=0 and stdout %q", name, rc, stdout, stderr, want)
		}
	}
}

// TestDiffReadsLineSeparators compares the input with spruce's merge
// output for it, which `graft diff` reads as the same document.
func TestDiffReadsLineSeparators(t *testing.T) {
	out := "? 'a   b'\n: 1\nk: 'a   b'\nm: |-\n  a   b\n  c\np: 'x   y'\n" +
		"r: 'a   b'\ns: |-\n  a \n  b\n"
	from, to := writeDiffInputs(t, lineSeparatorInput, out)
	stdout, stderr, rc := runMainCaptured(t, "diff", from, to)
	if rc != 0 || stdout != "\n\n" || stderr != "" {
		t.Errorf("graft diff: rc=%d stdout=%q stderr=%q, want rc=0 and no differences", rc, stdout, stderr)
	}
}
