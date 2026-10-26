package main

import (
	"errors"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/fivetwenty-io/graft/pkg/graft"
	"github.com/fivetwenty-io/graft/pkg/jq"
	"github.com/fivetwenty-io/graft/pkg/query/value"
	"github.com/fivetwenty-io/graft/pkg/sortkeys"
)

// jqOptions is a graft jq command line after parsing, with one field per
// flag in graft jq's flag table.
type jqOptions struct {
	// Input.
	NullInput   bool   // -n
	RawInput    bool   // -R
	Slurp       bool   // -s
	InputFormat string // --input-format: auto, json, or yaml
	Seq         bool   // --seq
	Stream      bool   // --stream
	StreamErrs  bool   // --stream-errors

	// Output layout. -c, --tab, and --indent replace one another, and the
	// last one given wins, as in jq.
	Compact   bool // -c
	Tab       bool // --tab, or --indent -1
	Indent    int  // --indent n, when IndentSet
	IndentSet bool

	// Output text.
	Raw          bool          // -r
	Join         bool          // -j
	RawOutput0   bool          // --raw-output0
	ASCII        bool          // -a
	SortKeys     sortkeys.Mode // -S or --sort-keys[=MODE]
	Output       string        // -o: json, j, yaml, or y
	FloatNumbers bool          // --float-numbers
	DepthLimit   int           // --depth-limit, or zero for the default
	ColorOutput  bool          // -C
	Monochrome   bool          // -M
	Unbuffered   bool          // --unbuffered

	// The program and its arguments.
	Program      string // the first positional, or the program file under -f
	HasProgram   bool
	FromFile     bool                    // -f
	Args         map[string]*value.Value // --arg, --argjson, --slurpfile, --rawfile
	ArgNames     []string                // the names in Args, in the order given
	Positional   []*value.Value          // --args and --jsonargs
	LibraryPaths []string                // -L
	Files        []string

	// Engine is the engine options for -m and -E, which the command fills
	// in from the root's config before it runs.
	Engine []graft.EngineOption

	// Composition. -m merges every input before the program runs, and -E
	// resolves each input document on its own. The rest are graft
	// merge's own flags, which both modes pass through to the merge.
	Merge          bool     // -m, --merge
	Eval           bool     // -E, --eval
	Prune          []string // --prune
	CherryPick     []string // --cherry-pick
	SkipEval       bool     // --skip-eval
	FallbackAppend bool     // --fallback-append
	GoPatch        bool     // --go-patch
	MultiDoc       bool     // --multi-doc
	SkipVault      bool     // --skip-vault
	SkipAws        bool     // --skip-aws
	SkipNats       bool     // --skip-nats

	// Modes that replace a normal run.
	ExitStatus   bool   // -e
	Help         bool   // -h
	Version      bool   // -V
	Repl         bool   // --repl
	RunTests     bool   // --run-tests
	RunTestsFile string // --run-tests FILE, or stdin when empty

	// RootArgs are graft's own persistent flags given to graft jq, in the
	// order given, for the command to hand back to the root's flag set.
	RootArgs []string
	// Theme is the resolved color theme, which the command fills in from
	// the root's flags before it runs.
	Theme string
}

// usageError is a command line that graft jq refuses. Phrase leads its
// diagnostic and is "Invalid flags" when empty.
type usageError struct {
	Msg    string
	Phrase string
}

func (e *usageError) Error() string { return e.Msg }

// The -o values, and the --input-format values beside auto.
const (
	jqFormatJSON = "json"
	jqFormatYAML = "yaml"
)

// jqEvalFlag is --eval, which shares its name with a graft command.
const jqEvalFlag = "eval"

// jqHelpFlag is --help, which graft jq reads as jq does rather than
// through cobra.
const jqHelpFlag = "help"

// The usage messages for options whose short and long forms share one.
const (
	libraryPathUsage = "-L takes a parameter: (e.g. -L /search/path or -L/search/path)"
	outputUsage      = "-o takes a parameter (json, yaml, j, or y)"
)

