package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
	"github.com/fivetwenty-io/graft/log"
)

// cliIdentityDir holds the corpus and the goldens that
// scripts/record-cli-identity.sh records from the released binary.
const cliIdentityDir = "../../tests/cli-identity"

// helpEntry is one line of a help block: a command in Available
// Commands, or a flag in Flags, split into the part cobra and pflag
// pad and the description that follows it.
type helpEntry struct {
	Block string
	Left  string
	Right string
}

var (
	jqCommandEntry = helpEntry{"Available Commands:", "jq", "Run a jq program over JSON and YAML input"}
	mergeSortKeys  = helpEntry{"Flags:", "      --sort-keys string", "Order of map keys in the output: spruce (default) or lexical (default \"spruce\")"}
	jsonSortKeys   = helpEntry{"Flags:", "      --sort-keys string", "Order of object keys in the JSON output: lexical (default) or spruce (default \"lexical\")"}
)

// knownAdditions are the only lines a help text may gain over the
// recorded goldens. The root help and `graft help` list each command
// added since the goldens were taken, and merge and json list the
// --sort-keys flag whose defaults keep their output unchanged. Each
// block is re-aligned the way cobra and pflag pad it, so a longer name
// moves the description column and nothing else. Every other golden
// must match byte for byte.
var knownAdditions = map[string][]helpEntry{
	"root-help":  {jqCommandEntry},
	"help":       {jqCommandEntry},
	"help-merge": {mergeSortKeys},
	"merge-help": {mergeSortKeys},
	"json-help":  {jsonSortKeys},
}

// cliIdentityCase is one line of tests/cli-identity/cases.
type cliIdentityCase struct {
	Name  string
	Stdin string
	Args  []string
}

func readCLIIdentityCases(t *testing.T, dir string) []cliIdentityCase {
	t.Helper()
	f, err := os.Open(filepath.Join(dir, "cases"))
	if err != nil {
		t.Fatalf("opening the cli identity cases: %s", err)
	}
	defer func() { _ = f.Close() }()

	var cases []cliIdentityCase
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 || strings.HasPrefix(fields[0], "#") {
			continue
		}
		if len(fields) < 2 {
			t.Fatalf("malformed cli identity case %q", scanner.Text())
		}
		cases = append(cases, cliIdentityCase{Name: fields[0], Stdin: fields[1], Args: fields[2:]})
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("reading the cli identity cases: %s", err)
	}
	return cases
}

// withKnownAdditions inserts entries into the help blocks of a
// recorded help text and re-renders each block it touched.
func withKnownAdditions(golden string, entries []helpEntry) string {
	lines := strings.SplitAfter(golden, "\n")
	for _, block := range []string{"Available Commands:", "Flags:"} {
		var added []helpEntry
		for _, e := range entries {
			if e.Block == block {
				added = append(added, e)
			}
		}
		if len(added) == 0 {
			continue
		}
		start := slices.Index(lines, block+"\n")
		if start < 0 {
			continue
		}
		end := start + 1
		for end < len(lines) && lines[end] != "\n" {
			end++
		}
		rows := added
		for _, line := range lines[start+1 : end] {
			rows = append(rows, parseHelpLine(block, strings.TrimSuffix(line, "\n")))
		}
		lines = slices.Concat(lines[:start+1], renderHelpBlock(block, rows), lines[end:])
	}
	return strings.Join(lines, "")
}

// parseHelpLine splits a recorded help line at the padding before its
// description.
func parseHelpLine(block, line string) helpEntry {
	if block == "Available Commands:" {
		name, rest, _ := strings.Cut(strings.TrimPrefix(line, "  "), " ")
		return helpEntry{block, name, strings.TrimLeft(rest, " ")}
	}
	indent := len(line) - len(strings.TrimLeft(line, " "))
	gap := strings.Index(line[indent:], "  ")
	if gap < 0 {
		return helpEntry{block, line, ""}
	}
	return helpEntry{block, line[:indent+gap], strings.TrimLeft(line[indent+gap:], " ")}
}

// renderHelpBlock sorts and pads a block's rows the way cobra pads a
// command list (names to at least 11 columns and one space) and pflag
// pads a flag list (three spaces past the longest flag).
func renderHelpBlock(block string, rows []helpEntry) []string {
	sortKey := func(e helpEntry) string {
		if block == "Available Commands:" {
			return e.Left
		}
		_, long, _ := strings.Cut(e.Left, "--")
		name, _, _ := strings.Cut(long, " ")
		return name
	}
	slices.SortFunc(rows, func(a, b helpEntry) int { return strings.Compare(sortKey(a), sortKey(b)) })
	width := 0
	for _, e := range rows {
		width = max(width, len(e.Left))
	}
	out := make([]string, 0, len(rows))
	for _, e := range rows {
		if block == "Available Commands:" {
			pad := max(width, 11)
			out = append(out, "  "+e.Left+strings.Repeat(" ", pad-len(e.Left)+1)+e.Right+"\n")
			continue
		}
		out = append(out, e.Left+strings.Repeat(" ", width-len(e.Left)+3)+e.Right+"\n")
	}
	return out
}

// cliExit is what the replay's exit seam panics with, so an exit stops
// the command the way os.Exit stops the real binary.
type cliExit int

