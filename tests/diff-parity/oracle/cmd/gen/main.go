// Command gen renders every diff parity case through dyff v1.12.0 and
// ytbx v1.5.0 exactly as spruce v1.35.16 does, in eight color and width
// modes, and writes the goldens graft's tests compare against.
//
// Usage, from tests/diff-parity/oracle:
//
//	go run ./cmd/gen ../cases                 # write every golden
//	go run ./cmd/gen -check ../cases          # fail if any golden is stale
//	go run ./cmd/gen -check -spruce /path/to/spruce ../cases
//
// A case directory holds from.<ext> and to.<ext>. A case that also holds a
// GRAFT_ONLY file is rendered from the quoted twin in its twin/ directory,
// because spruce reads unquoted {{...}} placeholders as maps and graft
// reads them as strings. The -spruce flag runs the published spruce
// release binary on every other case, once with its output piped against
// plain-80 and once under a pty for each other mode, and checks stdout,
// stderr, and the exit code against the goldens.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/creack/pty"
	"github.com/gonvenience/bunt"
	"github.com/gonvenience/term"
	"github.com/gonvenience/ytbx"
	"github.com/homeport/dyff/pkg/dyff"
)

type mode struct {
	name      string
	color     bool
	truecolor bool
	width     int
}

var modes = []mode{
	{"plain-80", false, false, 80},
	{"plain-0", false, false, 0},
	{"plain-200", false, false, 200},
	{"truecolor-80", true, true, 80},
	{"truecolor-0", true, true, 0},
	{"truecolor-200", true, true, 200},
	{"ansi16-80", true, false, 80},
	{"ansi16-0", true, false, 0},
}

// spruceVersion is the release the -spruce cross-check runs. Its go.mod
// pins the same dyff, ytbx, yaml.v3, bunt, and neat versions as this
// module.
const spruceVersion = "v1.35.17"

type result struct {
	Stdout []byte `json:"stdout"`
	Stderr []byte `json:"stderr"`
	Exit   int    `json:"exit"`
}

func main() {
	time.Local = time.FixedZone("UTC-4", -4*60*60)
	if os.Getenv("GEN_CHILD") == "1" {
		child()
		return
	}
	check := flag.Bool("check", false, "compare instead of writing")
	spruce := flag.String("spruce", "", "spruce "+spruceVersion+" release binary to cross-check every mode against")
	flag.Parse()
	if flag.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: gen [-check] [-spruce BIN] CASES_DIR")
		os.Exit(2)
	}
	casesDir, err := filepath.Abs(flag.Arg(0))
	if err != nil {
		fail(err)
	}
	entries, err := os.ReadDir(casesDir)
	if err != nil {
		fail(err)
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	if *spruce != "" {
		checkSpruceVersion(*spruce)
	}

	problems := 0
	for _, name := range names {
		dir := filepath.Join(casesDir, name)
		src := dir
		graftOnly := exists(filepath.Join(dir, "GRAFT_ONLY"))
		if graftOnly {
			src = filepath.Join(dir, "twin")
		}
		files := map[string][]byte{}
		exit := -1
		for _, m := range modes {
			r, err := renderMode(src, m)
			if err != nil {
				fail(fmt.Errorf("%s %s: %w", name, m.name, err))
			}
			if exit >= 0 && r.Exit != exit {
				fail(fmt.Errorf("%s: exit code differs between modes", name))
			}
			exit = r.Exit
			files[m.name] = r.Stdout
			files[m.name+".stderr"] = r.Stderr
		}
		files["exit"] = []byte(fmt.Sprintf("%d\n", exit))
		problems += writeOrCheck(filepath.Join(dir, "want"), files, *check)
		if *spruce != "" && !graftOnly {
			problems += crossCheck(*spruce, dir, files)
		}
	}
	if problems > 0 {
		fmt.Fprintf(os.Stderr, "%d golden problems; run make diff-goldens after reviewing them\n", problems)
		os.Exit(1)
	}
	fmt.Printf("%d cases %s\n", len(names), map[bool]string{true: "checked", false: "written"}[*check])
}

