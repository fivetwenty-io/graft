package main

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/pkg/query"
	parity "github.com/fivetwenty-io/graft/tests/query-parity"
)

// parityRoot is where the differential cases and their goldens live, as an
// absolute path because the replay changes directory.
var parityRoot = func() string {
	abs, err := filepath.Abs("../../tests/query-parity")
	if err != nil {
		panic(err)
	}
	return abs
}()

// caseDirMarker stands for a run's temporary copy of its case wherever the
// goldens would otherwise record that path.
const caseDirMarker = "$CASE_DIR"

// fileExpect is a file a run is expected to leave behind.
type fileExpect struct {
	Content []byte
	Mode    os.FileMode
}

// parityCase is one differential case: the arguments, the input it ships,
// and the golden the reference binary recorded for it.
type parityCase struct {
	Name            string
	Phase           string
	Args            []string
	GraftArgs       []string
	Stdin           string
	Dir             string
	WantGraftStdout string
	PinsGraftStdout bool
	WantStdout      string
	WantExit        int
	WantStderrFlag  bool
	WantStderr      string
	StderrExact     bool
	WantFiles       map[string]fileExpect
}

// parityControlFiles are the files in a case directory that describe the
// case, as opposed to input the program may read.
var parityControlFiles = map[string]bool{
	"args": true, "stdin": true, "graft_args": true, "graft-stdout": true,
	"phase": true, "stderr-exact": true, "want": true,
}

// readLines returns the lines of a file, or nil when it does not exist.
func readLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

// readOptional returns a file's text, or "" when it does not exist.
func readOptional(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return string(data)
}

// loadParityCases reads every case of a dialect, in name order.
func loadParityCases(t *testing.T, dialect string) []parityCase {
	t.Helper()
	root, err := filepath.Abs(filepath.Join(parityRoot, dialect, "cases"))
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	var cases []parityCase
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dir := filepath.Join(root, e.Name())
		c := parityCase{
			Name:            e.Name(),
			Phase:           strings.TrimSpace(readOptional(t, filepath.Join(dir, "phase"))),
			Args:            readLines(t, filepath.Join(dir, "args")),
			GraftArgs:       readLines(t, filepath.Join(dir, "graft_args")),
			Stdin:           readOptional(t, filepath.Join(dir, "stdin")),
			Dir:             dir,
			WantGraftStdout: readOptional(t, filepath.Join(dir, "graft-stdout")),
			WantStdout:      readOptional(t, filepath.Join(dir, "want", "stdout")),
			WantStderr:      readOptional(t, filepath.Join(dir, "want", "stderr")),
			WantFiles:       map[string]fileExpect{},
		}
		if c.Phase == "" {
			c.Phase = map[string]string{"jq": "J1", "yq": "Y1"}[dialect]
		}
		if c.Args == nil {
			t.Fatalf("case %s has no args file", c.Name)
		}
		code, err := strconv.Atoi(strings.TrimSpace(readOptional(t, filepath.Join(dir, "want", "exit"))))
		if err != nil {
			t.Fatalf("case %s has no golden exit code: %v", c.Name, err)
		}
		c.WantExit = code
		_, pinErr := os.Stat(filepath.Join(dir, "graft-stdout"))
		c.PinsGraftStdout = pinErr == nil
		c.WantStderrFlag = strings.TrimSpace(readOptional(t, filepath.Join(dir, "want", "stderr-flag"))) == "1"
		_, statErr := os.Stat(filepath.Join(dir, "stderr-exact"))
		c.StderrExact = statErr == nil

		// An input file the case ships must come out of the run as it
		// went in, so it is expected as well as the files the run writes.
		collectFiles(t, dir, func(rel string) bool {
			return !parityControlFiles[strings.SplitN(rel, string(filepath.Separator), 2)[0]]
		}, c.WantFiles)
		collectFiles(t, filepath.Join(dir, "want", "files"), func(string) bool { return true }, c.WantFiles)
		cases = append(cases, c)
	}
	sort.Slice(cases, func(i, j int) bool { return cases[i].Name < cases[j].Name })
	return cases
}

// collectFiles adds every file under root that keep accepts to into, keyed
// by its slash-separated path from root.
func collectFiles(t *testing.T, root string, keep func(rel string) bool, into map[string]fileExpect) {
	t.Helper()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) && path == root {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil || !keep(rel) {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path) // #nosec G122 -- the test reads its own checked-in cases
		if err != nil {
			return err
		}
		into[filepath.ToSlash(rel)] = fileExpect{Content: content, Mode: info.Mode().Perm()}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// copyCase copies a case directory, without its goldens, into a fresh
// temporary directory and returns the copy's path with symlinks resolved.
func copyCase(t *testing.T, src string) string {
	t.Helper()
	dst, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	err = filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "want" {
			return fs.SkipDir
		}
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o750)
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path) // #nosec G304 G122 -- the test copies its own checked-in cases
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, info.Mode().Perm()) // #nosec G703 -- target is under the test's temporary directory
	})
	if err != nil {
		t.Fatal(err)
	}
	return dst
}

