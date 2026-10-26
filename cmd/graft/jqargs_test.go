package main

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/pkg/sortkeys"
)

func TestParseJQArgs(t *testing.T) {
	Convey("parseJQArgs follows jq's main.c rules", t, func() {
		o, err := parseJQArgs([]string{"-nrc", "."}, personaJQ)
		So(err, ShouldBeNil)
		So(o.NullInput && o.Raw && o.Compact, ShouldBeTrue)
		So(o.Program, ShouldEqual, ".")
		o, _ = parseJQArgs([]string{"-n", "-1"}, personaJQ)
		So(o.Program, ShouldEqual, "-1") // a digit after - is a positional
		o, _ = parseJQArgs([]string{"--args", "--arg", "x", "7", "$x", "a", "b"}, personaJQ)
		So(o.Program, ShouldEqual, "$x")
		So(len(o.Positional), ShouldEqual, 2)
		So(o.Args["x"].Str(), ShouldEqual, "7") // options parse after --args
		o, _ = parseJQArgs([]string{"--arg", "x", "1", "--arg", "x", "2", "$x"}, personaJQ)
		So(o.Args["x"].Str(), ShouldEqual, "1") // first wins
		o, _ = parseJQArgs([]string{".", "--", "-n"}, personaJQ)
		So(o.Files, ShouldResemble, []string{"-n"}) // nothing parses after --
		_, err = parseJQArgs([]string{"--indent", "8", "."}, personaJQ)
		So(err.Error(), ShouldContainSubstring, "--indent takes a number between -1 and 7")
		_, err = parseJQArgs([]string{"--arg", "x"}, personaJQ)
		So(err.Error(), ShouldContainSubstring, "--arg takes two parameters")
		_, err = parseJQArgs([]string{"--bogus", "."}, personaJQ)
		So(err.Error(), ShouldContainSubstring, "--bogus")
		o, _ = parseJQArgs([]string{"--theme", "x", "--color=on", "."}, personaGraft)
		So(o.RootArgs, ShouldResemble, []string{"--theme", "x", "--color=on"})
		_, err = parseJQArgs([]string{"--theme", "x", "."}, personaJQ)
		So(err.Error(), ShouldContainSubstring, "--theme") // graft flags do not exist under the alias
		o, _ = parseJQArgs([]string{"--arg", "x", "--color", "."}, personaGraft)
		So(o.Args["x"].Str(), ShouldEqual, "--color") // a value is never a flag
	})

	Convey("parseJQArgs reads the rest of jq's option forms", t, func() {
		o, err := parseJQArgs([]string{"-L", "a", "-Lb", "--library-path", "c", "."}, personaJQ)
		So(err, ShouldBeNil)
		So(o.LibraryPaths, ShouldResemble, []string{"a", "b", "c"})
		o, _ = parseJQArgs([]string{"-nf", "prog.jq", "in.json"}, personaJQ)
		So(o.FromFile && o.NullInput, ShouldBeTrue)
		So(o.Program, ShouldEqual, "prog.jq")
		So(o.Files, ShouldResemble, []string{"in.json"})
		o, _ = parseJQArgs([]string{"-S", "."}, personaJQ)
		So(o.SortKeys, ShouldEqual, sortkeys.Lexical)
		o, _ = parseJQArgs([]string{"-o", "y", "."}, personaJQ)
		So(o.Output, ShouldEqual, "y")
		o, _ = parseJQArgs([]string{"-coyaml", "."}, personaJQ)
		So(o.Output, ShouldEqual, "yaml")
		So(o.Compact, ShouldBeTrue)
		o, _ = parseJQArgs([]string{"--run-tests", "t.test", "--bogus"}, personaJQ)
		So(o.RunTests, ShouldBeTrue) // --run-tests takes the rest of the line, as in jq
		So(o.RunTestsFile, ShouldEqual, "t.test")
		o, _ = parseJQArgs([]string{"--input-format", "yaml", "."}, personaJQ)
		So(o.InputFormat, ShouldEqual, "yaml")
		o, _ = parseJQArgs([]string{"-", "."}, personaJQ)
		So(o.Program, ShouldEqual, "-") // a lone - is a positional
		_, err = parseJQArgs([]string{"-nX", "."}, personaJQ)
		So(err.Error(), ShouldEqual, "-X")
		_, err = parseJQArgs([]string{"--input-format", "xml", "."}, personaJQ)
		So(err, ShouldNotBeNil)
		_, err = parseJQArgs([]string{"--argjson", "x", "{", "."}, personaJQ)
		So(err.Error(), ShouldContainSubstring, "invalid JSON text passed to --argjson")
		_, err = parseJQArgs([]string{"-D", "."}, personaGraft)
		So(err, ShouldNotBeNil) // root flags are taken by their long names only
		o, _ = parseJQArgs([]string{"--debug", "--no-color", "--config", "c.yml", "--max-loop-iterations=5", "."}, personaGraft)
		So(o.RootArgs, ShouldResemble, []string{"--debug", "--no-color", "--config", "c.yml", "--max-loop-iterations=5"})
		_, err = parseJQArgs([]string{"--theme"}, personaGraft)
		So(err, ShouldNotBeNil)
		o, _ = parseJQArgs([]string{"-V"}, personaGraft)
		So(o.Version, ShouldBeTrue) // jq's version flag, not the root's
		o, _ = parseJQArgs([]string{"--version"}, personaGraft)
		So(o.Version && len(o.RootArgs) == 0, ShouldBeTrue)
	})
}
