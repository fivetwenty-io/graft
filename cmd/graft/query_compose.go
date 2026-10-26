package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	"github.com/fivetwenty-io/graft/log"
	"github.com/fivetwenty-io/graft/pkg/graft"
	"github.com/fivetwenty-io/graft/pkg/query"
)

// composedName is the name a composed input goes by in error locations
// and input_filename.
const composedName = "<merged>"

// engineOptions are the options the engine is built with, which the
// root's config loading produces.
type engineOptions = []graft.EngineOption

// startupEngineKey keys the engine options a root's hook loaded in the
// command's context.
type startupEngineKey struct{}

// keepStartupEngine records the engine options a root's hook loaded, so
// -m and -E build their engine from the same config and feature flags.
func keepStartupEngine(cmd *cobra.Command, opts engineOptions) {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	cmd.SetContext(context.WithValue(ctx, startupEngineKey{}, opts))
}

// composeContext is everything composeQueryInputs needs to run graft's
// merge and eval passes ahead of a query.
type composeContext struct {
	Stdin  io.Reader
	Merge  mergeOpts
	Engine engineOptions
	Eval   bool
}

// composeFlags are the composition flags graft jq and graft yq share.
type composeFlags struct {
	Merge, Eval    bool
	Prune          []string
	CherryPick     []string
	SkipEval       bool
	FallbackAppend bool
	GoPatch        bool
	MultiDoc       bool
	SkipVault      bool
	SkipAws        bool
	SkipNats       bool
}

// composeSource is a parsed command line that carries composition flags.
type composeSource interface {
	composition() composeFlags
}

// composition returns the flags themselves, which graft jq and graft yq
// carry by embedding composeFlags.
func (f composeFlags) composition() composeFlags { return f }

// newComposeContext builds the context a parsed command line asks for,
// where the merge options come from the command's pass-through flags.
func newComposeContext(src composeSource, stdin io.Reader, engine engineOptions) composeContext {
	opts := src.composition()
	for _, skip := range []struct {
		set bool
		opt graft.EngineOption
	}{
		{opts.SkipVault, graft.WithSkipVault(true)},
		{opts.SkipAws, graft.WithSkipAws(true)},
		{opts.SkipNats, graft.WithSkipNats(true)},
	} {
		if skip.set {
			engine = append(engine[:len(engine):len(engine)], skip.opt)
		}
	}
	return composeContext{
		Stdin: stdin,
		Merge: mergeOpts{
			SkipEval:       opts.SkipEval,
			Prune:          opts.Prune,
			CherryPick:     opts.CherryPick,
			FallbackAppend: opts.FallbackAppend,
			EnableGoPatch:  opts.GoPatch,
			MultiDoc:       opts.MultiDoc,
			EngineOpts:     engine,
		},
		Engine: engine,
		Eval:   opts.Eval,
	}
}

// validateComposeFlags refuses the combinations of composition flags that
// have no meaning.
func validateComposeFlags(src composeSource) error {
	o := src.composition()
	if o.Merge && o.Eval {
		return &usageError{Msg: "-m and -E cannot be combined"}
	}
	if o.Merge || o.Eval {
		return nil
	}
	for _, passed := range []struct {
		set  bool
		flag string
	}{
		{len(o.Prune) > 0, "--prune"},
		{len(o.CherryPick) > 0, "--cherry-pick"},
		{o.SkipEval, "--skip-eval"},
		{o.FallbackAppend, "--fallback-append"},
		{o.GoPatch, "--go-patch"},
		{o.MultiDoc, "--multi-doc"},
		{o.SkipVault, "--skip-vault"},
		{o.SkipAws, "--skip-aws"},
		{o.SkipNats, "--skip-nats"},
	} {
		if passed.set {
			return &usageError{Msg: passed.flag + " needs -m or -E"}
		}
	}
	return nil
}

// composeQueryInputs runs the merge, or under Eval the eval of each
// document on its own, over files and returns the YAML a query then reads.
// A failure comes back as the text to print on stderr and the exit code.
func composeQueryInputs(cc composeContext, files []string) (merged []byte, errText []byte, code int) {
	if len(files) == 0 {
		files = []string{"-"}
	}
	var sources [][]YamlFile
	for _, file := range files {
		docs, err := cc.load(file)
		if err != nil {
			return nil, composeErrText(err), 2
		}
		sources = append(sources, docs)
	}

	opts := cc.Merge
	if cc.Eval {
		var out [][]byte
		for _, docs := range sources {
			for _, doc := range docs {
				text, err := cc.render([]YamlFile{doc}, &opts)
				if err != nil {
					return nil, composeErrText(err), 2
				}
				out = append(out, text)
			}
		}
		return bytes.Join(out, []byte("---\n")), nil, 0
	}

	var all []YamlFile
	for _, docs := range sources {
		all = append(all, docs...)
	}
	text, err := cc.render(all, &opts)
	if err != nil {
		return nil, composeErrText(err), 2
	}
	return text, nil, 0
}

