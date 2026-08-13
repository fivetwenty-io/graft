package graft_test

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/pkg/graft"
	_ "github.com/fivetwenty-io/graft/pkg/graft/operators"
)

func TestQuickMerge(t *testing.T) {
	Convey("QuickMerge", t, func() {
		Convey("merges YAML sources in order and evaluates operators", func() {
			out, err := graft.QuickMerge(
				"name: myapp\nversion: \"1.0\"\n",
				"version: \"2.0\"\ngreeting: (( concat \"hello \" name ))\n",
			)
			So(err, ShouldBeNil)
			So(string(out), ShouldContainSubstring, "name: myapp")
			So(string(out), ShouldContainSubstring, "version: \"2.0\"")
			So(string(out), ShouldContainSubstring, "greeting: hello myapp")
		})

		Convey("propagates parse errors", func() {
			_, err := graft.QuickMerge("name: myapp\n", ":\t{not yaml")
			So(err, ShouldNotBeNil)
		})

		Convey("propagates evaluation errors", func() {
			_, err := graft.QuickMerge("v: (( grab missing.path ))\n")
			So(err, ShouldNotBeNil)
			So(err.Error(), ShouldContainSubstring, "could not be found")
		})

		Convey("with no sources returns an empty document", func() {
			out, err := graft.QuickMerge()
			So(err, ShouldBeNil)
			So(string(out), ShouldEqual, "{}\n")
		})
	})
}

func TestQuickMergeFiles(t *testing.T) {
	Convey("QuickMergeFiles", t, func() {
		dir := t.TempDir()
		base := filepath.Join(dir, "base.yml")
		override := filepath.Join(dir, "override.yml")
		So(os.WriteFile(base, []byte("name: myapp\nreplicas: 1\n"), 0o644), ShouldBeNil)
		So(os.WriteFile(override, []byte("replicas: 3\n"), 0o644), ShouldBeNil)

		Convey("merges files in order", func() {
			out, err := graft.QuickMergeFiles(base, override)
			So(err, ShouldBeNil)
			So(string(out), ShouldContainSubstring, "name: myapp")
			So(string(out), ShouldContainSubstring, "replicas: 3")
		})

		Convey("propagates missing-file errors", func() {
			_, err := graft.QuickMergeFiles(base, filepath.Join(dir, "nope.yml"))
			So(err, ShouldNotBeNil)
		})

		Convey("with no paths returns an empty document", func() {
			out, err := graft.QuickMergeFiles()
			So(err, ShouldBeNil)
			So(string(out), ShouldEqual, "{}\n")
		})
	})
}
