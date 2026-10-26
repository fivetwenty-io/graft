package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/fivetwenty-io/graft/log"
	"github.com/fivetwenty-io/graft/pkg/query"
	"github.com/fivetwenty-io/graft/pkg/query/value"
	"github.com/fivetwenty-io/graft/pkg/yq"
)

// yqFrontMatterProcess is the -f value that prints the content after the
// front matter. Any other value only extracts the front matter, as in yq.
const yqFrontMatterProcess = "process"

// yqFailed is the one code yq exits with for every failure.
const yqFailed = 1

// yqStatus is the status for a failure under graft yq, which uses graft's
// message for the kind and yq's single failure code.
func yqStatus(kind errKind, err error) query.Status {
	st := queryCommandError(kind, err)
	st.Code = yqFailed
	return st
}

// yqArgsError is the status for a command line parseYQArgs refuses: an
// expression file that cannot be opened, or a usage error.
func yqArgsError(err error) query.Status {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return yqStatus(errOpen, err)
	}
	return yqStatus(errUsage, err)
}

// checkYQOptions refuses the flag combinations that have no meaning,
// before any input is read.
func checkYQOptions(opts yqOptions) error {
	if err := validateComposeFlags(opts); err != nil {
		return err
	}
	if opts.Inplace && (opts.SplitExp != "" || opts.SplitExpFile != "") {
		return &usageError{Msg: errSplitInplace}
	}
	if opts.Inplace && opts.Merge {
		return &usageError{Msg: "-m cannot be combined with -i"}
	}
	if opts.Inplace && (len(opts.Files) == 0 || opts.Files[0] == "-") {
		return &usageError{Msg: "-i needs a file to rewrite"}
	}
	if opts.NullInput && len(opts.Files) > 0 {
		return &usageError{Msg: "cannot pass files in when using null-input flag"}
	}
	return nil
}

// yqCompileOptions are the yq.Compile options a command line asks for.
func yqCompileOptions(opts yqOptions, stderr io.Writer) []yq.Option {
	yqOpts := []yq.Option{
		yq.WithStderr(stderr),
		yq.WithStringInterpolation(opts.StringInterpolate),
		yq.WithHeaderPreprocess(opts.HeaderPreprocess),
	}
	if opts.DisableEnvOps {
		yqOpts = append(yqOpts, yq.WithEnvOps(false))
	}
	if opts.DisableFileOps {
		yqOpts = append(yqOpts, yq.WithFileOps(false))
	}
	if opts.EnableSystem {
		yqOpts = append(yqOpts, yq.WithSystemOps(true))
	}
	if opts.MergeAnchorFix {
		yqOpts = append(yqOpts, yq.WithMergeAnchorFix(true))
	}
	if opts.FloatNumbers {
		yqOpts = append(yqOpts, yq.WithFloatNumbers())
	}
	return yqOpts
}

// yqCompileFailure is the status for an expression that did not compile.
func yqCompileFailure(err error) query.Status {
	var ce *query.CompileError
	if errors.As(err, &ce) {
		return yqStatus(errCompile, err)
	}
	return yqStatus(errUsage, err)
}

// yqOutputFormat is the format -o resolves to. Auto is JSON when the first
// input file's name ends in .json, and YAML otherwise, and it never
// follows the content of stdin.
func yqOutputFormat(opts yqOptions) yq.Format {
	switch opts.OutputFormat {
	case yqFormatJSON:
		return yq.FormatJSON
	case yqFormatYAML:
		return yq.FormatYAML
	}
	if yqJSONNamed(opts) && opts.InputFormat == yqFormatAuto {
		return yq.FormatJSON
	}
	return yq.FormatYAML
}

// yqJSONNamed reports whether the first file has a .json extension, which
// is what -o auto looks at and stdin never matches.
func yqJSONNamed(opts yqOptions) bool {
	return len(opts.Files) > 0 && strings.HasSuffix(strings.ToLower(opts.Files[0]), ".json")
}

