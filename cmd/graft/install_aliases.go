package main

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"slices"

	"github.com/spf13/cobra"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
)

// aliasTargetState is what already sits at the path an alias would
// occupy.
type aliasTargetState uint8

// The states a target can be in.
const (
	aliasAbsent aliasTargetState = iota
	aliasLinkToThisGraft
	aliasCopyOfThisGraft
	aliasCopyOfOtherGraft
	aliasForeign
)

// spruceName is the name graft answers to as spruce.
const spruceName = "spruce"

// aliasOutcome is what install-aliases did, or declined to do, for one
// name.
type aliasOutcome struct {
	Name, Path, Action, Reason string
}

// installAliasesOpts are the options of one install-aliases run. Names
// empty means the default set, whose foreign targets are skipped, where a
// name the caller typed is refused. copyMode installs copies in place of
// links. It is set on Windows, and by tests on every OS.
type installAliasesOpts struct {
	Dir                   string
	Force, Remove, DryRun bool
	Names                 []string
	copyMode              bool
}

// aliasCopyMarker is what a file in copy mode must contain for it to count
// as another graft build, since every graft binary carries its own name in
// its module path and usage text.
var aliasCopyMarker = []byte("graft")

// graftExecutablePath names the running graft binary with its symlinks
// resolved. It is a variable so tests can point the installer at a
// stand-in.
var graftExecutablePath = func() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

// defaultAliasNames are the names install-aliases installs when none are
// given.
func defaultAliasNames() []string {
	return []string{"jq", spruceName}
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

// inspectAliasTarget classifies what sits at path when aliases are links.
func inspectAliasTarget(path, graftPath string) (aliasTargetState, error) {
	return targetState(path, graftPath, false)
}

// targetState classifies what sits at path. A link counts as this graft's
// when the path it resolves to is graft's, and a regular file in copy mode
// is matched by SHA-256 against graft. It never runs the target, because
// what a program says about itself is no proof of what it is.
func targetState(path, graft string, copyMode bool) (aliasTargetState, error) {
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		return aliasAbsent, nil
	}
	if err != nil {
		return aliasForeign, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			// A link that leads nowhere is not ours.
			return aliasForeign, nil //nolint:nilerr // a dangling link is classified, not an error
		}
		want, err := filepath.EvalSymlinks(graft)
		if err != nil {
			return aliasForeign, err
		}
		if resolved == want {
			return aliasLinkToThisGraft, nil
		}
		return aliasForeign, nil
	}
	if !copyMode || !info.Mode().IsRegular() {
		return aliasForeign, nil
	}
	have, err := os.ReadFile(path) // #nosec G304 -- the path is an alias location the caller chose
	if err != nil {
		return aliasForeign, err
	}
	want, err := os.ReadFile(graft) // #nosec G304 -- the running binary
	if err != nil {
		return aliasForeign, err
	}
	if sha256.Sum256(have) == sha256.Sum256(want) {
		return aliasCopyOfThisGraft, nil
	}
	if bytes.Contains(have, aliasCopyMarker) {
		return aliasCopyOfOtherGraft, nil
	}
	return aliasForeign, nil
}

// copyFile copies src to dst as an executable file.
func copyFile(src, dst string) error {
	in, err := os.Open(src) // #nosec G304 -- the running binary
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755) // #nosec G302 G304 -- an alias must be executable
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

// refusal is the clause that says why a target is left alone.
func refusal(state aliasTargetState) string {
	if state == aliasCopyOfOtherGraft {
		return "is a copy of another graft (use --force to replace it)"
	}
	return "exists and is not a link to graft (use --force to replace it)"
}

// The actions an outcome can record.
const (
	actionLinked    = "linked"
	actionWouldLink = "would link"
	actionAlready   = "already installed"
	actionRemoved   = "removed"
	actionSkipped   = "skipped"
	actionRefused   = "refused"
)

