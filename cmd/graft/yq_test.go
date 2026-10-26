package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/pkg/query"
)

// runYQ runs `yq ARGS` with stdin as its input, the way the command does
// once cobra hands it the arguments under the yq name, and returns what it
// wrote to stdout and stderr and the status it ended with.
func runYQ(t *testing.T, args []string, stdin string) (out, errOut string, st query.Status) {
	t.Helper()
	isolateHome(t)
	var stdout, stderr bytes.Buffer
	opts, err := parseYQArgs(args, personaYQ)
	switch {
	case err != nil:
		st = yqArgsError(err)
	case opts.Version:
		stdout.WriteString(yqVersionOutput())
	default:
		st = handleYQ(opts, strings.NewReader(stdin), &stdout, &stderr)
	}
	stderr.WriteString(st.Message)
	return stdout.String(), stderr.String(), st
}

// runYQIn is runYQ in the directory dir, so that relative file names in
// args resolve there.
func runYQIn(t *testing.T, dir string, args []string) (stdout, stderr string, st query.Status) {
	t.Helper()
	t.Chdir(dir)
	return runYQ(t, args, "")
}

// runYQStdinScript runs `graft yq` with script as stdin, which is how
// --repl reads its commands.
func runYQStdinScript(t *testing.T, args []string, script string) (out, errOut string, st query.Status) {
	t.Helper()
	return runYQ(t, args, script)
}

// writeTempNamed writes content to a fresh file called name and returns
// its path.
func writeTempNamed(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// readTestFile returns the content of the file at path.
func readTestFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path) // #nosec G304 -- the test reads its own temporary file
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestYQEvaluates(t *testing.T) {
	Convey("graft yq evaluates and preserves", t, func() {
		out, _, st := runYQ(t, []string{".a"}, "a: 1 # c\n")
		So(out, ShouldEqual, "1\n")
		So(st.Code, ShouldEqual, 0)
		out, _, _ = runYQ(t, []string{"."}, "# head\na: 'x' # c\nb: &k [1]\nc: *k\n")
		So(out, ShouldEqual, "# head\na: 'x' # c\nb: &k [1]\nc: *k\n")
		out, _, _ = runYQ(t, []string{"-o", "json", "-I", "0", "."}, "a: 1\n")
		So(out, ShouldEqual, "{\"a\":1}\n")
		out, _, _ = runYQ(t, []string{".a + 1"}, "a: 9223372036854775807\n")
		So(out, ShouldEqual, "9223372036854775808\n") // exact by default
		out, _, st = runYQ(t, []string{"--float-numbers", ".a + 1"}, "a: 9223372036854775807\n")
		So(st.Code, ShouldEqual, 1)
		So(out, ShouldBeEmpty) // yq's overflows int64
		out, _, _ = runYQ(t, []string{"-P", "."}, `{"a":[1]}`)
		So(out, ShouldEqual, "a:\n  - 1\n")
		out, _, _ = runYQ(t, []string{"-N", "."}, "a: 1\n---\nb: 2\n")
		So(out, ShouldEqual, "a: 1\nb: 2\n")
		out, _, _ = runYQ(t, []string{"-r=false", ".a"}, "a: x\n")
		So(out, ShouldEqual, "x\n") // YAML output never quotes a plain string, so -r only matters for -o json
		out, _, _ = runYQ(t, []string{"-r=false", "-o", "json", ".a"}, "a: x\n")
		So(out, ShouldEqual, "\"x\"\n")
		out, _, _ = runYQ(t, []string{"ea", "-o", "json", "-I", "0", "[.a]"}, "a: 1\n---\na: 2\n")
		So(out, ShouldEqual, "[1,2]\n")
		out, _, _ = runYQ(t, []string{"ea", "-o", "json", "-I", "0", "[.[] | .a]"}, "a: 1\n---\na: 2\n")
		So(out, ShouldEqual, "[]\n") // .[] yields scalars, and .a on a scalar is empty
		out, _, _ = runYQ(t, []string{"-n", ".a = 1"}, "")
		So(out, ShouldEqual, "a: 1\n")
		out, _, _ = runYQ(t, []string{"-o", "json", "-I", "0", "."}, "a: 1.0\n")
		So(out, ShouldEqual, "{\"a\":1.0}\n") // the literal survives
	})
}

