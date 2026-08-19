package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ergochat/readline"
	"github.com/mattn/go-isatty"
)

// errREPLInterrupted is what a debugLineReader reports when the user pressed
// Ctrl+C. It is not a failure: the REPL discards the half-typed line and
// prompts again, the way a shell does.
var errREPLInterrupted = errors.New("interrupted")

// debugLineReader is the `graft debug` REPL's input source. Two things
// implement it: a full line editor when the debugger is driven from a
// terminal, and a plain scanner otherwise. The scanner is what every test
// and every piped script gets, so the two must agree on the basics - one
// line per call, io.EOF at the end - and differ only in what a human at a
// keyboard gets on top.
type debugLineReader interface {
	// ReadLine returns the next line without its terminator. It reports
	// io.EOF at end of input and errREPLInterrupted for Ctrl+C.
	ReadLine() (string, error)
	// SaveHistory records line for later recall, if this reader keeps
	// history and the line is worth keeping.
	SaveHistory(line string)
	Close() error
}

// newDebugLineReader returns the line editor when in and out are both a
// terminal, and the plain scanner otherwise. Both ends have to be a
// terminal: the editor redraws the line it is editing, which is meaningless
// when output is a file or a pipe. If the editor cannot be started for any
// reason the scanner takes over, so the debugger still runs.
func newDebugLineReader(in io.Reader, out io.Writer, prompt string, completer readline.AutoCompleter) debugLineReader {
	if debugInputIsInteractive(in, out) {
		historyFile := debugHistoryFile()
		rl, err := readline.NewFromConfig(&readline.Config{
			Prompt:            prompt,
			Stdin:             in,
			Stdout:            out,
			HistoryFile:       historyFile,
			HistorySearchFold: true,
			// History is saved by hand (see SaveHistory) so a line that
			// carries a secret never reaches the file.
			DisableAutoSaveHistory: true,
			// "\n" is readline's spelling of "print nothing here"; the
			// REPL prints its own newline when input ends.
			EOFPrompt:    "\n",
			AutoComplete: completer,
		})
		if err == nil {
			return &readlineLineReader{rl: rl, historyFile: historyFile, out: out}
		}
	}
	return &scannerLineReader{scanner: bufio.NewScanner(in), out: out, prompt: prompt}
}

func debugInputIsInteractive(in io.Reader, out io.Writer) bool {
	inFile, ok := in.(*os.File)
	if !ok || !isatty.IsTerminal(inFile.Fd()) {
		return false
	}
	outFile, ok := out.(*os.File)
	return ok && isatty.IsTerminal(outFile.Fd())
}

// scannerLineReader is the original REPL input path: print a prompt, read a
// line, no editing and no history.
type scannerLineReader struct {
	scanner *bufio.Scanner
	out     io.Writer
	prompt  string
}

func (r *scannerLineReader) ReadLine() (string, error) {
	_, _ = fmt.Fprint(r.out, r.prompt)
	if !r.scanner.Scan() {
		_, _ = fmt.Fprintln(r.out)
		// Scan also stops on a read error or a line longer than
		// bufio.Scanner's 64KiB buffer; without this check that is
		// indistinguishable from a clean EOF and every remaining command
		// is silently dropped.
		if err := r.scanner.Err(); err != nil {
			return "", err
		}
		return "", io.EOF
	}
	return r.scanner.Text(), nil
}

func (r *scannerLineReader) SaveHistory(string) {}

func (r *scannerLineReader) Close() error { return nil }

// readlineLineReader is the interactive path: arrow keys walk the history,
// Ctrl+R searches it, Tab completes, and the history outlives the session.
type readlineLineReader struct {
	rl          *readline.Instance
	historyFile string
	out         io.Writer
}

func (r *readlineLineReader) ReadLine() (string, error) {
	line, err := r.rl.ReadLine()
	switch {
	case errors.Is(err, readline.ErrInterrupt):
		return "", errREPLInterrupted
	case errors.Is(err, io.EOF):
		_, _ = fmt.Fprintln(r.out)
		return "", io.EOF
	case err != nil:
		return "", err
	}
	return line, nil
}

func (r *readlineLineReader) SaveHistory(line string) {
	if debugHistoryWorthSaving(line) {
		_ = r.rl.SaveToHistory(line)
	}
}

func (r *readlineLineReader) Close() error {
	err := r.rl.Close()
	// readline rewrites the history file through a temporary file when it
	// outgrows the limit, which restores default permissions; put them back.
	if r.historyFile != "" {
		_ = os.Chmod(r.historyFile, 0o600)
	}
	return err
}

// debugHistoryFile returns the path the REPL keeps its command history in,
// creating it 0600 first so no other user can read what was typed. An empty
// return means "no persistent history", which is not an error: the session
// still has its own in-memory history.
func debugHistoryFile() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	dir := filepath.Join(home, ".graft")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return ""
	}
	path := filepath.Join(dir, "debug_history")
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return ""
	}
	_ = f.Close()
	return path
}

// debugSecretConfigKeys are the `config` keys whose value is a credential.
// Setting one of these is kept out of the history file, since that file
// lives on disk and outlives the session.
var debugSecretConfigKeys = map[string]bool{debugConfigKeyVaultToken: true}