// rootFlagTakesValue lists the root's persistent flags that graft jq
// accepts under the graft name, by long name, and whether each one takes
// a value as the next argument. --color takes its optional value only
// after =, as pflag reads it. --version and --help are jq's own flags
// here, so they are left out.
var rootFlagTakesValue = map[string]bool{
	"debug":               false,
	"trace":               false,
	"color":               false,
	"no-color":            false,
	"theme":               true,
	"config":              true,
	"max-loop-iterations": true,
}

// jqShortFlags are the short options that take no value, by letter.
var jqShortFlags = map[byte]func(*jqOptions){
	's': func(o *jqOptions) { o.Slurp = true },
	'r': func(o *jqOptions) { o.Raw = true },
	'j': func(o *jqOptions) { o.Raw, o.Join = true, true },
	'c': (*jqOptions).setCompact,
	'C': func(o *jqOptions) { o.ColorOutput = true },
	'M': func(o *jqOptions) { o.Monochrome = true },
	'a': func(o *jqOptions) { o.ASCII = true },
	'S': func(o *jqOptions) { o.SortKeys = sortkeys.Lexical },
	'R': func(o *jqOptions) { o.RawInput = true },
	'n': func(o *jqOptions) { o.NullInput = true },
	'm': func(o *jqOptions) { o.Merge = true },
	'E': func(o *jqOptions) { o.Eval = true },
	'f': func(o *jqOptions) { o.FromFile = true },
	'e': func(o *jqOptions) { o.ExitStatus = true },
	'h': func(o *jqOptions) { o.Help = true },
	'V': func(o *jqOptions) { o.Version = true },
}

// jqLongFlags are the long options that take no value, by name.
var jqLongFlags = map[string]func(*jqOptions){
	"slurp":             func(o *jqOptions) { o.Slurp = true },
	"raw-output":        func(o *jqOptions) { o.Raw = true },
	"raw-output0":       func(o *jqOptions) { o.Raw, o.Join, o.RawOutput0 = true, true, true },
	"join-output":       func(o *jqOptions) { o.Raw, o.Join = true, true },
	"compact-output":    (*jqOptions).setCompact,
	"color-output":      func(o *jqOptions) { o.ColorOutput = true },
	"monochrome-output": func(o *jqOptions) { o.Monochrome = true },
	"ascii-output":      func(o *jqOptions) { o.ASCII = true },
	"unbuffered":        func(o *jqOptions) { o.Unbuffered = true },
	"sort-keys":         func(o *jqOptions) { o.SortKeys = sortkeys.Lexical },
	"raw-input":         func(o *jqOptions) { o.RawInput = true },
	"null-input":        func(o *jqOptions) { o.NullInput = true },
	"from-file":         func(o *jqOptions) { o.FromFile = true },
	"tab":               func(o *jqOptions) { o.Compact, o.IndentSet, o.Tab = false, false, true },
	"seq":               func(o *jqOptions) { o.Seq = true },
	"stream":            func(o *jqOptions) { o.Stream = true },
	"stream-errors":     func(o *jqOptions) { o.Stream, o.StreamErrs = true, true },
	"exit-status":       func(o *jqOptions) { o.ExitStatus = true },
	jqHelpFlag:          func(o *jqOptions) { o.Help = true },
	"version":           func(o *jqOptions) { o.Version = true },
	"repl":              func(o *jqOptions) { o.Repl = true },
	"float-numbers":     func(o *jqOptions) { o.FloatNumbers = true },
	"merge":             func(o *jqOptions) { o.Merge = true },
	jqEvalFlag:          func(o *jqOptions) { o.Eval = true },
	"skip-eval":         func(o *jqOptions) { o.SkipEval = true },
	"fallback-append":   func(o *jqOptions) { o.FallbackAppend = true },
	"go-patch":          func(o *jqOptions) { o.GoPatch = true },
	"multi-doc":         func(o *jqOptions) { o.MultiDoc = true },
	"skip-vault":        func(o *jqOptions) { o.SkipVault = true },
	"skip-aws":          func(o *jqOptions) { o.SkipAws = true },
	"skip-nats":         func(o *jqOptions) { o.SkipNats = true },
}