func TestYQEmptyInputIsOneNullDocument(t *testing.T) {
	Convey("graft yq runs once over a blank null when nothing is read", t, func() {
		out, _, st := runYQ(t, []string{"."}, "")
		So(out, ShouldEqual, "\n")
		So(st.Code, ShouldEqual, 0)
		out, _, _ = runYQ(t, []string{".a"}, "")
		So(out, ShouldEqual, "null\n")
		out, _, _ = runYQ(t, []string{"ea", ".a = 1"}, "")
		So(out, ShouldEqual, "a: 1\n")
		_, _, st = runYQ(t, []string{"-e", "."}, "")
		So(st.Code, ShouldEqual, 1)
		out, _, _ = runYQ(t, []string{"-n", "."}, "")
		So(out, ShouldEqual, "\n")
		empty := writeTempNamed(t, "empty.yaml", "")
		full := writeTempNamed(t, "full.yaml", "a: 1\n")
		out, _, _ = runYQ(t, []string{".", empty, full}, "")
		So(out, ShouldEqual, "a: 1\n") // only a run that read nothing at all falls back
	})
}

func TestYQArguments(t *testing.T) {
	Convey("graft yq argument rules, errors, and exit codes", t, func() {
		f := writeTemp(t, "a: 1\n")
		ef := writeTempNamed(t, "e.yq", ".a")
		out, _, _ := runYQ(t, []string{f}, "")
		So(out, ShouldEqual, "a: 1\n") // first positional is a file, expression defaults to .
		out, _, _ = runYQ(t, []string{ef, f}, "")
		So(out, ShouldEqual, "1\n")
		_, e, st := runYQ(t, []string{".a +", f}, "")
		So(st.Code, ShouldEqual, 1)
		So(e, ShouldStartWith, "Compile error: ")
		_, e, st = runYQ(t, []string{".", "/nonexistent"}, "")
		So(st.Code, ShouldEqual, 1)
		So(e, ShouldStartWith, "Unable to open file /nonexistent: ")
		_, e, st = runYQ(t, []string{"-o", "xml", "."}, "a: 1\n")
		So(st.Code, ShouldEqual, 1)
		So(e, ShouldEqual, "Invalid -o value: \"xml\" is not built into graft yq (auto, yaml, and json only)\n")
		_, e, st = runYQ(t, []string{"--output-format", "xml", "."}, "a: 1\n")
		So(st.Code, ShouldEqual, 1)
		So(e, ShouldStartWith, "Invalid -o value") // the long form is kept for yq compatibility and shares the message
		_, e, st = runYQ(t, []string{"-e", ".a"}, "a: null\n")
		So(st.Code, ShouldEqual, 1)
		So(e, ShouldEqual, "No matches found\n")
		_, _, st = runYQ(t, []string{"-e", ".[]"}, "- null\n- 1\n- false\n")
		So(st.Code, ShouldEqual, 0) // one truthy output is a match
		_, e, st = runYQ(t, []string{"--theme", "x", "."}, "a: 1\n")
		So(st.Code, ShouldEqual, 1)
		So(e, ShouldContainSubstring, "Unknown option") // graft persistent flags stay off the yq command
		out, _, _ = runYQ(t, []string{"-S", "."}, "b: 1\na:\n  \"10\": 1\n  \"2\": 2\n")
		So(out, ShouldEqual, "a:\n  \"10\": 1\n  \"2\": 2\nb: 1\n")
		out, _, _ = runYQ(t, []string{"--sort-keys=spruce", "."}, "b: 1\na:\n  \"10\": 1\n  \"2\": 2\n")
		So(out, ShouldEqual, "a:\n  \"2\": 2\n  \"10\": 1\nb: 1\n")
		_, _, st = runYQ(t, []string{"-e", ".a"}, "a: 1\n")
		So(st.Code, ShouldEqual, 0)
	})

	Convey("graft yq reads its flags the way yq does", t, func() {
		f := writeTemp(t, "a: 1\n")
		out, _, _ := runYQ(t, []string{"eval", ".a", f}, "")
		So(out, ShouldEqual, "1\n")
		out, _, _ = runYQ(t, []string{"e", "--expression", ".a", f}, "")
		So(out, ShouldEqual, "1\n") // --expression makes every positional a file
		ef := writeTempNamed(t, "x.txt", ".a\r\n")
		out, _, _ = runYQ(t, []string{"--from-file", ef, f}, "")
		So(out, ShouldEqual, "1\n")
		out, _, _ = runYQ(t, []string{"-nr", ".a = \"x\" | .a"}, "")
		So(out, ShouldEqual, "x\n") // short bools combine
		out, _, _ = runYQ(t, []string{"-I4", "-o=json", "."}, "a: {b: 1}\n")
		So(out, ShouldEqual, "{\n    \"a\": {\n        \"b\": 1\n    }\n}\n")
		out, _, _ = runYQ(t, []string{"-0", ".a"}, "a: x\n")
		So(out, ShouldEqual, "x\x00")
		out, _, _ = runYQ(t, []string{"--security-disable-env-ops", "-n", "1"}, "")
		So(out, ShouldEqual, "1\n")
		_, e, st := runYQ(t, []string{"--security-disable-env-ops", "-n", "env(HOME)"}, "")
		So(st.Code, ShouldEqual, 1)
		So(e, ShouldContainSubstring, "Runtime error")
		_, _, st = runYQ(t, []string{"--bogus", "."}, "a: 1\n")
		So(st.Code, ShouldEqual, 1)
	})

	Convey("graft yq picks the output format from the first file", t, func() {
		dir := t.TempDir()
		So(os.WriteFile(filepath.Join(dir, "in.json"), []byte(`{"a":1}`), 0o600), ShouldBeNil)
		out, _, _ := runYQIn(t, dir, []string{"-I", "0", ".", "in.json"})
		So(out, ShouldEqual, "{\"a\":1}\n")
		out, _, _ = runYQIn(t, dir, []string{"-o", "y", ".", "in.json"})
		So(out, ShouldEqual, "a: 1\n")
		out, _, _ = runYQ(t, []string{"."}, `{"a":1.0}`)
		So(out, ShouldEqual, "{\"a\": 1.0}\n") // stdin never selects JSON output
		out, e, _ := runYQ(t, []string{"-p", "json", "."}, `{"a":1}`)
		So(out, ShouldEqual, "a: 1\n") // stdin with -p json still prints YAML
		So(e, ShouldBeEmpty)
		out, e, _ = runYQIn(t, dir, []string{"-p", "json", ".", "in.json"})
		So(out, ShouldEqual, "a: 1\n") // an explicit -p keeps YAML for a .json file
		So(e, ShouldContainSubstring, "yq default output is now 'auto' (based on the filename extension).")
		So(e, ShouldContainSubstring, "drop the -p flag.")
		out, e, _ = runYQIn(t, dir, []string{"-p", "json", "-o", "json", "-I", "0", ".", "in.json"})
		So(out, ShouldEqual, "{\"a\":1}\n") // an explicit -o needs no warning
		So(e, ShouldBeEmpty)
	})
}

