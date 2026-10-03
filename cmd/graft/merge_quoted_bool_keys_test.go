package main

import "testing"

// TestMergeKeepsQuotedBoolWordKeys runs `graft merge` and `graft json` on
// a file whose keys are quoted YAML 1.1 boolean words, such as "yes",
// 'No', "on", and "OFF", at the top level, nested, and inside a sequence.
// Each key must come out as the word itself, exactly as spruce v1.35.17
// writes it. graft used to put a private-use marker character in front
// of each such key.
func TestMergeKeepsQuotedBoolWordKeys(t *testing.T) {
	in := "\"yes\": 1\n'No': 2\n\"on\": \"on\"\n\"OFF\": {\"Yes\": x}\nl: [{\"no\": 1}]\n"
	for _, c := range []struct {
		cmd, want string
	}{
		{"merge", "---\n\"No\": 2\n\"OFF\":\n  \"Yes\": x\nl:\n- \"no\": 1\n\"on\": \"on\"\n\"yes\": 1\n\n"},
		{"json", "{\"No\":2,\"OFF\":{\"Yes\":\"x\"},\"l\":[{\"no\":1}],\"on\":\"on\",\"yes\":1}\n"},
	} {
		t.Run(c.cmd, func(t *testing.T) {
			path := writeCRLFFile(t, "bools.yml", in)
			stdout, stderr, rc := runGraftCommand(t, []string{c.cmd, path})
			if rc != 0 || stdout != c.want || stderr != "" {
				t.Errorf("graft %s: rc=%d stdout=%q stderr=%q, want rc=0 and stdout %q", c.cmd, rc, stdout, stderr, c.want)
			}
		})
	}
}
