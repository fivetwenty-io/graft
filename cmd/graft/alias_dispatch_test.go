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
	t.Cleanup(removeGraftBinary)
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
		for _, args := range [][]string{{"--bogus", "."}, {".", "/nonexistent"}, {".a |"}} {
			r := execIn(t, ".", jqLink, args, "1")
			So(r.Stderr, ShouldNotContainSubstring, "Usage:")
			So(r.Code, ShouldNotEqual, 1)
			g := execIn(t, ".", graft, append([]string{"jq"}, args...), "1")
			So(g.Stderr, ShouldEqual, r.Stderr)
			So(g.Code, ShouldEqual, r.Code)
		}
	})

	Convey("normalizeLegacyColorArgs does not rewrite query arguments", t, func() {
		// --arg takes the next two tokens, so --color is the value and
		// $x is "--color" in both names.
		args := []string{"--arg", "x", "--color", "-n", "-r", "$x"}
		So(execIn(t, ".", graft, append([]string{"jq"}, args...), "").Stdout, ShouldEqual, "--color\n")
		So(execIn(t, ".", jqLink, args, "").Stdout, ShouldEqual, "--color\n")

		// The legacy rewrite would join --color and on into --color=on and
		// bind it to $x. Left alone, "on" is the program, which jq refuses.
		args = []string{"-n", "-r", "--arg", "x", "--color", "on", "$x"}
		g := execIn(t, ".", graft, append([]string{"jq"}, args...), "")
		l := execIn(t, ".", jqLink, args, "")
		So(g.Stdout, ShouldEqual, "")
		So(g.Stderr, ShouldContainSubstring, "on/0 is not defined")
		So(g.Code, ShouldEqual, 3)
		So(l, ShouldResemble, g)
	})

	Convey("the spruce link is unchanged", t, func() {
		So(execIn(t, ".", spruceLink, []string{"-v"}, "").Stdout, ShouldContainSubstring, "spruce - Version")
		So(execIn(t, ".", spruceLink, []string{"-v"}, "").Stdout, ShouldNotContainSubstring, "jq-")
	})
}