// jqLongValueFlags are the long options that read the arguments after
// them, or change how later positionals are read, by name.
var jqLongValueFlags = map[string]func(*jqParser) error{
	"library-path": func(p *jqParser) error {
		vals, err := p.need(1, libraryPathUsage)
		if err == nil {
			p.o.LibraryPaths = append(p.o.LibraryPaths, vals[0])
		}
		return err
	},
	"indent":       (*jqParser).indent,
	"arg":          (*jqParser).arg,
	"argjson":      (*jqParser).argJSON,
	"slurpfile":    func(p *jqParser) error { return p.argFile("slurpfile") },
	"rawfile":      func(p *jqParser) error { return p.argFile("rawfile") },
	"args":         func(p *jqParser) error { p.stringArgs, p.jsonArgs = true, false; return nil },
	"jsonargs":     func(p *jqParser) error { p.stringArgs, p.jsonArgs = false, true; return nil },
	"run-tests":    (*jqParser).runTests,
	"input-format": (*jqParser).inputFormat,
	"depth-limit":  (*jqParser).depthLimit,
	"prune":        func(p *jqParser) error { return p.mergeKeys("prune", &p.o.Prune) },
	"cherry-pick":  func(p *jqParser) error { return p.mergeKeys("cherry-pick", &p.o.CherryPick) },
}

// jqParser is the state of one parseJQArgs run.
type jqParser struct {
	o       jqOptions
	args    []string
	i       int
	persona persona

	// stringArgs and jsonArgs are --args and --jsonargs, which make later
	// positionals $ARGS.positional values, and done is a bare --.
	stringArgs, jsonArgs, done bool
}

// isOptish reports whether arg reads as an option, which is a dash followed
// by a letter or a second dash, so that "-" and "-1" are positionals.
func isOptish(arg string) bool {
	if len(arg) < 2 || arg[0] != '-' {
		return false
	}
	return arg[1] == '-' || ('a' <= arg[1] && arg[1] <= 'z') || ('A' <= arg[1] && arg[1] <= 'Z')
}

// parseJQArgs reads a jq command line the way jq 1.8.2's main does. Short
// flags combine, as in -nrc. Options are recognized anywhere until a bare
// --, including after --args and --jsonargs. The first positional is the
// program, and the rest are input files, or $ARGS.positional values once
// --args or --jsonargs has been given. The first binding for a name wins.
// -h, -V, and --run-tests end the parse, as they end jq's. Under the graft
// persona the root's persistent flags are accepted by their long names and
// collected in RootArgs.
func parseJQArgs(args []string, pers persona) (jqOptions, error) {
	p := &jqParser{o: jqOptions{Args: map[string]*value.Value{}}, args: args, persona: pers}
	for ; p.i < len(args) && !p.stopped(); p.i++ {
		arg := args[p.i]
		var err error
		switch {
		case p.done || !isOptish(arg):
			err = p.positional(arg)
		case arg == "--":
			p.done = true
		case strings.HasPrefix(arg, "--"):
			err = p.long(arg[2:])
		default:
			err = p.short(arg[1:])
		}
		if err != nil {
			return p.o, err
		}
	}
	return p.o, nil
}

// stopped reports whether an option has ended the parse.
func (p *jqParser) stopped() bool {
	return p.o.Help || p.o.Version || p.o.RunTests
}

// positional reads the program, an input file, or a positional value.
func (p *jqParser) positional(arg string) error {
	switch {
	case !p.o.HasProgram:
		p.o.Program, p.o.HasProgram = arg, true
	case p.stringArgs:
		p.o.Positional = append(p.o.Positional, value.Str(arg))
	case p.jsonArgs:
		v, err := decodeJQArg(arg, jq.FormatJSON)
		if err != nil {
			return &usageError{Msg: "invalid JSON text passed to --jsonargs"}
		}
		p.o.Positional = append(p.o.Positional, v)
	default:
		p.o.Files = append(p.o.Files, arg)
	}
	return nil
}

// need returns the n arguments after the current one, or a usage error
// with msg when there are fewer.
func (p *jqParser) need(n int, msg string) ([]string, error) {
	if p.i+n >= len(p.args) {
		return nil, &usageError{Msg: msg}
	}
	vals := p.args[p.i+1 : p.i+1+n]
	p.i += n
	return vals, nil
}

