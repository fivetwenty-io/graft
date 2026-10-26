package main

import (
	"bytes"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/log"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// runDebugSessionArgs runs `graft debug` with its query flags (--jq and
// --yq) and files in args, and script as the REPL's stdin.
func runDebugSessionArgs(args []string, script string) (stdout string, rc int) {
	var files []string
	var init debugInit
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--jq", "--yq":
			if i+1 < len(args) {
				init = debugInitFor(args[i+1], "")
				if args[i] == "--yq" {
					init = debugInitFor("", args[i+1])
				}
				i++
			}
		default:
			files = append(files, args[i])
		}
	}
	var out bytes.Buffer
	rc = handleDebugWith(files, &mergeOpts{}, strings.NewReader(script), &out, debugUIOptions{}, init)
	return out.String(), rc
}

// runJQStdinScript runs `graft jq` with script as stdin, which is how
// --repl reads its commands.
func runJQStdinScript(t *testing.T, args []string, script string) (out, errOut string, st query.Status) {
	t.Helper()
	return runJQ(t, args, script)
}

func TestDebugQuery(t *testing.T) {
	a, b := "testdata/debug/a.yml", "testdata/debug/b.yml"

	Convey("jq verb evaluates against the current tree", t, func() {
		out, rc := runDebugSession([]string{a}, "load\ncontinue\njq .meta.name\njq -c [.jobs[].name]\nquit\n")
		So(rc, ShouldEqual, 0)
		So(out, ShouldContainSubstring, "web\n")
		So(out, ShouldContainSubstring, "- nats\n- router\n")
	})

	Convey("a bare jq repeats the last program and a failure is reported", t, func() {
		out, _ := runDebugSession([]string{a}, "jq .a\nload\njq .meta.name\njq\njq .a |\nquit\n")
		So(out, ShouldContainSubstring, "No documents loaded. Run 'load' first.")
		So(strings.Count(out, "web\n"), ShouldEqual, 2)
		So(out, ShouldContainSubstring, "Compile error")
	})

	Convey("select narrows the path verbs and watch reports changes", t, func() {
		out, _ := runDebugSession([]string{a, b},
			"load\nselect .jobs[] | select(.name == \"nats\")\nwatch .jobs | length\nstep\nstep\ninspect\nquit\n")
		So(out, ShouldContainSubstring, "selected 1 path: jobs.[0]")
		So(out, ShouldContainSubstring, "watch (.jobs | length): 2 -> 3")
		So(out, ShouldContainSubstring, "jobs.[0]:")
	})

	Convey("unselect and unwatch clear what select and watch set", t, func() {
		out, _ := runDebugSession([]string{a, b},
			"load\nselect .meta\nunselect\ninspect\nwatch .jobs | length\nunwatch\nstep\nquit\n")
		So(out, ShouldContainSubstring, "Selection cleared")
		So(out, ShouldContainSubstring, "All watches removed")
		So(out, ShouldNotContainSubstring, "watch (.jobs | length): 2 -> 3")
		So(out, ShouldContainSubstring, "name: web")
	})

	Convey("query prefixes work wherever a path is accepted", t, func() {
		// The query names jobs[0], the only job with a port once a.yml is loaded.
		out, _ := runDebugSession([]string{a, b},
			"load\nbreak jq:.jobs[] | select(.port != null)\nbreaks\nquit\n")
		So(out, ShouldContainSubstring, "  - jobs.nats\n")
		So(out, ShouldNotContainSubstring, "jobs.[1]")
		out, _ = runDebugSession([]string{a}, "load\ncontinue\ntree jq:.jobs[0]\nconfig dialect\nquit\n")
		So(out, ShouldContainSubstring, "name: nats")
		So(out, ShouldContainSubstring, "dialect: jq")
	})

	Convey("yq evaluates against the current tree", t, func() {
		out, _ := runDebugSession([]string{a}, "load\nyq .meta.name\nconfig dialect yq\nselect .meta\nquit\n")
		So(out, ShouldContainSubstring, "web\n")
		So(out, ShouldContainSubstring, "selected 1 path: meta")
		So(out, ShouldNotContainSubstring, "not available")
	})

	Convey("entering the REPL from graft jq and graft debug", t, func() {
		out, errOut, st := runJQStdinScript(t, []string{"--repl", ".meta.name", a}, "quit\n")
		So(st.Code, ShouldEqual, 0)
		So(out, ShouldStartWith, "web\n")
		So(errOut, ShouldEqual, "")
		_, errOut, st = runJQStdinScript(t, []string{"--repl", ".a"}, "quit\n")
		So(st.Code, ShouldEqual, 1)
		So(errOut, ShouldContainSubstring, "Missing Input")
		out, rc := runDebugSessionArgs([]string{"--jq", ".meta.name", a}, "quit\n")
		So(rc, ShouldEqual, 0)
		So(out, ShouldStartWith, "web\n")
	})

	Convey("-D traces the engine under the graft name", t, func() {
		defer func() { log.DebugOn = false }()
		_, errOut, st := runGraftCommand(t, []string{"-D", "jq", ".a", a})
		So(st, ShouldEqual, 0)
		So(errOut, ShouldContainSubstring, "DEBUG> program: .a")
		So(errOut, ShouldContainSubstring, "DEBUG> input: "+a)
		_, errOut, st = runGraftCommand(t, []string{"-D", "jq", "-c", ".meta, .jobs[0].port", a})
		So(st, ShouldEqual, 0)
		So(errOut, ShouldContainSubstring, `DEBUG> output: {"name":"web"}`)
		So(errOut, ShouldContainSubstring, "DEBUG> output: 4222")
	})
}

