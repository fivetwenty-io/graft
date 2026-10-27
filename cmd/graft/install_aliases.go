package main

import (
	"bytes"
	"crypto/sha256"
	"debug/buildinfo"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"time"

	"github.com/spf13/cobra"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
)

// aliasTargetState is what already sits at the path an alias would
// occupy.
type aliasTargetState uint8

// The states a target can be in. Only a link to this graft and a copy of
// it are ever removed or left alone as installed. The foreign, other-graft
// states are replaced only with --force, and the running binary and a
// directory are never touched.
const (
	aliasAbsent aliasTargetState = iota
	aliasLinkToThisGraft
	aliasCopyOfThisGraft
	aliasCopyOfOtherGraft
	aliasLinkToOtherGraft
	aliasForeign
	aliasRunningBinary
	aliasDirectory
)

// spruceName is the name graft answers to as spruce.
const spruceName = "spruce"

// graftModulePath is the main package path a graft build records in its
// build info.
const graftModulePath = "github.com/fivetwenty-io/graft/cmd/graft"

// aliasOutcome is what install-aliases did, or declined to do, for one
// name.
type aliasOutcome struct {
	Name, Path, Action, Reason string
}

// installAliasesOpts are the options of one install-aliases run. Names
// empty means the default set, whose unreplaceable targets are skipped,
// where a name the caller typed is refused. copyMode installs copies in
// place of links. It is set on Windows, and by tests on every OS.
type installAliasesOpts struct {
	Dir                   string
	Force, Remove, DryRun bool
	Names                 []string
	copyMode              bool
}

// The actions an outcome can record.
const (
	actionLinked      = "linked"
	actionWouldLink   = "would link"
	actionAlready     = "already installed"
	actionRemoved     = "removed"
	actionWouldRemove = "would remove"
	actionSkipped     = "skipped"
	actionRefused     = "refused"
	actionFailed      = "failed"
)

// graftExecutablePath names the running graft binary as it was found,
// without resolving symlinks, so links made from it and the default
// directory follow the path the user runs graft by, such as the Homebrew
// bin directory, and not the versioned directory behind it. Targets are
// compared by file identity, not by this path. It is a variable so tests
// can point the installer at a stand-in.
var graftExecutablePath = func() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, err = filepath.Abs(exe)
	if err != nil {
		return "", err
	}
	return invokedGraftPath(os.Args[0], exe, exec.LookPath), nil
}

// invokedGraftPath is the path graft was run by, found from arg0 the way
// the shell found it, when that path leads to the running binary exe.
// Linux reports only the resolved path as the executable, which for a
// package manager's install is a versioned directory off PATH, so we
// prefer the name the user typed. Anything that does not lead to exe
// falls back to exe.
func invokedGraftPath(arg0, exe string, lookPath func(string) (string, error)) string {
	path := arg0
	if filepath.Base(arg0) == arg0 {
		found, err := lookPath(arg0)
		if err != nil {
			return exe
		}
		path = found
	}
	path, err := filepath.Abs(path)
	if err != nil {
		return exe
	}
	pi, err := os.Stat(path) // #nosec G703 -- only compared with the running binary, never opened
	if err != nil {
		return exe
	}
	ei, err := os.Stat(exe)
	if err != nil || !os.SameFile(pi, ei) {
		return exe
	}
	return path
}

// defaultAliasNames are the names install-aliases installs when none are
// given.
func defaultAliasNames() []string {
	return []string{"jq", "yq", spruceName}
}

// knownAliasName reports whether name is one graft answers to.
func knownAliasName(name string) bool {
	return slices.Contains([]string{"jq", spruceName, "yq"}, name)
}

// aliasPath is where an alias lives in dir, which is NAME.exe for a copy.
func aliasPath(dir, name string, copyMode bool) string {
	if copyMode {
		return filepath.Join(dir, name+".exe")
	}
	return filepath.Join(dir, name)
}

// aliasChecker classifies alias targets against the running graft.
type aliasChecker struct {
	info     os.FileInfo
	copyMode bool
	graft    string
	sum      []byte
}