// short reads a group of combined short options. -L and -o take the rest
// of the group as their value, or the next argument when the group ends
// with them.
func (p *jqParser) short(group string) error {
	for k := 0; k < len(group) && !p.stopped(); k++ {
		c := group[k]
		if set, ok := jqShortFlags[c]; ok {
			set(&p.o)
			continue
		}
		if c != 'L' && c != 'o' {
			return &usageError{Phrase: "Unknown option", Msg: "-" + string(rune(c))}
		}
		val := group[k+1:]
		if val == "" {
			msg := libraryPathUsage
			if c == 'o' {
				msg = outputUsage
			}
			vals, err := p.need(1, msg)
			if err != nil {
				return err
			}
			val = vals[0]
		}
		if c == 'o' {
			return p.o.setOutput(val)
		}
		p.o.LibraryPaths = append(p.o.LibraryPaths, val)
		return nil
	}
	return nil
}

// long reads one long option.
func (p *jqParser) long(name string) error {
	if set, ok := jqLongFlags[name]; ok {
		set(&p.o)
		return nil
	}
	if read, ok := jqLongValueFlags[name]; ok {
		return read(p)
	}
	return p.longWithValue(name)
}

// bind sets $name unless an earlier option already did.
func (p *jqParser) bind(name string, v *value.Value) {
	if _, ok := p.o.Args[name]; ok {
		return
	}
	p.o.Args[name] = v
	p.o.ArgNames = append(p.o.ArgNames, name)
}

func (p *jqParser) indent() error {
	vals, err := p.need(1, "--indent takes one parameter")
	if err != nil {
		return err
	}
	n, err := strconv.Atoi(vals[0])
	if err != nil || n < -1 || n > 7 {
		return &usageError{Msg: "--indent takes a number between -1 and 7"}
	}
	p.o.setIndent(n)
	return nil
}

// depthLimit reads --depth-limit N, the bound on call nesting and on input
// nesting together. The values --argjson, --slurpfile, and --rawfile read
// are decoded as their options are met, so they keep the default bound.
func (p *jqParser) depthLimit() error {
	const usage = "--depth-limit takes a positive number"
	vals, err := p.need(1, usage)
	if err != nil {
		return err
	}
	n, err := strconv.Atoi(vals[0])
	if err != nil || n < 1 {
		return &usageError{Msg: usage}
	}
	p.o.DepthLimit = n
	return nil
}

func (p *jqParser) arg() error {
	vals, err := p.need(2, "--arg takes two parameters (e.g. --arg varname value)")
	if err == nil {
		p.bind(vals[0], value.Str(vals[1]))
	}
	return err
}

func (p *jqParser) argJSON() error {
	vals, err := p.need(2, "--argjson takes two parameters (e.g. --argjson varname text)")
	if err != nil {
		return err
	}
	if _, ok := p.o.Args[vals[0]]; ok {
		return nil
	}
	v, err := decodeJQArg(vals[1], jq.FormatAuto)
	if err != nil {
		return &usageError{Msg: "invalid JSON text passed to --argjson"}
	}
	p.bind(vals[0], v)
	return nil
}

// argFile reads --slurpfile or --rawfile, whose file is read when the
// option is, as jq reads it.
func (p *jqParser) argFile(name string) error {
	vals, err := p.need(2, "--"+name+" takes two parameters (e.g. --"+name+" varname filename)")
	if err != nil {
		return err
	}
	if _, ok := p.o.Args[vals[0]]; ok {
		return nil
	}
	v, err := loadJQArgFile(vals[1], name == "rawfile")
	if err != nil {
		var pe *os.PathError
		if errors.As(err, &pe) {
			return err
		}
		return &usageError{Phrase: "Invalid input", Msg: "--" + name + " " + vals[0] + " " + vals[1] + ": " + err.Error()}
	}
	p.bind(vals[0], v)
	return nil
}

// runTests reads --run-tests, which takes the next argument as its file
// when there is one and ends the parse.
func (p *jqParser) runTests() error {
	p.o.RunTests = true
	if p.i+1 < len(p.args) {
		p.o.RunTestsFile = p.args[p.i+1]
	}
	return nil
}