func TestDottedPathKeepsKeysDistinct(t *testing.T) {
	Convey("dottedPath quotes a key that a dotted path could not hold", t, func() {
		str, num := value.Str, value.Num
		So(dottedPath(value.Path{str("jobs"), num(0), str("port")}), ShouldEqual, "jobs.[0].port")
		So(dottedPath(value.Path{str("a.b")}), ShouldEqual, `["a.b"]`)
		So(dottedPath(value.Path{str("a"), str("b")}), ShouldEqual, "a.b")
		So(dottedPath(value.Path{str("")}), ShouldEqual, `[""]`)
		So(dottedPath(value.Path{str("80")}), ShouldEqual, `["80"]`)
	})

	Convey("lookupPath follows typed segments", t, func() {
		s := &debugSession{tree: map[string]interface{}{"a.b": []interface{}{"x", "y"}, "80": 1}}
		v, ok := s.lookupPath(value.Path{value.Str("a.b"), value.Num(1)})
		So(ok, ShouldBeTrue)
		So(v, ShouldEqual, "y")
		_, ok = s.lookupPath(value.Path{value.Str("a"), value.Str("b")})
		So(ok, ShouldBeFalse)
		v, ok = s.lookupPath(value.Path{value.Str("80")})
		So(ok, ShouldBeTrue)
		So(v, ShouldEqual, 1)
	})
}

func TestDebugRootRebuild(t *testing.T) {
	a, b := "testdata/debug/a.yml", "testdata/debug/b.yml"

	Convey("evalQuery converts the tree again only after the merge advances", t, func() {
		var out bytes.Buffer
		s, err := newDebugSession([]string{a, b}, &mergeOpts{}, &out, debugUIOptions{})
		So(err, ShouldBeNil)
		s.cmdLoad()
		for i := 0; i < 3; i++ {
			_, err = s.evalQuery(debugDialectJQ, ".meta")
			So(err, ShouldBeNil)
		}
		So(s.rootBuilds, ShouldEqual, 1)
		s.cmdStep()
		_, err = s.evalQuery(debugDialectJQ, ".meta")
		So(err, ShouldBeNil)
		So(s.rootBuilds, ShouldEqual, 2)
	})

	Convey("eval changes the tree, so the next query sees the edit", t, func() {
		var out bytes.Buffer
		s, err := newDebugSession([]string{"testdata/debug/e.yml"}, &mergeOpts{}, &out, debugUIOptions{})
		So(err, ShouldBeNil)
		s.cmdLoad()
		before, err := s.evalQuery(debugDialectJQ, `.["80"]`)
		So(err, ShouldBeNil)
		So(before[0].Str(), ShouldContainSubstring, "((")
		s.cmdEval(`["80"]`)
		after, err := s.evalQuery(debugDialectJQ, `.["80"]`)
		So(err, ShouldBeNil)
		So(after[0].Str(), ShouldEqual, "rs")
	})
}

func TestDebugQueryPrefixIsExact(t *testing.T) {
	a, b := "testdata/debug/a.yml", "testdata/debug/b.yml"

	Convey("only jq: and yq: name a query", t, func() {
		So(hasQueryPrefix("jq:.a"), ShouldBeTrue)
		So(hasQueryPrefix(" yq: .a"), ShouldBeTrue)
		So(hasQueryPrefix("ns:x"), ShouldBeFalse)
		So(hasQueryPrefix("xq:.a"), ShouldBeFalse)
		So(hasQueryPrefix("JQ:.a"), ShouldBeFalse)
	})

	Convey("a path that starts with two letters and a colon stays a path", t, func() {
		out, _ := runDebugSession([]string{a, b}, "load\nbreak ns:x\nbreaks\nquit\n")
		So(out, ShouldContainSubstring, "Breakpoint set on ns:x")
		So(out, ShouldContainSubstring, "  - ns:x\n")
	})

	Convey("resolveDebugPaths still names an unknown prefix", t, func() {
		_, err := resolveDebugPaths(&debugSession{}, "xq:.a")
		So(err, ShouldNotBeNil)
		So(err.Error(), ShouldContainSubstring, `unknown query prefix "xq:" (jq: or yq:)`)
	})
}

func TestDebugTreeTakesTheQueryWhole(t *testing.T) {
	Convey("tree keeps a query's spaces, quotes, and leading dashes", t, func() {
		out, _ := runDebugSession([]string{"testdata/debug/a.yml"},
			"load\ntree -k jq:.jobs[] | select(.name == \"nats\"  and .port - 1 > 0)\nquit\n")
		So(out, ShouldContainSubstring, "port")
		So(out, ShouldNotContainSubstring, "unknown flag")
	})
}