func writeOrCheck(dir string, files map[string][]byte, check bool) int {
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	problems := 0
	for _, k := range keys {
		path := filepath.Join(dir, k)
		if check {
			have, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(have, files[k]) {
				fmt.Fprintf(os.Stderr, "stale: %s\n", path)
				problems++
			}
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fail(err)
		}
		if err := os.WriteFile(path, files[k], 0o644); err != nil {
			fail(err)
		}
	}
	return problems
}

func checkSpruceVersion(spruce string) {
	out, err := exec.Command(spruce, "--version").CombinedOutput()
	if err != nil {
		fail(fmt.Errorf("%s --version: %w", spruce, err))
	}
	if !strings.Contains(string(out), "Version "+spruceVersion+"\n") {
		fail(fmt.Errorf("%s is not spruce %s: %q", spruce, spruceVersion, out))
	}
}

// crossCheck runs spruce on one case in every mode. plain-80 runs with
// stdout and stderr piped, as Genesis's own calls do. The other modes run
// under a pty of the mode's width, with TERM=dumb for the plain modes and
// COLORTERM=truecolor for the truecolor ones, and the pty's single stream
// is checked against the golden stdout followed by the golden stderr.
func crossCheck(spruce, dir string, files map[string][]byte) int {
	from, to := single(dir, "from"), single(dir, "to")
	problems := 0
	for _, m := range modes {
		var got, want []byte
		var code int
		if m.name == "plain-80" {
			var stdout, stderr bytes.Buffer
			cmd := exec.Command(spruce, "diff", from, to)
			cmd.Dir = dir
			cmd.Env = spruceEnv(m)
			cmd.Stdout, cmd.Stderr = &stdout, &stderr
			code = exitCode(cmd.Run())
			got = append(stdout.Bytes(), 0)
			got = append(got, stderr.Bytes()...)
			want = append(append([]byte{}, files[m.name]...), 0)
			want = append(want, files[m.name+".stderr"]...)
		} else {
			got, code = runPTY(spruce, dir, m, from, to)
			want = append(append([]byte{}, files[m.name]...), files[m.name+".stderr"]...)
		}
		if !bytes.Equal(got, want) || fmt.Sprintf("%d\n", code) != string(files["exit"]) {
			fmt.Fprintf(os.Stderr, "spruce disagrees with the oracle on %s in %s (exit %d)\n", dir, m.name, code)
			problems++
		}
	}
	return problems
}

func spruceEnv(m mode) []string {
	var env []string
	for _, kv := range os.Environ() {
		switch strings.SplitN(kv, "=", 2)[0] {
		case "TERM", "COLORTERM", "NO_COLOR", "TZ":
			continue
		}
		env = append(env, kv)
	}
	term := "dumb"
	if m.color {
		term = "xterm-256color"
	}
	env = append(env, "TZ=America/New_York", "TERM="+term)
	if m.truecolor {
		env = append(env, "COLORTERM=truecolor")
	}
	return env
}

// runPTY runs spruce under a pty sized to the mode's width, 0x0 for width
// 0 as under Genesis's fake_tty, and returns what it wrote.
func runPTY(spruce, dir string, m mode, from, to string) ([]byte, int) {
	cmd := exec.Command(spruce, "diff", from, to)
	cmd.Dir = dir
	cmd.Env = spruceEnv(m)
	size := &pty.Winsize{Cols: uint16(m.width), Rows: 25}
	if m.width == 0 {
		size.Rows = 0
	}
	ptmx, err := pty.StartWithSize(cmd, size)
	if err != nil {
		fail(err)
	}
	var out bytes.Buffer
	_, _ = io.Copy(&out, ptmx)
	code := exitCode(cmd.Wait())
	_ = ptmx.Close()
	return undoONLCR(out.Bytes()), code
}

// undoONLCR turns a pty's output back into the bytes the program wrote.
// The terminal driver writes each "\n" as "\r\n". macOS also repeats the
// "\r" when that pair straddles the end of a 1,024-byte block of output,
// so a "\r" that ends a block and precedes "\r\n" is dropped first. A "\r"
// the program wrote itself survives, because only the "\r" directly
// before each "\n" is removed.
func undoONLCR(b []byte) []byte {
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		if b[i] == '\r' && (i+1)%1024 == 0 && bytes.HasPrefix(b[i+1:], []byte("\r\n")) {
			continue
		}
		if b[i] == '\r' && i+1 < len(b) && b[i+1] == '\n' {
			continue
		}
		out = append(out, b[i])
	}
	return out
}

