package main

import (
	"fmt"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

// withVersionVars runs fn with the build-stamped version variables and the
// build-info reader swapped out, restoring all four afterward so the
// package-level state never leaks between test cases.
func withVersionVars(commit, buildDate string, info *debug.BuildInfo, infoOK bool, fn func()) {
	prevCommit, prevDate, prevReader := Commit, BuildDate, readBuildInfo
	defer func() {
		Commit, BuildDate, readBuildInfo = prevCommit, prevDate, prevReader
	}()
	Commit, BuildDate = commit, buildDate
	readBuildInfo = func() (*debug.BuildInfo, bool) { return info, infoOK }
	fn()
}

// buildInfoWith builds a *debug.BuildInfo carrying just the vcs settings a
// test cares about.
func buildInfoWith(settings map[string]string) *debug.BuildInfo {
	info := &debug.BuildInfo{}
	for key, value := range settings {
		info.Settings = append(info.Settings, debug.BuildSetting{Key: key, Value: value})
	}
	return info
}

func TestVersionOutput(t *testing.T) {
	Convey("Version output", t, func() {
		Convey("Default shape names graft and the build stamps", func() {
			withVersionVars("5ac4f795d3c1b2a", "2026-08-24T22:05:31Z", nil, false, func() {
				out := versionOutput("graft")
				So(out, ShouldEqual, fmt.Sprintf(
					"graft version %s (commit: 5ac4f79, built: 2026-08-24T22:05:31Z, go: %s, os/arch: %s/%s)\n",
					Version, runtime.Version(), runtime.GOOS, runtime.GOARCH))
			})
		})

		Convey("A short commit from the linker is left as-is", func() {
			withVersionVars("5ac4f79", "2026-08-24T22:05:31Z", nil, false, func() {
				So(versionOutput("graft"), ShouldContainSubstring, "commit: 5ac4f79,")
			})
		})

		Convey("Falls back to the toolchain-embedded revision and time", func() {
			info := buildInfoWith(map[string]string{
				"vcs.revision": "abcdef1234567890",
				"vcs.time":     "2026-08-01T10:00:00Z",
			})
			withVersionVars("", "", info, true, func() {
				out := versionOutput("graft")
				So(out, ShouldContainSubstring, "commit: abcdef1,")
				So(out, ShouldContainSubstring, "built: 2026-08-01T10:00:00Z,")
			})
		})

		Convey("Marks an embedded revision from a modified tree as dirty", func() {
			info := buildInfoWith(map[string]string{
				"vcs.revision": "abcdef1234567890",
				"vcs.modified": "true",
			})
			withVersionVars("", "", info, true, func() {
				So(versionOutput("graft"), ShouldContainSubstring, "commit: abcdef1-dirty,")
			})
		})

		Convey("A linker commit never picks up the dirty marker", func() {
			info := buildInfoWith(map[string]string{
				"vcs.revision": "abcdef1234567890",
				"vcs.modified": "true",
			})
			withVersionVars("5ac4f79", "2026-08-24T22:05:31Z", info, true, func() {
				So(versionOutput("graft"), ShouldContainSubstring, "commit: 5ac4f79,")
			})
		})

		Convey("Reports unknown when nothing supplies commit or date", func() {
			withVersionVars("", "", nil, false, func() {
				out := versionOutput("graft")
				So(out, ShouldContainSubstring, "commit: unknown,")
				So(out, ShouldContainSubstring, "built: unknown,")
			})
		})

		Convey("Under a spruce name the spruce line comes first", func() {
			withVersionVars("5ac4f79", "2026-08-24T22:05:31Z", nil, false, func() {
				out := versionOutput("/usr/local/bin/spruce")
				lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
				So(lines, ShouldHaveLength, 2)
				// Byte for byte what spruce itself prints: argv[0]
				// verbatim, full path included.
				So(lines[0], ShouldEqual, fmt.Sprintf("/usr/local/bin/spruce - Version %s", Version))
				So(lines[1], ShouldEqual, fmt.Sprintf(
					"graft version %s (commit: 5ac4f79, built: 2026-08-24T22:05:31Z, go: %s, os/arch: %s/%s)",
					Version, runtime.Version(), runtime.GOOS, runtime.GOARCH))
			})
		})

		Convey("Genesis's probe still matches the spruce-mode output", func() {
			// genesis's check_prereqs() applies qr(.*version\s+(\S+).*)i
			// to the whole captured stdout; the spruce line must be the
			// one it lands on.
			probe := regexp.MustCompile(`(?i).*version\s+(\S+).*`)
			matches := probe.FindStringSubmatch(versionOutput("spruce"))
			So(matches, ShouldNotBeNil)
			So(matches[1], ShouldEqual, Version)
		})

		Convey("Genesis's probe still matches the default output", func() {
			probe := regexp.MustCompile(`(?i).*version\s+(\S+).*`)
			matches := probe.FindStringSubmatch(versionOutput("graft"))
			So(matches, ShouldNotBeNil)
			So(matches[1], ShouldEqual, Version)
		})

		Convey("Spruce mode keys off the invoked base name", func() {
			So(invokedAsSpruce("spruce"), ShouldBeTrue)
			So(invokedAsSpruce("./spruce"), ShouldBeTrue)
			So(invokedAsSpruce("/opt/homebrew/bin/spruce"), ShouldBeTrue)
			So(invokedAsSpruce("spruce.exe"), ShouldBeTrue)
			So(invokedAsSpruce("SPRUCE"), ShouldBeTrue)

			So(invokedAsSpruce("graft"), ShouldBeFalse)
			So(invokedAsSpruce("/usr/local/bin/graft"), ShouldBeFalse)
			So(invokedAsSpruce("graft-darwin-arm64"), ShouldBeFalse)
			So(invokedAsSpruce("spruce-head"), ShouldBeFalse)
			So(invokedAsSpruce(""), ShouldBeFalse)
		})

		Convey("A non-spruce name prints only graft's own line", func() {
			out := versionOutput("./graft-head")
			So(strings.Count(out, "\n"), ShouldEqual, 1)
			So(out, ShouldNotContainSubstring, " - Version ")
			So(out, ShouldStartWith, "graft version ")
		})
	})
}
