package main

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestYQFrontMatter(t *testing.T) {
	md := "---\ntitle: x\n---\n# body\n"

	Convey("front matter", t, func() {
		out, _, st := runYQ(t, []string{"-f", "extract", ".title"}, md)
		So(st.Code, ShouldEqual, 0)
		So(out, ShouldEqual, "x\n")
		out, _, _ = runYQ(t, []string{"-f", "process", `.title = "y"`}, md)
		So(out, ShouldEqual, "---\ntitle: y\n---\n# body\n")
		out, _, _ = runYQ(t, []string{"-f", "extract", `.title = "y"`}, md)
		So(out, ShouldEqual, "---\ntitle: y\n")
		out, _, _ = runYQ(t, []string{"-f", "other", "."}, md)
		So(out, ShouldEqual, "---\ntitle: x\n") // anything but process extracts
	})

	Convey("front matter prints the content once, after the first output", t, func() {
		out, _, _ := runYQ(t, []string{"-f", "process", ".l[]"}, "---\nl: [1, 2]\n---\nbody\n")
		So(out, ShouldEqual, "1\n2\n---\nbody\n")
		out, _, _ = runYQ(t, []string{"-f", "process", "select(.q)"}, md)
		So(out, ShouldEqual, "") // nothing matched, so nothing is printed
		out, _, _ = runYQ(t, []string{"-f", "process", "-o", "json", "."}, md)
		So(out, ShouldEqual, "{\n  \"title\": \"x\"\n}\n---\n# body\n")
		out, _, _ = runYQ(t, []string{"-f", "process", ".title = \"y\""}, "---\ntitle: x\n---\n")
		So(out, ShouldEqual, "---\ntitle: y\n---\n")
	})

	Convey("input with no front matter reads as plain YAML", t, func() {
		out, _, _ := runYQ(t, []string{"-f", "process", ".a = 1"}, "no front\n")
		So(out, ShouldEqual, "no front\n")
		out, _, _ = runYQ(t, []string{"-f", "extract", "."}, "title: x\n---\nbody\n")
		So(out, ShouldEqual, "title: x\n---\nbody\n")
		out, _, _ = runYQ(t, []string{"-f", "process", ".n = 1"}, "---\nt: 1\n")
		So(out, ShouldEqual, "---\nt: 1\nn: 1\n") // unterminated, so no content
	})

	Convey("front matter applies to the first file only, and -i rewrites it", t, func() {
		dir := t.TempDir()
		So(os.WriteFile(filepath.Join(dir, "a.md"), []byte(md), 0o600), ShouldBeNil)
		So(os.WriteFile(filepath.Join(dir, "z.yml"), []byte("z: 9\n"), 0o600), ShouldBeNil)
		out, _, _ := runYQIn(t, dir, []string{"-f", "process", ".z", "a.md", "z.yml"})
		So(out, ShouldEqual, "null\n---\n# body\n---\n9\n")
		_, _, st := runYQIn(t, dir, []string{"-i", "-f", "process", `.title = "q"`, "a.md"})
		So(st.Code, ShouldEqual, 0)
		data, err := os.ReadFile(filepath.Join(dir, "a.md"))
		So(err, ShouldBeNil)
		So(string(data), ShouldEqual, "---\ntitle: q\n---\n# body\n")
	})

	Convey("front matter with a split leaves the content in the last split file", t, func() {
		dir := t.TempDir()
		_, _, st := runYQ2(t, dir, []string{"-f", "process", "-s", `"o"`, ".t = 2"}, "---\nt: 1\n---\nbody\n")
		So(st.Code, ShouldEqual, 0)
		So(readSplit(t, filepath.Join(dir, "o.yml")), ShouldEqual, "---\nbody\n")
	})
}
