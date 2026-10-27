package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	. "github.com/smartystreets/goconvey/convey"
)

func TestAliasDispatch(t *testing.T) {
	graft := buildGraftBinary(t)
	jqLink := linkAs(t, graft, "jq")
	yqLink := linkAs(t, graft, "yq")
	spruceLink := linkAs(t, graft, "spruce")

	Convey("the link composes with -m and -E as graft jq does", t, func() {
		for _, args := range [][]string{
			{"-m", "-c", ".", "testdata/compose/a.yml", "testdata/compose/b.yml"},
			{"-E", "-c", ".", "testdata/compose/a.yml", "testdata/compose/c.yml"},
			{"-m", "--prune", "a", "-c", ".", "testdata/compose/a.yml"},
			{"-m", ".", "testdata/compose/missing.yml"},
		} {
			a := execIn(t, ".", graft, append([]string{"jq"}, args...), "")
			b := execIn(t, ".", jqLink, args, "")
			So(b.Stdout, ShouldEqual, a.Stdout)
			So(b.Stderr, ShouldEqual, a.Stderr)
			So(b.Code, ShouldEqual, a.Code)
		}
		So(execIn(t, ".", jqLink, []string{"-m", "-c", ".", "testdata/compose/a.yml", "testdata/compose/b.yml"}, "").Stdout,
			ShouldEqual, "{\"a\":1,\"b\":2,\"base\":1,\"list\":[1,2]}\n")
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
		for _, c := range []struct {
			args []string
			err  string
			code int
		}{
			{[]string{"--bogus", "."}, "Unknown option: --bogus\n", 2},
			{[]string{".", "/nonexistent"}, "Unable to open file /nonexistent: no such file or directory\n", 2},
			{[]string{".a |"}, "Compile error: unexpected end of input at line 1, column 5\n", 3},
		} {
			args := c.args
			r := execIn(t, ".", jqLink, args, "1")
			So(r.Stderr, ShouldEqual, c.err)
			So(r.Code, ShouldEqual, c.code)
			g := execIn(t, ".", graft, append([]string{"jq"}, args...), "1")
			So(g.Stderr, ShouldEqual, r.Stderr)
			So(g.Code, ShouldEqual, r.Code)
		}
	})

	Convey("normalizeLegacyColorArgs does not rewrite query arguments", t, func() {
		// The legacy rewrite would join --color and on into one
		// positional, --color=on.
		args := []string{"-n", "-r", "$ARGS.positional[]", "--args", "--", "--color", "on"}
		want := "--color\non\n"
		So(execIn(t, ".", graft, append([]string{"jq"}, args...), "").Stdout, ShouldEqual, want)
		So(execIn(t, ".", jqLink, args, "").Stdout, ShouldEqual, want)
		// A root flag given before the verb still leaves the query's
		// arguments alone. Left alone, "on" is the program, which jq
		// refuses, where the rewrite would have bound $x and succeeded.
		pre := []string{"--theme", "mono", "jq", "-n", "-r", "--arg", "x", "--color", "on", "$x"}
		r := execIn(t, ".", graft, pre, "")
		So(r.Stderr, ShouldContainSubstring, "on/0 is not defined")
		So(r.Code, ShouldEqual, 3)

		// --arg takes the next two tokens, so --color is the value.
		args = []string{"--arg", "x", "--color", "-n", "-r", "$x"}
		So(execIn(t, ".", graft, append([]string{"jq"}, args...), "").Stdout, ShouldEqual, "--color\n")
		So(execIn(t, ".", jqLink, args, "").Stdout, ShouldEqual, "--color\n")

		// Left alone, "on" is the program, which jq refuses.
		args = []string{"-n", "-r", "--arg", "x", "--color", "on", "$x"}
		g := execIn(t, ".", graft, append([]string{"jq"}, args...), "")
		l := execIn(t, ".", jqLink, args, "")
		So(g.Stdout, ShouldEqual, "")
		So(g.Stderr, ShouldContainSubstring, "on/0 is not defined")
		So(g.Code, ShouldEqual, 3)
		So(l, ShouldResemble, g)
	})

	Convey("the jq name matches the way a symlink is named or chained", t, func() {
		for _, name := range []string{"jq.exe", "JQ"} {
			So(execIn(t, ".", linkAs(t, graft, name), []string{"--version"}, "").Stdout, ShouldStartWith, "jq-"+jqCompatVersion+"\n")
		}
		hop := linkAs(t, graft, "hop")
		chain := filepath.Join(t.TempDir(), "jq")
		So(os.Symlink(hop, chain), ShouldBeNil)
		So(execIn(t, ".", chain, []string{"-n", "1+1"}, "").Stdout, ShouldEqual, "2\n")
		So(execIn(t, ".", linkAs(t, graft, "jq-1.7"), []string{"-n", "1"}, "").Code, ShouldNotEqual, 0) // runs as graft
	})

	Convey("an invalid GRAFT_THEME warns the same under both names", t, func() {
		env := []string{"GRAFT_THEME=nope"}
		g := execInEnv(t, env, ".", graft, []string{"jq", "-n", "1"}, "")
		l := execInEnv(t, env, ".", jqLink, []string{"-n", "1"}, "")
		So(g.Stderr, ShouldContainSubstring, "Invalid GRAFT_THEME value")
		So(l, ShouldResemble, g)
		env = []string{"GRAFT_THEME=mono"}
		args := []string{"-C", "-o", "yaml", "-n", `{"a":[1,"x"]}`}
		g = execInEnv(t, env, ".", graft, append([]string{"jq"}, args...), "")
		So(execInEnv(t, env, ".", jqLink, args, ""), ShouldResemble, g)
	})

	Convey("the yq link reports yq's version and runs expressions as graft yq does", t, func() {
		for _, bin := range [][]string{{yqLink}, {graft, "yq"}} {
			v := execIn(t, ".", bin[0], append(bin[1:], "--version"), "")
			lines := strings.Split(v.Stdout, "\n")
			So(v.Code, ShouldEqual, 0)
			So(lines[0], ShouldEqual, "yq (https://github.com/mikefarah/yq/) version v"+yqCompatVersion)
			So(lines[1], ShouldContainSubstring, "graft")
		}
		// -v is verbose under the yq name and graft's version under graft.
		v := execIn(t, ".", yqLink, []string{"-v", ".a"}, "a: 1\n")
		So(v.Code, ShouldEqual, 0)
		So(v.Stdout, ShouldEqual, "1\n")
		So(v.Stdout, ShouldNotContainSubstring, "graft")
		g := execIn(t, ".", graft, []string{"yq", "-v"}, "")
		So(g.Code, ShouldEqual, 0)
		So(g.Stdout, ShouldContainSubstring, "graft")
	})

	Convey("graft's flags do not exist under the yq alias", t, func() {
		r := execIn(t, ".", yqLink, []string{"--theme", "x", "."}, "a: 1\n")
		So(r.Code, ShouldEqual, 1)
		So(r.Stderr, ShouldContainSubstring, "Unknown option: --theme")
	})

	Convey("the yq link and graft yq agree", t, func() {
		for _, args := range [][]string{
			{"-m", "-o", "json", "-I", "0", ".", "testdata/compose/a.yml", "testdata/compose/b.yml"},
			{".a +", "testdata/compose/a.yml"},
			{"-n", ".a = 1"},
		} {
			a := execIn(t, ".", graft, append([]string{"yq"}, args...), "")
			b := execIn(t, ".", yqLink, args, "")
			So(b, ShouldResemble, a)
		}
	})

	Convey("the spruce link is unchanged", t, func() {
		So(execIn(t, ".", spruceLink, []string{"-v"}, "").Stdout, ShouldContainSubstring, "spruce - Version")
		So(execIn(t, ".", spruceLink, []string{"-v"}, "").Stdout, ShouldNotContainSubstring, "jq-")
	})
}

