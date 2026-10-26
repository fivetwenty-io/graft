package main

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

// The path verbs take a path a query chose without flattening it, so a key
// that holds a dot, or spells a number, still names itself.
func TestDebugTypedPathVerbs(t *testing.T) {
	c, d, e := "testdata/debug/c.yml", "testdata/debug/d.yml", "testdata/debug/e.yml"

	Convey("break and unbreak keep a dotted key whole", t, func() {
		out, _ := runDebugSession([]string{c, d}, "load\nbreak jq:.[\"a.b\"]\nbreaks\nstep\nunbreak jq:.[\"a.b\"]\nbreaks\nquit\n")
		So(out, ShouldContainSubstring, "Breakpoint set on a.b")
		So(out, ShouldContainSubstring, "Breakpoint hit: a.b")
		So(out, ShouldContainSubstring, "Breakpoint removed")
		So(out, ShouldContainSubstring, "No breakpoints set.")
	})

	Convey("break on a numeric key is not an index", t, func() {
		out, _ := runDebugSession([]string{c, d}, "load\nbreak jq:.[\"80\"]\nstep\nquit\n")
		So(out, ShouldContainSubstring, "Breakpoint hit: 80")
	})

	Convey("history finds a dotted key", t, func() {
		out, _ := runDebugSession([]string{c, d}, "load\nhistory jq:.[\"a.b\"]\nquit\n")
		So(out, ShouldNotContainSubstring, "No history recorded")
		So(out, ShouldContainSubstring, "Final")
	})

	Convey("defer protects a dotted and a numeric key through evaluation", t, func() {
		out, _ := runDebugSession([]string{e}, "load\ndefer jq:.[\"a.b\"]\ndefer jq:.[\"80\"]\ncontinue\ninspect jq:.[\"a.b\"]\ninspect jq:.[\"80\"]\nquit\n")
		So(out, ShouldContainSubstring, "(( concat")
		So(out, ShouldNotContainSubstring, "\npq\n")
		So(out, ShouldNotContainSubstring, "\nrs\n")
	})

	Convey("eval resolves a dotted and a numeric key", t, func() {
		out, _ := runDebugSession([]string{e}, "load\neval jq:.[\"a.b\"]\neval jq:.[\"80\"]\nquit\n")
		So(out, ShouldContainSubstring, "Result: pq")
		So(out, ShouldContainSubstring, "Result: rs")
	})

	Convey("a quoted segment works as a plain argument too", t, func() {
		out, _ := runDebugSession([]string{e}, "load\neval [\"a.b\"]\nquit\n")
		So(out, ShouldContainSubstring, "Result: pq")
	})
}

func TestDebugQueryBreakpoints(t *testing.T) {
	a, b := "testdata/debug/a.yml", "testdata/debug/b.yml"

	Convey("a breakpoint set through a query names the entry and fires beneath it", t, func() {
		out, _ := runDebugSession([]string{a, b},
			"load\nbreak jq:.jobs[] | select(.name == \"nats\")\nbreaks\nstep\nquit\n")
		So(out, ShouldContainSubstring, "Breakpoint set on jobs.nats")
		So(out, ShouldContainSubstring, "Breakpoint hit: jobs.nats.port")
	})

	Convey("a plain break stays exact", t, func() {
		out, _ := runDebugSession([]string{a, b}, "load\nbreak jobs.nats\nstep\nquit\n")
		So(out, ShouldNotContainSubstring, "Breakpoint hit")
		out, _ = runDebugSession([]string{a, b}, "load\nbreak jobs.nats.port\nstep\nquit\n")
		So(out, ShouldContainSubstring, "Breakpoint hit: jobs.nats.port")
	})

	Convey("a plain break on a path replaces a query's covering break", t, func() {
		out, _ := runDebugSession([]string{a, b},
			"load\nbreak jq:.jobs[] | select(.name == \"nats\")\nbreak jobs.nats\nstep\nquit\n")
		So(out, ShouldNotContainSubstring, "Breakpoint hit")
	})

	Convey("unbreak removes a query's breakpoint", t, func() {
		out, _ := runDebugSession([]string{a, b},
			"load\nbreak jq:.jobs[] | select(.name == \"nats\")\nunbreak jq:.jobs[] | select(.name == \"nats\")\nstep\nquit\n")
		So(out, ShouldContainSubstring, "Breakpoint removed")
		So(out, ShouldNotContainSubstring, "Breakpoint hit")
	})
}
