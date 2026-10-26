package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"
	"github.com/fivetwenty-io/graft/log"
	"github.com/fivetwenty-io/graft/pkg/jq"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
)

// jqFailed is the code an uncaught error or input that does not parse
// exits with.
const jqFailed = 5

// errKind is the kind of failure a query command reports, which picks its
// message and its exit code.
type errKind int

// The failure kinds, each with jq's exit code.
const (
	errCompile errKind = iota // 3
	errRuntime                // 5
	errInput                  // 5, input that does not parse
	errOpen                   // 2, a file that cannot be opened
	errUsage                  // 2
)

// locatedError is a runtime error or an input error with the place jq
// names for it, which is the input's name and line, or <unknown> before
// any input has been read.
type locatedError struct {
	Where string
	Err   error
}

func (e *locatedError) Error() string { return e.Err.Error() }
func (e *locatedError) Unwrap() error { return e.Err }

// jqInputError is input that does not parse, as the input reader reports
// it to the program and to the main loop.
type jqInputError struct{ locatedError }

// colored renders text in an ansi color code. The text is substituted
// after the markup is processed, so text that happens to hold @X{...}
// markup prints as it is.
func colored(code, text string) string {
	return strings.Replace(ansi.Sprintf("%s", "@"+code+"{\x00}"), "\x00", text, 1)
}

// queryCommandError turns a failure into the status a query command ends
// with, using jq's exit code for the kind and graft's colored message.
func queryCommandError(kind errKind, err error) query.Status {
	switch kind {
	case errCompile:
		return query.Status{Code: 3, Message: colored("R", "Compile error") + ": " + err.Error() + "\n"}
	case errRuntime:
		return locatedStatus("Runtime error", err)
	case errInput:
		return locatedStatus("Invalid input", err)
	case errOpen:
		var pe *fs.PathError
		if errors.As(err, &pe) {
			return query.Status{Code: 2, Message: colored("R", "Unable to open file") + " " + colored("m", pe.Path) + ": " + pe.Err.Error() + "\n"}
		}
		return query.Status{Code: 2, Message: colored("R", "Unable to open file") + ": " + err.Error() + "\n"}
	default:
		phrase := "Invalid flags"
		var ue *usageError
		if errors.As(err, &ue) && ue.Phrase != "" {
			phrase = ue.Phrase
		}
		return query.Status{Code: 2, Message: colored("R", phrase) + ": " + err.Error() + "\n"}
	}
}

// locatedStatus is the status for a runtime or input error, which names
// where jq was reading and marks an error value that is not a string the
// way jq does.
func locatedStatus(phrase string, err error) query.Status {
	where, msg := "<unknown>", err.Error()
	var le *locatedError
	var ie *jqInputError
	switch {
	case errors.As(err, &ie):
		where = ie.Where
	case errors.As(err, &le):
		where = le.Where
	}
	at := colored("R", phrase) + " (at " + colored("m", where) + ")"
	var re *query.RuntimeError
	if errors.As(err, &re) && re.Value != nil && re.Value.Kind() != value.KindString {
		return query.Status{Code: jqFailed, Message: at + " (not a string): " + strings.TrimSuffix(msg, " (not a string)") + "\n"}
	}
	return query.Status{Code: jqFailed, Message: at + ": " + msg + "\n"}
}

// jqArgsError is the status for a command line parseJQArgs refuses: a
// --slurpfile or --rawfile that cannot be opened, or a usage error.
func jqArgsError(err error) query.Status {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return queryCommandError(errOpen, err)
	}
	return queryCommandError(errUsage, err)
}

