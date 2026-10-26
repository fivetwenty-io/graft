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

	Convey("a module that cannot be found is a compile error", t, func() {
		_, errOut, st := runJQ(t, []string{"-L", lib, `import "nope" as n; 1`}, "null")
		So(st.Code, ShouldEqual, 3)
		So(errOut, ShouldContainSubstring, "module not found: nope")
	})
}
