package main

import (
	"os"
	"strconv"
	"strings"

	"github.com/fivetwenty-io/graft/pkg/graft"
	"github.com/fivetwenty-io/graft/pkg/sortkeys"
)

// Words the jq and yq command lines both spell out.
const (
	unknownSource       = "<unknown>"
	flagVersion         = "version"
	flagSortKeys        = "sort-keys"
	flagNullInput       = "null-input"
	flagFromFile        = "from-file"
	flagExitStatus      = "exit-status"
	flagIndent          = "indent"
	flagInputFormat     = "input-format"
	flagCherryPick      = "cherry-pick"
	phraseUnknownOption = "Unknown option"
)

// yqOptions is a graft yq command line after parsing, with one field per
// flag in graft yq's flag table.
type yqOptions struct {
	// Input.
	NullInput        bool   // -n
	InputFormat      string // -p, --input-format: auto, yaml, or json
	FrontMatter      string // -f, --front-matter
	HeaderPreprocess bool   // --header-preprocess, true unless =false

	// Output.
	OutputFormat      string // -o, --output-format: auto, yaml, or json
	Indent            int    // -I, --indent, when IndentSet
	IndentSet         bool
	NoDoc             bool          // -N
	PrettyPrint       bool          // -P
	UnwrapScalar      bool          // -r, true unless =false
	NulOutput         bool          // -0
	Colors            bool          // -C
	NoColors          bool          // -M
	CompactSeqIndent  bool          // -c
	SortKeys          sortkeys.Mode // -S or --sort-keys[=MODE]
	FloatNumbers      bool          // --float-numbers
	StringInterpolate bool          // --string-interpolation, true unless =false

	// What a run does with its result.
	Inplace    bool // -i
	ExitStatus bool // -e

	// Sandboxing, which maps onto the engine's capabilities.
	DisableEnvOps  bool // --security-disable-env-ops
	DisableFileOps bool // --security-disable-file-ops
	EnableSystem   bool // --security-enable-system-operator
	MergeAnchorFix bool // --yaml-fix-merge-anchor-to-spec

	// The expression and the files it runs over. Expr is "." when none
	// was given, and HasExpr says whether one was.
	All     bool // eval-all
	Expr    string
	HasExpr bool
	Files   []string

	// Engine is the engine options for -m, -E, and --repl, which the
	// command fills in from the root's config before it runs.
	Engine []graft.EngineOption

	// Composition, as graft jq has it.
	composeFlags

	// Modes that replace a normal run.
	Help    bool // -h
	Version bool // -V, or -v under the graft name
	Verbose bool // -v under the yq name
	Repl    bool // --repl

	// RootArgs are graft's own persistent flags given to graft yq, in the
	// order given, for the command to hand back to the root's flag set.
	RootArgs []string
	// Theme is the resolved color theme, which the command fills in from
	// the root's flags before it runs.
	Theme string
}

// The -o and -p values, once normalized.
const (
	yqFormatAuto = "auto"
	yqFormatYAML = jqFormatYAML
	yqFormatJSON = jqFormatJSON
)

// yqLaterRelease is what a flag that belongs to a later release answers.
const yqLaterRelease = " is not available until a later release"

