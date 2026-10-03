package main

import "testing"

// TestMergeRereadsIndentIndicatorBlocks merges a value that starts with
// a space or a line break, which spruce v1.35.17 writes as a block
// scalar with an indentation indicator such as "|2-" or "|2+", and then
// merges graft's own output again. Each first merge matches spruce's
// output, and each second merge matches what spruce gives for the same
// second merge. That is the first output again, except for a keep
// ("+") block at the end of the document, which gains the trailing
// blank line the merge prints after the document, in spruce as in
// graft. graft used to fail the second merge with "invalid number of
// indent is specified in the multi-line header" or "could not find
// multi-line content".
func TestMergeRereadsIndentIndicatorBlocks(t *testing.T) {
	for _, c := range []struct {
		name, in, first, second string
	}{
		{"leading break", "k: \"\\na\"\n", "k: |2-\n\n  a\n", ""},
		{"leading space", "k: \" a\\nb\"\n", "k: |2-\n   a\n  b\n", ""},
		{"nested", "a: {k: \"\\na\"}\n", "a:\n  k: |2-\n\n    a\n", ""},
		{"keep block before another key", "a: \"\\n\"\nb: 1\n", "a: |2+\n\nb: 1\n", ""},
		{"sequence items", "l: [\"\\n\", \" a\\nb\"]\n", "l:\n- |2+\n\n- |2-\n   a\n  b\n", ""},
		{"mapping in a sequence", "l: [{k: \"\\n\", z: 1}, \" a\\nb\"]\n", "l:\n- k: |2+\n\n  z: 1\n- |2-\n   a\n  b\n", ""},
		{"explicit key", "\"\\na\": 1\n", "? |2-\n\n  a\n: 1\n", ""},
		{"keep block last", "k: \"\\n\"\n", "k: |2+\n\n", "k: |2+\n\n\n"},
		{"two breaks last", "k: \"\\n\\n\"\n", "k: |2+\n\n\n", "k: |2+\n\n\n\n"},
		{"keep block under an explicit key", "\" a\\nb\": {c: \"\\n\"}\n", "? |2-\n   a\n  b\n: c: |2+\n\n", "? |2-\n   a\n  b\n: c: |2+\n\n\n"},
	} {
		t.Run(c.name, func(t *testing.T) {
			second := c.second
			if second == "" {
				second = c.first
			}
			path := writeCRLFFile(t, "in.yml", c.in)
			stdout, stderr, rc := runGraftCommand(t, []string{"merge", path})
			if want := "---\n" + c.first + "\n"; rc != 0 || stdout != want || stderr != "" {
				t.Fatalf("first merge: rc=%d stdout=%q stderr=%q, want rc=0 and stdout %q", rc, stdout, stderr, want)
			}
			again := writeCRLFFile(t, "out.yml", stdout)
			stdout, stderr, rc = runGraftCommand(t, []string{"merge", again})
			if want := "---\n" + second + "\n"; rc != 0 || stdout != want || stderr != "" {
				t.Errorf("second merge: rc=%d stdout=%q stderr=%q, want rc=0 and stdout %q", rc, stdout, stderr, want)
			}
		})
	}
}

// TestJSONAndDiffReadIndentIndicatorBlocks reads spruce's output for
// values that start with a line break with `graft json` and `graft
// diff`, which read them as spruce does.
func TestJSONAndDiffReadIndentIndicatorBlocks(t *testing.T) {
	in := "a: |2+\n\nb:\n- |2-\n\n  x\nc: |2+\n\n\n"
	path := writeCRLFFile(t, "in.yml", in)
	stdout, stderr, rc := runGraftCommand(t, []string{"json", path})
	if want := "{\"a\":\"\\n\",\"b\":[\"\\nx\"],\"c\":\"\\n\\n\"}\n"; rc != 0 || stdout != want || stderr != "" {
		t.Errorf("graft json: rc=%d stdout=%q stderr=%q, want rc=0 and stdout %q", rc, stdout, stderr, want)
	}
	from, to := writeDiffInputs(t, in, "a: \"\\n\"\nb: [\"\\nx\"]\nc: \"\\n\\n\"\n")
	stdout, stderr, rc = runMainCaptured(t, "diff", from, to)
	if rc != 0 || stdout != "\n\n" || stderr != "" {
		t.Errorf("graft diff: rc=%d stdout=%q stderr=%q, want rc=0 and no differences", rc, stdout, stderr)
	}
}

// TestBlockPaddingKeepsOtherBlocks reads files that need the padding for
// an indentation indicator block and also hold another block scalar
// whose content looks like such a header followed by an empty line, as a
// stringified map with a value that starts with a line break does. The
// other block's value must come through byte for byte, as it does in
// spruce v1.35.17. graft used to pad its empty line as well, so the
// value gained spaces and `graft diff` reported a difference spruce does
// not.
func TestBlockPaddingKeepsOtherBlocks(t *testing.T) {
	in := "script: |\n  x: |4\n\n  y\nk: |2+\n\nb: 1\n"
	path := writeCRLFFile(t, "in.yml", in)
	stdout, stderr, rc := runGraftCommand(t, []string{"json", path})
	if want := "{\"b\":1,\"k\":\"\\n\",\"script\":\"x: |4\\n\\ny\\n\"}\n"; rc != 0 || stdout != want || stderr != "" {
		t.Errorf("graft json: rc=%d stdout=%q stderr=%q, want rc=0 and stdout %q", rc, stdout, stderr, want)
	}
	from, to := writeDiffInputs(t, in, "script: |\n  x: |4\n\n  y\nk: \"\\n\"\nb: 1\n")
	stdout, stderr, rc = runMainCaptured(t, "diff", from, to)
	if rc != 0 || stdout != "\n\n" || stderr != "" {
		t.Errorf("graft diff: rc=%d stdout=%q stderr=%q, want rc=0 and no differences", rc, stdout, stderr)
	}

	chain := writeCRLFFile(t, "chain.yml", "m: {k: \"\\na\", j: 1}\ns: (( stringify m ))\nz: \"\\nlast\"\n")
	stdout, stderr, rc = runGraftCommand(t, []string{"merge", chain})
	first := "---\nm:\n  j: 1\n  k: |2-\n\n    a\ns: |\n  j: 1\n  k: |2-\n\n    a\nz: |2-\n\n  last\n\n"
	if rc != 0 || stdout != first || stderr != "" {
		t.Fatalf("graft merge: rc=%d stdout=%q stderr=%q, want rc=0 and stdout %q", rc, stdout, stderr, first)
	}
	again := writeCRLFFile(t, "out.yml", stdout)
	stdout, stderr, rc = runGraftCommand(t, []string{"json", again})
	if want := "{\"m\":{\"j\":1,\"k\":\"\\na\"},\"s\":\"j: 1\\nk: |2-\\n\\n  a\\n\",\"z\":\"\\nlast\"}\n"; rc != 0 || stdout != want || stderr != "" {
		t.Errorf("graft json of the merge output: rc=%d stdout=%q stderr=%q, want rc=0 and stdout %q", rc, stdout, stderr, want)
	}
	stdout, stderr, rc = runGraftCommand(t, []string{"merge", again})
	if rc != 0 || stdout != first || stderr != "" {
		t.Errorf("second merge: rc=%d stdout=%q stderr=%q, want rc=0 and stdout %q", rc, stdout, stderr, first)
	}
}