// applyParityEnv gives the test the fixed environment the goldens were
// recorded under, which is HOME and PWD at the copy, TZ=UTC, LANG=C.UTF-8,
// and PATH=/usr/bin:/bin, with every other variable cleared, and runs it from the copy.
func applyParityEnv(t *testing.T, dir string) {
	t.Helper()
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if name != "PATH" {
			t.Setenv(name, "")
			_ = os.Unsetenv(name)
		}
	}
	// env.sh's PATH, so the replay sees what the goldens were recorded
	// under. The binary under test is run in process.
	t.Setenv("PATH", "/usr/bin:/bin")
	t.Chdir(dir)
	t.Setenv("HOME", dir)
	t.Setenv("TZ", "UTC")
	t.Setenv("LANG", "C.UTF-8")
	t.Setenv("PWD", dir)
}

// runJQIn runs a case through handleJQ in a fresh copy of its directory
// under the fixed environment. It returns what the run wrote to stdout and
// stderr, with the copy's path written as $CASE_DIR the way the goldens
// record it, the status it ended with, and the copy's path.
func runJQIn(t *testing.T, c parityCase) (stdout, stderr string, st query.Status, dir string) {
	t.Helper()
	dir = copyCase(t, c.Dir)
	applyParityEnv(t, dir)

	var out, errOut bytes.Buffer
	args := append(append([]string{}, c.Args...), c.GraftArgs...)
	opts, err := parseJQArgs(args, personaGraft)
	if err != nil {
		st = jqArgsError(err)
	} else {
		st = handleJQ(opts, strings.NewReader(c.Stdin), &out, &errOut)
	}
	errOut.WriteString(st.Message)
	scrub := func(s string) string { return strings.ReplaceAll(s, dir, caseDirMarker) }
	return scrub(out.String()), scrub(errOut.String()), st, dir
}

// diffFiles lists how the files in dir differ from what the case expects:
// a file missing, changed, with other mode bits, or not expected at all.
func diffFiles(dir string, want map[string]fileExpect) []string {
	var problems []string
	got := map[string]fileExpect{}
	walkErr := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if parityControlFiles[strings.SplitN(rel, string(filepath.Separator), 2)[0]] {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		content, err := os.ReadFile(path) // #nosec G304 G122 -- the test reads its own temporary copy
		if err != nil {
			return err
		}
		got[filepath.ToSlash(rel)] = fileExpect{
			Content: []byte(strings.ReplaceAll(string(content), dir, caseDirMarker)),
			Mode:    info.Mode().Perm(),
		}
		return nil
	})
	if walkErr != nil {
		problems = append(problems, "reading the copy failed: "+walkErr.Error())
	}
	for name, w := range want {
		g, ok := got[name]
		switch {
		case !ok:
			problems = append(problems, name+" is missing")
		case !bytes.Equal(g.Content, w.Content):
			problems = append(problems, name+" has other content: "+strconv.Quote(string(g.Content)))
		case g.Mode != w.Mode:
			problems = append(problems, name+" has mode "+g.Mode.String()+", want "+w.Mode.String())
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			problems = append(problems, name+" is not expected")
		}
	}
	sort.Strings(problems)
	return problems
}

// compareFiles fails the test for every way the files in dir differ from
// what the case expects.
func compareFiles(t *testing.T, dir string, want map[string]fileExpect) {
	t.Helper()
	for _, p := range diffFiles(dir, want) {
		t.Error(p)
	}
}

// stderrMatches applies the stderr rule: byte for byte for a case whose
// stderr is program output, and only whether anything was written for the
// rest, because graft's diagnostics have their own wording.
func stderrMatches(c parityCase, errOut string) bool {
	if c.StderrExact {
		return errOut == c.WantStderr
	}
	return (errOut != "") == c.WantStderrFlag
}

// openParityFile opens a file under the parity root or the tests
// directory, and returns nil with no error when it does not exist.
func openParityFile(t *testing.T, rel string) *os.File {
	t.Helper()
	f, err := os.Open(filepath.Join(parityRoot, rel)) // #nosec G304 -- the test reads its own checked-in files
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	return f
}

// mustReadAllowlist reads a dialect's allowlist, which may be absent.
func mustReadAllowlist(t *testing.T, dialect string) map[string]parity.AllowEntry {
	t.Helper()
	f := openParityFile(t, filepath.Join(dialect, "allowlist.txt"))
	if f == nil {
		return map[string]parity.AllowEntry{}
	}
	entries, err := parity.ReadAllowlist(f)
	if err != nil {
		t.Fatal(err)
	}
	return entries
}

// mustReadFloors reads tests/compat-floors.env, which may be absent.
func mustReadFloors(t *testing.T) parity.Floors {
	t.Helper()
	f := openParityFile(t, filepath.Join("..", "compat-floors.env"))
	if f == nil {
		return parity.Floors{Phase: "J1", Values: map[string]int{}}
	}
	floors, err := parity.ReadFloors(f)
	if err != nil {
		t.Fatal(err)
	}
	return floors
}

