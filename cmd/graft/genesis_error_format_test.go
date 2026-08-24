package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/log"
)

// genesisAdaptiveMergeErrorRx mirrors genesis's own stderr-parsing regex from
// lib/Genesis/Env/ManifestProvider.pm's _adaptive_merge (`/^ - \$\.([^:]*): (.*)$/`).
// Every merge/eval error line graft writes to stderr must match this pattern
// or genesis's adaptive-merge retry logic silently drops the error.
var genesisAdaptiveMergeErrorRx = regexp.MustCompile(`^ - \$\.([^:]*): (.*)$`)

// runGraftCapturingOutput invokes main() with the given CLI args and
// returns the captured stderr, restoring the previous test hooks
// afterward. Stdout is captured too - so it stays out of the test's own
// output - but no caller asserts on it.
func runGraftCapturingOutput(t *testing.T, args []string) (stderr string, rc int) {
	t.Helper()

	prevPrintStdOutf := printStdOutf
	prevPrintStdErrf := log.PrintStdErrf
	prevExit := exit
	prevUsage := usage
	prevArgs := os.Args
	defer func() {
		printStdOutf = prevPrintStdOutf
		log.PrintStdErrf = prevPrintStdErrf
		exit = prevExit
		usage = prevUsage
		os.Args = prevArgs
	}()

	printStdOutf = func(string, ...interface{}) {}
	log.PrintStdErrf = func(format string, fmtArgs ...interface{}) {
		stderr += fmt.Sprintf(format, fmtArgs...)
	}
	rc = 256 // sentinel: unset if exit is never called
	exit = func(code int) {
		rc = code
	}
	usage = func() {
		exit(1)
	}

	os.Args = append([]string{"graft"}, args...)
	main()

	return stderr, rc
}

// adaptiveMergeErrorLines returns every stderr line that looks like it is
// part of the "N error(s) detected:" block (i.e. starts with " - "), in the
// order they were printed.
func adaptiveMergeErrorLines(stderr string) []string {
	var lines []string
	for _, line := range strings.Split(stderr, "\n") {
		if strings.HasPrefix(line, " - ") {
			lines = append(lines, line)
		}
	}
	return lines
}

// TestGenesisAdaptiveMergeErrorFormat pins graft's merge/eval stderr format to
// the exact contract genesis's _adaptive_merge parses:
// “ - $.<path>: <message>“ (one error per line, no ANSI color codes on a
// non-tty stderr), with a MultiError's lines lexicographically sorted.
func TestGenesisAdaptiveMergeErrorFormat(t *testing.T) {
	Convey("graft merge stderr matches genesis's _adaptive_merge regex", t, func() {
		Convey("a single unresolved-reference error is one regex-matching line", func() {
			stderr, rc := runGraftCapturingOutput(t, []string{"merge", "../../assets/errors/multi.yml"})

			So(rc, ShouldEqual, 2)

			lines := adaptiveMergeErrorLines(stderr)
			So(lines, ShouldHaveLength, 1)

			m := genesisAdaptiveMergeErrorRx.FindStringSubmatch(lines[0])
			So(m, ShouldNotBeNil)
			path, message := m[1], m[2]
			So(path, ShouldEqual, "an-error")
			So(message, ShouldEqual, "missing param!")

			// No leftover color escape codes on a non-tty stderr.
			So(stderr, ShouldNotContainSubstring, "\033[")
		})

		Convey("multiple errors at the same level: one regex-matching line each, sorted by path", func() {
			stderr, rc := runGraftCapturingOutput(t, []string{"merge", "../../assets/errors/multi2.yml"})

			So(rc, ShouldEqual, 2)

			lines := adaptiveMergeErrorLines(stderr)
			So(lines, ShouldHaveLength, 3)

			var paths, messages []string
			for _, line := range lines {
				m := genesisAdaptiveMergeErrorRx.FindStringSubmatch(line)
				So(m, ShouldNotBeNil)
				paths = append(paths, m[1])
				messages = append(messages, m[2])
			}

			// multi2.yml defines params a/first, b/second, c/third; the
			// MultiError sort must yield them in lexicographic path order.
			So(paths, ShouldResemble, []string{"a", "b", "c"})
			So(messages, ShouldResemble, []string{"first", "second", "third"})

			So(stderr, ShouldNotContainSubstring, "\033[")
		})

		Convey("a cycle error is one regex-matching line with no escapes", func() {
			dir := t.TempDir()
			a := filepath.Join(dir, "a.yml")
			b := filepath.Join(dir, "b.yml")
			So(os.WriteFile(a, []byte("meta:\n  foo: (( grab meta.bar ))\n"), 0o600), ShouldBeNil)
			So(os.WriteFile(b, []byte("meta:\n  bar: (( grab meta.foo ))\n"), 0o600), ShouldBeNil)

			stderr, rc := runGraftCapturingOutput(t, []string{"merge", a, b})

			So(rc, ShouldNotEqual, 0)
			So(stderr, ShouldNotContainSubstring, "\033[")

			// The provenance block adds many lines, and none of them may
			// look like an error line to genesis's scraper.
			lines := adaptiveMergeErrorLines(stderr)
			So(len(lines), ShouldEqual, 1)
			So(lines[0], ShouldContainSubstring, "cycle detected")

			// The block itself must still be present.
			So(stderr, ShouldContainSubstring, "cycle (2 nodes):")
		})
	})
}