// runBinaryIn runs bin with lead and then the case's arguments in a fresh
// copy of the case directory, under the environment the goldens were
// recorded in. It returns what the run wrote, with the copy's path written
// as $CASE_DIR, the exit code, and how the files left behind differ from
// what the case expects.
func runBinaryIn(t *testing.T, bin string, lead []string, c parityCase) (stdout, stderr string, code int, files []string) {
	t.Helper()
	dir := copyCase(t, c.Dir)
	args := append(append(append([]string{}, lead...), c.Args...), c.GraftArgs...)
	cmd := exec.CommandContext(context.Background(), bin, args...)
	cmd.Dir = dir
	cmd.Env = []string{"HOME=" + dir, "TZ=UTC", "LANG=C.UTF-8", "PWD=" + dir, "PATH=/usr/bin:/bin"}
	cmd.Stdin = bytes.NewBufferString(c.Stdin)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	var ee *exec.ExitError
	if err := cmd.Run(); errors.As(err, &ee) {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	scrub := func(s string) string { return strings.ReplaceAll(s, dir, caseDirMarker) }
	return scrub(out.String()), scrub(errOut.String()), code, diffFiles(dir, c.WantFiles)
}

// TestAliasParity runs every case of the jq corpus as `graft jq ARGS` and
// as `jq ARGS` through a link, and asserts that the two names agree on
// stdout, stderr, the exit code, and the files left behind. TestQueryParity
// judges `graft jq` against the reference goldens.
func TestAliasParity(t *testing.T) {
	runAliasParity(t, "jq")
}

// TestAliasParityYQ is TestAliasParity for the yq corpus.
func TestAliasParityYQ(t *testing.T) {
	runAliasParity(t, "yq")
}

// runAliasParity runs the cases of one dialect as parallel subtests. Each
// case gets its own copy of its directory from t.TempDir, passes the child
// process a fixed environment, and sets no working directory of its own, so
// the cases share no state. A failure is reported on the subtest named for
// its case. The parent counts the cases that ran and fails when that is
// fewer than the case directories on disk, so a case can't drop out
// silently.
func runAliasParity(t *testing.T, dialect string) {
	// The race detector only sees races inside the test process. These cases
	// spawn the built binary, which go test -race does not instrument, and the
	// test process only waits on its pipes, so a -race pass finds nothing the
	// plain run misses and costs a third of a minute or more. make test-unit
	// still runs every case.
	if raceEnabled {
		t.Skip("the cases spawn the built binary, which the race detector cannot instrument, so a -race pass would only repeat the plain run")
	}
	graft := buildGraftBinary(t)
	link := linkAs(t, graft, dialect)
	cases := loadParityCases(t, dialect)
	want := countCaseDirs(t, dialect)
	var ran atomic.Int64
	t.Cleanup(func() {
		if got := int(ran.Load()); got < want {
			t.Errorf("%s alias parity ran %d cases, but %d case directories exist", dialect, got, want)
		}
	})
	for i := range cases {
		c := cases[i]
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()
			ran.Add(1)
			wantOut, wantErr, wantCode, wantFiles := runBinaryIn(t, graft, []string{dialect}, c)
			out, errOut, code, files := runBinaryIn(t, link, nil, c)
			if out != wantOut {
				t.Errorf("stdout differs between the %s link and graft %s:\nlink:  %q\ngraft: %q", dialect, dialect, out, wantOut)
			}
			if errOut != wantErr {
				t.Errorf("stderr differs between the %s link and graft %s:\nlink:  %q\ngraft: %q", dialect, dialect, errOut, wantErr)
			}
			if code != wantCode {
				t.Errorf("exit code differs between the %s link and graft %s: link %d, graft %d", dialect, dialect, code, wantCode)
			}
			if !reflect.DeepEqual(files, wantFiles) {
				t.Errorf("files left behind differ between the %s link and graft %s:\nlink:  %v\ngraft: %v", dialect, dialect, files, wantFiles)
			}
		})
	}
}

// countCaseDirs counts the case directories of a dialect straight from the
// file system, apart from loadParityCases, so the check on how many cases ran
// does not share a bug with the loader.
func countCaseDirs(t *testing.T, dialect string) int {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(parityRoot, dialect, "cases"))
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() {
			n++
		}
	}
	return n
}