// yqAutoOutputWarning is what yq says when -o auto would have chosen JSON
// for a .json file but an explicit -p keeps the output YAML.
const yqAutoOutputWarning = "yq default output is now 'auto' (based on the filename extension). " +
	"Normally yq would output 'json', but for backwards compatibility 'yaml' has been set. " +
	"Please use -oy to specify yaml, or drop the -p flag."

// yqIndent is the Indent an Encoder takes for -I. Unset keeps yq's
// default, zero is compact JSON, and YAML clamps to 2 through 9.
func yqIndent(opts yqOptions, format yq.Format) int {
	switch {
	case !opts.IndentSet:
		return 0
	case opts.Indent <= 0:
		return -1
	case format == yq.FormatYAML:
		return min(max(opts.Indent, 2), 9)
	}
	return opts.Indent
}

// yqUnwrap reports whether a top-level string prints bare. YAML output
// unwraps unless -r=false says otherwise, and JSON output keeps the
// quotes unless -r is given.
func yqUnwrap(opts yqOptions, format yq.Format) bool {
	if opts.UnwrapSet || format == yq.FormatYAML {
		return opts.UnwrapScalar
	}
	return false
}

// yqOutput writes each output value the way yq prints it, coloring YAML
// through graft's YAML colorizer when color is on, and ending each value
// with a NUL under -0.
type yqOutput struct {
	enc    *yq.Encoder
	buf    *bytes.Buffer // the encoder's target when a value is post-processed
	out    io.Writer
	nul    bool
	eo     yq.EncodeOptions
	split  func(doc yq.Document, v *value.Value) (io.WriteCloser, error)
	sink   *splitSink // where the encoder writes under split
	last   string     // the split file written last
	styler debugStyler
	color  bool
}

func newYQOutput(opts yqOptions, format yq.Format, out io.Writer, color bool) *yqOutput {
	eo := yq.EncodeOptions{
		Format:          format,
		Indent:          yqIndent(opts, format),
		Colors:          color && format == yq.FormatJSON,
		UnwrapScalar:    yqUnwrap(opts, format),
		NoDocSeparators: opts.NoDoc,
		PrettyPrint:     opts.PrettyPrint,
		SortKeys:        opts.SortKeys,
		FloatNumbers:    opts.FloatNumbers,
	}
	o := &yqOutput{out: out, nul: opts.NulOutput, eo: eo}
	if opts.NulOutput || (color && format == yq.FormatYAML) {
		o.buf = &bytes.Buffer{}
		o.enc = yq.NewEncoder(o.buf, eo)
		if color && format == yq.FormatYAML {
			o.color = true
			o.styler = debugStyler{theme: resolveDebugTheme(opts.Theme), enabled: true}
		}
		return o
	}
	o.enc = yq.NewEncoder(out, eo)
	return o
}

func (o *yqOutput) write(v *value.Value) error {
	if o.split != nil {
		return o.writeSplit(v)
	}
	if err := o.enc.Encode(v); err != nil {
		return err
	}
	if o.buf == nil {
		return nil
	}
	if err := o.enc.Flush(); err != nil {
		return err
	}
	text := o.buf.Bytes()
	if o.color {
		text = colorizeYAML(text, o.styler)
	}
	if o.nul {
		text = append(bytes.TrimSuffix(slices.Clone(text), []byte("\n")), 0)
	}
	_, err := o.out.Write(text)
	o.buf.Reset()
	return err
}

func (o *yqOutput) flush() error { return o.enc.Flush() }

// raw writes data after everything written so far, untouched.
func (o *yqOutput) raw(data []byte) error {
	if o.split != nil {
		return o.rawSplit(data)
	}
	if err := o.enc.Flush(); err != nil {
		return err
	}
	_, err := o.out.Write(data)
	return err
}

