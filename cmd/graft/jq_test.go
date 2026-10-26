package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
	"github.com/fivetwenty-io/graft/pkg/query"
)

// runJQ runs `graft jq ARGS` with stdin as its input, the way the command
// does once cobra hands it the arguments, and returns what it wrote to
// stdout and stderr and the status it ended with.
// launchHome is the home directory the test binary started with. A jq
// run would read a .jq file there, so the helpers swap it for an empty
// directory unless the test has set a home of its own.
var launchHome = os.Getenv("HOME")

func isolateHome(t *testing.T) {
	t.Helper()
	if os.Getenv("HOME") == launchHome {
		t.Setenv("HOME", t.TempDir())
	}
}

func runJQ(t *testing.T, args []string, stdin string) (out, errOut string, st query.Status) {
	t.Helper()
	isolateHome(t)
	var stdout, stderr bytes.Buffer
	opts, err := parseJQArgs(args, personaGraft)
	if err != nil {
		st = jqArgsError(err)
	} else {
		st = handleJQ(opts, strings.NewReader(stdin), &stdout, &stderr)
	}
	stderr.WriteString(st.Message)
	return stdout.String(), stderr.String(), st
}

// writeTemp writes content to a fresh file and returns its path.
func writeTemp(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "input")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestJQRunsPrograms(t *testing.T) {
	Convey("graft jq runs programs over JSON and YAML", t, func() {
		out, errOut, st := runJQ(t, []string{".a"}, "{\"a\":1}\n")
		So(out, ShouldEqual, "1\n")
		So(errOut, ShouldEqual, "")
		So(st.Code, ShouldEqual, 0)
		out, _, _ = runJQ(t, []string{"-c", "."}, "a: 1\n---\nb: 2\n")
		So(out, ShouldEqual, "{\"a\":1}\n{\"b\":2}\n")
		out, _, _ = runJQ(t, []string{"-n", "[inputs]", "-c"}, "1 2 3")
		So(out, ShouldEqual, "[1,2,3]\n")
		out, _, _ = runJQ(t, []string{"-s", "-c", "."}, "1\n2\n")
		So(out, ShouldEqual, "[1,2]\n")
		out, _, _ = runJQ(t, []string{"-R", "."}, "x\ny\n")
		So(out, ShouldEqual, "\"x\"\n\"y\"\n")
		out, _, _ = runJQ(t, []string{"--arg", "v", "z", "--argjson", "j", "[1]", "-c", "[$v, $j]"}, "null")
		So(out, ShouldEqual, "[\"z\",[1]]\n")
		out, _, _ = runJQ(t, []string{"-c", "$ARGS", "--args", "a", "b"}, "null")
		So(out, ShouldEqual, "{\"positional\":[\"a\",\"b\"],\"named\":{}}\n")
	})
}