// checkJQOptions refuses the flags graft jq does not run yet and the
// layout flags that describe JSON text under -o yaml, before any input is
// read.
func checkJQOptions(opts jqOptions) error {
	if err := validateComposeFlags(opts); err != nil {
		return err
	}
	for _, deferred := range []struct {
		set  bool
		flag string
	}{
		{opts.Seq, "--seq"},
		{opts.StreamErrs, "--stream-errors"},
		{opts.Stream, "--stream"},
	} {
		if deferred.set {
			return &usageError{Msg: deferred.flag + " is not available until a later release"}
		}
	}
	if !opts.yamlOutput() {
		return nil
	}
	for _, conflict := range []struct {
		set  bool
		flag string
	}{
		{opts.Compact, "-c"},
		{opts.Tab, "--tab"},
		{opts.ASCII, "-a"},
		{opts.RawOutput0, "--raw-output0"},
	} {
		if conflict.set {
			return &usageError{Msg: conflict.flag + " cannot be combined with -o " + opts.Output}
		}
	}
	if opts.IndentSet && opts.Indent < 2 {
		return &usageError{Msg: "--indent " + strconv.Itoa(opts.Indent) + " cannot be combined with -o " + opts.Output + " (YAML indents by 2 through 7)"}
	}
	return nil
}

// jqInput is one input source: a file, or stdin under the name jq gives
// it.
type jqInput struct {
	name string
	dec  *jq.Decoder
	file *os.File
}

// jqInputs reads the input sources in order, the way jq's input reader
// does. Files open as they are reached, and one that cannot be opened is
// reported on stderr and skipped, which also tells the main loop to stop
// after the value it is reading. Under -s every source opens on the first
// read and the sources decode as one slurped value.
type jqInputs struct {
	opts   jqOptions
	stdin  io.Reader
	stderr io.Writer
	files  []string

	next int
	cur  *jqInput
	read bool

	// The first source that could not be opened, which ends the main
	// loop after the value being read and makes the run exit 2.
	openErr error

	// The source last closed, which input_filename and error locations
	// still name until the next one opens.
	lastName string
	lastLine int

	// Under -s, whether the slurped value has been read.
	slurped bool

	// composed is the YAML that graft's merge or eval passes produced,
	// which stands in for the input files when it is set.
	composed []byte
}

// compose runs the merge or eval pass a command line asks for and makes
// its result the only input. It returns the status to end with when the
// pass fails.
func (in *jqInputs) compose(stdin io.Reader) *query.Status {
	if !in.opts.Merge && !in.opts.Eval {
		return nil
	}
	merged, errText, code := composeQueryInputs(newComposeContext(in.opts, stdin, in.opts.Engine), in.opts.Files)
	if code != 0 {
		_, _ = in.stderr.Write(errText)
		return &query.Status{Code: code}
	}
	in.composed, in.files = merged, []string{composedName}
	return nil
}

// decodeOptions is how every input source decodes.
func (in *jqInputs) decodeOptions() jq.DecodeOptions {
	format := jq.FormatAuto
	if in.composed != nil {
		return jq.DecodeOptions{Format: jq.FormatYAML, Raw: in.opts.RawInput, DepthLimit: in.opts.DepthLimit}
	}
	switch in.opts.InputFormat {
	case jqFormatJSON:
		format = jq.FormatJSON
	case jqFormatYAML:
		format = jq.FormatYAML
	}
	return jq.DecodeOptions{Format: format, Raw: in.opts.RawInput, DepthLimit: in.opts.DepthLimit}
}

// open opens the source at the next index, reporting a failure on stderr.
func (in *jqInputs) open() (*jqInput, bool) {
	path := in.files[in.next]
	in.next++
	if in.composed != nil {
		return &jqInput{name: path, dec: jq.NewDecoder(bytes.NewReader(in.composed), in.decodeOptions())}, true
	}
	if path == "-" {
		return &jqInput{name: "<stdin>", dec: jq.NewDecoder(in.stdin, in.decodeOptions())}, true
	}
	f, err := os.Open(path) // #nosec G304 -- the user names the input files
	if err != nil {
		if in.openErr == nil {
			in.openErr = err
		}
		_, _ = io.WriteString(in.stderr, queryCommandError(errOpen, err).Message)
		return nil, false
	}
	return &jqInput{name: path, dec: jq.NewDecoder(f, in.decodeOptions()), file: f}, true
}