// yqSource is where the documents come from: the input files in order, or
// the composed YAML a merge or eval pass produced.
type yqSource struct {
	opts     yqOptions
	stdin    io.Reader
	files    []string
	composed []byte
}

// yqRun is where a run stands: the name of the source being read, which
// errors name, and the facts yq.ExitCode takes.
type yqRun struct {
	where     string
	appendix  []byte // front matter content to print after the first output
	produced  bool
	anyTruthy bool
	err       error
}

// decodeOptions is how the source at index decodes. Auto reads a file named
// *.json as JSON, as -p json does, and anything else as YAML.
func (s *yqSource) decodeOptions(name string, index int) yq.DecodeOptions {
	format := yq.FormatYAML
	if s.opts.InputFormat == yqFormatJSON ||
		(s.opts.InputFormat == yqFormatAuto && strings.HasSuffix(strings.ToLower(name), ".json")) {
		format = yq.FormatJSON
	}
	return yq.DecodeOptions{Format: format, Filename: name, FileIndex: index, NoHeaderPreprocess: !s.opts.HeaderPreprocess}
}

// each calls fn with every document, in order, and stops at the first
// error. A file that cannot be opened is an open error, and one that does
// not parse is an input error.
func (s *yqSource) each(run *yqRun, fn func(doc *value.Value) error) error {
	if s.composed != nil {
		run.where = composedName
		return s.drain(run, yq.NewDecoder(bytes.NewReader(s.composed), s.decodeOptions(composedName, 0)), fn)
	}
	for i, path := range s.files {
		if err := s.eachInFile(run, i, path, fn); err != nil {
			return err
		}
	}
	return nil
}

func (s *yqSource) eachInFile(run *yqRun, index int, path string, fn func(doc *value.Value) error) error {
	if path == "-" {
		run.where = "<stdin>"
		return s.drain(run, yq.NewDecoder(s.frontMatter(run, index, s.stdin), s.decodeOptions("-", index)), fn)
	}
	run.where = path
	f, err := os.Open(path) // #nosec G304 -- the user names the input files
	if err != nil {
		return &yqOpenError{err}
	}
	defer func() { _ = f.Close() }()
	return s.drain(run, yq.NewDecoder(s.frontMatter(run, index, f), s.decodeOptions(path, index)), fn)
}

// frontMatter is the reader the decoder takes for the file at index. Under
// -f it is only the YAML at the head of the first file, and -f process
// keeps the content after it for the run to print once the first output
// is written. Every other file reads as it is.
func (s *yqSource) frontMatter(run *yqRun, index int, r io.Reader) io.Reader {
	if index != 0 || s.opts.FrontMatter == "" {
		return r
	}
	head, rest, _, err := yq.SplitFrontMatter(r)
	if err != nil {
		return &failingReader{err}
	}
	if s.opts.FrontMatter == yqFrontMatterProcess {
		run.appendix = rest
	}
	return bytes.NewReader(head)
}

// failingReader is a reader whose first read fails with err.
type failingReader struct{ err error }

func (f *failingReader) Read([]byte) (int, error) { return 0, f.err }

func (s *yqSource) drain(run *yqRun, dec *yq.Decoder, fn func(doc *value.Value) error) error {
	for {
		doc, err := dec.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return &jqInputError{locatedError{Where: run.where, Err: err}}
		}
		if err := fn(doc); err != nil {
			return err
		}
	}
}

// yqOpenError is an input file that could not be opened.
type yqOpenError struct{ error }

func (e *yqOpenError) Unwrap() error { return e.error }

// truthy reports whether v is neither null nor false.
func truthy(v *value.Value) bool {
	return v != nil && v.Kind() != value.KindNull && v.Kind() != value.KindFalse
}

