package main

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

// TestBuildMergeHistoryStepsLimit locks the limit parameter's session-step
// semantics: limit k builds file steps 0..min(k, N-1), adds the
// "<evaluated>" step only when k >= N, and never builds the post step;
// limit -1 preserves the original full behavior.
func TestBuildMergeHistoryStepsLimit(t *testing.T) {
	files := []string{
		"../../assets/history/base.yml",
		"../../assets/history/env.yml",
		"../../assets/history/secrets.yml",
	}

	Convey("buildMergeHistorySteps limit", t, func() {
		opts := &mergeOpts{Files: files}

		Convey("limit -1 keeps the full step sequence", func() {
			steps, docCount, err := buildMergeHistorySteps(opts, nil, -1)
			So(err, ShouldBeNil)
			So(docCount, ShouldEqual, 3)
			So(len(steps), ShouldEqual, 4)
			So(steps[3].Label, ShouldEqual, "<evaluated>")
		})

		Convey("limit -1 with prune keeps the post step", func() {
			pruneOpts := &mergeOpts{Files: files, Prune: []string{"meta"}}
			steps, _, err := buildMergeHistorySteps(pruneOpts, nil, -1)
			So(err, ShouldBeNil)
			So(steps[len(steps)-1].Label, ShouldEqual, "<pruned>")
		})

		Convey("limit 0 builds only the base document's step", func() {
			steps, _, err := buildMergeHistorySteps(opts, nil, 0)
			So(err, ShouldBeNil)
			So(len(steps), ShouldEqual, 1)
			So(steps[0].Label, ShouldEndWith, "base.yml")
		})

		Convey("limit 1 stops after the second file with no eval step", func() {
			steps, _, err := buildMergeHistorySteps(opts, nil, 1)
			So(err, ShouldBeNil)
			So(len(steps), ShouldEqual, 2)
			So(steps[1].Label, ShouldEndWith, "env.yml")
		})

		Convey("limit equal to the file count adds evaluation", func() {
			steps, _, err := buildMergeHistorySteps(opts, nil, 3)
			So(err, ShouldBeNil)
			So(len(steps), ShouldEqual, 4)
			So(steps[3].Label, ShouldEqual, "<evaluated>")
		})

		Convey("limit beyond the file count behaves like the file count", func() {
			steps, _, err := buildMergeHistorySteps(opts, nil, 99)
			So(err, ShouldBeNil)
			So(len(steps), ShouldEqual, 4)
		})

		Convey("limit skips the post step even when prune is set", func() {
			pruneOpts := &mergeOpts{Files: files, Prune: []string{"meta"}}
			steps, _, err := buildMergeHistorySteps(pruneOpts, nil, 0)
			So(err, ShouldBeNil)
			So(len(steps), ShouldEqual, 1)
		})
	})
}