// handleInstallAliases installs or removes the aliases opts names and
// returns the exit code. A foreign target the caller named gives 1, after
// the other names are handled, where a foreign target among the defaults
// is skipped.
func handleInstallAliases(opts installAliasesOpts, stdout, stderr io.Writer) int {
	graft, err := graftExecutablePath()
	if err != nil {
		_, _ = fmt.Fprintln(stderr, ansi.Sprintf("@R{Cannot locate graft}: %s", err.Error()))
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

	code := 0
	var outcomes []aliasOutcome
	for _, name := range names {
		o, ok := installOne(opts, graft, aliasPath(dir, name, opts.copyMode), explicit, stderr)
		o.Name = name
		if !ok || o.Action == actionRefused {
			code = 1
		}
		if o.Action != "" {
			outcomes = append(outcomes, o)
		}
	}
	printAliasOutcomes(stdout, outcomes, graft, opts)
	return code
}

// installOne installs or removes the alias at path. It returns what it did
// and false when it could not, having said why on stderr, in which case
// the outcome has no action.
func installOne(opts installAliasesOpts, graft, path string, explicit bool, stderr io.Writer) (aliasOutcome, bool) {
	o := aliasOutcome{Path: path}
	fail := func(phrase string, err error) (aliasOutcome, bool) {
		_, _ = fmt.Fprintln(stderr, ansi.Sprintf("@R{%s} @m{%s}: %s", phrase, path, err.Error()))
		return aliasOutcome{Path: path}, false
	}
	state, err := targetState(path, graft, opts.copyMode)
	if err != nil {
		return fail("Cannot inspect", err)
	}
	if opts.Remove && state == aliasAbsent {
		o.Action, o.Reason = actionSkipped, path+" does not exist"
		return o, true
	}
	if (state == aliasForeign || state == aliasCopyOfOtherGraft) && !opts.Force {
		o.Reason = path + " " + refusal(state)
		o.Action = actionSkipped
		if explicit {
			o.Action = actionRefused
			_, _ = fmt.Fprintln(stderr, ansi.Sprintf("@R{Refusing to replace} @m{%s}: %s", path, refusal(state)))
		}
		return o, true
	}
	o.Action = aliasAction(opts, state)
	if opts.DryRun || o.Action == actionAlready {
		return o, true
	}
	if err := applyAlias(opts, graft, path, state); err != nil {
		return fail("Cannot update", err)
	}
	return o, true
}

// aliasAction names what a run does about a target in the given state,
// once refusals are out of the way.
func aliasAction(opts installAliasesOpts, state aliasTargetState) string {
	switch {
	case opts.Remove:
		return actionRemoved
	case state == aliasLinkToThisGraft || state == aliasCopyOfThisGraft:
		return actionAlready
	case opts.DryRun:
		return actionWouldLink
	}
	return actionLinked
}

// applyAlias makes the change on disk: it clears whatever sits at path,
// and then, unless it is removing, puts the alias there.
func applyAlias(opts installAliasesOpts, graft, path string, state aliasTargetState) error {
	if state != aliasAbsent {
		if err := os.Remove(path); err != nil {
			return err
		}
	}
	if opts.Remove {
		return nil
	}
	if opts.copyMode {
		return copyFile(graft, path)
	}
	return linkOrCopy(graft, path)
}

// printAliasOutcomes writes one line per name that was acted on or
// skipped, then the summary. A refusal was already written to stderr.
func printAliasOutcomes(w io.Writer, outcomes []aliasOutcome, graft string, opts installAliasesOpts) {
	counts := map[string]int{}
	for _, o := range outcomes {
		counts[o.Action]++
		switch o.Action {
		case actionLinked, actionWouldLink:
			_, _ = fmt.Fprintf(w, "%s %s -> %s\n", o.Action, o.Name, graft)
		case actionAlready, actionRemoved:
			_, _ = fmt.Fprintf(w, "%s %s\n", o.Action, o.Name)
		case actionSkipped:
			_, _ = fmt.Fprintf(w, "skipped %s: %s\n", o.Name, o.Reason)
		}
	}
	skipped := counts[actionSkipped] + counts[actionRefused]
	switch {
	case opts.Remove:
		_, _ = fmt.Fprintf(w, "removed %d, skipped %d\n", counts[actionRemoved], skipped)
	case opts.DryRun:
		_, _ = fmt.Fprintf(w, "would link %d, already installed %d, skipped %d\n", counts[actionWouldLink], counts[actionAlready], skipped)
	default:
		_, _ = fmt.Fprintf(w, "linked %d, already installed %d, skipped %d\n", counts[actionLinked], counts[actionAlready], skipped)
	}
}

// newInstallAliasesCmd builds the install-aliases command.
func newInstallAliasesCmd() *cobra.Command {
	var opts installAliasesOpts
	cmd := &cobra.Command{
		Use:   "install-aliases [flags] [NAME...]",
		Short: "Install jq and spruce names that run graft",
		Long: "Install links named jq and spruce, or the NAMEs given, that run graft under\n" +
			"those names. They go beside the graft binary unless --dir says otherwise. An\n" +
			"existing file or link that is not graft's is skipped among the defaults and\n" +
			"refused when named, and --force replaces it. --remove takes the links away,\n" +
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
	cmd.Flags().StringVar(&opts.Dir, "dir", "", "Directory to install into (default: the directory of the graft binary)")
	cmd.Flags().BoolVar(&opts.Force, "force", false, "Replace a target that is not graft's")
	cmd.Flags().BoolVar(&opts.Remove, "remove", false, "Remove the aliases instead of installing them")
	cmd.Flags().BoolVar(&opts.DryRun, "dry-run", false, "Show what would happen without changing anything")
	return cmd
}
