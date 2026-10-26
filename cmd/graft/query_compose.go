package main

import (
	"bytes"
	"errors"
	"io"
	"strings"

	"github.com/fivetwenty-io/graft/pkg/graft"
)

// composedName is the name a composed input goes by in error locations
// and input_filename.
const composedName = "<merged>"

// engineOptions are the options the engine is built with, which the
// root's config loading produces.
type engineOptions = []graft.EngineOption

// composeContext is everything composeQueryInputs needs to run graft's
// merge and eval passes ahead of a query.
type composeContext struct {
	Stdin  io.Reader
	Merge  mergeOpts
	Engine engineOptions
	Eval   bool
}

// newComposeContext builds the context a parsed command line asks for,
// where the merge options come from the command's pass-through flags.
func newComposeContext(opts jqOptions, stdin io.Reader, engine engineOptions) composeContext {
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
func validateComposeFlags(o jqOptions) error {
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