// mustReadBaseline reads a dialect's baseline, which may be absent.
func mustReadBaseline(t *testing.T, dialect string) parity.Baseline {
	t.Helper()
	f := openParityFile(t, filepath.Join(dialect, "baseline.txt"))
	if f == nil {
		return parity.Baseline{Passing: map[string]bool{}}
	}
	b, err := parity.ReadBaseline(f)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// allowFor returns the allowlist entry of the inventory entry that names
// the case, or nil. A case no entry names has no allowlist entry.
func allowFor(t *testing.T, dialect, name string) *parity.AllowEntry {
	t.Helper()
	allow := mustReadAllowlist(t, dialect)
	for _, e := range loadInventory(t, dialect) {
		if slices.Contains(e.Cases, name) {
			if a, ok := allow[e.ID]; ok {
				return &a
			}
		}
	}
	return nil
}

// currentParityPhase is the phase the compatibility floors are at.
func currentParityPhase(t *testing.T, dialect string) string {
	t.Helper()
	if dialect != "jq" {
		t.Fatalf("no floors for dialect %s", dialect)
	}
	return mustReadFloors(t).Phase
}

// caseMatches replays a case and reports whether stdout, the exit code,
// the stderr rule, and the files it left all matched the golden. The
// second result describes a mismatch.
func caseMatches(t *testing.T, c parityCase) (bool, string) {
	t.Helper()
	out, errOut, st, dir := runJQIn(t, c)
	problems := diffFiles(dir, c.WantFiles)
	matched := out == c.WantStdout && st.Code == c.WantExit && stderrMatches(c, errOut) && len(problems) == 0
	detail := fmt.Sprintf("%s\nargs: %q\nstdout: %q\nwant:   %q\nexit: %d want %d\nstderr: %q want %q (exact %v, flag %v)\nfiles: %v",
		c.Name, c.Args, out, c.WantStdout, st.Code, c.WantExit, errOut, c.WantStderr, c.StderrExact, c.WantStderrFlag, problems)
	return matched, detail
}

// caseNamed returns the case of that name.
func caseNamed(t *testing.T, dialect, name string) parityCase {
	t.Helper()
	cases := loadParityCases(t, dialect)
	for i := range cases {
		if cases[i].Name == name {
			return cases[i]
		}
	}
	t.Fatalf("no %s case named %s", dialect, name)
	return parityCase{}
}

func TestQueryParity(t *testing.T) {
	phase := currentParityPhase(t, "jq")
	cases := loadParityCases(t, "jq")
	for i := range cases {
		c := cases[i]
		t.Run(c.Name, func(t *testing.T) {
			Convey("the case matches the reference golden", t, func() {
				matched, detail := caseMatches(t, c)
				v := parity.Evaluate(parity.Expectation{Entry: c.Name, Phase: c.Phase, Allow: allowFor(t, "jq", c.Name)}, matched, phase)
				// Every replay goes through the verdict evaluator, so a
				// ruled departure never turns the gate red.
				if v != parity.Pass && v != parity.ExpectedFail {
					t.Logf("%v: %s", v, detail)
				}
				So(v, ShouldBeIn, parity.Pass, parity.ExpectedFail)
				// A departure still pins graft's own output.
				if v == parity.ExpectedFail && c.PinsGraftStdout {
					out, _, _, _ := runJQIn(t, c)
					So(out, ShouldEqual, c.WantGraftStdout)
				}
			})
		})
	}
}

func TestFreshCopyPerRun(t *testing.T) {
	Convey("every run starts from a fresh copy", t, func() {
		c := caseNamed(t, "jq", "identity-file-input")
		_, _, _, dir := runJQIn(t, c)
		So(dir, ShouldNotEqual, c.Dir)
		So(os.WriteFile(filepath.Join(dir, "in.json"), []byte("tampered"), 0o600), ShouldBeNil)
		_, _, _, dir2 := runJQIn(t, c)
		So(dir2, ShouldNotEqual, dir)
		b, err := os.ReadFile(filepath.Join(dir2, "in.json"))
		So(err, ShouldBeNil)
		So(string(b), ShouldNotEqual, "tampered")
		checked, err := os.ReadFile(filepath.Join(c.Dir, "in.json"))
		So(err, ShouldBeNil)
		So(string(checked), ShouldNotEqual, "tampered")
	})

	Convey("a tampered or extra file is caught by the file comparison", t, func() {
		c := caseNamed(t, "jq", "identity-file-input")
		_, _, _, dir := runJQIn(t, c)
		compareFiles(t, dir, c.WantFiles)
		So(os.WriteFile(filepath.Join(dir, "in.json"), []byte("tampered"), 0o600), ShouldBeNil)
		So(os.WriteFile(filepath.Join(dir, "extra.json"), []byte("{}"), 0o600), ShouldBeNil)
		So(diffFiles(dir, c.WantFiles), ShouldHaveLength, 2)
	})
}

func TestHugeLiteralArithmeticPinsEmptyStdout(t *testing.T) {
	c := caseNamed(t, "jq", "n-huge-literal-arith")
	if !c.PinsGraftStdout || c.WantGraftStdout != "" {
		t.Errorf("n-huge-literal-arith should pin an empty graft stdout at the digit bound, got pinned=%v %q", c.PinsGraftStdout, c.WantGraftStdout)
	}
}