// slurp opens every source and reads them as one value.
func (in *jqInputs) slurp() (*value.Value, error) {
	in.slurped = true
	var decs []*jq.Decoder
	var opened []*os.File
	defer func() {
		for _, f := range opened {
			_ = f.Close()
		}
	}()
	for in.next < len(in.files) {
		src, ok := in.open()
		if !ok {
			continue
		}
		decs = append(decs, src.dec)
		if src.file != nil {
			opened = append(opened, src.file)
		}
		in.lastName = src.name
	}
	whole := jq.Slurp(decs...)
	v, err := whole.Next()
	in.lastLine = whole.Line()
	if err != nil {
		return nil, &jqInputError{locatedError{Where: in.where(), Err: err}}
	}
	in.read = true
	return v, nil
}

// Next returns the next input value, and io.EOF after the last one.
func (in *jqInputs) Next() (*value.Value, error) {
	if in.opts.Slurp {
		if in.slurped {
			return nil, io.EOF
		}
		return in.slurp()
	}
	for {
		if in.cur == nil {
			if in.next >= len(in.files) {
				return nil, io.EOF
			}
			src, ok := in.open()
			if !ok {
				continue
			}
			in.cur = src
		}
		v, err := in.cur.dec.Next()
		if errors.Is(err, io.EOF) {
			in.closeCurrent()
			continue
		}
		if err != nil {
			return nil, &jqInputError{locatedError{Where: in.where(), Err: err}}
		}
		in.read = true
		return v, nil
	}
}

// closeCurrent closes the source being read, keeping its name for
// input_filename and error locations until the next one opens.
func (in *jqInputs) closeCurrent() {
	if in.cur.file != nil {
		_ = in.cur.file.Close()
	}
	in.lastName, in.lastLine = in.cur.name, in.cur.dec.Line()
	in.cur = nil
}

// filename is what input_filename reports, which is nothing until the
// first value has been read.
func (in *jqInputs) filename() string {
	if !in.read {
		return ""
	}
	if in.cur != nil {
		return in.cur.name
	}
	return in.lastName
}

// where is the location jq names in an error, which is the current
// source's name and the lines read from it so far.
func (in *jqInputs) where() string {
	switch {
	case in.cur != nil:
		return in.cur.name + ":" + strconv.Itoa(in.cur.dec.Line())
	case in.lastName != "":
		return in.lastName + ":" + strconv.Itoa(in.lastLine)
	}
	return "<unknown>"
}

// close closes whatever source is still open.
func (in *jqInputs) close() {
	if in.cur != nil && in.cur.file != nil {
		_ = in.cur.file.Close()
	}
}

// readerIsTTY reports whether r is a terminal.
func readerIsTTY(r io.Reader) bool {
	f, ok := r.(*os.File)
	return ok && isatty.IsTerminal(f.Fd())
}

// terminalWriter is a writer that knows whether it is a terminal, which
// the command's stdout seam is.
type terminalWriter interface {
	isTerminal() bool
}

// stdoutIsTTY reports whether the command's stdout is a terminal.
func stdoutIsTTY(w io.Writer) bool {
	if tw, ok := w.(terminalWriter); ok {
		return tw.isTerminal()
	}
	return writerIsTTY(w)
}

// jqOutput writes each output value the way jq's process() does,
// coloring YAML through graft's YAML colorizer when color is on.
type jqOutput struct {
	enc    *jq.Encoder
	buf    *bytes.Buffer // the encoder's target under colored YAML
	out    io.Writer
	styler debugStyler
}

func newJQOutput(opts jqOptions, stdout io.Writer, color bool, colors string) *jqOutput {
	eo := jq.EncodeOptions{
		Compact:      opts.Compact,
		Tab:          opts.Tab,
		ASCII:        opts.ASCII,
		Raw:          opts.Raw && !opts.Join,
		Join:         opts.Join && !opts.RawOutput0,
		RawNUL:       opts.RawOutput0,
		FloatNumbers: opts.FloatNumbers,
		SortKeys:     opts.SortKeys,
		// jq's stdout is line-buffered on a terminal, so each output
		// shows up as it is produced and before any later diagnostic.
		Unbuffered: opts.Unbuffered || stdoutIsTTY(stdout),
		Color:      color,
		Colors:     colors,
	}
	if opts.IndentSet {
		eo.Indent = opts.Indent
		if opts.Indent == 0 {
			eo.Indent = -1
		}
	}
	o := &jqOutput{out: stdout}
	if opts.yamlOutput() {
		eo.Format = jq.FormatYAML
		if color {
			o.buf = &bytes.Buffer{}
			o.styler = debugStyler{theme: resolveDebugTheme(opts.Theme), enabled: true}
			o.enc = jq.NewEncoder(o.buf, eo)
			return o
		}
	}
	o.enc = jq.NewEncoder(stdout, eo)
	return o
}

