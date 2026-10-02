package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/fivetwenty-io/graft/log"
)

// runMainCaptured runs main() with args and returns what it printed to
// stdout and stderr and the exit code it requested.
func runMainCaptured(t *testing.T, args ...string) (stdout, stderr string, rc int) {
	t.Helper()
	prevOut, prevErr, prevExit, prevUsage, prevArgs := printStdOutf, log.PrintStdErrf, exit, usage, os.Args
	t.Cleanup(func() {
		printStdOutf, log.PrintStdErrf, exit, usage, os.Args = prevOut, prevErr, prevExit, prevUsage, prevArgs
	})
	rc = 256
	printStdOutf = func(format string, a ...interface{}) { stdout += fmt.Sprintf(format, a...) }
	log.PrintStdErrf = func(format string, a ...interface{}) { stderr += fmt.Sprintf(format, a...) }
	exit = func(code int) { rc = code }
	usage = func() { exit(1) }
	os.Args = append([]string{"graft"}, args...)
	main()
	return stdout, stderr, rc
}

func TestMergeReadsConcoursePlaceholdersAsStrings(t *testing.T) {
	stdout, stderr, rc := runMainCaptured(t, "merge", "../../assets/concourse/first.yml", "../../assets/concourse/second.yml")
	want := "---\njobs:\n- curlies: '{{my-variable_123}}'\n  name: thing1\n- curlies: '{{more}}'\n  name: thing2\n\n"
	if rc != 0 || stderr != "" || stdout != want {
		t.Fatalf("merge rc=%d stderr=%q stdout=%q, want rc=0 and %q", rc, stderr, stdout, want)
	}
}

func TestJSONReadsBracePlaceholdersAsStrings(t *testing.T) {
	stdout, stderr, rc := runMainCaptured(t, "json", "../../assets/concourse/first.yml")
	want := `{"jobs":[{"curlies":"{{my-variable_123}}","name":"thing1"}]}` + "\n"
	if rc != 0 || stderr != "" || stdout != want {
		t.Fatalf("json rc=%d stderr=%q stdout=%q, want rc=0 and %q", rc, stderr, stdout, want)
	}
}

func TestLoadReadsBracePlaceholdersAsStrings(t *testing.T) {
	abs, err := filepath.Abs("../../assets/concourse/first.yml")
	if err != nil {
		t.Fatal(err)
	}
	loader := filepath.Join(t.TempDir(), "load.yml")
	if err := os.WriteFile(loader, []byte(fmt.Sprintf("x: (( load %q ))\n", abs)), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, rc := runMainCaptured(t, "merge", loader)
	want := "---\nx:\n  jobs:\n  - curlies: '{{my-variable_123}}'\n    name: thing1\n\n"
	if rc != 0 || stderr != "" || stdout != want {
		t.Fatalf("load rc=%d stderr=%q stdout=%q, want rc=0 and %q", rc, stderr, stdout, want)
	}
}

func TestJSONAppliesMergeInputRewrites(t *testing.T) {
	in := filepath.Join(t.TempDir(), "in.yml")
	if err := os.WriteFile(in, []byte("l:\n- a\n-\nk: 1\nm:\n  <<<: (( inject n ))\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, rc := runMainCaptured(t, "json", in)
	want := `{"k":1,"l":["a",null],"m":{"\u003c\u003c\u003c":"(( inject n ))"}}` + "\n"
	if rc != 0 || stderr != "" || stdout != want {
		t.Fatalf("json rc=%d stderr=%q stdout=%q, want rc=0 and %q", rc, stderr, stdout, want)
	}
}