// composeErrText is err as a line of stderr text, ending in one newline
// whether or not the error carries its own.
func composeErrText(err error) []byte {
	return []byte(strings.TrimSuffix(err.Error(), "\n") + "\n")
}

// render merges docs and writes the result as YAML without a document
// start.
func (cc composeContext) render(docs []YamlFile, opts *mergeOpts) ([]byte, error) {
	tree, _, err := mergeAllDocs(docs, opts)
	if err != nil {
		return nil, err
	}
	out, rc := marshalMergedDocument(tree, "", true, opts.keyLess())
	if rc != 0 {
		return nil, errors.New("unable to render the merged result")
	}
	return out, nil
}

// load reads one file, or stdin for "-", as the documents it holds. A
// file is one document unless it is being split, which Eval and
// --multi-doc both do. It reads and reports errors as graft merge does.
func (cc composeContext) load(file string) ([]YamlFile, error) {
	yf, err := loadYamlFile(file)
	if err != nil {
		return nil, err
	}
	if yf.Path != "-" {
		defer func() { _ = yf.Reader.Close() }()
	}
	data, err := readFileFrom(&yf, cc.Stdin)
	if err != nil {
		return nil, err
	}
	if !cc.Eval && !cc.Merge.MultiDoc {
		return []YamlFile{{Path: yf.Path, Reader: io.NopCloser(bytes.NewReader(data))}}, nil
	}
	return splitYamlBytes(yf.Path, data), nil
}

// queryCmdSpec is what the jq and yq commands differ in. Both parse their
// own arguments, hand graft's persistent flags back to the root, and then
// run the query, so the steps between are shared.
type queryCmdSpec struct {
	// asRoot is true when the command is the root itself, where cobra has
	// already run the hook and there are no graft flags to parse.
	asRoot bool
	// parse reads the arguments after the verb and returns the root flags
	// found among them, what the parse ended on, and whether the options
	// need the engine for composition.
	parse      func(args []string) (rootArgs []string, help, version, wantEngine bool, err error)
	argsStatus func(error) query.Status
	version    func() string
	configure  func(theme string, engine engineOptions)
	run        func() query.Status
}

// runQueryCmd is the RunE of a jq or yq command.
func runQueryCmd(cmd *cobra.Command, args []string, spec queryCmdSpec) error {
	// cobra hands over the flags given before the verb as well, which are
	// graft's own in every form cobra reads.
	n := preVerbArgs(cmd, args)
	applyRoot := func(rootArgs []string) bool {
		return spec.asRoot || applyJQRootFlags(cmd, rootArgs)
	}
	parsed, help, version, wantEngine, err := spec.parse(args[n:])
	rootArgs := append(slices.Clip(args[:n]), parsed...)
	if err != nil {
		// The root's flags given before the bad one still decide whether
		// the diagnostic is colored.
		if !applyRoot(rootArgs) {
			return nil
		}
		st := spec.argsStatus(err)
		log.PrintStdErrf("%s", st.Message)
		exit(st.Code)
		return nil
	}
	if help {
		return cmd.Help()
	}
	if version {
		printStdOutf("%s", spec.version())
		exit(0)
		return nil
	}
	if !applyRoot(rootArgs) {
		return nil
	}
	themeChanged, themeValue := false, themeNameAuto
	if theme := cmd.Root().PersistentFlags().Lookup("theme"); theme != nil {
		themeChanged, themeValue = theme.Changed, theme.Value.String()
	}
	themeName, _, _ := resolveTheme(cmd, themeChanged, themeValue)
	var engine engineOptions
	if wantEngine {
		if engine, err = composeEngineOpts(cmd); err != nil {
			log.PrintStdErrf("%s\n", err.Error())
			exit(1)
			return nil
		}
	}
	spec.configure(themeName, engine)
	st := spec.run()
	if st.Message != "" {
		log.PrintStdErrf("%s", st.Message)
	}
	exit(st.Code)
	return nil
}