func (o *jqOutput) write(v *value.Value) error {
	if err := o.enc.Encode(v); err != nil {
		return err
	}
	if o.buf == nil {
		return nil
	}
	if err := o.enc.Flush(); err != nil {
		return err
	}
	_, err := o.out.Write(colorizeYAML(o.buf.Bytes(), o.styler))
	o.buf.Reset()
	return err
}

func (o *jqOutput) flush() error { return o.enc.Flush() }

// writeHalt prints a halt_error message the way jq does: a string as it
// is, null as nothing, and any other value as compact JSON on its own
// line.
func writeHalt(stderr io.Writer, msg *value.Value) {
	switch {
	case msg == nil || msg.Kind() == value.KindNull:
	case msg.Kind() == value.KindString:
		_, _ = io.WriteString(stderr, msg.Str())
	default:
		enc := jq.NewEncoder(stderr, jq.EncodeOptions{Compact: true})
		_ = enc.Encode(msg)
		_ = enc.Flush()
	}
}

// handleJQ runs a parsed graft jq command line with the streams it reads
// and writes, following jq 1.8.2's main loop: each input runs through the
// program, an uncaught error is reported and the next input runs, a halt
// or input that does not parse ends the loop, and the exit code follows
// jq's rules for -e, halts, and files that could not be opened.
func handleJQ(opts jqOptions, stdin io.Reader, stdout, stderr io.Writer) query.Status {
	if err := checkJQOptions(opts); err != nil {
		return queryCommandError(errUsage, err)
	}
	if opts.RunTests {
		return runJQTests(opts, stdin, stdout)
	}
	if opts.Repl {
		return runJQRepl(opts, stdin, stdout)
	}
	program, origin, st := loadJQProgram(opts, stdin, stdout)
	if st != nil {
		return *st
	}
	log.DEBUG("program: %s", program)

	colors := os.Getenv("JQ_COLORS")
	if colors != "" && jq.CheckColors(colors) != nil {
		_, _ = io.WriteString(stderr, "Failed to set $JQ_COLORS\n")
		colors = ""
	}

	files := opts.Files
	if len(files) == 0 {
		files = []string{"-"}
	}
	for _, f := range files {
		log.DEBUG("input: %s", f)
	}
	in := &jqInputs{opts: opts, stdin: stdin, stderr: stderr, files: files}
	if st := in.compose(stdin); st != nil {
		return *st
	}
	defer in.close()

	q, err := jq.Compile(program, jqCompileOptions(opts, in, origin, stderr)...)
	if err != nil {
		var ce *query.CompileError
		if errors.As(err, &ce) {
			return queryCommandError(errCompile, err)
		}
		return queryCommandError(errUsage, err)
	}

	color := !opts.Monochrome && (opts.ColorOutput || (os.Getenv("NO_COLOR") == "" && stdoutIsTTY(stdout)))
	out := newJQOutput(opts, stdout, color, colors)
	run := runJQInputs(q, opts, in, out, stderr)
	if err := out.flush(); err != nil {
		_, _ = io.WriteString(stderr, colored("R", "Unable to write output")+": "+err.Error()+"\n")
		run.err = &fs.PathError{Op: "write", Path: "<stdout>", Err: err}
	}
	return query.Status{Code: jq.ExitCode(run.last, run.produced, run.err, opts.ExitStatus)}
}