func exitCode(err error) int {
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode()
	}
	if err != nil {
		fail(err)
	}
	return 0
}

// renderMode renders a case in one mode. Positive widths run in process
// with gonvenience/term's fixed width. Width 0 cannot be expressed that
// way, so it re-runs this program under a pty sized 0x0, which is what
// Genesis's fake_tty produces, and reads the result from a file so the
// pty's newline translation never touches the bytes.
func renderMode(dir string, m mode) (result, error) {
	if m.width > 0 {
		setColor(m.color, m.truecolor)
		term.FixedTerminalWidth, term.FixedTerminalHeight = m.width, 25
		defer func() { term.FixedTerminalWidth, term.FixedTerminalHeight = -1, -1 }()
		return render(dir), nil
	}

	tmp, err := os.CreateTemp("", "gen-*.json")
	if err != nil {
		return result{}, err
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	_ = tmp.Close()

	cmd := exec.Command(os.Args[0])
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GEN_CHILD=1", "GEN_OUT="+tmp.Name(),
		fmt.Sprintf("GEN_COLOR=%t", m.color), fmt.Sprintf("GEN_TRUECOLOR=%t", m.truecolor))
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 0, Cols: 0})
	if err != nil {
		return result{}, err
	}
	_, _ = io.Copy(io.Discard, ptmx)
	if err := cmd.Wait(); err != nil {
		return result{}, fmt.Errorf("child: %w", err)
	}
	_ = ptmx.Close()

	data, err := os.ReadFile(tmp.Name())
	if err != nil {
		return result{}, err
	}
	var r result
	return r, json.Unmarshal(data, &r)
}

func child() {
	setColor(os.Getenv("GEN_COLOR") == "true", os.Getenv("GEN_TRUECOLOR") == "true")
	r := render(".")
	data, err := json.Marshal(r)
	if err != nil {
		fail(err)
	}
	if err := os.WriteFile(os.Getenv("GEN_OUT"), data, 0o600); err != nil {
		fail(err)
	}
}

func setColor(color, truecolor bool) {
	switch {
	case color && truecolor:
		bunt.SetColorSettings(bunt.ON, bunt.ON)
	case color:
		bunt.SetColorSettings(bunt.ON, bunt.OFF)
	default:
		bunt.SetColorSettings(bunt.OFF, bunt.OFF)
	}
}

// render reproduces spruce v1.35.16's `spruce diff` (cmd/spruce/main.go
// diffFiles and its caller): a load or compare error prints "%s\n" on
// stderr with exit 2, a render error is ignored, and the report prints
// as "%s\n" with exit 1 when there are differences.
func render(dir string) result {
	from, to := single(dir, "from"), single(dir, "to")
	cwd, err := os.Getwd()
	if err != nil {
		fail(err)
	}
	if err := os.Chdir(dir); err != nil {
		fail(err)
	}
	defer func() { _ = os.Chdir(cwd) }()

	f, t, err := ytbx.LoadFiles(from, to)
	if err != nil {
		return result{Stderr: []byte(err.Error() + "\n"), Exit: 2}
	}
	report, err := dyff.CompareInputFiles(f, t)
	if err != nil {
		return result{Stderr: []byte(err.Error() + "\n"), Exit: 2}
	}
	var buf bytes.Buffer
	w := bufio.NewWriter(&buf)
	_ = (&dyff.HumanReport{Report: report, OmitHeader: true}).WriteReport(w)
	_ = w.Flush()
	buf.WriteByte('\n')
	if len(report.Diffs) > 0 {
		return result{Stdout: buf.Bytes(), Exit: 1}
	}
	return result{Stdout: buf.Bytes(), Exit: 0}
}

func single(dir, prefix string) string {
	matches, err := filepath.Glob(filepath.Join(dir, prefix+".*"))
	if err != nil || len(matches) != 1 {
		fail(fmt.Errorf("%s: want exactly one %s.* file, found %v", dir, prefix, matches))
	}
	return filepath.Base(matches[0])
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gen:", err)
	os.Exit(1)
}
