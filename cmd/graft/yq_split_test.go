package main

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/pkg/query"
)

func readSplit(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- a test reads its own output
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// runYQ2 runs yq in dir with stdin as its input.
func runYQ2(t *testing.T, dir string, args []string, stdin string) (out, errOut string, st query.Status) {
	t.Helper()
	t.Chdir(dir)
	return runYQ(t, args, stdin)
}

func TestYQSplit(t *testing.T) {
	Convey("split expressions", t, func() {
		dir := t.TempDir()
		out, _, st := runYQ2(t, dir, []string{"-s", `.name`, ".[]"}, "- name: a\n  v: 1\n- name: b\n  v: 2\n")
		So(st.Code, ShouldEqual, 0)
		So(out, ShouldEqual, "")
		So(readSplit(t, filepath.Join(dir, "a.yml")), ShouldEqual, "name: a\nv: 1\n")
		So(readSplit(t, filepath.Join(dir, "b.yml")), ShouldEqual, "name: b\nv: 2\n")
	})

	Convey("split files are json under -o json", t, func() {
		dir := t.TempDir()
		_, _, st := runYQ2(t, dir, []string{"-s", ".name", "-o", "json", ".[]"}, "- name: a\n")
		So(st.Code, ShouldEqual, 0)
		So(readSplit(t, filepath.Join(dir, "a.json")), ShouldEqual, "{\n  \"name\": \"a\"\n}\n")
	})

	Convey("a name that has an extension keeps it", t, func() {
		dir := t.TempDir()
		runYQ2(t, dir, []string{"-s", `.name`, ".[]"}, "- name: a.out\n- name: b.\n- name: .c\n")
		So(readSplit(t, filepath.Join(dir, "a.out")), ShouldEqual, "name: a.out\n")
		So(readSplit(t, filepath.Join(dir, "b..yml")), ShouldEqual, "name: b.\n")
		So(readSplit(t, filepath.Join(dir, ".c")), ShouldEqual, "name: .c\n")
	})

	Convey("$index counts the files written", t, func() {
		dir := t.TempDir()
		runYQ2(t, dir, []string{"-s", `"doc_" + $index`, ".[]"}, "[a, b]\n---\n[c]\n")
		So(readSplit(t, filepath.Join(dir, "doc_0.yml")), ShouldEqual, "a\n")
		So(readSplit(t, filepath.Join(dir, "doc_2.yml")), ShouldEqual, "---\nc\n")
	})

	Convey("a later document with the same name replaces the earlier one", t, func() {
		dir := t.TempDir()
		runYQ2(t, dir, []string{"-s", ".name", ".[]"}, "- name: a\n  v: 1\n- name: a\n  v: 2\n")
		So(readSplit(t, filepath.Join(dir, "a.yml")), ShouldEqual, "name: a\nv: 2\n")
	})

	Convey("the name is the first value, as text", t, func() {
		dir := t.TempDir()
		runYQ2(t, dir, []string{"-s", ".", ".[]"}, "- 1\n- {x: y}\n- null\n")
		So(readSplit(t, filepath.Join(dir, "1.yml")), ShouldEqual, "1\n")
		So(readSplit(t, filepath.Join(dir, ".yml")), ShouldEqual, "{x: y}\n")
		So(readSplit(t, filepath.Join(dir, "null.yml")), ShouldEqual, "null\n")
		runYQ2(t, dir, []string{"-s", ".a, .b", "."}, "a: 1\nb: 2\n")
		So(readSplit(t, filepath.Join(dir, "1.yml")), ShouldEqual, "a: 1\nb: 2\n")
		runYQ2(t, dir, []string{"-s", "select(false)", "."}, "a: 1\n")
		So(readSplit(t, filepath.Join(dir, ".yml")), ShouldEqual, "a: 1\n")
	})

	Convey("a name with a directory makes the directory", t, func() {
		dir := t.TempDir()
		runYQ2(t, dir, []string{"-s", `"sub/" + .name`, "."}, "name: x\n")
		So(readSplit(t, filepath.Join(dir, "sub", "x.yml")), ShouldEqual, "name: x\n")
	})

	Convey("a second document in a file starts with a separator", t, func() {
		dir := t.TempDir()
		runYQ2(t, dir, []string{"-s", ".n", "."}, "n: a\n---\nn: b\n")
		So(readSplit(t, filepath.Join(dir, "a.yml")), ShouldEqual, "n: a\n")
		So(readSplit(t, filepath.Join(dir, "b.yml")), ShouldEqual, "---\nn: b\n")
	})

	Convey("--split-exp-file reads the expression, and wins over -s", t, func() {
		dir := t.TempDir()
		So(os.WriteFile(filepath.Join(dir, "e.yq"), []byte(".name + \"_f\"\n"), 0o600), ShouldBeNil)
		_, _, st := runYQ2(t, dir, []string{"-s", `"s"`, "--split-exp-file", "e.yq", ".[]"}, "- name: a\n")
		So(st.Code, ShouldEqual, 0)
		So(readSplit(t, filepath.Join(dir, "a_f.yml")), ShouldEqual, "name: a\n")
		_, e, st := runYQ2(t, dir, []string{"--split-exp-file", "nope.yq", "."}, "a: 1\n")
		So(st.Code, ShouldEqual, 1)
		So(e, ShouldContainSubstring, "nope.yq")
	})

	Convey("split output follows the output options", t, func() {
		dir := t.TempDir()
		runYQ2(t, dir, []string{"-s", `"s"`, ".a"}, "a: x\n")
		So(readSplit(t, filepath.Join(dir, "s.yml")), ShouldEqual, "x\n")
		runYQ2(t, dir, []string{"-n", "-s", `"n"`, `{"a": 1}`}, "")
		So(readSplit(t, filepath.Join(dir, "n.yml")), ShouldEqual, "a: 1\n")
	})

	Convey("a bad or failing name expression writes nothing", t, func() {
		dir := t.TempDir()
		_, e, st := runYQ2(t, dir, []string{"-s", ".[", "."}, "a: 1\n")
		So(st.Code, ShouldEqual, 1)
		So(e, ShouldContainSubstring, "bad split document expression")
		_, _, st = runYQ2(t, dir, []string{"-s", `error("x")`, "."}, "a: 1\n")
		So(st.Code, ShouldEqual, 1)
		entries, err := os.ReadDir(dir)
		So(err, ShouldBeNil)
		So(entries, ShouldBeEmpty)
	})

	Convey("-i cannot be combined with a split", t, func() {
		dir := t.TempDir()
		So(os.WriteFile(filepath.Join(dir, "in.yml"), []byte("a: 1\n"), 0o600), ShouldBeNil)
		_, e, st := runYQ2(t, dir, []string{"-i", "-s", `"s"`, ".", "in.yml"}, "")
		So(st.Code, ShouldEqual, 1)
		So(e, ShouldContainSubstring, "write in place cannot be used with split file")
	})
}