// emit runs the evaluator over docs and writes each output, and it stops
// at the first error.
func (r *yqRun) emit(seq iter.Seq2[*value.Value, error], out *yqOutput) error {
	wrote := false
	for v, err := range seq {
		if err == nil {
			err = out.write(v)
		}
		if err != nil {
			return &locatedError{Where: r.where, Err: err}
		}
		wrote, r.produced = true, true
		r.anyTruthy = r.anyTruthy || truthy(v)
	}
	if wrote && r.appendix != nil {
		appendix := r.appendix
		r.appendix = nil
		if err := out.raw(appendix); err != nil {
			return &locatedError{Where: r.where, Err: err}
		}
	}
	return nil
}

// yqBlankNull is the root yq evaluates under -n and when its inputs held
// no document at all: a null with no text, which prints as an empty line.
func yqBlankNull() *value.Value {
	return value.Null().WithMeta(&value.Meta{})
}

// evalAll runs the evaluator once with every document as a root, or with
// a blank null when there is none.
func (s *yqSource) evalAll(ctx context.Context, ev *yq.Evaluator, run *yqRun, out *yqOutput) error {
	var docs []*value.Value
	if !s.opts.NullInput {
		if err := s.each(run, func(doc *value.Value) error {
			docs = append(docs, doc)
			return nil
		}); err != nil {
			return err
		}
	}
	if len(docs) == 0 {
		docs = []*value.Value{yqBlankNull()}
	}
	run.where = unknownSource
	return run.emit(ev.EvalAll(ctx, docs), out)
}

// evalEach runs the evaluator once per document, and once over a blank
// null when the inputs held no document at all.
func (s *yqSource) evalEach(ctx context.Context, ev *yq.Evaluator, run *yqRun, out *yqOutput) error {
	read := false
	if !s.opts.NullInput {
		if err := s.each(run, func(doc *value.Value) error {
			read = true
			return run.emit(ev.Eval(ctx, doc), out)
		}); err != nil || read {
			return err
		}
	}
	run.where = unknownSource
	return run.emit(ev.Eval(ctx, yqBlankNull()), out)
}

// yqFailure is the status for the error that ended a run.
func yqFailure(err error) query.Status {
	var oe *yqOpenError
	var ie *jqInputError
	switch {
	case errors.As(err, &oe):
		return yqStatus(errOpen, oe.error)
	case errors.As(err, &ie):
		return yqStatus(errInput, err)
	}
	return yqStatus(errRuntime, err)
}

// handleYQ runs a parsed graft yq command line with the streams it reads
// and writes, following yq 4.54.1's main loop: eval runs the expression
// once for each document, eval-all runs it once over every document, any
// error ends the run, and every failure exits 1.
func handleYQ(opts yqOptions, stdin io.Reader, stdout, stderr io.Writer) query.Status {
	if err := checkYQOptions(opts); err != nil {
		return yqStatus(errUsage, err)
	}
	if opts.Repl {
		return runYQRepl(opts, stdin, stdout)
	}
	if opts.Verbose {
		log.DebugOn = true
	}
	log.DEBUG("expression: %s", opts.Expr)
	if opts.OutputFormat == yqFormatAuto && opts.InputFormat != yqFormatAuto && yqJSONNamed(opts) {
		_, _ = io.WriteString(stderr, colored("Y", "Warning")+": "+yqAutoOutputWarning+"\n")
	}

	ev, err := yq.Compile(opts.Expr, yqCompileOptions(opts, stderr)...)
	if err != nil {
		return yqCompileFailure(err)
	}

	split, st, ok := newYQSplit(opts, stderr)
	if !ok {
		return st
	}

	src, st, ok := newYQSource(opts, stdin, stderr)
	if !ok {
		return st
	}
	for _, f := range src.files {
		log.DEBUG("input: %s", f)
	}

	var rewritten bytes.Buffer
	out := newYQRunOutput(opts, split, stdout, &rewritten)

	run := &yqRun{where: unknownSource}
	ctx := context.Background()
	if opts.All {
		run.err = src.evalAll(ctx, ev, run, out)
	} else {
		run.err = src.evalEach(ctx, ev, run, out)
	}
	if ferr := out.flush(); ferr != nil && run.err == nil {
		run.err = ferr
	}
	if run.err != nil {
		return yqFailure(run.err)
	}
	return finishYQ(opts, run, rewritten.Bytes())
}

