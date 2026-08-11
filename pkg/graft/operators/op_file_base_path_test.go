package operators

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// TestResolveWithFileBasePath locks the GRAFT_FILE_BASE_PATH /
// SPRUCE_FILE_BASE_PATH precedence and absolute-path bypass used by the
// (( file )) and (( load )) operators, matching spruce's op_file.go /
// op_load.go semantics.
func TestResolveWithFileBasePath(t *testing.T) {
	Convey("resolveWithFileBasePath", t, func() {
		Convey("relative path is joined with GRAFT_FILE_BASE_PATH when only that var is set", func() {
			t.Setenv("GRAFT_FILE_BASE_PATH", "/graft/base")
			t.Setenv("SPRUCE_FILE_BASE_PATH", "")

			got := resolveWithFileBasePath("sub/file.yml")
			So(got, ShouldEqual, filepath.Join("/graft/base", "sub/file.yml"))
		})

		Convey("relative path falls back to SPRUCE_FILE_BASE_PATH when GRAFT_FILE_BASE_PATH is unset", func() {
			t.Setenv("GRAFT_FILE_BASE_PATH", "")
			t.Setenv("SPRUCE_FILE_BASE_PATH", "/spruce/base")

			got := resolveWithFileBasePath("sub/file.yml")
			So(got, ShouldEqual, filepath.Join("/spruce/base", "sub/file.yml"))
		})

		Convey("GRAFT_FILE_BASE_PATH wins when both vars are set", func() {
			t.Setenv("GRAFT_FILE_BASE_PATH", "/graft/base")
			t.Setenv("SPRUCE_FILE_BASE_PATH", "/spruce/base")

			got := resolveWithFileBasePath("sub/file.yml")
			So(got, ShouldEqual, filepath.Join("/graft/base", "sub/file.yml"))
		})

		Convey("relative path is left relative when neither var is set", func() {
			t.Setenv("GRAFT_FILE_BASE_PATH", "")
			t.Setenv("SPRUCE_FILE_BASE_PATH", "")

			got := resolveWithFileBasePath("sub/file.yml")
			So(got, ShouldEqual, filepath.Join("sub/file.yml"))
		})

		Convey("absolute path bypasses both base path vars", func() {
			t.Setenv("GRAFT_FILE_BASE_PATH", "/graft/base")
			t.Setenv("SPRUCE_FILE_BASE_PATH", "/spruce/base")

			got := resolveWithFileBasePath("/abs/sub/file.yml")
			So(got, ShouldEqual, "/abs/sub/file.yml")
		})
	})
}

// TestFileOperatorBasePathEnv exercises (( file )) end to end through the
// engine to confirm GRAFT_FILE_BASE_PATH and its SPRUCE_FILE_BASE_PATH
// fallback resolve relative filenames against a real file on disk.
func TestFileOperatorBasePathEnv(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "greeting.txt"), []byte("hello base path"), 0o600); err != nil {
		t.Fatalf("failed to write fixture file: %s", err)
	}

	Convey("(( file )) resolves a relative path via GRAFT_FILE_BASE_PATH", t, func() {
		t.Setenv("SPRUCE_FILE_BASE_PATH", "")
		t.Setenv("GRAFT_FILE_BASE_PATH", dir)

		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
result: (( file "greeting.txt" ))
`))
		So(err, ShouldBeNil)

		out, err := engine.Evaluate(context.TODO(), doc)
		So(err, ShouldBeNil)

		val, err := out.GetString("result")
		So(err, ShouldBeNil)
		So(val, ShouldEqual, "hello base path")
	})

	Convey("(( file )) resolves a relative path via SPRUCE_FILE_BASE_PATH fallback", t, func() {
		t.Setenv("GRAFT_FILE_BASE_PATH", "")
		t.Setenv("SPRUCE_FILE_BASE_PATH", dir)

		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
result: (( file "greeting.txt" ))
`))
		So(err, ShouldBeNil)

		out, err := engine.Evaluate(context.TODO(), doc)
		So(err, ShouldBeNil)

		val, err := out.GetString("result")
		So(err, ShouldBeNil)
		So(val, ShouldEqual, "hello base path")
	})

	Convey("(( file )) prefers GRAFT_FILE_BASE_PATH when both vars are set", t, func() {
		wrongDir := t.TempDir()
		t.Setenv("SPRUCE_FILE_BASE_PATH", wrongDir)
		t.Setenv("GRAFT_FILE_BASE_PATH", dir)

		engine, err := graft.NewEngine()
		So(err, ShouldBeNil)

		doc, err := engine.ParseYAML([]byte(`
result: (( file "greeting.txt" ))
`))
		So(err, ShouldBeNil)

		out, err := engine.Evaluate(context.TODO(), doc)
		So(err, ShouldBeNil)

		val, err := out.GetString("result")
		So(err, ShouldBeNil)
		So(val, ShouldEqual, "hello base path")
	})
}
