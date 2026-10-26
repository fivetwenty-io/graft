package main

import (
	"bytes"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

// graftMergeOutput is what `graft merge FILES` writes, which -m must feed
// to the query.
func graftMergeOutput(t *testing.T, files ...string) string {
	t.Helper()
	out, errText, code := composeQueryInputs(composeContext{Stdin: bytes.NewReader(nil)}, files)
	if code != 0 {
		t.Fatalf("compose %v: %s", files, errText)
	}
	return string(out)
}

func TestQueryCompose(t *testing.T) {
	Convey("-m equals graft merge piped into jq", t, func() {
		want := graftMergeOutput(t, "testdata/compose/a.yml", "testdata/compose/b.yml")
		wantJQ, _, _ := runJQ(t, []string{"-S", "."}, want)
		got, _, st := runJQ(t, []string{"-m", "-S", ".", "testdata/compose/a.yml", "testdata/compose/b.yml"}, "")
		So(st.Code, ShouldEqual, 0)
		So(got, ShouldEqual, wantJQ)
		got, _, _ = runJQ(t, []string{"-m", "-S", "-c", ".", "testdata/compose/a.yml", "testdata/compose/b.yml"}, "")
		So(got, ShouldEqual, "{\"a\":1,\"b\":2,\"base\":1,\"list\":[1,2]}\n")
	})

	Convey("-E resolves each document on its own", t, func() {
		got, _, st := runJQ(t, []string{"-E", "-c", ".", "testdata/compose/a.yml", "testdata/compose/b.yml"}, "")
		So(st.Code, ShouldEqual, 0)
		So(got, ShouldEqual, "{\"a\":1,\"base\":1,\"list\":[1]}\n{\"b\":2,\"list\":[2]}\n")
		got, _, _ = runJQ(t, []string{"-E", "-c", ".", "testdata/compose/c.yml"}, "")
		So(got, ShouldEqual, "{\"x\":1,\"y\":1}\n{\"x\":2,\"y\":2}\n") // one file, two documents, each resolved alone
		got, _, _ = runJQ(t, []string{"-E", "-c", ".", "-"}, "x: 3\ny: (( grab x ))\n")
		So(got, ShouldEqual, "{\"x\":3,\"y\":3}\n") // stdin through composeContext.Stdin
	})

	Convey("compose flag rules", t, func() {
		_, e, st := runJQ(t, []string{"-m", "-E", ".", "testdata/compose/a.yml"}, "")
		So(st.Code, ShouldEqual, 2)
		So(e, ShouldContainSubstring, "-m and -E cannot be combined")
		_, e, st = runJQ(t, []string{"-m", ".", "testdata/compose/missing.yml"}, "")
		So(st.Code, ShouldEqual, 2)
		So(e, ShouldContainSubstring, "missing.yml")
		_, _, st = runJQ(t, []string{"-m", "--skip-eval", "-c", ".a", "testdata/compose/a.yml"}, "")
		So(st.Code, ShouldEqual, 0)
		_, e, st = runJQ(t, []string{"--prune", "a", ".", "testdata/compose/a.yml"}, "")
		So(st.Code, ShouldEqual, 2)
		So(e, ShouldContainSubstring, "--prune needs -m or -E")
	})

	Convey("merge flags pass through", t, func() {
		got, _, _ := runJQ(t, []string{"-m", "--skip-eval", "-c", ".a", "testdata/compose/a.yml"}, "")
		So(got, ShouldEqual, "\"(( grab base ))\"\n")
		got, _, _ = runJQ(t, []string{"-m", "--prune", "a", "--prune=base", "-c", ".", "testdata/compose/a.yml"}, "")
		So(got, ShouldEqual, "{\"list\":[1]}\n")
		got, _, _ = runJQ(t, []string{"-m", "--cherry-pick", "list", "-c", ".", "testdata/compose/a.yml"}, "")
		So(got, ShouldEqual, "{\"list\":[1]}\n")
		got, _, _ = runJQ(t, []string{"-m", "--multi-doc", "-c", ".", "testdata/compose/c.yml"}, "")
		So(got, ShouldEqual, "{\"x\":2,\"y\":2}\n")
	})
}