// loadJQProgram returns the program text and, for a program read with -f,
// the directory it came from. With no program it is ".", unless stdin and
// stdout are both terminals, where jq asks for one.
func loadJQProgram(opts jqOptions, stdin io.Reader, stdout io.Writer) (program, origin string, st *query.Status) {
	switch {
	case opts.FromFile && !opts.HasProgram:
		s := queryCommandError(errUsage, &usageError{Msg: "-f takes a program file"})
		return "", "", &s
	case opts.FromFile:
		data, err := os.ReadFile(opts.Program) // #nosec G304 -- the user names the program file
		if err != nil {
			s := queryCommandError(errOpen, err)
			return "", "", &s
		}
		if abs, err := filepath.Abs(opts.Program); err == nil {
			origin = filepath.Dir(abs)
		}
		return string(data), origin, nil
	case opts.HasProgram:
		return opts.Program, "", nil
	case readerIsTTY(stdin) && stdoutIsTTY(stdout):
		s := queryCommandError(errUsage, &usageError{Msg: "a program is required (see graft jq --help)"})
		return "", "", &s
	}
	return ".", "", nil
}

// jqCompileOptions are the jq.Compile options a command line asks for.
func jqCompileOptions(opts jqOptions, in *jqInputs, origin string, stderr io.Writer) []jq.Option {
	jqOpts := []jq.Option{
		jq.WithInputs(in.Next),
		jq.WithInputFilename(in.filename),
		jq.WithStderr(stderr),
		jq.WithPositional(opts.Positional),
		jq.WithDepthLimit(opts.DepthLimit),
	}
	for _, name := range opts.ArgNames {
		jqOpts = append(jqOpts, jq.WithArg(name, opts.Args[name]))
	}
	if origin != "" {
		jqOpts = append(jqOpts, jq.WithProgramOrigin(origin))
	}
	jqOpts = append(jqOpts, jq.WithModulePaths(jqLibraryPaths(opts.LibraryPaths)))
	if opts.FloatNumbers {
		jqOpts = append(jqOpts, jq.WithFloatNumbers())
	}
	return jqOpts
}

// jqDefaultLibraryPaths is the search list jq uses when no -L is given.
var jqDefaultLibraryPaths = []string{"~/.jq", "$ORIGIN/../lib/jq", "$ORIGIN/../lib"}

// jqLibraryPaths resolves each -L directory to its real path, the way jq
// does, and keeps one that does not exist as it was given, which is how an
// entry such as ~/.jq or $ORIGIN/lib gets through to the search. With no
// -L it returns jq's default list, because the library reads no module
// files unless it is told where to look.
func jqLibraryPaths(paths []string) []string {
	if len(paths) == 0 {
		return append([]string(nil), jqDefaultLibraryPaths...)
	}
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = p
		if abs, err := filepath.Abs(p); err == nil {
			if resolved, err := filepath.EvalSymlinks(abs); err == nil {
				out[i] = resolved
			}
		}
	}
	return out
}

// jqRun is where jq's main loop stands, in the terms jq.ExitCode takes:
// the last output of the last input whose program ended without an error
// and produced something, and the error that ended the last input.
type jqRun struct {
	last     *value.Value
	produced bool
	err      error
}

// input records how the program ended on one input and reports whether
// it halted, which ends the main loop.
func (r *jqRun) input(last *value.Value, err error) (halted bool) {
	r.err = err
	if err == nil && last != nil {
		r.last, r.produced = last, true
	}
	var he *query.HaltError
	return errors.As(err, &he)
}

// runJQInputs is jq's main loop. It runs the program once on null under
// -n, and otherwise once per input until the inputs run out, a halt, input
// that does not parse, or a file that could not be opened stops it.
func runJQInputs(q *jq.Query, opts jqOptions, in *jqInputs, out *jqOutput, stderr io.Writer) jqRun {
	var run jqRun
	if opts.NullInput {
		run.input(runJQProgram(q, nil, in, out, stderr))
	}
	for !opts.NullInput && in.openErr == nil {
		v, err := in.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			_, _ = io.WriteString(stderr, queryCommandError(errInput, err).Message)
			run.err = err
			break
		}
		if run.input(runJQProgram(q, v, in, out, stderr)) {
			break
		}
	}
	if in.openErr != nil {
		run.err = in.openErr
	}
	return run
}