func newAliasChecker(graft string, copyMode bool) (*aliasChecker, error) {
	info, err := os.Stat(graft)
	if err != nil {
		return nil, err
	}
	return &aliasChecker{info: info, copyMode: copyMode, graft: graft}, nil
}

// inspectAliasTarget classifies what sits at path when aliases are links.
func inspectAliasTarget(path, graftPath string) (aliasTargetState, error) {
	return targetState(path, graftPath, false)
}

// targetState classifies what sits at path.
func targetState(path, graft string, copyMode bool) (aliasTargetState, error) {
	c, err := newAliasChecker(graft, copyMode)
	if err != nil {
		return aliasForeign, err
	}
	return c.state(path)
}

// state classifies what sits at path. A link counts as this graft's when
// it leads to the same file as the running binary, and a regular file in
// copy mode when its SHA-256 equals the binary's. Another graft is a Go
// executable whose build info names graft's main package. Nothing is
// ever run, because what a program says about itself is no proof of what
// it is.
func (c *aliasChecker) state(path string) (aliasTargetState, error) {
	li, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return aliasAbsent, nil
	}
	if err != nil {
		return aliasForeign, err
	}
	if li.IsDir() {
		return aliasDirectory, nil
	}
	if li.Mode()&os.ModeSymlink != 0 {
		return c.linkState(path), nil
	}
	if os.SameFile(li, c.info) {
		return aliasRunningBinary, nil
	}
	if !c.copyMode || !li.Mode().IsRegular() {
		return aliasForeign, nil
	}
	if li.Size() == c.info.Size() {
		same, err := c.sameContent(path)
		if err != nil {
			return aliasForeign, err
		}
		if same {
			return aliasCopyOfThisGraft, nil
		}
	}
	if isGraftBuild(path) {
		return aliasCopyOfOtherGraft, nil
	}
	return aliasForeign, nil
}

// linkState classifies a symbolic link by where it leads.
func (c *aliasChecker) linkState(path string) aliasTargetState {
	ti, err := os.Stat(path)
	if err != nil {
		// A link that leads nowhere, or loops, is not ours.
		return aliasForeign
	}
	if os.SameFile(ti, c.info) {
		return aliasLinkToThisGraft
	}
	if resolved, err := filepath.EvalSymlinks(path); err == nil && isGraftBuild(resolved) {
		return aliasLinkToOtherGraft
	}
	return aliasForeign
}

// sameContent reports whether the file at path has graft's SHA-256. The
// running binary is hashed once per run.
func (c *aliasChecker) sameContent(path string) (bool, error) {
	if c.sum == nil {
		sum, err := fileSum(c.graft)
		if err != nil {
			return false, err
		}
		c.sum = sum
	}
	sum, err := fileSum(path)
	if err != nil {
		return false, err
	}
	return bytes.Equal(sum, c.sum), nil
}

// fileSum streams a file through SHA-256.
func fileSum(path string) ([]byte, error) {
	f, err := os.Open(path) // #nosec G304 -- an alias location or the running binary
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return nil, err
	}
	return h.Sum(nil), nil
}

// isGraftBuild reports whether path is a Go executable built from graft's
// main package. It reads the build info and never runs the file, and a
// file whose build info does not parse is not graft.
func isGraftBuild(path string) bool {
	bi, err := buildinfo.ReadFile(path)
	return err == nil && bi.Path == graftModulePath
}

// copyFile copies src to dst as an executable file. It never overwrites,
// and it removes what it wrote if the copy fails.
func copyFile(src, dst string) error {
	in, err := os.Open(src) // #nosec G304 -- the running binary
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755) // #nosec G302 G304 -- an alias must be executable
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return err
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(dst)
		return err
	}
	return nil
}

// clause says why a target is left alone.
func clause(state aliasTargetState, remove bool) string {
	var base string
	switch state {
	case aliasCopyOfOtherGraft:
		base = "is a copy of another graft"
	case aliasLinkToOtherGraft:
		base = "is a link to another graft"
	case aliasRunningBinary:
		return "is the running graft binary"
	case aliasDirectory:
		return "is a directory"
	default:
		base = "exists and is not a link to graft"
	}
	if remove {
		return base + " (not removing it)"
	}
	return base + " (use --force to replace it)"
}