func TestYQInplace(t *testing.T) {
	Convey("graft yq -i", t, func() {
		f := writeTemp(t, "a: 1 # keep\n")
		_, _, st := runYQ(t, []string{"-i", ".b = 2", f}, "")
		So(st.Code, ShouldEqual, 0)
		So(readTestFile(t, f), ShouldEqual, "a: 1 # keep\nb: 2\n")
		bad := writeTemp(t, "a: [\n")
		_, _, st = runYQ(t, []string{"-i", ".b = 2", bad}, "")
		So(st.Code, ShouldEqual, 1)
		So(readTestFile(t, bad), ShouldEqual, "a: [\n") // untouched on failure
		_, e, st := runYQ(t, []string{"-i", "-m", ".", f}, "")
		So(st.Code, ShouldEqual, 1)
		So(e, ShouldContainSubstring, "-m cannot be combined with -i")
		_, e, st = runYQ(t, []string{"-i", ".b = 2"}, "a: 1\n")
		So(st.Code, ShouldEqual, 1)
		So(e, ShouldContainSubstring, "-i needs a file")
		info, err := os.Stat(f)
		So(err, ShouldBeNil)
		So(info.Mode().Perm(), ShouldEqual, os.FileMode(0o600)) // the rewrite keeps the mode
	})
}

func TestYQCompose(t *testing.T) {
	Convey("graft yq composes with -m and -E as graft jq does", t, func() {
		out, _, st := runYQ(t, []string{"-m", "-o", "json", "-I", "0", ".", "testdata/compose/a.yml", "testdata/compose/b.yml"}, "")
		So(st.Code, ShouldEqual, 0)
		So(out, ShouldEqual, "{\"a\":1,\"b\":2,\"base\":1,\"list\":[1,2]}\n")
		_, _, st = runYQ(t, []string{"-m", ".", "testdata/compose/missing.yml"}, "")
		So(st.Code, ShouldEqual, 1)
		_, e, st := runYQ(t, []string{"--prune", "a", "."}, "a: 1\n")
		So(st.Code, ShouldEqual, 1)
		So(e, ShouldContainSubstring, "--prune needs -m or -E")
	})
}