// runJQProgram runs the program on one input the way jq's process() does
// and returns its last output, or nil when it produced none, and the halt
// or uncaught error that ended it, which it has already reported.
func runJQProgram(q *jq.Query, input *value.Value, in *jqInputs, out *jqOutput, stderr io.Writer) (last *value.Value, err error) {
	for v, err := range q.Run(context.Background(), input) {
		if err != nil {
			var he *query.HaltError
			if errors.As(err, &he) {
				writeHalt(stderr, he.Message)
				return nil, err
			}
			kind := errRuntime
			var ie *jqInputError
			if errors.As(err, &ie) {
				kind = errInput
			} else {
				err = &locatedError{Where: in.where(), Err: err}
			}
			_, _ = io.WriteString(stderr, queryCommandError(kind, err).Message)
			return nil, err
		}
		traceOutput(v)
		if err := out.write(v); err != nil {
			err = &locatedError{Where: in.where(), Err: err}
			_, _ = io.WriteString(stderr, queryCommandError(errRuntime, err).Message)
			return nil, err
		}
		last = v
	}
	return last, nil
}

// runJQTests runs --run-tests over the named file, or stdin when none is
// named, and exits 1 when any case fails.
func runJQTests(opts jqOptions, stdin io.Reader, stdout io.Writer) query.Status {
	r := stdin
	if opts.RunTestsFile != "" {
		f, err := os.Open(opts.RunTestsFile)
		if err != nil {
			// jq exits 1 here rather than its usual 2 for a file it
			// cannot open.
			st := queryCommandError(errOpen, err)
			st.Code = 1
			return st
		}
		defer func() { _ = f.Close() }()
		r = f
	}
	_, failed, err := jq.RunTestFile(r, stdout)
	if err != nil {
		return queryCommandError(errUsage, &usageError{Phrase: "Invalid test file", Msg: err.Error()})
	}
	if failed > 0 {
		return query.Status{Code: 1}
	}
	return query.Status{}
}

// jqStdout is graft jq's stdout, written through the printStdOutf seam.
type jqStdout struct{}

func (jqStdout) Write(p []byte) (int, error) {
	printStdOutf("%s", p)
	return len(p), nil
}

func (jqStdout) isTerminal() bool { return isStdoutTTY() }

// jqStderr is graft jq's stderr, written through log.PrintStdErrf so that
// what a program writes reaches stderr byte for byte.
type jqStderr struct{}

func (jqStderr) Write(p []byte) (int, error) {
	log.PrintStdErrf("%s", p)
	return len(p), nil
}

// newJQCmd returns the graft jq command. Its arguments follow jq's own
// rules rather than cobra's, so it parses them itself and hands any of
// graft's persistent flags back to the root's flag set.
func newJQCmd() *cobra.Command {
	return newJQCmdFor(personaGraft)
}