// newYQRunOutput is the output of a run, which writes to the split files
// when there is a split writer and to the stream or the -i buffer
// otherwise.
func newYQRunOutput(opts yqOptions, split func(yq.Format) func(yq.Document, *value.Value) (io.WriteCloser, error), stdout io.Writer, rewritten *bytes.Buffer) *yqOutput {
	format := yqOutputFormat(opts)
	if split == nil {
		return newYQOutput(opts, format, yqTarget(opts, stdout, rewritten), yqColor(opts, stdout))
	}
	sink := &splitSink{w: io.Discard}
	opts.NulOutput = false
	out := newYQOutput(opts, format, sink, false)
	out.split, out.sink = split(format), sink
	return out
}

// newYQSplit compiles the file-name expression of -s or --split-exp-file.
// It returns nil when neither was given, and otherwise a function that
// builds the split writer for an output format. When the expression
// cannot be read or compiled it returns the status to end with and false.
func newYQSplit(opts yqOptions, stderr io.Writer) (func(yq.Format) func(yq.Document, *value.Value) (io.WriteCloser, error), query.Status, bool) {
	expr, err := yqSplitExpression(opts)
	if err != nil {
		return nil, yqArgsError(err), false
	}
	if expr == "" {
		return nil, query.Status{}, true
	}
	ev, err := yq.Compile(expr, yqCompileOptions(opts, stderr)...)
	if err != nil {
		return nil, yqStatus(errCompile, fmt.Errorf("bad split document expression: %w", err)), false
	}
	return func(format yq.Format) func(yq.Document, *value.Value) (io.WriteCloser, error) {
		return splitWriter(expr, ev, yqSplitExtension(format))
	}, query.Status{}, true
}

// newYQSource builds the source of the documents a run reads, composing
// the inputs first under -m and -E. When composing fails it returns the
// status to end with and false.
func newYQSource(opts yqOptions, stdin io.Reader, stderr io.Writer) (*yqSource, query.Status, bool) {
	src := &yqSource{opts: opts, stdin: stdin, files: opts.Files}
	if len(src.files) == 0 {
		src.files = []string{"-"}
	}
	if opts.Merge || opts.Eval {
		merged, errText, code := composeQueryInputs(newComposeContext(opts, stdin, opts.Engine), opts.Files)
		if code != 0 {
			_, _ = stderr.Write(errText)
			return nil, query.Status{Code: yqFailed}, false
		}
		src.composed = merged
	}
	return src, query.Status{}, true
}

// finishYQ writes the rewritten file under -i and works out the exit
// status from what the run produced.
func finishYQ(opts yqOptions, run *yqRun, rewritten []byte) query.Status {
	if opts.Inplace {
		if err := writeInplace(opts.Files[0], rewritten); err != nil {
			return yqStatus(errRuntime, &locatedError{Where: opts.Files[0], Err: err})
		}
	}
	code := yq.ExitCode(run.produced, run.anyTruthy, nil, opts.ExitStatus)
	if code != 0 {
		return query.Status{Code: code, Message: colored("R", "No matches found") + "\n"}
	}
	return query.Status{}
}

// yqTarget is where the output goes, which is the buffer that replaces the
// first file under -i and the stream otherwise.
func yqTarget(opts yqOptions, stdout io.Writer, rewritten *bytes.Buffer) io.Writer {
	if opts.Inplace {
		return rewritten
	}
	return stdout
}

// yqColor reports whether the output is colored, which it never is when
// it replaces a file.
func yqColor(opts yqOptions, stdout io.Writer) bool {
	if opts.Inplace || opts.NoColors {
		return false
	}
	return opts.Colors || (os.Getenv("NO_COLOR") == "" && stdoutIsTTY(stdout))
}

