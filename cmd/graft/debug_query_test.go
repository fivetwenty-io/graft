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
			"load\nbreak jq:.jobs[] | select(.port != null)\nbreaks\nbreak xq:.a\nquit\n")
		So(out, ShouldContainSubstring, "  - jobs.nats\n")
		So(out, ShouldNotContainSubstring, "jobs.[1]")
		So(out, ShouldContainSubstring, "unknown query prefix \"xq:\" (jq: or yq:)")
		out, _ = runDebugSession([]string{a}, "load\ncontinue\ntree jq:.jobs[0]\nconfig dialect\nquit\n")
		So(out, ShouldContainSubstring, "name: nats")
		So(out, ShouldContainSubstring, "dialect: jq")
	})

	Convey("yq says it is not available yet", t, func() {
		out, _ := runDebugSession([]string{a}, "load\nyq .meta.name\nconfig dialect yq\nselect .meta\nquit\n")
		So(strings.Count(out, "yq is not available until a later release"), ShouldEqual, 2)
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