func TestJQExitCodes(t *testing.T) {
	Convey("graft jq exit codes and error shapes", t, func() {
		_, e, st := runJQ(t, []string{"-e", "."}, "null")
		So(st.Code, ShouldEqual, 1)
		So(e, ShouldEqual, "")
		_, _, st = runJQ(t, []string{"-e", "empty"}, "null")
		So(st.Code, ShouldEqual, 4)
		_, _, st = runJQ(t, []string{"-e", "false, true"}, "null")
		So(st.Code, ShouldEqual, 0) // the last output decides
		_, _, st = runJQ(t, []string{"-e", "true, null"}, "null")
		So(st.Code, ShouldEqual, 1)
		_, e, st = runJQ(t, []string{"."}, "{")
		So(st.Code, ShouldEqual, 5) // jq 1.8.2 exits 5 on malformed input, not 2
		So(e, ShouldStartWith, "Invalid input (at <stdin>:")
		out, _, st := runJQ(t, []string{"-n", "limit(1; inputs)"}, "1\n{")
		So(out, ShouldEqual, "1\n")
		So(st.Code, ShouldEqual, 0) // the broken value is never read
		_, e, st = runJQ(t, []string{".a |"}, "null")
		So(st.Code, ShouldEqual, 3)
		So(e, ShouldStartWith, "Compile error: ")
		So(e, ShouldNotContainSubstring, "compile error\n")
		_, e, st = runJQ(t, []string{".a"}, "[1]")
		So(st.Code, ShouldEqual, 5)
		So(e, ShouldEqual, "Runtime error (at <stdin>:1): Cannot index array with string (\"a\")\n")
		_, e, st = runJQ(t, []string{".", "/nonexistent"}, "")
		So(st.Code, ShouldEqual, 2)
		So(e, ShouldStartWith, "Unable to open file /nonexistent: ")
		_, e, st = runJQ(t, []string{"--bogus", "."}, "")
		So(st.Code, ShouldEqual, 2)
		So(e, ShouldStartWith, "Unknown option: --bogus")
		_, e, st = runJQ(t, []string{"--repl", "."}, "1")
		So(st.Code, ShouldEqual, 1)
		So(e, ShouldContainSubstring, "Missing Input")
		_, _, st = runJQ(t, []string{"--indent", "8", "."}, "1")
		So(st.Code, ShouldEqual, 2)
		_, e, st = runJQ(t, []string{`halt_error(7)`}, "\"bye\\n\"")
		So(st.Code, ShouldEqual, 7)
		So(e, ShouldEqual, "bye\n")
		_, e, st = runJQ(t, []string{"--stream", "."}, "1")
		So(st.Code, ShouldEqual, 2)
		So(e, ShouldContainSubstring, "--stream is not available until a later release")
	})

	Convey("graft jq keeps going the way jq does after an error", t, func() {
		out, e, st := runJQ(t, []string{"-c", ".a"}, "{\"a\":1} [2] {\"a\":3}")
		So(out, ShouldEqual, "1\n3\n")
		So(e, ShouldEqual, "Runtime error (at <stdin>:1): Cannot index array with string (\"a\")\n")
		So(st.Code, ShouldEqual, 0) // the last input decides, as in jq
		good := writeTemp(t, "1 2")
		out, e, st = runJQ(t, []string{".", "/nonexistent", good}, "")
		So(out, ShouldEqual, "1\n") // jq stops after the value it was reading when the open failed
		So(e, ShouldStartWith, "Unable to open file /nonexistent: ")
		So(st.Code, ShouldEqual, 2)
		out, _, st = runJQ(t, []string{"-c", "-n", "[inputs]", "/nonexistent", good}, "")
		So(out, ShouldEqual, "[1,2]\n")
		So(st.Code, ShouldEqual, 2)
		_, e, st = runJQ(t, []string{"-n", `error("x")`}, "")
		So(e, ShouldEqual, "Runtime error (at <unknown>): x\n")
		So(st.Code, ShouldEqual, 5)
		_, e, st = runJQ(t, []string{"-n", `"a\n", {"b":1} | halt_error`}, "")
		So(e, ShouldEqual, "a\n")
		So(st.Code, ShouldEqual, 5)
		_, e, st = runJQ(t, []string{"-n", `{"b":1} | halt_error(3)`}, "")
		So(e, ShouldEqual, "{\"b\":1}\n")
		So(st.Code, ShouldEqual, 3)
		_, e, st = runJQ(t, []string{"-n", `null | halt_error(1)`}, "")
		So(e, ShouldEqual, "")
		So(st.Code, ShouldEqual, 1)
		out, e, st = runJQ(t, []string{"-n", `1, halt, 2`}, "")
		So(out, ShouldEqual, "1\n")
		So(e, ShouldEqual, "")
		So(st.Code, ShouldEqual, 0)
		_, _, st = runJQ(t, []string{"-n", `"x" | halt_error(-1)`}, "")
		So(st.Code, ShouldEqual, 0) // a negative halt code exits 0 without -e
		_, _, st = runJQ(t, []string{"-e", `if . == null then halt_error(-4) else . end`}, "1 null")
		So(st.Code, ShouldEqual, 0) // and halt_error(-4) reads as no output under -e
		_, _, st = runJQ(t, []string{"-e", ".a"}, "[] {\"a\":true}")
		So(st.Code, ShouldEqual, 0)
		_, e, st = runJQ(t, []string{".", t.TempDir()}, "")
		So(e, ShouldStartWith, "Invalid input (at ")
		So(st.Code, ShouldEqual, 2) // a source that cannot be read exits 2, as in jq
		out, _, _ = runJQ(t, []string{"input_filename"}, "1")
		So(out, ShouldEqual, "\"<stdin>\"\n")
		out, _, _ = runJQ(t, []string{"-n", "input_filename"}, "1")
		So(out, ShouldEqual, "null\n")
	})
}

