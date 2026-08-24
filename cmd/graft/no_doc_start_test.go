package main

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/fivetwenty-io/graft/log"
	"github.com/fivetwenty-io/graft/pkg/graft"
)

// runGraftMerge drives main() with the given os.Args tail and captures
// stdout, stderr, and the exit code, following the harness pattern of
// genesis_contract_pin_test.go so the full flag/env resolution path
// (PersistentPreRunE + merge RunE) is exercised, not just handleMerge.
func runGraftMerge(t *testing.T, args ...string) (string, string, int) {
	t.Helper()

	prevPrintStdOutf := printStdOutf
	prevPrintStdErrf := log.PrintStdErrf
	prevExit := exit
	prevUsage := usage
	prevArgs := os.Args
	defer func() {
		printStdOutf = prevPrintStdOutf
		log.PrintStdErrf = prevPrintStdErrf
		exit = prevExit
		usage = prevUsage
		os.Args = prevArgs
	}()

	var stdout, stderr string
	rc := 256
	printStdOutf = func(format string, args ...interface{}) {
		stdout += fmt.Sprintf(format, args...)
	}
	log.PrintStdErrf = func(format string, args ...interface{}) {
		stderr += fmt.Sprintf(format, args...)
	}
	exit = func(code int) { rc = code }
	usage = func() { exit(1) }

	os.Args = append([]string{"graft"}, args...)
	main()
	return stdout, stderr, rc
}

func TestResolveNoDocStart(t *testing.T) {
	cases := []struct {
		name        string
		flagChanged bool
		flagValue   bool
		envValue    string
		want        bool
	}{
		{"default keeps marker", false, false, "", false},
		{"flag suppresses", true, true, "", true},
		{"env true suppresses", false, false, "true", true},
		{"env 1 suppresses", false, false, "1", true},
		{"env yes suppresses", false, false, "yes", true},
		{"env on suppresses", false, false, "on", true},
		{"env false keeps marker", false, false, "false", false},
		{"env 0 keeps marker", false, false, "0", false},
		{"env garbage ignored", false, false, "definitely", false},
		{"explicit flag false beats env true", true, false, "1", false},
		{"explicit flag true beats env false", true, true, "false", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveNoDocStart(tc.flagChanged, tc.flagValue, tc.envValue)
			if got != tc.want {
				t.Errorf("resolveNoDocStart(%v, %v, %q) = %v, want %v",
					tc.flagChanged, tc.flagValue, tc.envValue, got, tc.want)
			}
		})
	}
}

func TestRenderMergedTreeNoDocStart(t *testing.T) {
	tree := map[string]interface{}{"key": "value"}

	withMarker, rc := renderMergedTreeWithReport(tree, nil, reportPlacementBeginning, false)
	if rc != 0 {
		t.Fatalf("rc = %d with marker", rc)
	}
	if !strings.HasPrefix(string(withMarker), "---\n") {
		t.Fatalf("default output must keep the document-start marker: %q", withMarker)
	}

	without, rc := renderMergedTreeWithReport(tree, nil, reportPlacementBeginning, true)
	if rc != 0 {
		t.Fatalf("rc = %d without marker", rc)
	}
	if strings.HasPrefix(string(without), "---") {
		t.Fatalf("noDocStart output must not start with the marker: %q", without)
	}
	if string(withMarker) != "---\n"+string(without) {
		t.Fatalf("noDocStart output must be the default minus the marker:\nwith:    %q\nwithout: %q",
			withMarker, without)
	}
}

// TestRenderMergedTreeNoDocStartPlacements covers the two non-default
// assembly paths (finishMergedDocument): "inline" and "end" placements
// with at least one deferred path, which the plain-placement test above
// never reaches.
func TestRenderMergedTreeNoDocStartPlacements(t *testing.T) {
	tree := map[string]interface{}{"key": "value"}
	deferred := []graft.DeferredPath{{Path: "key", Reason: "test reason"}}

	for _, placement := range []reportPlacement{reportPlacementBeginning, reportPlacementInline, reportPlacementEnd} {
		t.Run(string(placement), func(t *testing.T) {
			out, rc := renderMergedTreeWithReport(tree, deferred, placement, true)
			if rc != 0 {
				t.Fatalf("rc = %d", rc)
			}
			if strings.HasPrefix(string(out), "---") {
				t.Fatalf("placement %s: output must not start with the marker: %q", placement, out)
			}
			if !strings.Contains(string(out), "deferred") {
				t.Fatalf("placement %s: deferred report comment missing: %q", placement, out)
			}
		})
	}
}

func TestMergeNoDocStartFlag(t *testing.T) {
	stdout, stderr, rc := runGraftMerge(t, "merge", "--no-doc-start", "../../assets/merge/first.yml")
	if rc != 0 {
		t.Fatalf("rc = %d, stderr: %s", rc, stderr)
	}
	if strings.HasPrefix(stdout, "---") {
		t.Fatalf("--no-doc-start output must not start with the marker: %q", stdout)
	}

	withMarker, stderr, rc := runGraftMerge(t, "merge", "../../assets/merge/first.yml")
	if rc != 0 {
		t.Fatalf("default merge rc = %d, stderr: %s", rc, stderr)
	}
	if withMarker != "---\n"+stdout {
		t.Fatalf("--no-doc-start output must be the default minus the marker:\ndefault: %q\nflagged: %q",
			withMarker, stdout)
	}
}

func TestMergeNoDocStartEnv(t *testing.T) {
	t.Setenv("GRAFT_NO_DOC_START", "1")
	stdout, stderr, rc := runGraftMerge(t, "merge", "../../assets/merge/first.yml")
	if rc != 0 {
		t.Fatalf("rc = %d, stderr: %s", rc, stderr)
	}
	if strings.HasPrefix(stdout, "---") {
		t.Fatalf("GRAFT_NO_DOC_START=1 output must not start with the marker: %q", stdout)
	}
}

func TestMergeNoDocStartFlagOverridesEnv(t *testing.T) {
	t.Setenv("GRAFT_NO_DOC_START", "1")
	stdout, stderr, rc := runGraftMerge(t, "merge", "--no-doc-start=false", "../../assets/merge/first.yml")
	if rc != 0 {
		t.Fatalf("rc = %d, stderr: %s", rc, stderr)
	}
	if !strings.HasPrefix(stdout, "---\n") {
		t.Fatalf("explicit --no-doc-start=false must beat the env var and keep the marker: %q", stdout)
	}
}

// The cached-merge replay path stores the rendered stdout bytes, so the
// marker choice must participate in the cache key or a --no-doc-start
// run could replay a marker-bearing entry (and vice versa).
func TestMergeOutputCacheKeyNoDocStartSensitivity(t *testing.T) {
	opts := &mergeOpts{}
	inputs := docs("a: 1\n", "b: 2\n")
	baseKey := mergeOutputCacheKey(opts, inputs, false)

	opts.NoDocStart = true
	if mergeOutputCacheKey(opts, inputs, false) == baseKey {
		t.Fatal("NoDocStart must change the merge output cache key")
	}
}