// yqBoolFlags are the options that take no value, by long name, with the
// short letter that spells each one in a group. A bool also takes =true
// or =false on its long name, and on its short letter in the last place.
var yqBoolFlags = map[string]func(*yqOptions, bool){
	"colors":                          func(o *yqOptions, v bool) { o.Colors = v },
	"no-colors":                       func(o *yqOptions, v bool) { o.NoColors = v },
	flagExitStatus:                    func(o *yqOptions, v bool) { o.ExitStatus = v },
	"inplace":                         func(o *yqOptions, v bool) { o.Inplace = v },
	flagNullInput:                     func(o *yqOptions, v bool) { o.NullInput = v },
	"no-doc":                          func(o *yqOptions, v bool) { o.NoDoc = v },
	"prettyPrint":                     func(o *yqOptions, v bool) { o.PrettyPrint = v },
	"unwrapScalar":                    func(o *yqOptions, v bool) { o.UnwrapScalar = v },
	"nul-output":                      func(o *yqOptions, v bool) { o.NulOutput = v },
	"yaml-compact-seq-indent":         func(o *yqOptions, v bool) { o.CompactSeqIndent = v },
	"header-preprocess":               func(o *yqOptions, v bool) { o.HeaderPreprocess = v },
	"string-interpolation":            func(o *yqOptions, v bool) { o.StringInterpolate = v },
	"security-disable-env-ops":        func(o *yqOptions, v bool) { o.DisableEnvOps = v },
	"security-disable-file-ops":       func(o *yqOptions, v bool) { o.DisableFileOps = v },
	"security-enable-system-operator": func(o *yqOptions, v bool) { o.EnableSystem = v },
	"yaml-fix-merge-anchor-to-spec":   func(o *yqOptions, v bool) { o.MergeAnchorFix = v },
	"float-numbers":                   func(o *yqOptions, v bool) { o.FloatNumbers = v },
	"repl":                            func(o *yqOptions, v bool) { o.Repl = v },
	"merge":                           func(o *yqOptions, v bool) { o.Merge = v },
	jqEvalFlag:                        func(o *yqOptions, v bool) { o.Eval = v },
	"skip-eval":                       func(o *yqOptions, v bool) { o.SkipEval = v },
	"fallback-append":                 func(o *yqOptions, v bool) { o.FallbackAppend = v },
	"go-patch":                        func(o *yqOptions, v bool) { o.GoPatch = v },
	"multi-doc":                       func(o *yqOptions, v bool) { o.MultiDoc = v },
	"skip-vault":                      func(o *yqOptions, v bool) { o.SkipVault = v },
	"skip-aws":                        func(o *yqOptions, v bool) { o.SkipAws = v },
	"skip-nats":                       func(o *yqOptions, v bool) { o.SkipNats = v },
	jqHelpFlag:                        func(o *yqOptions, v bool) { o.Help = v },
	flagVersion:                       func(o *yqOptions, v bool) { o.Version = v },
	"verbose":                         func(o *yqOptions, v bool) { o.Verbose = v },
}

// yqShortBools maps the short letters that are bool flags to their long
// names. -v is left out because its meaning depends on the persona.
var yqShortBools = map[byte]string{
	'C': "colors", 'M': "no-colors", 'e': flagExitStatus, 'i': "inplace",
	'n': flagNullInput, 'N': "no-doc", 'P': "prettyPrint", 'r': "unwrapScalar",
	'0': "nul-output", 'c': "yaml-compact-seq-indent", 'm': "merge", 'E': jqEvalFlag,
	'h': jqHelpFlag, 'V': flagVersion,
}

// yqValueFlags are the options that take a value, by long name, with the
// short letter that spells each one.
var yqValueFlags = map[string]byte{
	flagIndent: 'I', "output-format": 'o', flagInputFormat: 'p', "front-matter": 'f',
	"split-exp": 's', "split-exp-file": 0, "expression": 0, flagFromFile: 0,
	"prune": 0, flagCherryPick: 0,
}

// yqParser is the state of one parseYQArgs run.
type yqParser struct {
	o       yqOptions
	args    []string
	i       int
	persona persona

	// command is set once eval or eval-all was read as the first
	// positional, and positionals are everything else that is not a flag.
	command     bool
	positionals []string
	done        bool

	expression, fromFile string
	hasExpression        bool
	hasFromFile          bool
}