func TestJQRunTests(t *testing.T) {
	Convey("graft jq run-tests", t, func() {
		f := writeTemp(t, ".a\n{\"a\":1}\n1\n")
		out, _, st := runJQ(t, []string{"--run-tests", f}, "")
		So(st.Code, ShouldEqual, 0)
		So(out, ShouldContainSubstring, "1 of 1 tests passed")
		out, _, st = runJQ(t, []string{"--run-tests"}, ".a\n{\"a\":1}\n2\n")
		So(st.Code, ShouldEqual, 1)
		So(out, ShouldContainSubstring, "0 of 1 tests passed")
		_, e, st := runJQ(t, []string{"--run-tests", "/nonexistent"}, "")
		So(e, ShouldStartWith, "Unable to open file /nonexistent: ")
		So(st.Code, ShouldEqual, 1) // jq exits 1 when it cannot open the test file
	})
}

func TestJQOutputFormat(t *testing.T) {
	Convey("graft jq output format and key sorting", t, func() {
		in := "{\"b\":1,\"a\":{\"10\":1,\"2\":2,\"x\":3}}"
		out, _, st := runJQ(t, []string{"-o", "yaml", "."}, in)
		So(st.Code, ShouldEqual, 0)
		So(out, ShouldEqual, "b: 1\na:\n  \"10\": 1\n  \"2\": 2\n  x: 3\n")
		out, _, _ = runJQ(t, []string{"-o", "y", "-r", ".a.x | tostring"}, in)
		So(out, ShouldEqual, "3\n")
		out, _, _ = runJQ(t, []string{"-c", "-S", "."}, in)
		So(out, ShouldEqual, "{\"a\":{\"10\":1,\"2\":2,\"x\":3},\"b\":1}\n")
		out, _, _ = runJQ(t, []string{"-c", "--sort-keys", "."}, in)
		So(out, ShouldEqual, "{\"a\":{\"10\":1,\"2\":2,\"x\":3},\"b\":1}\n")
		out, _, _ = runJQ(t, []string{"-c", "--sort-keys=spruce", "."}, in)
		So(out, ShouldEqual, "{\"a\":{\"2\":2,\"10\":1,\"x\":3},\"b\":1}\n")
		out, _, _ = runJQ(t, []string{"-c", "--sort-keys=none", "."}, in)
		So(out, ShouldEqual, "{\"b\":1,\"a\":{\"10\":1,\"2\":2,\"x\":3}}\n")
		_, e, st := runJQ(t, []string{"-o", "xml", "."}, in)
		So(st.Code, ShouldEqual, 2)
		So(e, ShouldStartWith, "Invalid -o value: ")
		_, e, st = runJQ(t, []string{"--output-format", "yaml", "."}, in)
		So(st.Code, ShouldEqual, 2)
		So(e, ShouldStartWith, "Unknown option: --output-format") // jq has no long form, and graft jq adds none
		_, e, st = runJQ(t, []string{"-o", "yaml", "-c", "."}, in)
		So(st.Code, ShouldEqual, 2)
		So(e, ShouldStartWith, "Invalid flags: -c cannot be combined with -o yaml")
		_, e, st = runJQ(t, []string{"--sort-keys=alpha", "."}, in)
		So(st.Code, ShouldEqual, 2)
		So(e, ShouldContainSubstring, "unknown sort mode \"alpha\"")
		out, _, _ = runJQ(t, []string{"-c", "-n", "0.1 + 0.2, 1e17 + 0"}, "")
		So(out, ShouldEqual, "0.3\n100000000000000000\n") // exact by default
		out, _, _ = runJQ(t, []string{"-c", "-n", "--float-numbers", "0.1 + 0.2, 1e17 + 0"}, "")
		So(out, ShouldEqual, "0.30000000000000004\n1e+17\n") // jq 1.8.2 byte for byte
		out, _, _ = runJQ(t, []string{"-c", "-n", "--float-numbers", "have_decnum"}, "")
		So(out, ShouldEqual, "true\n")
	})

	Convey("graft jq lays out JSON the way jq does", t, func() {
		in := "{\"a\":[1]}"
		out, _, _ := runJQ(t, []string{"--indent", "0", "."}, in)
		So(out, ShouldEqual, "{\n\"a\": [\n1\n]\n}\n")
		out, _, _ = runJQ(t, []string{"-c", "--indent", "1", "."}, in)
		So(out, ShouldEqual, "{\n \"a\": [\n  1\n ]\n}\n") // the last layout flag wins
		out, _, _ = runJQ(t, []string{"--indent", "-1", "."}, in)
		So(out, ShouldEqual, "{\n\t\"a\": [\n\t\t1\n\t]\n}\n")
		out, _, _ = runJQ(t, []string{"--tab", "-c", "."}, in)
		So(out, ShouldEqual, "{\"a\":[1]}\n")
		out, _, _ = runJQ(t, []string{"-j", "-n", `"a", 1`}, "")
		So(out, ShouldEqual, "a1")
		out, _, _ = runJQ(t, []string{"--raw-output0", "-n", `"a", "b"`}, "")
		So(out, ShouldEqual, "a\x00b\x00")
		_, e, st := runJQ(t, []string{"-o", "yaml", "--indent", "1", "."}, in)
		So(st.Code, ShouldEqual, 2)
		So(e, ShouldStartWith, "Invalid flags: --indent 1 cannot be combined with -o yaml")
		out, _, _ = runJQ(t, []string{"-o", "yaml", "--indent", "4", "."}, in)
		So(out, ShouldEqual, "a:\n    - 1\n")
	})

	Convey("graft jq reads programs, arguments, and files the way jq does", t, func() {
		prog := writeTemp(t, ".a + $x")
		data := writeTemp(t, "{\"a\": 1}")
		out, _, st := runJQ(t, []string{"--argjson", "x", "2", "-f", prog, data}, "")
		So(out, ShouldEqual, "3\n")
		So(st.Code, ShouldEqual, 0)
		out, _, _ = runJQ(t, []string{"-c", "--slurpfile", "s", data, "--rawfile", "r", prog, "-n", "[$s, $r]"}, "")
		So(out, ShouldEqual, "[[{\"a\":1}],\".a + $x\"]\n")
		out, _, _ = runJQ(t, []string{"-c", "-n", "$ARGS.positional", "--jsonargs", "1", "{\"b\":2}"}, "")
		So(out, ShouldEqual, "[1,{\"b\":2}]\n")
		_, e, st := runJQ(t, []string{"-n", ".", "--jsonargs", "{"}, "")
		So(st.Code, ShouldEqual, 2)
		So(e, ShouldStartWith, "Invalid flags: invalid JSON text passed to --jsonargs")
		for _, text := range []string{"-n", "a: 1"} {
			_, e, st = runJQ(t, []string{"-n", ".", "--jsonargs", "--", text}, "")
			So(st.Code, ShouldEqual, 2) // --jsonargs takes JSON alone, as in jq
			So(e, ShouldStartWith, "Invalid flags: invalid JSON text passed to --jsonargs")
		}
		out, _, _ = runJQ(t, []string{"-c", "-n", "$x", "--argjson", "x", "a: 1"}, "")
		So(out, ShouldEqual, "{\"a\":1}\n")    // while --argjson takes YAML too
		out, _, _ = runJQ(t, nil, "{\"a\":1}") // no program means . when stdin is not a terminal
		So(out, ShouldEqual, "{\n  \"a\": 1\n}\n")
		out, _, _ = runJQ(t, []string{".", "-"}, "7")
		So(out, ShouldEqual, "7\n")
	})
}