func TestYQDebugREPL(t *testing.T) {
	Convey("yq in the debug REPL", t, func() {
		out, rc := runDebugSession([]string{"testdata/debug/a.yml"}, "load\ncontinue\nyq .meta.name\nconfig dialect yq\nselect .jobs[] | select(.name == \"nats\")\nbreak yq:.jobs[].port\nbreaks\nquit\n")
		So(rc, ShouldEqual, 0)
		So(out, ShouldContainSubstring, "web\n")
		So(out, ShouldContainSubstring, "selected 1 path: jobs.[0]")
		So(out, ShouldContainSubstring, "Breakpoint set on jobs.nats.port")
		out, _, st := runYQStdinScript(t, []string{"--repl", ".meta.name", "testdata/debug/a.yml"}, "quit\n")
		So(st.Code, ShouldEqual, 0)
		So(out, ShouldStartWith, "web\n")
		out, _, st = runYQStdinScript(t, []string{"--repl", "testdata/debug/a.yml"}, "config dialect\nquit\n")
		So(st.Code, ShouldEqual, 0)
		So(out, ShouldContainSubstring, "dialect: yq") // a session from graft yq speaks yq
		_, e, st := runYQ(t, []string{"--repl", ".a"}, "")
		So(st.Code, ShouldEqual, 1)
		So(e, ShouldContainSubstring, "graft yq --repl requires at least one file")
	})
}

func TestYQVersionAndPersona(t *testing.T) {
	Convey("yq version and persona", t, func() {
		out, _, _ := runYQ(t, []string{"--version"}, "")
		So(strings.Split(out, "\n")[0], ShouldEqual, "yq (https://github.com/mikefarah/yq/) version v"+yqCompatVersion)
		root, _ := newPersonaRoot(personaYQ)
		So(root.DisableFlagParsing, ShouldBeTrue)
		So(root.PersistentFlags().Lookup("theme"), ShouldBeNil)
		So(root.Commands(), ShouldBeEmpty)
		p, ok := queryInvocation([]string{"yq", "."})
		So(ok, ShouldBeTrue)
		So(p, ShouldEqual, personaYQ)
		opts, err := parseYQArgs([]string{"-v", "."}, personaYQ)
		So(err, ShouldBeNil)
		So(opts.Verbose, ShouldBeTrue) // -v is verbose under yq, never version
		So(opts.Version, ShouldBeFalse)
		opts, err = parseYQArgs([]string{"-v", "."}, personaGraft)
		So(err, ShouldBeNil)
		So(opts.Version, ShouldBeTrue) // and graft's version under the graft name
		So(defaultAliasNames(), ShouldResemble, []string{"jq", "yq", "spruce"})
	})
}

func TestYQMergeKeyWarning(t *testing.T) {
	const doc = "base: &base\n  size: 3\nitem:\n  <<: *base\n  extra: true\n"
	Convey("explode warns once about the default merge key behavior", t, func() {
		out, errOut, st := runYQ(t, []string{"explode(.) | .item"}, doc)
		So(st.Code, ShouldEqual, 0)
		So(out, ShouldEqual, "size: 3\nextra: true\n")
		So(strings.Count(errOut, "--yaml-fix-merge-anchor-to-spec"), ShouldEqual, 1)
	})

	Convey("the spec flag and a document with no merge key stay quiet", t, func() {
		_, errOut, _ := runYQ(t, []string{"--yaml-fix-merge-anchor-to-spec", "explode(.)"}, doc)
		So(errOut, ShouldEqual, "")
		_, errOut, _ = runYQ(t, []string{"explode(.)"}, "a: 1\n")
		So(errOut, ShouldEqual, "")
	})
}

func TestYQJSONNamedInputDecodesAsJSON(t *testing.T) {
	const doc = `{"b": 2, "a": [1, 2]}` + "\n"
	const block = "b: 2\na:\n  - 1\n  - 2\n"
	path := writeTempNamed(t, "in.json", doc)
	Convey("a .json file decodes as JSON, so YAML output has no flow style", t, func() {
		for _, flag := range []string{"-o=yaml", "-oy"} {
			out, _, st := runYQ(t, []string{flag, ".", path}, "")
			So(st.Code, ShouldEqual, 0)
			So(out, ShouldEqual, block)
		}
		out, _, _ := runYQ(t, []string{"-I=4", "-o=yaml", ".", path}, "")
		So(out, ShouldEqual, "b: 2\na:\n    - 1\n    - 2\n")
	})

	Convey("-p yaml and stdin keep decoding as YAML", t, func() {
		out, _, _ := runYQ(t, []string{"-p=yaml", "-o=yaml", ".", path}, "")
		So(out, ShouldEqual, doc)
		out, _, _ = runYQ(t, []string{"-o=yaml", "."}, doc)
		So(out, ShouldEqual, doc)
	})
}