// replayCLIIdentity runs main() in process the way the record script
// runs the binary, with stdin, stdout, and stderr on files, and returns
// what each stream received and the exit code.
func replayCLIIdentity(t *testing.T, dir string, c cliIdentityCase) (stdout, stderr string, rc int) {
	t.Helper()
	fixtures := filepath.Join(dir, "fixtures")
	input := os.DevNull
	if c.Stdin != "-" {
		input = filepath.Join(dir, c.Stdin)
	}

	// The record script runs the binary under env -i with PATH and the
	// four variables below, so the replay clears everything else.
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if name != "PATH" {
			t.Setenv(name, "")
			_ = os.Unsetenv(name)
		}
	}
	t.Setenv("TERM", "dumb")
	t.Setenv("NO_COLOR", "1")
	t.Setenv("HOME", fixtures)
	t.Setenv("GRAFT_CACHE_L2_PATH", "cache")
	t.Chdir(fixtures)
	_ = os.Unsetenv("PWD") // t.Chdir sets it, and env -i passes none

	scratch := t.TempDir()
	in, err := os.Open(input)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = in.Close() }()
	outFile, err := os.Create(filepath.Join(scratch, "out"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = outFile.Close() }()
	errFile, err := os.Create(filepath.Join(scratch, "err"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = errFile.Close() }()

	prevStdin, prevStdout, prevStderr := os.Stdin, os.Stdout, os.Stderr
	prevPrintStdOutf, prevPrintStdErrf := printStdOutf, log.PrintStdErrf
	prevExit, prevArgs := exit, os.Args
	prevDebug, prevTrace := log.DebugOn, log.TraceOn
	defer func() {
		os.Stdin, os.Stdout, os.Stderr = prevStdin, prevStdout, prevStderr
		printStdOutf, log.PrintStdErrf = prevPrintStdOutf, prevPrintStdErrf
		exit, os.Args = prevExit, prevArgs
		log.DebugOn, log.TraceOn = prevDebug, prevTrace
		ansi.Color(false)
	}()

	os.Stdin, os.Stdout, os.Stderr = in, outFile, errFile
	printStdOutf = func(format string, args ...interface{}) { _, _ = fmt.Fprintf(os.Stdout, format, args...) }
	log.PrintStdErrf = func(format string, args ...interface{}) { _, _ = fmt.Fprintf(os.Stderr, format, args...) }
	exit = func(code int) { panic(cliExit(code)) }
	os.Args = append([]string{"graft"}, c.Args...)

	rc = func() (code int) {
		defer func() {
			if r := recover(); r != nil {
				exitCode, ok := r.(cliExit)
				if !ok {
					panic(r)
				}
				code = int(exitCode)
			}
		}()
		main()
		return 0
	}()

	os.Stdin, os.Stdout, os.Stderr = prevStdin, prevStdout, prevStderr
	gotOut, err := os.ReadFile(outFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	gotErr, err := os.ReadFile(errFile.Name())
	if err != nil {
		t.Fatal(err)
	}
	return string(gotOut), string(gotErr), rc
}

func readGolden(t *testing.T, dir, name, ext string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "golden", name+ext))
	if err != nil {
		t.Fatalf("reading golden %s%s: %s", name, ext, err)
	}
	return string(data)
}

func TestCLIIdentity(t *testing.T) {
	dir, err := filepath.Abs(cliIdentityDir)
	if err != nil {
		t.Fatal(err)
	}
	cases := readCLIIdentityCases(t, dir)
	if len(cases) == 0 {
		t.Fatal("the cli identity corpus is empty")
	}
	for _, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			wantOut := withKnownAdditions(readGolden(t, dir, c.Name, ".out"), knownAdditions[c.Name])
			wantErr := readGolden(t, dir, c.Name, ".err")
			wantRC, err := strconv.Atoi(strings.TrimSpace(readGolden(t, dir, c.Name, ".rc")))
			if err != nil {
				t.Fatalf("golden %s.rc: %s", c.Name, err)
			}
			gotOut, gotErr, gotRC := replayCLIIdentity(t, dir, c)

			Convey("graft "+strings.Join(c.Args, " ")+" matches its recorded golden", t, func() {
				So(gotOut, ShouldEqual, wantOut)
				So(gotErr, ShouldEqual, wantErr)
				So(gotRC, ShouldEqual, wantRC)
			})
		})
	}
}

func TestCLIIdentityReplayEnvironment(t *testing.T) {
	dir, err := filepath.Abs(cliIdentityDir)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("JQ_COLORS", "bogus")
	t.Setenv("DEFAULT_ARRAY_MERGE_KEY", "id")
	out, errOut, rc := replayCLIIdentity(t, dir, cliIdentityCase{Name: "env", Stdin: "-", Args: []string{"jq", "-n", "-c", "env | keys"}})

	Convey("the replay runs under the environment the record script gives env -i", t, func() {
		So(out, ShouldEqual, "[\"GRAFT_CACHE_L2_PATH\",\"HOME\",\"NO_COLOR\",\"PATH\",\"TERM\"]\n")
		So(errOut, ShouldEqual, "")
		So(rc, ShouldEqual, 0)
	})
}
