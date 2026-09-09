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
	return runGraftMergeAs(t, "graft", args...)
}

// runGraftMergeAs is runGraftMerge with an explicit argv[0], so the
// spruce-name default of resolveNoDocStart can be exercised through the
// real flag/env/os.Args resolution path.
func runGraftMergeAs(t *testing.T, argv0 string, args ...string) (string, string, int) {
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

	os.Args = append([]string{argv0}, args...)
	main()
	return stdout, stderr, rc
}

func TestResolveNoDocStart(t *testing.T) {
	cases := []struct {
		name        string
		flagChanged bool
		flagValue   bool
		envValue    string
		argv0       string
		want        bool
	}{
		{"graft name, nothing set, keeps marker", false, false, "", "graft", false},
		{"graft full path, nothing set, keeps marker", false, false, "", "/usr/local/bin/graft", false},
		{"empty argv0 keeps marker", false, false, "", "", false},
		{"spruce name, nothing set, omits marker", false, false, "", "spruce", true},
		{"spruce full path omits marker", false, false, "", "/opt/homebrew/bin/spruce", true},
		{"spruce.exe omits marker", false, false, "", "spruce.exe", true},
		{"spruce-head is not spruce", false, false, "", "spruce-head", false},
		{"flag suppresses", true, true, "", "graft", true},
		{"env true suppresses", false, false, "true", "graft", true},
		{"env 1 suppresses", false, false, "1", "graft", true},
		{"env yes suppresses", false, false, "yes", "graft", true},
		{"env on suppresses", false, false, "on", "graft", true},
		{"env false keeps marker", false, false, "false", "graft", false},
		{"env 0 keeps marker", false, false, "0", "graft", false},
		{"env garbage ignored under graft", false, false, "definitely", "graft", false},
		{"env garbage ignored under spruce", false, false, "definitely", "spruce", true},
		{"env false beats spruce default", false, false, "false", "spruce", false},
		{"env 0 beats spruce default", false, false, "0", "spruce", false},
		{"env no beats spruce default", false, false, "no", "spruce", false},
		{"env off beats spruce default", false, false, "off", "spruce", false},
		{"env OFF with whitespace beats spruce default", false, false, " OFF ", "spruce", false},
		{"env true under spruce still omits", false, false, "true", "spruce", true},
		{"explicit flag false beats env true", true, false, "1", "graft", false},
		{"explicit flag true beats env false", true, true, "false", "graft", true},
		{"explicit flag false beats spruce default", true, false, "", "spruce", false},
		{"explicit flag false beats env true and spruce", true, false, "1", "spruce", false},
		{"explicit flag true beats env false under spruce", true, true, "false", "spruce", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveNoDocStart(tc.flagChanged, tc.flagValue, tc.envValue, tc.argv0)
			if got != tc.want {
				t.Errorf("resolveNoDocStart(%v, %v, %q, %q) = %v, want %v",
					tc.flagChanged, tc.flagValue, tc.envValue, tc.argv0, got, tc.want)
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

// Under a spruce name (argv[0] base "spruce", the Genesis drop-in
// deployment) merge must match spruce's own output byte for byte, so the
// marker goes away with no flag and no env var.
func TestMergeNoDocStartUnderSpruceName(t *testing.T) {
	for _, argv0 := range []string{"spruce", "/usr/local/bin/spruce", "./spruce.exe"} {
		t.Run(argv0, func(t *testing.T) {
			asSpruce, stderr, rc := runGraftMergeAs(t, argv0, "merge", "../../assets/merge/first.yml")
			if rc != 0 {
				t.Fatalf("rc = %d, stderr: %s", rc, stderr)
			}
			if strings.HasPrefix(asSpruce, "---") {
				t.Fatalf("merge under %q must not start with the marker: %q", argv0, asSpruce)
			}

			asGraft, stderr, rc := runGraftMerge(t, "merge", "../../assets/merge/first.yml")
			if rc != 0 {
				t.Fatalf("graft-name merge rc = %d, stderr: %s", rc, stderr)
			}
			if asGraft != "---\n"+asSpruce {
				t.Fatalf("spruce-name output must be the graft-name output minus the marker:\ngraft:  %q\nspruce: %q",
					asGraft, asSpruce)
			}
		})
	}
}

// Genesis's own invocation shape: `spruce merge --skip-eval -` on stdin.
// This is the call whose output Genesis prepends a header to when it
// writes .genesis/config, so it is the one that must not carry a marker.
func TestMergeNoDocStartUnderSpruceNameSkipEvalStdin(t *testing.T) {
	prevStdin := os.Stdin
	defer func() { os.Stdin = prevStdin }()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.WriteString("key: value\nlist:\n- a\n- b\n"); err != nil {
		t.Fatal(err)
	}
	_ = w.Close()
	os.Stdin = r

	stdout, stderr, rc := runGraftMergeAs(t, "spruce", "merge", "--skip-eval", "-")
	if rc != 0 {
		t.Fatalf("rc = %d, stderr: %s", rc, stderr)
	}
	if strings.HasPrefix(stdout, "---") {
		t.Fatalf("spruce merge --skip-eval - must not start with the marker: %q", stdout)
	}
	if !strings.HasPrefix(stdout, "key: value\n") {
		t.Fatalf("unexpected output shape: %q", stdout)
	}
}

// An operator can force the marker back on under the spruce name with a
// false GRAFT_NO_DOC_START value; an explicit flag beats even that.
func TestMergeNoDocStartSpruceNameEnvFalseRestoresMarker(t *testing.T) {
	for _, val := range []string{"false", "0", "no", "off"} {
		t.Run(val, func(t *testing.T) {
			t.Setenv("GRAFT_NO_DOC_START", val)
			stdout, stderr, rc := runGraftMergeAs(t, "spruce", "merge", "../../assets/merge/first.yml")
			if rc != 0 {
				t.Fatalf("rc = %d, stderr: %s", rc, stderr)
			}
			if !strings.HasPrefix(stdout, "---\n") {
				t.Fatalf("GRAFT_NO_DOC_START=%s under the spruce name must keep the marker: %q", val, stdout)
			}
		})
	}

	t.Run("explicit flag beats env false", func(t *testing.T) {
		t.Setenv("GRAFT_NO_DOC_START", "false")
		stdout, stderr, rc := runGraftMergeAs(t, "spruce", "merge", "--no-doc-start", "../../assets/merge/first.yml")
		if rc != 0 {
			t.Fatalf("rc = %d, stderr: %s", rc, stderr)
		}
		if strings.HasPrefix(stdout, "---") {
			t.Fatalf("--no-doc-start must beat GRAFT_NO_DOC_START=false: %q", stdout)
		}
	})

	t.Run("explicit flag false beats spruce default", func(t *testing.T) {
		stdout, stderr, rc := runGraftMergeAs(t, "spruce", "merge", "--no-doc-start=false", "../../assets/merge/first.yml")
		if rc != 0 {
			t.Fatalf("rc = %d, stderr: %s", rc, stderr)
		}
		if !strings.HasPrefix(stdout, "---\n") {
			t.Fatalf("--no-doc-start=false under the spruce name must keep the marker: %q", stdout)
		}
	})
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