func TestJQColor(t *testing.T) {
	Convey("graft jq colors its output only when asked or on a terminal", t, func() {
		t.Setenv("NO_COLOR", "")
		t.Setenv("JQ_COLORS", "")
		out, _, _ := runJQ(t, []string{"-C", "-c", "."}, "[1]")
		So(out, ShouldContainSubstring, "\x1b[")
		out, _, _ = runJQ(t, []string{"-C", "-M", "-c", "."}, "[1]")
		So(out, ShouldEqual, "[1]\n") // -M wins over -C, as in jq
		out, _, _ = runJQ(t, []string{"-c", "."}, "[1]")
		So(out, ShouldEqual, "[1]\n") // a buffer is not a terminal
		out, _, _ = runJQ(t, []string{"-C", "-o", "yaml", "."}, "{\"a\":1}")
		So(out, ShouldContainSubstring, "\x1b[")
		So(ansi.StripEscapes(out), ShouldEqual, "a: 1\n")
		t.Setenv("JQ_COLORS", "bogus")
		_, e, _ := runJQ(t, []string{"-M", "."}, "1")
		So(e, ShouldEqual, "Failed to set $JQ_COLORS\n")
	})
}

// streamLog records what stdout and stderr receive in the order they
// receive it, with stdout written as "out:" and stderr as "err:".
type streamLog struct {
	writes []string
}