// newJQCmdFor returns the jq command for a persona, which is graft when it
// is a subcommand of the graft root and jq when it is the root itself.
func newJQCmdFor(pers persona) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "jq [flags] PROGRAM [FILE...]",
		Short: "Run a jq program over JSON and YAML input",
		Long: "Run a jq program over JSON and YAML input, the way jq " + jqCompatVersion + " does.\n\n" +
			"graft jq takes jq's flags, reads JSON or YAML from each FILE or from stdin, and\n" +
			"writes JSON, or YAML with -o yaml. -S and --sort-keys[=MODE] sort object keys,\n" +
			"where MODE is lexical, spruce, or none, --input-format chooses auto, json, or\n" +
			"yaml input, and --float-numbers does arithmetic in doubles as jq does.",
		DisableFlagParsing: true,
		Args:               cobra.ArbitraryArgs,
		// The root's hook runs from RunE once graft's own flags are
		// parsed out of the jq arguments.
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
		RunE: func(cmd *cobra.Command, args []string) error {
			// cobra hands over the flags given before the verb as
			// well, which are graft's own in every form cobra reads.
			n := preVerbArgs(cmd, args)
			// As the root, cobra has already run the hook, and there
			// are no graft flags to parse.
			applyRoot := func(rootArgs []string) bool {
				return pers == personaJQ || applyJQRootFlags(cmd, rootArgs)
			}
			opts, err := parseJQArgs(args[n:], pers)
			opts.RootArgs = append(slices.Clip(args[:n]), opts.RootArgs...)
			if err != nil {
				// The root's flags given before the bad one still
				// decide whether the diagnostic is colored.
				if !applyRoot(opts.RootArgs) {
					return nil
				}
				st := jqArgsError(err)
				log.PrintStdErrf("%s", st.Message)
				exit(st.Code)
				return nil
			}
			if opts.Help {
				return cmd.Help()
			}
			if opts.Version {
				printStdOutf("%s", jqVersionOutput())
				exit(0)
				return nil
			}
			if !applyRoot(opts.RootArgs) {
				return nil
			}
			if theme := cmd.Root().PersistentFlags().Lookup("theme"); theme != nil {
				opts.Theme, _, _ = resolveTheme(cmd, theme.Changed, theme.Value.String())
			} else {
				opts.Theme, _, _ = resolveTheme(cmd, false, themeNameAuto)
			}
			if opts.Merge || opts.Eval || opts.Repl {
				var err error
				if opts.Engine, err = composeEngineOpts(cmd); err != nil {
					log.PrintStdErrf("%s\n", err.Error())
					exit(1)
					return nil
				}
			}
			st := handleJQ(opts, os.Stdin, jqStdout{}, jqStderr{})
			if st.Message != "" {
				log.PrintStdErrf("%s", st.Message)
			}
			exit(st.Code)
			return nil
		},
	}
	cmd.SetHelpFunc(func(c *cobra.Command, a []string) { jqHelp(c, a, pers) })
	return cmd
}

// composeEngineOpts are the engine options the root's config loading
// produces, which graft jq needs for -m and -E as graft merge does.
func composeEngineOpts(cmd *cobra.Command) (engineOptions, error) {
	if ctx := cmd.Context(); ctx != nil {
		if kept, ok := ctx.Value(startupEngineKey{}).(engineOptions); ok {
			return kept, nil
		}
	}
	path := ""
	if f := cmd.Root().PersistentFlags().Lookup("config"); f != nil {
		path = f.Value.String()
	}
	cfg, err := resolveStartupConfig(path)
	if err != nil {
		return nil, err
	}
	return configEngineOpts(cfg, resolveStartupFeatureFlags()), nil
}

// jqFlagsHelp summarizes the flags graft jq reads after the verb.
const jqFlagsHelp = `Flags:
  -n, --null-input            run the program once on null rather than on each input
  -R, --raw-input             read each line of input as a string
  -s, --slurp                 read every input into one array and run the program on it
      --input-format FORMAT   read input as auto, json, or yaml
  -c, --compact-output        print each output on one line
  -r, --raw-output            print strings without quotes or escapes
  -j, --join-output           print as -r does, with no newline after each output
      --raw-output0           print as -r does, with a NUL after each output
  -a, --ascii-output          escape every character outside ASCII
  -o FORMAT                   print json, or yaml (also y)
  -S, --sort-keys[=MODE]      sort object keys, where MODE is lexical, spruce, or none
  -C, --color-output          color the output
  -M, --monochrome-output     never color the output
      --tab                   indent with a tab
      --indent N              indent with N spaces, from 0 to 7
      --unbuffered            write each output as soon as it is produced
      --float-numbers         do arithmetic in doubles, as jq does
      --depth-limit N         bound call and input-file nesting (default 10000)
  -f, --from-file FILE        read the program from FILE
      --arg NAME VALUE        bind $NAME to the string VALUE
      --argjson NAME TEXT     bind $NAME to the JSON or YAML value TEXT
      --slurpfile NAME FILE   bind $NAME to an array of every value in FILE
      --rawfile NAME FILE     bind $NAME to the text of FILE
      --args                  read the remaining arguments as positional strings
      --jsonargs              read the remaining arguments as positional JSON texts
  -m, --merge                 merge every input with graft merge before the program runs
  -E, --eval                  resolve each input document on its own before the program runs
      --prune KEY             with -m or -E, prune KEY from the result (repeatable)
      --cherry-pick KEY       with -m or -E, keep only KEY in the result (repeatable)
      --skip-eval, --fallback-append, --go-patch, --multi-doc, --skip-vault,
      --skip-aws, --skip-nats
                              with -m or -E, as graft merge reads them
  -e, --exit-status           exit 1 when the last output is false or null, and 4 when there is none
      --run-tests [FILE]      run the jq test cases in FILE, or on stdin
  -h, --help                  print this help
      --                      read every later argument as a program, file, or positional
`