// writeInplace replaces the file at path with data. The data goes to a
// temporary file in the same directory first, with the original's mode,
// and the rename happens only after the write succeeded, so a failure
// leaves the original as it was.
func writeInplace(path string, data []byte) error {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return err
	}
	info, err := os.Stat(target)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(target), ".graft-yq-*")
	if err != nil {
		return err
	}
	done := false
	defer func() {
		if !done {
			_ = tmp.Close()
			_ = os.Remove(tmp.Name())
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return err
	}
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp.Name(), target); err != nil {
		return err
	}
	done = true
	return nil
}

// runYQRepl is `graft yq --repl [EXPR] FILE...`: the debugger over the
// files, with the yq dialect and the expression run once if one was given.
func runYQRepl(opts yqOptions, stdin io.Reader, stdout io.Writer) query.Status {
	init := debugInit{Dialect: debugDialectYQ}
	if opts.HasExpr {
		init.Query = opts.Expr
	}
	return runQueryRepl("graft yq", opts.Files, init, opts.Engine, opts.Theme, stdin, stdout)
}

// yqStdout is graft yq's stdout, written through the printStdOutf seam.
type yqStdout = jqStdout

// yqStderr is graft yq's stderr, written through log.PrintStdErrf.
type yqStderr = jqStderr

// newYQCmd returns the graft yq command. Its arguments follow yq's own
// rules rather than cobra's, so it parses them itself and hands any of
// graft's persistent flags back to the root's flag set.
func newYQCmd() *cobra.Command {
	return newYQCmdFor(personaGraft)
}

// newYQCmdFor returns the yq command for a persona, which is graft when it
// is a subcommand of the graft root and yq when it is the root itself.
func newYQCmdFor(pers persona) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "yq [flags] [EXPRESSION] [FILE...]",
		Short: "Run a yq expression over YAML and JSON input",
		Long: "Run a yq expression over YAML and JSON input, the way yq " + yqCompatVersion + " does.\n\n" +
			"graft yq takes yq's flags and reads YAML or JSON from each FILE or from stdin.\n" +
			"It keeps comments, styles, anchors, and tags, and writes YAML, or JSON with\n" +
			"-o json. The eval command (alias e) runs the expression on each document and\n" +
			"is the default, and eval-all (alias ea) runs it once over every document.\n" +
			"-S and --sort-keys[=MODE] sort object keys, where MODE is lexical, spruce, or\n" +
			"none, and --float-numbers does arithmetic in int64 and doubles as yq does.",
		DisableFlagParsing: true,
		Args:               cobra.ArbitraryArgs,
		// The root's hook runs from RunE once graft's own flags are
		// parsed out of the yq arguments.
		PersistentPreRunE: func(*cobra.Command, []string) error { return nil },
		RunE: func(cmd *cobra.Command, args []string) error {
			var opts yqOptions
			return runQueryCmd(cmd, args, queryCmdSpec{
				asRoot: pers == personaYQ,
				parse: func(a []string) ([]string, bool, bool, bool, error) {
					var err error
					opts, err = parseYQArgs(a, pers)
					return opts.RootArgs, opts.Help, opts.Version, opts.Merge || opts.Eval || opts.Repl, err
				},
				argsStatus: yqArgsError,
				version:    yqVersionOutput,
				configure:  func(theme string, engine engineOptions) { opts.Theme, opts.Engine = theme, engine },
				run:        func() query.Status { return handleYQ(opts, os.Stdin, yqStdout{}, yqStderr{}) },
			})
		},
	}
	cmd.SetHelpFunc(func(c *cobra.Command, a []string) { yqHelp(c, a, pers) })
	return cmd
}

