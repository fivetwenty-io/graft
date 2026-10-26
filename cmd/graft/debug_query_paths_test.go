package main

import (
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/pkg/query/value"
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

func TestDebugStepFollowsSelection(t *testing.T) {
	a, b := "testdata/debug/a.yml", "testdata/debug/b.yml"

	Convey("step prints every change when nothing is selected", t, func() {
		out, _ := runDebugSession([]string{a, b}, "load\nstep\nquit\n")
		So(out, ShouldContainSubstring, "jobs.nats.port")
		So(out, ShouldContainSubstring, "jobs[2]")
	})

	Convey("step prints only the changes under the selection", t, func() {
		out, _ := runDebugSession([]string{a, b},
			"load\nselect .jobs[] | select(.name == \"nats\")\nstep\nquit\n")
		So(out, ShouldContainSubstring, "jobs.nats.port")
		So(out, ShouldNotContainSubstring, "jobs[2]")
	})

	Convey("unselect restores the full list of changes", t, func() {
		out, _ := runDebugSession([]string{a, b},
			"load\nselect .jobs[] | select(.name == \"nats\")\nunselect\nstep\nquit\n")
		So(out, ShouldContainSubstring, "jobs[2]")
	})
}

func TestHistPathNamesEntries(t *testing.T) {
	Convey("histPath names a list entry by its name and falls back to its index", t, func() {
		s := &debugSession{tree: map[string]interface{}{
			"jobs":  []interface{}{map[string]interface{}{"name": "nats"}, map[string]interface{}{"port": 1}},
			"plain": []interface{}{"x", "y"},
			"a.b":   map[string]interface{}{"c": 1},
		}}
		p := func(segs ...interface{}) value.Path {
			out := make(value.Path, len(segs))
			for i, sg := range segs {
				if n, ok := sg.(int); ok {
					out[i] = value.Num(float64(n))
				} else {
					out[i] = value.Str(sg.(string))
				}
			}
			return out
		}
		So(s.histPath(p("jobs", 0, "port")), ShouldEqual, "jobs.nats.port")
		So(s.histPath(p("jobs", 1, "port")), ShouldEqual, "jobs[1].port")
		So(s.histPath(p("plain", 1)), ShouldEqual, "plain[1]")
		So(s.histPath(p("a.b", "c")), ShouldEqual, "a.b.c")
		So(s.trackPath(p("a.b", "c")), ShouldEqual, `"a.b".c`)
	})
}

func TestJQReplTakesAnyDocument(t *testing.T) {
	list, scalar, null := "testdata/debug/list.yml", "testdata/debug/scalar.yml", "testdata/debug/null.yml"

	Convey("an array document enters the debugger and answers queries", t, func() {
		out, errOut, st := runJQStdinScript(t, []string{"--repl", ".[2].a", list}, "jq length\njq .[1]\ninspect\ninspect jq:.[2]\nquit\n")
		So(st.Code, ShouldEqual, 0)
		So(errOut, ShouldEqual, "")
		So(out, ShouldStartWith, "3\n")
		So(out, ShouldContainSubstring, "Welcome to the Graft Debugger")
		So(out, ShouldContainSubstring, "two\n")
		So(out, ShouldContainSubstring, "- a: 3\n")
		So(out, ShouldContainSubstring, "[2]:\na: 3\n")
	})

	Convey("a scalar and null document enter the debugger", t, func() {
		out, _, st := runJQStdinScript(t, []string{"--repl", ". + 1", scalar}, "jq .\nquit\n")
		So(st.Code, ShouldEqual, 0)
		So(out, ShouldStartWith, "43\n")
		So(out, ShouldContainSubstring, "42\n")
		out, _, st = runJQStdinScript(t, []string{"--repl", ".", null}, "jq type\nquit\n")
		So(st.Code, ShouldEqual, 0)
		So(out, ShouldStartWith, "null\n")
		So(out, ShouldContainSubstring, "null\n")
	})

	Convey("the verbs that need a mapping say so", t, func() {
		out, _, _ := runJQStdinScript(t, []string{"--repl", ".", list},
			"step\ncontinue\nbreak jq:.[0]\nunbreak x\ndefer x\nhistory x\neval x\nquit\n")
		So(strings.Count(out, "needs a mapping at the root, and the loaded document is an array"), ShouldEqual, 7)
		So(out, ShouldContainSubstring, "step needs a mapping")
		So(out, ShouldContainSubstring, "break needs a mapping")
		So(out, ShouldContainSubstring, "defer needs a mapping")
		out, _, _ = runJQStdinScript(t, []string{"--repl", ".", scalar}, "step\nquit\n")
		So(out, ShouldContainSubstring, "the loaded document is a number")
	})

	Convey("a non-mapping document cannot join other documents", t, func() {
		_, errOut, st := runJQStdinScript(t, []string{"--repl", ".", list, "testdata/debug/a.yml"}, "quit\n")
		So(st.Code, ShouldEqual, 1)
		So(errOut, ShouldContainSubstring, "cannot be combined")
	})

	Convey("mappings still merge as graft debug merges them", t, func() {
		out, _, st := runJQStdinScript(t, []string{"--repl", ".jobs | length", "testdata/debug/a.yml", "testdata/debug/b.yml"}, "load\nstep\nquit\n")
		So(st.Code, ShouldEqual, 0)
		So(out, ShouldStartWith, "3\n")
		So(out, ShouldContainSubstring, "jobs.nats.port")
	})
}