// replaceable reports whether --force may replace a target in this state.
func replaceable(state aliasTargetState) bool {
	return state == aliasCopyOfOtherGraft || state == aliasLinkToOtherGraft || state == aliasForeign
}

// handleInstallAliases installs or removes the aliases opts names and
// returns the exit code. A target the caller named that is not graft's
// gives 1, after the other names are handled, where one among the
// defaults is skipped.
func handleInstallAliases(opts installAliasesOpts, stdout, stderr io.Writer) int {
	graft, err := graftExecutablePath()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, ansi.Sprintf("@R{Cannot locate graft}: %s", err.Error()))
		return 1
	}
	checker, err := newAliasChecker(graft, opts.copyMode)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, ansi.Sprintf("@R{Cannot locate graft}: %s", reasonOf(err)))
		return 1
	}
	dir := opts.Dir
	if dir == "" {
		dir = filepath.Dir(graft)
	}
	explicit := len(opts.Names) > 0
	names := opts.Names
	if !explicit {
		names = defaultAliasNames()
	}
	for _, name := range names {
		if !knownAliasName(name) {
			_, _ = fmt.Fprintln(stderr, ansi.Sprintf("@R{Unknown alias}: %s (expected jq, spruce, or yq)", name))
			return 2
		}
	}
	if opts.copyMode && !opts.Remove {
		_, _ = fmt.Fprintln(stdout, "Installing copies of graft named NAME.exe in place of links.")
	}

	code := 0
	var outcomes []aliasOutcome
	for _, name := range names {
		o := installOne(opts, checker, aliasPath(dir, name, opts.copyMode), explicit, stderr)
		o.Name = name
		if o.Action == actionRefused || o.Action == actionFailed {
			code = 1
		}
		outcomes = append(outcomes, o)
	}
	printAliasOutcomes(stdout, outcomes, graft, opts)
	return code
}

// reasonOf is the cause inside a path or link error, which repeats the
// paths the caller already knows.
func reasonOf(err error) string {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return le.Err.Error()
	}
	return err.Error()
}

// installOne installs or removes the alias at path. It returns what it
// did, and says why on stderr when it refused or could not.
func installOne(opts installAliasesOpts, checker *aliasChecker, path string, explicit bool, stderr io.Writer) aliasOutcome {
	o := aliasOutcome{Path: path}
	fail := func(phrase string, err error) aliasOutcome {
		_, _ = fmt.Fprintln(stderr, ansi.Sprintf("@R{%s} @m{%s}: %s", phrase, path, reasonOf(err)))
		if errors.Is(err, fs.ErrPermission) {
			_, _ = fmt.Fprintln(stderr, "Rerun with sudo, or give --dir a directory you can write to.")
		}
		return aliasOutcome{Path: path, Action: actionFailed}
	}
	decline := func(state aliasTargetState) aliasOutcome {
		o.Reason = path + " " + clause(state, opts.Remove)
		o.Action = actionSkipped
		if explicit {
			o.Action = actionRefused
			phrase := "Refusing to replace"
			if opts.Remove {
				phrase = "Not removing"
			}
			_, _ = fmt.Fprintln(stderr, ansi.Sprintf("@R{%s} @m{%s}: %s", phrase, path, clause(state, opts.Remove)))
		}
		return o
	}
	state, err := checker.state(path)
	if err != nil {
		return fail("Unable to read", err)
	}
	if opts.Remove {
		return removeOne(opts, path, state, o, decline, fail)
	}
	switch {
	case state == aliasLinkToThisGraft || state == aliasCopyOfThisGraft:
		o.Action = actionAlready
		return o
	case state != aliasAbsent && (!replaceable(state) || !opts.Force):
		return decline(state)
	case opts.DryRun:
		o.Action = actionWouldLink
		return o
	}
	if err := installAlias(opts, checker.graft, path); err != nil {
		return fail("Unable to write", err)
	}
	o.Action = actionLinked
	return o
}

