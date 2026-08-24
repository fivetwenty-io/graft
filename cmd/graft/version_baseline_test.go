package main

import (
	"os"
	"regexp"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

// changelogReleaseHeading matches a released CHANGELOG section heading,
// `## [1.39.0] - 2026-08-24`, and skips `## [Unreleased]`.
var changelogReleaseHeading = regexp.MustCompile(`(?m)^## \[(\d+\.\d+\.\d+)\]`)

func TestVersionMatchesChangelog(t *testing.T) {
	Convey("The version baseline names the newest released version", t, func() {
		// A binary built without `-ldflags "-X main.Version=..."` reports
		// this baseline, so a release that forgets to bump it ships an
		// ad-hoc build claiming the previous release's number. Pinning it
		// to the CHANGELOG catches that in the release job's test run,
		// with no git tags or network needed.
		changelog, err := os.ReadFile("../../CHANGELOG.md")
		So(err, ShouldBeNil)

		newest := changelogReleaseHeading.FindSubmatch(changelog)
		So(newest, ShouldNotBeNil)
		So(Version, ShouldEqual, string(newest[1]))
	})
}