// mergeKeys reads --prune or --cherry-pick, each of which names one key
// path and may be given more than once.
func (p *jqParser) mergeKeys(name string, into *[]string) error {
	vals, err := p.need(1, "--"+name+" takes a parameter")
	if err == nil {
		*into = append(*into, vals[0])
	}
	return err
}

func (p *jqParser) inputFormat() error {
	vals, err := p.need(1, "--input-format takes a parameter (auto, json, or yaml)")
	if err != nil {
		return err
	}
	return p.o.setInputFormat(vals[0])
}

// longWithValue reads the long options that are graft's own: the
// --name=VALUE forms of graft jq's extensions and, under the graft
// persona, the root's persistent flags.
func (p *jqParser) longWithValue(name string) error {
	key, val, hasVal := strings.Cut(name, "=")
	switch {
	case key == "sort-keys" && hasVal:
		mode, err := sortkeys.Parse(val)
		if err != nil {
			return &usageError{Phrase: "Invalid --sort-keys value", Msg: err.Error()}
		}
		p.o.SortKeys = mode
		return nil
	case key == "input-format" && hasVal:
		return p.o.setInputFormat(val)
	case key == "prune" && hasVal:
		p.o.Prune = append(p.o.Prune, val)
		return nil
	case key == "cherry-pick" && hasVal:
		p.o.CherryPick = append(p.o.CherryPick, val)
		return nil
	}
	takesValue, isRoot := rootFlagTakesValue[key]
	if p.persona != personaGraft || !isRoot {
		return &usageError{Phrase: "Unknown option", Msg: "--" + name}
	}
	if !takesValue || hasVal {
		p.o.RootArgs = append(p.o.RootArgs, p.args[p.i])
		return nil
	}
	vals, err := p.need(1, "--"+key+" takes a parameter")
	if err == nil {
		p.o.RootArgs = append(p.o.RootArgs, "--"+key, vals[0])
	}
	return err
}

func (o *jqOptions) setCompact() {
	o.Compact, o.Tab, o.IndentSet = true, false, false
}

// setIndent applies --indent n, where -1 means a tab.
func (o *jqOptions) setIndent(n int) {
	o.Compact, o.Tab, o.IndentSet, o.Indent = false, false, false, 0
	if n == -1 {
		o.Tab = true
		return
	}
	o.IndentSet, o.Indent = true, n
}

func (o *jqOptions) setOutput(val string) error {
	switch val {
	case jqFormatJSON, "j", jqFormatYAML, "y":
		o.Output = val
		return nil
	}
	return &usageError{Phrase: "Invalid -o value", Msg: val + " (json, yaml, j, or y)"}
}

func (o *jqOptions) setInputFormat(val string) error {
	switch val {
	case "auto", jqFormatJSON, jqFormatYAML:
		o.InputFormat = val
		return nil
	}
	return &usageError{Phrase: "Invalid --input-format value", Msg: val + " (auto, json, or yaml)"}
}

// yamlOutput reports whether -o selects YAML.
func (o *jqOptions) yamlOutput() bool {
	return o.Output == jqFormatYAML || o.Output == "y"
}

// decodeJQArg reads exactly one value from text in format, which is JSON
// alone for --jsonargs, as in jq, and JSON or YAML for --argjson.
func decodeJQArg(text string, format jq.Format) (*value.Value, error) {
	dec := jq.NewDecoder(strings.NewReader(text), jq.DecodeOptions{Format: format})
	v, err := dec.Next()
	if err != nil {
		return nil, err
	}
	if _, err := dec.Next(); !errors.Is(err, io.EOF) {
		return nil, errors.New("more than one value")
	}
	return v, nil
}

// loadJQArgFile reads a file for --rawfile, as one string, or for
// --slurpfile, as an array of every value in it.
func loadJQArgFile(path string, raw bool) (*value.Value, error) {
	data, err := os.ReadFile(path) // #nosec G304 -- the user names the file
	if err != nil {
		return nil, err
	}
	if raw {
		return value.Str(string(data)), nil
	}
	return jq.Slurp(jq.NewDecoder(strings.NewReader(string(data)), jq.DecodeOptions{})).Next()
}