// debugHistoryWorthSaving reports whether line should be written to the
// history file. Reading a secret key (`config vault.token`) is fine; only
// setting one exposes anything.
func debugHistoryWorthSaving(line string) bool {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return false
	}
	if fields[0] == "config" && len(fields) >= 3 && debugSecretConfigKeys[fields[1]] {
		return false
	}
	return true
}

// debugCompleter is the REPL's Tab completion. It completes command names
// on the first word and, after that, whatever the command in hand takes:
// document paths for the path commands, from the tree as it stands at the
// current step rather than from a fixed list.
type debugCompleter struct {
	sess *debugSession
}

// Do implements readline.AutoCompleter: given the line and cursor, it
// returns each candidate with the already-typed part cut off, plus how many
// runes of the line that part was.
func (c *debugCompleter) Do(line []rune, pos int) ([][]rune, int) {
	if pos > len(line) {
		pos = len(line)
	}
	head := string(line[:pos])

	fields := strings.Fields(head)
	word := ""
	if len(fields) > 0 && !endsInSpace(head) {
		word = fields[len(fields)-1]
	}
	argIndex := len(fields)
	if word != "" {
		argIndex--
	}

	var candidates []string
	if argIndex == 0 {
		candidates = debugCommandCandidates()
	} else {
		candidates = c.completeArgument(fields[0], argIndex, word)
	}
	return completionSuffixes(candidates, word)
}

func endsInSpace(s string) bool {
	return s == "" || strings.HasSuffix(s, " ") || strings.HasSuffix(s, "\t")
}

// completionSuffixes keeps the candidates that extend word and returns each
// one's remainder, in readline's (suffixes, prefix-length) form.
func completionSuffixes(candidates []string, word string) ([][]rune, int) {
	out := make([][]rune, 0, len(candidates))
	for _, cand := range candidates {
		if !strings.HasPrefix(cand, word) {
			continue
		}
		out = append(out, []rune(cand[len(word):]))
	}
	return out, len([]rune(word))
}

// debugCommandCandidates is every REPL command, sorted, each ready to be
// followed by an argument.
func debugCommandCandidates() []string {
	names := make([]string, 0, len(debugCommands)+2)
	for name := range debugCommands {
		names = append(names, name+" ")
	}
	names = append(names, "quit ", "exit ")
	sort.Strings(names)
	return names
}

// completeArgument returns the candidates for argument argIndex of cmd,
// according to what the command's help entry says its argument is.
func (c *debugCompleter) completeArgument(cmd string, argIndex int, word string) []string {
	if argIndex != 1 {
		// Only `config <key> <value>` takes a second argument, and a
		// value is free-form.
		return nil
	}
	switch debugArgKindFor(cmd) {
	case debugArgCommand:
		return debugCommandCandidates()
	case debugArgConfigKey:
		keys := make([]string, 0, len(debugConfigKeyOrder))
		for _, key := range debugConfigKeyOrder {
			keys = append(keys, key+" ")
		}
		sort.Strings(keys)
		return keys
	case debugArgBreakpoint:
		// Only a path that actually has a breakpoint can be unset.
		paths := make([]string, 0, len(c.sess.breakpoints))
		for path := range c.sess.breakpoints {
			paths = append(paths, path+" ")
		}
		sort.Strings(paths)
		return paths
	case debugArgPath:
		return c.completePath(word)
	case debugArgFile:
		return debugFileCandidates(word)
	case debugArgNone:
		return nil
	}
	return nil
}

// debugFileCandidates completes an export filename against the filesystem.
// A directory keeps its separator instead of a trailing space, so the next
// Tab descends into it.
func debugFileCandidates(word string) []string {
	matches, err := filepath.Glob(word + "*")
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(matches))
	for _, match := range matches {
		if info, statErr := os.Stat(match); statErr == nil && info.IsDir() {
			out = append(out, match+string(os.PathSeparator))
			continue
		}
		out = append(out, match+" ")
	}
	sort.Strings(out)
	return out
}

// completePath offers the children of the last complete segment of word, so
// completion walks the document one level per Tab rather than listing every
// path in it. A candidate with children of its own is left without a
// trailing space, so the next Tab carries on from the dot.
func (c *debugCompleter) completePath(word string) []string {
	if c.sess == nil || c.sess.tree == nil {
		return nil
	}

	parent := ""
	if dot := strings.LastIndex(word, "."); dot >= 0 {
		parent = word[:dot]
	}
	node, ok := lookupDottedPath(c.sess.tree, parent)
	if !ok {
		return nil
	}

	prefix := ""
	if parent != "" {
		prefix = parent + "."
	}

	var out []string
	switch typed := node.(type) {
	case map[string]interface{}:
		for key, value := range typed {
			out = append(out, prefix+key+childSuffix(value))
		}
	case []interface{}:
		// A list entry is addressed by bracketed index, as
		// lookupDottedPath's parseIndexSegment expects.
		for i, value := range typed {
			out = append(out, fmt.Sprintf("%s[%d]%s", prefix, i, childSuffix(value)))
		}
	}
	sort.Strings(out)
	return out
}

// childSuffix is the trailing space that finishes a completed word, or the
// empty string when the value has children and the path can go deeper.
func childSuffix(value interface{}) string {
	switch typed := value.(type) {
	case map[string]interface{}:
		if len(typed) > 0 {
			return ""
		}
	case []interface{}:
		if len(typed) > 0 {
			return ""
		}
	}
	return " "
}
