package main

import (
	"os"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/spf13/cobra"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// graftMergeOutput is what `graft merge FILES` writes, through the merge
// command's own evaluation and rendering, which -m must feed to the query.
func graftMergeOutput(t *testing.T, opts mergeOpts, files ...string) string {
	t.Helper()
	opts.Files = files
	tree, _, err := cmdMergeEval(&opts)
	if err != nil {
		t.Fatalf("merge %v: %v", files, err)
	}
	out, rc := marshalMergedDocument(tree, "", true, opts.keyLess())
	if rc != 0 {
		t.Fatalf("render %v: %d", files, rc)
	}
	return string(out)
}

func TestQueryCompose(t *testing.T) {
	Convey("-m equals graft merge piped into jq", t, func() {
		want := graftMergeOutput(t, mergeOpts{}, "testdata/compose/a.yml", "testdata/compose/b.yml")
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

func TestQueryComposeSharedLoading(t *testing.T) {
	Convey("the merge command's loader reads from the stream it is given", t, func() {
		f := YamlFile{Path: "-"}
		data, err := readFileFrom(&f, strings.NewReader("a: 1\n"))
		So(err, ShouldBeNil)
		So(string(data), ShouldEqual, "a: 1\n")
		So(f.Path, ShouldEqual, stdinPath)
		_, err = readFileFrom(&YamlFile{Path: "-"}, strings.NewReader(""))
		So(err.Error(), ShouldContainSubstring, "no data found")
	})

	Convey("stdin that is a terminal-like device is refused as graft merge refuses it", t, func() {
		null, err := os.Open(os.DevNull)
		So(err, ShouldBeNil)
		defer func() { _ = null.Close() }()
		_, errText, code := composeQueryInputs(composeContext{Stdin: null, Merge: mergeOpts{}}, nil)
		So(code, ShouldEqual, 2)
		So(string(errText), ShouldContainSubstring, "no data found")
	})

	Convey("a config that does not load is reported", t, func() {
		root := &cobra.Command{Use: "graft"}
		root.PersistentFlags().String("config", "", "")
		sub := &cobra.Command{Use: "jq"}
		root.AddCommand(sub)
		So(root.PersistentFlags().Set("config", "testdata/compose/missing-config.yml"), ShouldBeNil)
		_, err := composeEngineOpts(sub)
		So(err, ShouldNotBeNil)
	})

	Convey("merge flags and stdin compose as graft merge does", t, func() {
		a, b := "testdata/compose/a.yml", "testdata/compose/b.yml"
		for _, tc := range []struct {
			name string
			args []string
			opts mergeOpts
		}{
			{"fallback-append", []string{"--fallback-append"}, mergeOpts{FallbackAppend: true}},
			{"go-patch", []string{"--go-patch"}, mergeOpts{EnableGoPatch: true}},
			{"skip-vault", []string{"--skip-vault"}, mergeOpts{EngineOpts: engineOptions{graft.WithSkipVault(true)}}},
		} {
			want := graftMergeOutput(t, tc.opts, a, b)
			wantJQ, _, _ := runJQ(t, []string{"-c", "."}, want)
			args := append(append([]string{"-m", "-c"}, tc.args...), ".", a, b)
			got, _, st := runJQ(t, args, "")
			So(st.Code, ShouldEqual, 0)
			So(got, ShouldEqual, wantJQ)
		}
		wantJQ, _, _ := runJQ(t, []string{"-c", "."}, graftMergeOutput(t, mergeOpts{}, a, b))
		got, _, st := runJQ(t, []string{"-m", "-c", ".", "-", b}, "base: 1\na: (( grab base ))\nlist: [1]\n")
		So(st.Code, ShouldEqual, 0)
		So(got, ShouldEqual, wantJQ)
	})
}

func TestQueryComposeErrorText(t *testing.T) {
	Convey("an unreadable input is reported on one line, as graft merge reports it", t, func() {
		_, errText, code := composeQueryInputs(composeContext{}, []string{"testdata/compose"})
		So(code, ShouldEqual, 2)
		So(strings.HasSuffix(string(errText), "\n"), ShouldBeTrue)
		So(strings.HasSuffix(string(errText), "\n\n"), ShouldBeFalse)
	})
}