// parseYQArgs reads a yq command line the way yq 4.54.1 does. Short bool
// flags combine, as in -ne, and a flag may come before or after the
// expression. A first positional of eval, e, eval-all, or ea picks the
// command. The expression-or-file rule is yq's, so that a first positional
// that names an existing file is a file, and one that ends in .yq is an
// expression file. Under the graft persona the root's persistent flags are
// accepted by their long names and collected in RootArgs, and -v is
// graft's version, where under the yq persona it is verbose.
func parseYQArgs(args []string, pers persona) (yqOptions, error) {
	p := &yqParser{
		o: yqOptions{
			OutputFormat: yqFormatAuto, InputFormat: yqFormatAuto,
			UnwrapScalar: true, HeaderPreprocess: true, StringInterpolate: true,
		},
		args: args, persona: pers,
	}
	for ; p.i < len(args) && !p.stopped(); p.i++ {
		arg := args[p.i]
		var err error
		switch {
		case p.done || (!isOptish(arg) && arg != "-0"):
			p.positional(arg)
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
	if p.stopped() {
		return p.o, nil
	}
	return p.o, p.resolveExpression()
}

// stopped reports whether an option has ended the parse.
func (p *yqParser) stopped() bool {
	return p.o.Help || p.o.Version
}

// positional reads eval or eval-all when it is the first positional, and
// otherwise keeps the argument for the expression-or-file rule.
func (p *yqParser) positional(arg string) {
	if !p.command && len(p.positionals) == 0 && !p.done {
		switch arg {
		case "eval", "e":
			p.command = true
			return
		case "eval-all", "ea":
			p.command, p.o.All = true, true
			return
		}
	}
	p.positionals = append(p.positionals, arg)
}

// need returns the argument after the current one, or a usage error with
// msg when there is none.
func (p *yqParser) need(msg string) (string, error) {
	if p.i+1 >= len(p.args) {
		return "", &usageError{Msg: msg}
	}
	p.i++
	return p.args[p.i], nil
}

// short reads a group of combined short options. -I, -o, -p, -f, and -s
// take the rest of the group as their value, or the next argument when
// the group ends with them, and a bool may take =true or =false last.
func (p *yqParser) short(group string) error {
	for k := 0; k < len(group) && !p.stopped(); k++ {
		c := group[k]
		switch {
		case c == 'v':
			p.o.Version, p.o.Verbose = p.persona == personaGraft, p.persona != personaGraft
		case c == 'S':
			p.o.SortKeys = sortkeys.Lexical
		case yqShortBools[c] != "":
			set := yqBoolFlags[yqShortBools[c]]
			val, hasVal := strings.CutPrefix(group[k+1:], "=")
			if !hasVal {
				set(&p.o, true)
				continue
			}
			b, err := strconv.ParseBool(val)
			if err != nil {
				return &usageError{Msg: "invalid argument " + strconv.Quote(val) + " for -" + string(rune(c))}
			}
			set(&p.o, b)
			return nil
		default:
			name := yqShortValueName(c)
			if name == "" {
				return &usageError{Phrase: phraseUnknownOption, Msg: "-" + string(rune(c))}
			}
			val, hasRest := strings.CutPrefix(group[k+1:], "=")
			if !hasRest {
				val = group[k+1:]
			}
			if val == "" {
				var err error
				if val, err = p.need("-" + string(rune(c)) + " takes a parameter"); err != nil {
					return err
				}
			}
			return p.value(name, val)
		}
	}
	return nil
}

// yqShortValueName is the long name of the value flag a short letter
// spells, or "" when none does.
func yqShortValueName(c byte) string {
	for name, short := range yqValueFlags {
		if short == c && short != 0 {
			return name
		}
	}
	return ""
}

// long reads one long option, whose value follows an = or the next
// argument.
func (p *yqParser) long(name string) error {
	key, val, hasVal := strings.Cut(name, "=")
	if set, ok := yqBoolFlags[key]; ok {
		b := true
		if hasVal {
			var err error
			if b, err = strconv.ParseBool(val); err != nil {
				return &usageError{Msg: "invalid argument " + strconv.Quote(val) + " for --" + key}
			}
		}
		set(&p.o, b)
		return nil
	}
	if key == flagSortKeys {
		p.o.SortKeys = sortkeys.Lexical
		if hasVal {
			mode, err := sortkeys.Parse(val)
			if err != nil {
				return &usageError{Phrase: "Invalid --sort-keys value", Msg: err.Error()}
			}
			p.o.SortKeys = mode
		}
		return nil
	}
	if _, ok := yqValueFlags[key]; ok {
		if !hasVal {
			var err error
			if val, err = p.need("--" + key + " takes a parameter"); err != nil {
				return err
			}
		}
		return p.value(key, val)
	}
	return p.rootFlag(key, hasVal, name)
}

// rootFlag reads one of graft's persistent flags under the graft name,
// and refuses every other long name.
func (p *yqParser) rootFlag(key string, hasVal bool, name string) error {
	takesValue, isRoot := rootFlagTakesValue[key]
	if p.persona != personaGraft || !isRoot {
		return &usageError{Phrase: phraseUnknownOption, Msg: "--" + name}
	}
	if !takesValue || hasVal {
		p.o.RootArgs = append(p.o.RootArgs, p.args[p.i])
		return nil
	}
	vals, err := p.need("--" + key + " takes a parameter")
	if err == nil {
		p.o.RootArgs = append(p.o.RootArgs, "--"+key, vals)
	}
	return err
}

// value applies the value of a flag that takes one.
func (p *yqParser) value(name, val string) error {
	switch name {
	case flagIndent:
		n, err := strconv.Atoi(val)
		if err != nil {
			return &usageError{Msg: "invalid argument " + strconv.Quote(val) + " for -I, --indent"}
		}
		p.o.Indent, p.o.IndentSet = n, true
	case "output-format":
		format, err := yqFormat("-o", val)
		p.o.OutputFormat = format
		return err
	case flagInputFormat:
		format, err := yqFormat("-p", val)
		p.o.InputFormat = format
		return err
	case "front-matter":
		return &usageError{Msg: "-f" + yqLaterRelease}
	case "split-exp":
		return &usageError{Msg: "-s" + yqLaterRelease}
	case "split-exp-file":
		return &usageError{Msg: "--split-exp-file" + yqLaterRelease}
	case "expression":
		p.expression, p.hasExpression = val, true
	case flagFromFile:
		p.fromFile, p.hasFromFile = val, true
	case "prune":
		p.o.Prune = append(p.o.Prune, val)
	case flagCherryPick:
		p.o.CherryPick = append(p.o.CherryPick, val)
	}
	return nil
}

// yqFormat normalizes an -o or -p value, which may be spelled in full or
// by its first letter, and refuses a format graft yq does not carry.
func yqFormat(flag, val string) (string, error) {
	switch val {
	case "auto", "a":
		return yqFormatAuto, nil
	case "yaml", "y":
		return yqFormatYAML, nil
	case "json", "j":
		return yqFormatJSON, nil
	}
	return "", &usageError{Phrase: "Invalid " + flag + " value", Msg: strconv.Quote(val) + " is not built into graft yq (auto, yaml, and json only)"}
}

// maybeYQFile reports whether name is an existing file that is not a
// directory, which is yq's test for a positional that may be a file.
func maybeYQFile(name string) bool {
	info, err := os.Stat(name)
	return err == nil && !info.IsDir()
}

// resolveExpression applies yq's expression-or-file rule to the
// positionals. A positional that is not the expression is a file.
func (p *yqParser) resolveExpression() error {
	args := p.positionals
	expr, fromFile := p.expression, p.fromFile
	hasFile := p.hasFromFile
	if !hasFile && !p.hasExpression && len(args) > 0 && maybeYQFile(args[0]) && strings.HasSuffix(args[0], ".yq") {
		fromFile, hasFile, args = args[0], true, args[1:]
	}
	if hasFile {
		data, err := os.ReadFile(fromFile) // #nosec G304 -- the user names the expression file
		if err != nil {
			return err
		}
		expr = strings.ReplaceAll(string(data), "\r\n", "\n")
		p.o.HasExpr = true
	}
	if p.hasExpression {
		p.o.HasExpr = true
	}
	if !p.o.HasExpr && len(args) > 0 && args[0] != "-" && !maybeYQFile(args[0]) {
		expr, args = args[0], args[1:]
		p.o.HasExpr = true
	}
	if expr == "" {
		expr = "."
	}
	p.o.Expr, p.o.Files = expr, args
	return nil
}