// streamLogWriter is one stream of a streamLog, a terminal when tty is set.
type streamLogWriter struct {
	log  *streamLog
	name string
	tty  bool
}

func (w streamLogWriter) Write(p []byte) (int, error) {
	w.log.writes = append(w.log.writes, w.name+":"+string(p))
	return len(p), nil
}

func (w streamLogWriter) isTerminal() bool { return w.tty }

func TestJQBuffering(t *testing.T) {
	Convey("graft jq writes each output at once only on a terminal or under --unbuffered", t, func() {
		isolateHome(t)
		run := func(tty bool, args ...string) []string {
			sl := &streamLog{}
			opts, err := parseJQArgs(args, personaGraft)
			So(err, ShouldBeNil)
			handleJQ(opts, strings.NewReader("{\"a\":1} [2] {\"a\":3}"),
				streamLogWriter{sl, "out", tty}, streamLogWriter{sl, "err", false})
			return sl.writes
		}
		interleaved := []string{"out:1\n", "err:Runtime error (at <stdin>:1): Cannot index array with string (\"a\")\n", "out:3\n"}
		So(run(true, "-M", ".a"), ShouldResemble, interleaved)
		So(run(false, "-M", "--unbuffered", ".a"), ShouldResemble, interleaved)
		So(run(false, "-M", ".a"), ShouldResemble, []string{interleaved[1], "out:1\n3\n"})
	})
}

