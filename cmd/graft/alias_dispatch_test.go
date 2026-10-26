package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestAliasDispatch(t *testing.T) {
	graft := buildGraftBinary(t)
	jqLink := linkAs(t, graft, "jq")
	spruceLink := linkAs(t, graft, "spruce")

	Convey("the jq link behaves as graft jq byte for byte", t, func() {
		for _, c := range []struct {
			args  []string
			stdin string
		}{
			{[]string{"."}, `{"a":[1,2,{"b":null}]}`},
			{[]string{"-c", ".a[]"}, `{"a":[1,2,{"b":null}]}`},
			{[]string{"-r", ".s"}, `{"s":"line"}`},
			{[]string{"-n", "[1,2]|add"}, ""},
			{[]string{"-o", "yaml", ".a"}, `{"a":{"b":[1,2]}}`},
			{[]string{"--arg", "x", "7", "-n", "$x"}, ""},
			{[]string{".a | error"}, `{"a":"boom"}`},
			{[]string{"-e", ".a"}, `{"a":false}`},
		} {
			a := execIn(t, ".", graft, append([]string{"jq"}, c.args...), c.stdin)
			b := execIn(t, ".", jqLink, c.args, c.stdin)
			So(b.Stdout, ShouldEqual, a.Stdout)
			So(b.Stderr, ShouldEqual, a.Stderr)
			So(b.Code, ShouldEqual, a.Code)
		}
	})

	Convey("the link composes with -m and -E as graft jq does", t, func() {
		for _, args := range [][]string{
			{"-m", "-c", ".", "testdata/compose/a.yml", "testdata/compose/b.yml"},
			{"-E", "-c", ".", "testdata/compose/a.yml", "testdata/compose/c.yml"},
			{"-m", "--prune", "a", "-c", ".", "testdata/compose/a.yml"},
			{"-m", ".", "testdata/compose/missing.yml"},
		} {
			a := execIn(t, ".", graft, append([]string{"jq"}, args...), "")
			b := execIn(t, ".", jqLink, args, "")
			So(b.Stdout, ShouldEqual, a.Stdout)
			So(b.Stderr, ShouldEqual, a.Stderr)
			So(b.Code, ShouldEqual, a.Code)
		}
		So(execIn(t, ".", jqLink, []string{"-m", "-c", ".", "testdata/compose/a.yml", "testdata/compose/b.yml"}, "").Stdout,
			ShouldEqual, "{\"a\":1,\"b\":2,\"base\":1,\"list\":[1,2]}\n")
	})

	Convey("the link reads files relative to its working directory", t, func() {
		dir := t.TempDir()
		So(os.WriteFile(filepath.Join(dir, "in.json"), []byte(`{"a":1}`), 0o600), ShouldBeNil)
		r := execIn(t, dir, jqLink, []string{"-c", ".", "in.json"}, "")
		So(r.Stdout, ShouldEqual, "{\"a\":1}\n")
		So(r.Code, ShouldEqual, 0)
	})

	Convey("the version and help output", t, func() {
		for _, bin := range [][]string{{jqLink}, {graft, "jq"}} {
			for _, flag := range []string{"--version", "-V"} {
				v := execIn(t, ".", bin[0], append(bin[1:], flag), "")
				lines := strings.Split(v.Stdout, "\n")
				So(v.Code, ShouldEqual, 0)
				So(lines[0], ShouldEqual, "jq-"+jqCompatVersion)
				So(lines[1], ShouldContainSubstring, "graft")
			}
		}
		h := execIn(t, ".", jqLink, []string{"--help"}, "")
		So(h.Code, ShouldEqual, 0)
		So(h.Stdout, ShouldContainSubstring, "jq [options] <jq filter> [file...]")
		So(h.Stdout, ShouldNotContainSubstring, "Global Flags")
	})

	Convey("graft's flags do not exist under the alias", t, func() {
		So(execIn(t, ".", jqLink, []string{"--theme", "x", "."}, "1").Code, ShouldEqual, 2)
	})

	Convey("the legacy usage fallback is never reached", t, func() {
		for _, c := range []struct {
			args []string
			err  string
			code int
		}{
			{[]string{"--bogus", "."}, "Unknown option: --bogus\n", 2},
			{[]string{".", "/nonexistent"}, "Unable to open file /nonexistent: no such file or directory\n", 2},
			{[]string{".a |"}, "Compile error: unexpected end of input at line 1, column 5\n", 3},
		} {
			args := c.args
			r := execIn(t, ".", jqLink, args, "1")
			So(r.Stderr, ShouldEqual, c.err)
			So(r.Code, ShouldEqual, c.code)
			g := execIn(t, ".", graft, append([]string{"jq"}, args...), "1")
			So(g.Stderr, ShouldEqual, r.Stderr)
			So(g.Code, ShouldEqual, r.Code)
		}
	})

	Convey("normalizeLegacyColorArgs does not rewrite query arguments", t, func() {
		// The legacy rewrite would join --color and on into one
		// positional, --color=on.
		args := []string{"-n", "-r", "$ARGS.positional[]", "--args", "--", "--color", "on"}
		want := "--color\non\n"
		So(execIn(t, ".", graft, append([]string{"jq"}, args...), "").Stdout, ShouldEqual, want)
		So(execIn(t, ".", jqLink, args, "").Stdout, ShouldEqual, want)
		// A root flag given before the verb still leaves the query's
		// arguments alone. Left alone, "on" is the program, which jq
		// refuses, where the rewrite would have bound $x and succeeded.
		pre := []string{"--theme", "mono", "jq", "-n", "-r", "--arg", "x", "--color", "on", "$x"}
		r := execIn(t, ".", graft, pre, "")
		So(r.Stderr, ShouldContainSubstring, "on/0 is not defined")
		So(r.Code, ShouldEqual, 3)

		// --arg takes the next two tokens, so --color is the value.
		args = []string{"--arg", "x", "--color", "-n", "-r", "$x"}
		So(execIn(t, ".", graft, append([]string{"jq"}, args...), "").Stdout, ShouldEqual, "--color\n")
		So(execIn(t, ".", jqLink, args, "").Stdout, ShouldEqual, "--color\n")

		// Left alone, "on" is the program, which jq refuses.
		args = []string{"-n", "-r", "--arg", "x", "--color", "on", "$x"}
		g := execIn(t, ".", graft, append([]string{"jq"}, args...), "")
		l := execIn(t, ".", jqLink, args, "")
		So(g.Stdout, ShouldEqual, "")
		So(g.Stderr, ShouldContainSubstring, "on/0 is not defined")
		So(g.Code, ShouldEqual, 3)
		So(l, ShouldResemble, g)
	})

	Convey("the jq name matches the way a symlink is named or chained", t, func() {
		for _, name := range []string{"jq.exe", "JQ"} {
			So(execIn(t, ".", linkAs(t, graft, name), []string{"--version"}, "").Stdout, ShouldStartWith, "jq-"+jqCompatVersion+"\n")
		}
		hop := linkAs(t, graft, "hop")
		chain := filepath.Join(t.TempDir(), "jq")
		So(os.Symlink(hop, chain), ShouldBeNil)
		So(execIn(t, ".", chain, []string{"-n", "1+1"}, "").Stdout, ShouldEqual, "2\n")
		So(execIn(t, ".", linkAs(t, graft, "jq-1.7"), []string{"-n", "1"}, "").Code, ShouldNotEqual, 0) // runs as graft
	})

	Convey("an invalid GRAFT_THEME warns the same under both names", t, func() {
		env := []string{"GRAFT_THEME=nope"}
		g := execInEnv(t, env, ".", graft, []string{"jq", "-n", "1"}, "")
		l := execInEnv(t, env, ".", jqLink, []string{"-n", "1"}, "")
		So(g.Stderr, ShouldContainSubstring, "Invalid GRAFT_THEME value")
		So(l, ShouldResemble, g)
		env = []string{"GRAFT_THEME=mono"}
		args := []string{"-C", "-o", "yaml", "-n", `{"a":[1,"x"]}`}
		g = execInEnv(t, env, ".", graft, append([]string{"jq"}, args...), "")
		So(execInEnv(t, env, ".", jqLink, args, ""), ShouldResemble, g)
	})

	Convey("the spruce link is unchanged", t, func() {
		So(execIn(t, ".", spruceLink, []string{"-v"}, "").Stdout, ShouldContainSubstring, "spruce - Version")
		So(execIn(t, ".", spruceLink, []string{"-v"}, "").Stdout, ShouldNotContainSubstring, "jq-")
	})
}