// removeOne removes the alias at path when it is a link to this graft or a
// copy of it, and declines everything else whatever --force says.
func removeOne(opts installAliasesOpts, path string, state aliasTargetState, o aliasOutcome,
	decline func(aliasTargetState) aliasOutcome, fail func(string, error) aliasOutcome) aliasOutcome {
	switch {
	case state == aliasAbsent:
		o.Action, o.Reason = actionSkipped, path+" does not exist"
	case state != aliasLinkToThisGraft && state != aliasCopyOfThisGraft:
		return decline(state)
	case opts.DryRun:
		o.Action = actionWouldRemove
	default:
		if err := os.Remove(path); err != nil {
			return fail("Unable to write", err)
		}
		o.Action = actionRemoved
	}
	return o
}

// installAlias puts the alias at path. It builds the link or copy under a
// temporary name beside path and renames it into place, so a failure
// leaves whatever was there untouched, and rename never follows the
// destination.
func installAlias(opts installAliasesOpts, graft, path string) error {
	tmp := filepath.Join(filepath.Dir(path), "."+filepath.Base(path)+".graft-"+strconv.FormatInt(time.Now().UnixNano(), 36))
	create := linkOrCopy
	if opts.copyMode {
		create = copyFile
	}
	if err := create(graft, tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// printAliasOutcomes writes one line per name that was acted on or
// skipped, then the summary. A refusal was already written to stderr.
func printAliasOutcomes(w io.Writer, outcomes []aliasOutcome, graft string, opts installAliasesOpts) {
	counts := map[string]int{}
	for _, o := range outcomes {
		counts[o.Action]++
		switch o.Action {
		case actionLinked:
			_, _ = fmt.Fprintf(w, "linked %s -> %s\n", o.Name, graft)
		case actionWouldLink:
			_, _ = fmt.Fprintf(w, "would link %s -> %s at %s\n", o.Name, graft, o.Path)
		case actionAlready, actionRemoved, actionWouldRemove:
			_, _ = fmt.Fprintf(w, "%s %s\n", o.Action, o.Name)
		case actionSkipped:
			_, _ = fmt.Fprintf(w, "skipped %s: %s\n", o.Name, o.Reason)
		}
	}
	skipped := counts[actionSkipped] + counts[actionRefused]
	var line string
	switch {
	case opts.Remove && opts.DryRun:
		line = fmt.Sprintf("would remove %d, skipped %d", counts[actionWouldRemove], skipped)
	case opts.Remove:
		line = fmt.Sprintf("removed %d, skipped %d", counts[actionRemoved], skipped)
	case opts.DryRun:
		line = fmt.Sprintf("would link %d, already installed %d, skipped %d", counts[actionWouldLink], counts[actionAlready], skipped)
	default:
		line = fmt.Sprintf("linked %d, already installed %d, skipped %d", counts[actionLinked], counts[actionAlready], skipped)
	}
	if n := counts[actionFailed]; n > 0 {
		line += fmt.Sprintf(", failed %d", n)
	}
	_, _ = fmt.Fprintln(w, line)
}

// newInstallAliasesCmd builds the install-aliases command.
func newInstallAliasesCmd() *cobra.Command {
	var opts installAliasesOpts
	cmd := &cobra.Command{
		Use:   "install-aliases [flags] [NAME...]",
		Short: "Install jq and spruce names that run graft",
		Long: "Install links named jq, yq, and spruce, or the NAMEs given, that run graft under\n" +
			"those names. They go in the directory graft was run from unless --dir says\n" +
			"otherwise, and they point at the path graft was run by, as the shell found it\n" +
			"on PATH, and not the versioned directory behind a link. A target that\n" +
			"is not graft's is skipped among the defaults and refused when named, and\n" +
			"--force replaces it, except for a directory or the running binary, which are\n" +
			"never touched. --remove takes away only links to this graft and copies of it,\n" +
			"and --dry-run shows what would happen. On Windows graft installs copies named\n" +
			"NAME.exe in place of links.",
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.Names = args
			opts.copyMode = runtime.GOOS == "windows"
			if code := handleInstallAliases(opts, cmd.OutOrStdout(), cmd.ErrOrStderr()); code != 0 {
				exit(code)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.Dir, "dir", "", "Directory to install into (default: the directory graft was run from)")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "Replace a target that is not graft's")
	cmd.Flags().BoolVar(&opts.Remove, "remove", false, "Remove the aliases instead of installing them")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Show what would happen without changing anything")
	return cmd
}
