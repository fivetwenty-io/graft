package main

import (
	"os"
	"path/filepath"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestJQLibraryPaths(t *testing.T) {
	dir := t.TempDir()
	lib := filepath.Join(dir, "lib")
	if err := os.MkdirAll(lib, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lib, "m.jq"), []byte(`def inc: . + 1;`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(lib, "d.json"), []byte(`{"k":3}`), 0o600); err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.EvalSymlinks(lib)
	if err != nil {
		t.Fatal(err)
	}

	Convey("graft jq -L searches the directories for imports and includes", t, func() {
		out, errOut, st := runJQ(t, []string{"-L", lib, `import "m" as m; m::inc`}, "1")
		So(out, ShouldEqual, "2\n")
		So(errOut, ShouldEqual, "")
		So(st.Code, ShouldEqual, 0)
		out, _, _ = runJQ(t, []string{"-L" + lib, `include "m"; inc`}, "1")
		So(out, ShouldEqual, "2\n")
		out, _, _ = runJQ(t, []string{"--library-path", lib, "-c", `import "d" as $d; $d`}, "null")
		So(out, ShouldEqual, "[{\"k\":3}]\n")
	})

	Convey("get_search_list reports the real path of each directory", t, func() {
		out, _, _ := runJQ(t, []string{"-c", "-L", lib, "-L", "~/.jq", "get_search_list"}, "null")
		So(out, ShouldEqual, "[\""+resolved+"\",\"~/.jq\"]\n")
	})

	Convey("without -L the search list is jq's default list", t, func() {
		out, _, _ := runJQ(t, []string{"-c", "get_search_list"}, "null")
		So(out, ShouldEqual, "[\"~/.jq\",\"$ORIGIN/../lib/jq\",\"$ORIGIN/../lib\"]\n")
	})

	Convey("a module that cannot be found is a compile error", t, func() {
		_, errOut, st := runJQ(t, []string{"-L", lib, `import "nope" as n; 1`}, "null")
		So(st.Code, ShouldEqual, 3)
		So(errOut, ShouldContainSubstring, "module not found: nope")
	})
}

func TestJQHomeAutoInclude(t *testing.T) {
	home := func(t *testing.T, body string) string {
		t.Helper()
		dir := t.TempDir()
		if body != "" {
			if err := os.WriteFile(filepath.Join(dir, ".jq"), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		t.Setenv("HOME", dir)
		return dir
	}

	Convey("a ~/.jq file is included in every program", t, func() {
		home(t, `def hello: "hi";`)
		out, errOut, st := runJQ(t, []string{"-n", "hello"}, "")
		So(out, ShouldEqual, "\"hi\"\n")
		So(errOut, ShouldEqual, "")
		So(st.Code, ShouldEqual, 0)
		out, _, _ = runJQ(t, []string{"-n", `def hello: "mine"; hello`}, "")
		So(out, ShouldEqual, "\"mine\"\n")
	})

	Convey("a missing ~/.jq changes nothing", t, func() {
		home(t, "")
		out, _, st := runJQ(t, []string{"-n", "1"}, "")
		So(out, ShouldEqual, "1\n")
		So(st.Code, ShouldEqual, 0)
		_, errOut, st := runJQ(t, []string{"-n", "hello"}, "")
		So(st.Code, ShouldEqual, 3)
		So(errOut, ShouldContainSubstring, "hello/0 is not defined")
	})

	Convey("a ~/.jq directory is a search path and is not included", t, func() {
		dir := home(t, "")
		lib := filepath.Join(dir, ".jq")
		So(os.MkdirAll(lib, 0o750), ShouldBeNil)
		So(os.WriteFile(filepath.Join(lib, "m.jq"), []byte(`def inc: . + 1;`), 0o600), ShouldBeNil)
		out, _, st := runJQ(t, []string{`import "m" as m; m::inc`}, "1")
		So(out, ShouldEqual, "2\n")
		So(st.Code, ShouldEqual, 0)
	})

	Convey("a ~/.jq that does not parse is a compile error", t, func() {
		home(t, `def x: ;`)
		_, errOut, st := runJQ(t, []string{"-n", "1"}, "")
		So(st.Code, ShouldEqual, 3)
		So(errOut, ShouldContainSubstring, ".jq")
	})

	Convey("a ~/.jq with a main expression is a compile error", t, func() {
		home(t, `def f: 1; f`)
		_, errOut, st := runJQ(t, []string{"-n", "1"}, "")
		So(st.Code, ShouldEqual, 3)
		So(errOut, ShouldContainSubstring, "library should only have function definitions")
	})

	Convey("a ~/.jq that cannot be read is skipped", t, func() {
		dir := home(t, `def hello: "hi";`)
		So(os.Chmod(filepath.Join(dir, ".jq"), 0), ShouldBeNil)
		if f, err := os.Open(filepath.Join(dir, ".jq")); err == nil {
			_ = f.Close()
			SkipSo("the file is readable despite mode 0", ShouldBeNil)
		}
		out, _, st := runJQ(t, []string{"-n", "1"}, "")
		So(out, ShouldEqual, "1\n")
		So(st.Code, ShouldEqual, 0)
	})
}
