package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/log"
)

// TestMergeDeleteGuardCLI locks the CLI contract for an argument-bearing
// (( delete ... )) in map-value position: exit 2 with the merge-error
// wording on both merge paths, instead of the marker leaking into the
// output as literal data with exit 0. The bare (( delete )) form keeps its
// passthrough (spruce parity) with exit 0.
func TestMergeDeleteGuardCLI(t *testing.T) {
	var stdout, stderr string
	printStdOutf = func(format string, args ...interface{}) {
		stdout += fmt.Sprintf(format, args...)
	}
	log.PrintStdErrf = func(format string, args ...interface{}) {
		stderr += fmt.Sprintf(format, args...)
	}

	rc := 256
	exit = func(code int) { rc = code }
	usage = func() {
		stderr = "usage was called"
		exit(1)
	}

	reset := func() {
		stdout = ""
		stderr = ""
		rc = 256
	}

	dir := t.TempDir()
	write := func(name, content string) string {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("failed to write fixture %s: %v", name, err)
		}
		return path
	}

	base := write("base.yml", "scalarkey: hello\ntags:\n- x\n")
	overlayFast := write("overlay-fast.yml", "scalarkey: (( delete \"hello\" ))\n")
	overlayLegacy := write("overlay-legacy.yml", "scalarkey: (( delete \"hello\" ))\ntags:\n- (( replace ))\n- x\n")
	overlayBare := write("overlay-bare.yml", "scalarkey: (( delete ))\n")

	const guardMsg = `$.scalarkey: inappropriate use of (( delete )) operator outside of a list`

	Convey("graft merge with (( delete ... )) in map-value position", t, func() {
		Convey("errors with exit 2 on the simple-merge fast path", func() {
			reset()
			os.Args = []string{"graft", "merge", base, overlayFast}
			main()
			So(stdout, ShouldEqual, "")
			So(stderr, ShouldContainSubstring, guardMsg)
			So(rc, ShouldEqual, 2)
		})

		Convey("errors with exit 2 on the legacy merger path", func() {
			reset()
			os.Args = []string{"graft", "merge", base, overlayLegacy}
			main()
			So(stdout, ShouldEqual, "")
			So(stderr, ShouldContainSubstring, guardMsg)
			So(rc, ShouldEqual, 2)
		})

		Convey("bare (( delete )) still passes through with exit 0 (spruce parity)", func() {
			reset()
			os.Args = []string{"graft", "merge", base, overlayBare}
			main()
			So(stderr, ShouldEqual, "")
			So(rc, ShouldEqual, 0)
			So(stdout, ShouldContainSubstring, "scalarkey: (( delete ))")
		})
	})
}