// jqHelp prints graft jq's help. Its flags are jq's, which cobra cannot
// list because the command parses them itself, and graft's own flags are
// listed only in the forms graft jq takes after the verb, which leaves
// out -D, -T, and -v, since -v and --version are jq's there.
func jqHelp(cmd *cobra.Command, _ []string, pers persona) {
	if pers == personaJQ {
		_, _ = io.WriteString(cmd.OutOrStdout(), cmd.Long+"\n\n"+
			"Usage:\n"+
			"  jq [options] <jq filter> [file...]\n"+
			"  jq [options] --args <jq filter> [strings...]\n"+
			"  jq [options] --jsonargs <jq filter> [JSON_TEXTS...]\n\n"+
			jqFlagsHelp)
		return
	}
	global := pflag.NewFlagSet("", pflag.ContinueOnError)
	cmd.Root().PersistentFlags().VisitAll(func(f *pflag.Flag) {
		if f.Name == "version" {
			return
		}
		shown := *f
		shown.Shorthand = ""
		global.AddFlag(&shown)
	})
	w := cmd.OutOrStdout()
	_, _ = io.WriteString(w, cmd.Long+"\n\n"+
		"Usage:\n"+
		"  graft jq [flags] PROGRAM [FILE...]\n"+
		"  graft jq [flags] --args PROGRAM [STRING...]\n"+
		"  graft jq [flags] --jsonargs PROGRAM [JSON...]\n\n"+
		jqFlagsHelp+"\n"+
		"Global Flags:\n"+global.FlagUsages())
}

// preVerbArgs returns how many of args came before the verb on graft's
// command line, which cobra leaves at the front of the arguments of a
// command that parses its own flags. It reads them with the root's own
// flag definitions, so every form cobra takes before any other verb is
// taken here too, and it leaves checking the values to the real parse.
func preVerbArgs(cmd *cobra.Command, args []string) int {
	line := os.Args[1:]
	flags := pflag.NewFlagSet("", pflag.ContinueOnError)
	flags.SetInterspersed(false)
	flags.SetOutput(io.Discard)
	cmd.Root().PersistentFlags().VisitAll(func(f *pflag.Flag) {
		shadow := *f
		shadow.Value = anyFlagValue(f.Value.Type())
		flags.AddFlag(&shadow)
	})
	if flags.Parse(line) != nil || flags.ArgsLenAtDash() != -1 {
		return 0
	}
	rest := flags.Args()
	n := len(line) - len(rest)
	if len(rest) == 0 || rest[0] != cmd.Name() || n > len(args) || !slices.Equal(line[:n], args[:n]) {
		return 0
	}
	return n
}

// anyFlagValue is a flag value that takes any text, so preVerbArgs can
// find where graft's flags end without setting them.
type anyFlagValue string

func (anyFlagValue) String() string   { return "" }
func (anyFlagValue) Set(string) error { return nil }
func (v anyFlagValue) Type() string   { return string(v) }

// applyJQRootFlags parses graft's own flags out of a graft jq command line
// and runs the root's hook over them, which cobra could not do before the
// command parsed its arguments. It reports false when the command must
// stop, because a flag was refused or the hook ended the run.
func applyJQRootFlags(cmd *cobra.Command, rootArgs []string) bool {
	root := cmd.Root()
	if err := root.PersistentFlags().Parse(rootArgs); err != nil {
		ansi.Color(ansi.ResolveColor(nil, isStderrTTY()))
		st := queryCommandError(errUsage, &usageError{Msg: err.Error()})
		log.PrintStdErrf("%s", st.Message)
		exit(st.Code)
		return false
	}
	return root.PersistentPreRunE(cmd, nil) == nil
}