// yqFlagsHelp summarizes the flags graft yq reads after the verb.
const yqFlagsHelp = `Commands:
  eval, e                     run the expression on each document of each file (the default)
  eval-all, ea                run the expression once over every document of every file

Flags:
  -n, --null-input            run the expression once on null, reading no input
  -i, --inplace               rewrite the first file with the result
  -e, --exit-status           exit 1 when nothing matches, or every output is null or false
  -o, --output-format FORMAT  print auto, yaml (also y), or json (also j)
  -p, --input-format FORMAT   read input as auto, yaml, or json
  -I, --indent N              indent with N spaces (default 2), and 0 prints JSON on one line
  -N, --no-doc                leave out the --- line between YAML documents
  -P, --prettyPrint           drop quoting, flow, and block styles from YAML output
  -r, --unwrapScalar          print a top-level string with no quotes (default true)
  -0, --nul-output            end each output with a NUL
  -C, --colors                color the output
  -M, --no-colors             never color the output
  -c, --yaml-compact-seq-indent
                              accepted, no effect yet
  -S, --sort-keys[=MODE]      sort object keys, where MODE is lexical, spruce, or none
      --float-numbers         do arithmetic in int64 and doubles, as yq does
      --expression EXPR       take the expression from EXPR, so every argument is a file
      --from-file FILE        read the expression from FILE
      --header-preprocess     accepted, no effect yet (default true)
      --string-interpolation  accepted, no effect yet (default true)
      --security-disable-env-ops, --security-disable-file-ops
                              turn off the env or the file operators
      --security-enable-system-operator
                              accepted, since graft yq has no system operator
      --yaml-fix-merge-anchor-to-spec
                              make a merge key follow the YAML specification
  -f, --front-matter MODE     with extract, read the YAML at the head of the first file, and with
                              process, print the text after it too
  -s, --split-exp EXPR        write each output to a file named by EXPR
      --split-exp-file FILE   read the file-name expression from FILE
  -m, --merge                 merge every input with graft merge before the expression runs
  -E, --eval                  resolve each input document on its own before the expression runs
      --prune KEY             with -m or -E, prune KEY from the result (repeatable)
      --cherry-pick KEY       with -m or -E, keep only KEY in the result (repeatable)
      --skip-eval, --fallback-append, --go-patch, --multi-doc, --skip-vault,
      --skip-aws, --skip-nats
                              with -m or -E, as graft merge reads them
      --repl                  open the debugger on the files, with the expression run once
  -V, --version               print the version
  -h, --help                  print this help
`

// yqHelp prints graft yq's help. Its flags are yq's, which cobra cannot
// list because the command parses them itself, and graft's own flags are
// listed only in the forms graft yq takes after the verb, which leaves
// out -D and -T, and -v, since -v is graft's version there and verbose
// under the yq name.
func yqHelp(cmd *cobra.Command, _ []string, pers persona) {
	if pers == personaYQ {
		_, _ = io.WriteString(cmd.OutOrStdout(), cmd.Long+"\n\n"+
			"Usage:\n"+
			"  yq [flags] [expression] [file...]\n"+
			"  yq eval [flags] [expression] [file...]\n"+
			"  yq eval-all [flags] [expression] [file...]\n\n"+
			yqFlagsHelp+
			"  -v, --verbose               verbose mode\n")
		return
	}
	global := pflag.NewFlagSet("", pflag.ContinueOnError)
	cmd.Root().PersistentFlags().VisitAll(func(f *pflag.Flag) {
		if f.Name == flagVersion {
			return
		}
		shown := *f
		shown.Shorthand = ""
		global.AddFlag(&shown)
	})
	_, _ = io.WriteString(cmd.OutOrStdout(), cmd.Long+"\n\n"+
		"Usage:\n"+
		"  graft yq [flags] [EXPRESSION] [FILE...]\n"+
		"  graft yq eval [flags] [EXPRESSION] [FILE...]\n"+
		"  graft yq eval-all [flags] [EXPRESSION] [FILE...]\n\n"+
		yqFlagsHelp+
		"  -v                          print graft's version\n\n"+
		"Global Flags:\n"+global.FlagUsages())
}