func TestJQCommand(t *testing.T) {
	dir, err := filepath.Abs(cliIdentityDir)
	if err != nil {
		t.Fatal(err)
	}
	run := func(stdin string, args ...string) (string, string, int) {
		return replayCLIIdentity(t, dir, cliIdentityCase{Name: "jq", Stdin: stdin, Args: args})
	}

	Convey("graft jq runs from graft's command line", t, func() {
		out, errOut, rc := run("-", "--no-color", "jq", "-c", "keys", "t1.yml")
		So(out, ShouldEqual, "[\"replicas\"]\n")
		So(errOut, ShouldEqual, "")
		So(rc, ShouldEqual, 0)
		out, _, rc = run("fixtures/t1.yml", "jq", ".replicas")
		So(out, ShouldEqual, "1\n") // stdin is read when no file is named
		So(rc, ShouldEqual, 0)
		out, _, rc = run("-", "-D", "jq", "-c", ".", "t1.yml")
		So(out, ShouldEqual, "{\"replicas\":1}\n") // graft's flags before the verb are graft's
		So(rc, ShouldEqual, 0)
		for _, pre := range [][]string{
			{"-D", "--color=on"},
			{"--theme=mono", "-T"},
			{"-T", "--theme=mono"},
			{"-DT"},
			{"--theme", "mono", "-D"},
			{"--max-loop-iterations=50", "--no-color"},
		} {
			out, _, rc = run("-", append(pre, "jq", "-M", "-c", ".", "t1.yml")...)
			So(out, ShouldEqual, "{\"replicas\":1}\n") // in every form merge takes
			So(rc, ShouldEqual, 0)
		}
		out, _, rc = run("-", "jq", "--theme", "mono", "-c", "--arg", "x", "--debug", "-n", "$x")
		So(out, ShouldEqual, "\"--debug\"\n") // a value is never a flag
		So(rc, ShouldEqual, 0)
		_, errOut, rc = run("-", "jq", "--bogus", ".")
		So(errOut, ShouldEqual, "Unknown option: --bogus\n")
		So(rc, ShouldEqual, 2)
		_, errOut, rc = run("-", "jq", "--theme", "bogus", ".")
		So(errOut, ShouldStartWith, "Invalid --theme value: \"bogus\"")
		So(rc, ShouldEqual, 1)
		_, errOut, rc = run("-", "jq", ".", "nope.yml")
		So(errOut, ShouldEqual, "Unable to open file nope.yml: no such file or directory\n")
		So(rc, ShouldEqual, 2)
		out, _, rc = run("-", "jq", "--help")
		So(out, ShouldStartWith, "Run a jq program over JSON and YAML input, the way jq "+jqCompatVersion+" does.\n")
		So(rc, ShouldEqual, 0)
		for _, flag := range []string{"-n, --null-input", "-e, --exit-status", "--arg NAME VALUE", "-o FORMAT", "--sort-keys[=MODE]", "      --debug", "      --trace", "--theme"} {
			So(out, ShouldContainSubstring, flag) // jq's flags, and graft's in the forms graft jq takes
		}
		for _, flag := range []string{"-D, --debug", "-T, --trace", "-v, --version", "Display version information"} {
			So(out, ShouldNotContainSubstring, flag) // flags graft jq refuses after the verb
		}
	})
}

func TestJQDepthLimitFlag(t *testing.T) {
	const deep = "def f: if . == 0 then 0 else (. - 1 | f) + 1 end; 20000 | f"
	Convey("--depth-limit moves the recursion bound", t, func() {
		_, errOut, st := runJQ(t, []string{deep}, "null")
		So(st.Code, ShouldEqual, 5)
		So(errOut, ShouldContainSubstring, "exceeds depth limit of 10000")

		out, _, st := runJQ(t, []string{"--depth-limit", "30000", deep}, "null")
		So(st.Code, ShouldEqual, 0)
		So(out, ShouldEqual, "20000\n")

		_, errOut, st = runJQ(t, []string{"--depth-limit", "0", "."}, "null")
		So(st.Code, ShouldEqual, 2)
		So(errOut, ShouldContainSubstring, "--depth-limit takes a positive number")
		_, errOut, st = runJQ(t, []string{"--depth-limit", "x", "."}, "null")
		So(st.Code, ShouldEqual, 2)
		So(errOut, ShouldContainSubstring, "--depth-limit takes a positive number")
	})

	Convey("--depth-limit bounds the nesting of the input too", t, func() {
		_, errOut, st := runJQ(t, []string{"--depth-limit", "3", "."}, "[[[[1]]]]")
		So(st.Code, ShouldEqual, 5)
		So(errOut, ShouldContainSubstring, "nesting exceeds maximum depth of 3")
		out, _, st := runJQ(t, []string{"--depth-limit", "3", "-c", "."}, "[[1]]")
		So(st.Code, ShouldEqual, 0)
		So(out, ShouldEqual, "[[1]]\n")
		out, _, st = runJQ(t, []string{"--depth-limit", "3", "-c", "."}, "[[[1]]]")
		So(st.Code, ShouldEqual, 0)
		So(out, ShouldEqual, "[[[1]]]\n")
	})
}
