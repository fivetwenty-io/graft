package main

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestPersona(t *testing.T) {
	Convey("personaFromArgv0 and queryInvocation", t, func() {
		for in, want := range map[string]persona{
			"jq": personaJQ, "./jq": personaJQ, "/usr/local/bin/JQ.exe": personaJQ, "yq": personaYQ,
			"spruce": personaSpruce, "jq-1.7": personaGraft, "gojq": personaGraft, "": personaGraft, "graft": personaGraft,
		} {
			So(personaFromArgv0(in), ShouldEqual, want)
		}
		So(invokedAsSpruce("spruce.exe"), ShouldBeTrue)
		So(invokedAsSpruce("jq"), ShouldBeFalse)

		p, ok := queryInvocation([]string{"jq", "."})
		So(ok, ShouldBeTrue)
		So(p, ShouldEqual, personaJQ)
		p, ok = queryInvocation([]string{"graft", "jq", "."})
		So(ok, ShouldBeTrue)
		So(p, ShouldEqual, personaGraft)
		_, ok = queryInvocation([]string{"graft", "merge", "a.yml"})
		So(ok, ShouldBeFalse)
		_, ok = queryInvocation([]string{"spruce", "jq", "."})
		So(ok, ShouldBeFalse) // spruce never had jq
		_, ok = queryInvocation(nil)
		So(ok, ShouldBeFalse)
	})
}

func TestAliasRoot(t *testing.T) {
	Convey("the alias root has jq's shape and none of graft's flags", t, func() {
		root, aborted := newPersonaRoot(personaJQ)
		So(*aborted, ShouldBeFalse)
		So(root.Use, ShouldStartWith, "jq [options] <jq filter> [file...]")
		So(root.Short, ShouldContainSubstring, "jq "+jqCompatVersion+" compatible")
		So(root.DisableFlagParsing, ShouldBeTrue)
		So(root.PersistentFlags().Lookup("theme"), ShouldBeNil)
		So(root.Commands(), ShouldBeEmpty)
		So(root.PersistentPreRunE, ShouldNotBeNil)
	})

	Convey("jqVersionOutput leads with jq's own line", t, func() {
		So(jqVersionOutput(), ShouldEqual, "jq-"+jqCompatVersion+"\n"+graftVersionLine()+"\n")
	})
}
