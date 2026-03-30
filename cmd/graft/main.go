package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/cppforlife/go-patch/patch"
	"github.com/gonvenience/ytbx"
	"github.com/homeport/dyff/pkg/dyff"
	"github.com/mattn/go-isatty"
	"github.com/spf13/cobra"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"

	"github.com/fivetwenty-io/graft/log"
	"github.com/fivetwenty-io/graft/pkg/graft"
	_ "github.com/fivetwenty-io/graft/pkg/graft/operators" // Register operators

	"github.com/goccy/go-yaml"
)

// Version holds the Current version of graft.
var Version = "(development)"

var printStdOutf = func(format string, args ...interface{}) {
	_, _ = fmt.Fprintf(os.Stdout, format, args...)
}

var exit = func(code int) {
	os.Exit(code)
}

var usage = func() {
	// Default implementation; overridden in tests and replaced in main()
	fmt.Fprintf(os.Stderr, "Usage: graft <command> [options] [files...]\n")
	exit(1)
}

func envFlag(varname string) bool {
	val := os.Getenv(varname)
	return val != "" && !strings.EqualFold(val, "false") && val != "0"
}

type YamlFile struct {
	Path   string
	Reader io.ReadCloser
}

type jsonOpts struct {
	Strict bool
	Files  []string
}

type mergeOpts struct {
	SkipEval       bool
	Prune          []string
	CherryPick     []string
	FallbackAppend bool
	EnableGoPatch  bool
	MultiDoc       bool
	DataflowOrder  string
	Files          []string
	EngineOpts     []graft.EngineOption // Programmatic engine options (not from CLI flags)
}

func handleColorFlag(colorOpt string) (bool, bool) {
	switch colorOpt {
	case "on":
		return true, true
	case "off":
		return false, true
	case "auto", "":
		return isatty.IsTerminal(os.Stderr.Fd()), true
	default:
		log.PrintStdErrf("Invalid --color option: %s. Must be 'on', 'off', or 'auto'.\n", colorOpt)
		return false, false
	}
}

func handleMerge(opts *mergeOpts) int {
	tree, _, err := cmdMergeEval(opts)
	if err != nil {
		log.PrintStdErrf("%s\n", err.Error())
		return 2
	}

	log.TRACE("Converting the following data back to YML:")
	log.TRACE("%#v", tree)

	if cycleErr := graft.CheckForCycles(tree, 4096); cycleErr != nil {
		log.PrintStdErrf("%s\n", cycleErr.Error())
		return 2
	}

	merged, err := graft.MarshalYAML(tree)
	if err != nil {
		log.PrintStdErrf("Unable to convert merged result back to YAML: %s\nData:\n%#v", err.Error(), tree)
		return 2
	}

	printStdOutf("%s\n", string(merged))
	return 0
}

func handleFan(opts *mergeOpts) int {
	trees, err := cmdFanEval(opts)
	if err != nil {
		log.PrintStdErrf("%s\n", err.Error())
		return 2
	}

	for _, tree := range trees {
		log.TRACE("Converting the following data back to YML:")
		log.TRACE("%#v", tree)

		if err := graft.CheckForCycles(tree, 4096); err != nil {
			log.PrintStdErrf("%s\n", err.Error())
			return 2
		}

		merged, err := graft.MarshalYAML(tree)
		if err != nil {
			log.PrintStdErrf("Unable to convert merged result back to YAML: %s\nData:\n%#v", err.Error(), tree)
			return 2
		}

		printStdOutf("---\n%s\n", string(merged))
	}
	return 0
}

func handleVaultInfo(vaultFiles []string, enableGoPatch bool) int {
	opts := &mergeOpts{
		Files:         vaultFiles,
		EnableGoPatch: enableGoPatch,
		EngineOpts:    []graft.EngineOption{graft.WithSkipVault(true)},
	}
	_, engine, err := cmdMergeEval(opts)
	if err != nil {
		log.PrintStdErrf("%s\n", err.Error())
		return 2
	}

	printStdOutf("%s\n", formatVaultRefs(engine))
	return 0
}

func handleJSON(opts jsonOpts) int {
	jsons, err := cmdJSONEval(opts)
	if err != nil {
		log.PrintStdErrf("%s\n", err)
		return 2
	}
	for _, output := range jsons {
		printStdOutf("%s\n", output)
	}
	return 0
}

func handleDiff(files []string, colorOpt string) int {
	if colorOpt == "auto" || colorOpt == "" {
		ansi.Color(isatty.IsTerminal(os.Stdout.Fd()))
	}
	if len(files) != 2 {
		usage()
		return 1
	}
	output, differences, err := diffFiles(files)
	if err != nil {
		log.PrintStdErrf("%s\n", err)
		return 2
	}
	printStdOutf("%s\n", output)
	if differences {
		return 1
	}
	return 0
}

// newRootCmd creates a fresh Cobra command tree. Called each time main() runs
// so that tests calling main() multiple times get clean flag state.
func newRootCmd() (*cobra.Command, *bool) {
	var debug, trace, version bool
	var colorOpt string

	// Track whether PersistentPreRunE signaled an abort (e.g., invalid color)
	var aborted bool

	rootCmd := &cobra.Command{
		Use:           "graft",
		Short:         "graft - YAML merging and operator evaluation",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(_ *cobra.Command, _ []string) error {
			// Handle debug/trace flags
			if envFlag("DEBUG") || debug {
				log.DebugOn = true
			}
			if envFlag("TRACE") || trace {
				log.TraceOn = true
				log.DebugOn = true
			}

			// Handle color flag
			colorEnabled, colorValid := handleColorFlag(colorOpt)
			if !colorValid {
				aborted = true
				exit(1)
				return fmt.Errorf("invalid color option")
			}
			ansi.Color(colorEnabled)
			return nil
		},
		RunE: func(_ *cobra.Command, _ []string) error {
			if aborted {
				return nil
			}
			// Root command with no subcommand
			if version {
				printStdOutf("%s - Version %s\n", os.Args[0], Version)
				exit(0)
				return nil
			}
			// No subcommand given: call usage
			usage()
			return nil
		},
	}

	rootCmd.PersistentFlags().BoolVarP(&debug, "debug", "D", false, "Enable debugging")
	rootCmd.PersistentFlags().BoolVarP(&trace, "trace", "T", false, "Enable trace mode debugging (very verbose)")
	rootCmd.PersistentFlags().BoolVarP(&version, "version", "v", false, "Display version information")
	rootCmd.PersistentFlags().StringVar(&colorOpt, "color", "", "Control color output (on/off/auto, default: auto)")

	// merge command
	var mergeSkipEval, mergeFallbackAppend, mergeGoPatch, mergeMultiDoc bool
	var mergePrune, mergeCherryPick []string
	var mergeDataflowOrder string

	mergeCmd := &cobra.Command{
		Use:   "merge [files...]",
		Short: "Merge multiple YAML/JSON files",
		RunE: func(_ *cobra.Command, args []string) error {
			opts := &mergeOpts{
				SkipEval:       mergeSkipEval,
				Prune:          mergePrune,
				CherryPick:     mergeCherryPick,
				FallbackAppend: mergeFallbackAppend,
				EnableGoPatch:  mergeGoPatch,
				MultiDoc:       mergeMultiDoc,
				DataflowOrder:  mergeDataflowOrder,
				Files:          args,
			}
			exit(handleMerge(opts))
			return nil
		},
	}
	mergeCmd.Flags().BoolVar(&mergeSkipEval, "skip-eval", false, "Do not evaluate graft logic after merging docs")
	mergeCmd.Flags().StringArrayVar(&mergePrune, "prune", nil, "Specify keys to prune from final output (may be specified more than once)")
	mergeCmd.Flags().StringArrayVar(&mergeCherryPick, "cherry-pick", nil, "The opposite of prune, specify keys to cherry-pick from final output (may be specified more than once)")
	mergeCmd.Flags().BoolVar(&mergeFallbackAppend, "fallback-append", false, "Default merge normally tries to key merge, then inline. This flag says do an append instead of an inline.")
	mergeCmd.Flags().BoolVar(&mergeGoPatch, "go-patch", false, "Enable the use of go-patch when parsing files to be merged")
	mergeCmd.Flags().BoolVarP(&mergeMultiDoc, "multi-doc", "m", false, "Treat multi-doc yaml as multiple files.")
	mergeCmd.Flags().StringVar(&mergeDataflowOrder, "dataflow-order", "", "Order of operations in dataflow output: alphabetical (default) or insertion")

	// fan command
	var fanSkipEval, fanFallbackAppend, fanGoPatch, fanMultiDoc bool
	var fanPrune, fanCherryPick []string
	var fanDataflowOrder string

	fanCmd := &cobra.Command{
		Use:   "fan [files...]",
		Short: "Fan out source document across target documents",
		RunE: func(_ *cobra.Command, args []string) error {
			opts := &mergeOpts{
				SkipEval:       fanSkipEval,
				Prune:          fanPrune,
				CherryPick:     fanCherryPick,
				FallbackAppend: fanFallbackAppend,
				EnableGoPatch:  fanGoPatch,
				MultiDoc:       fanMultiDoc,
				DataflowOrder:  fanDataflowOrder,
				Files:          args,
			}
			exit(handleFan(opts))
			return nil
		},
	}
	fanCmd.Flags().BoolVar(&fanSkipEval, "skip-eval", false, "Do not evaluate graft logic after merging docs")
	fanCmd.Flags().StringArrayVar(&fanPrune, "prune", nil, "Specify keys to prune from final output (may be specified more than once)")
	fanCmd.Flags().StringArrayVar(&fanCherryPick, "cherry-pick", nil, "The opposite of prune, specify keys to cherry-pick from final output (may be specified more than once)")
	fanCmd.Flags().BoolVar(&fanFallbackAppend, "fallback-append", false, "Default merge normally tries to key merge, then inline. This flag says do an append instead of an inline.")
	fanCmd.Flags().BoolVar(&fanGoPatch, "go-patch", false, "Enable the use of go-patch when parsing files to be merged")
	fanCmd.Flags().BoolVarP(&fanMultiDoc, "multi-doc", "m", false, "Treat multi-doc yaml as multiple files.")
	fanCmd.Flags().StringVar(&fanDataflowOrder, "dataflow-order", "", "Order of operations in dataflow output: alphabetical (default) or insertion")

	// json command
	var jsonStrict bool

	jsonCmd := &cobra.Command{
		Use:   "json [files...]",
		Short: "Convert YAML to JSON",
		RunE: func(_ *cobra.Command, args []string) error {
			opts := jsonOpts{
				Strict: jsonStrict,
				Files:  args,
			}
			exit(handleJSON(opts))
			return nil
		},
	}
	jsonCmd.Flags().BoolVar(&jsonStrict, "strict", false, "Refuse to convert non-string keys to strings")

	// diff command
	diffCmd := &cobra.Command{
		Use:   "diff [file1] [file2]",
		Short: "Show the semantic differences between two YAML files",
		RunE: func(_ *cobra.Command, args []string) error {
			exit(handleDiff(args, colorOpt))
			return nil
		},
	}

	// vaultinfo command
	var vaultInfoGoPatch bool

	vaultinfoCmd := &cobra.Command{
		Use:   "vaultinfo [files...]",
		Short: "List vault references in the given files",
		RunE: func(_ *cobra.Command, args []string) error {
			exit(handleVaultInfo(args, vaultInfoGoPatch))
			return nil
		},
	}
	vaultinfoCmd.Flags().BoolVar(&vaultInfoGoPatch, "go-patch", false, "Enable the use of go-patch when parsing files to be merged")

	rootCmd.AddCommand(mergeCmd, fanCmd, jsonCmd, diffCmd, vaultinfoCmd)

	return rootCmd, &aborted
}

func main() {
	rootCmd, aborted := newRootCmd()

	err := rootCmd.Execute()

	// If already aborted (e.g., invalid --color), don't call usage
	if *aborted {
		return
	}

	// If Cobra returned an error (unknown command, bad flags, etc.), call usage
	if err != nil {
		usage()
		return
	}
}

func isArrayError(err error) bool {
	var rootArrayErr RootIsArrayError
	return errors.As(err, &rootArrayErr)
}

func parseGoPatch(data []byte) (patch.Ops, error) {
	opdefs := []patch.OpDefinition{}
	err := yaml.Unmarshal(data, &opdefs)
	if err != nil {
		return nil, ansi.Errorf("@R{Root of YAML document is not a hash/map. Tried parsing it as go-patch, but got}: %s\n", err)
	}
	ops, err := patch.NewOpsFromDefinitions(opdefs)
	if err != nil {
		return nil, ansi.Errorf("@R{Unable to parse go-patch definitions: %s\n", err)
	}
	return ops, nil
}

func parseYAML(data []byte) (map[string]interface{}, error) {
	// Handle empty document
	if len(bytes.TrimSpace(data)) == 0 {
		log.DEBUG("YAML doc is empty, creating empty hash/map")
		return make(map[string]interface{}), nil
	}

	// First, unmarshal into a generic interface to detect root type
	var raw interface{}
	if err := yaml.Unmarshal(graft.QuoteInjectKeys(data), &raw); err != nil {
		return nil, ansi.Errorf("@R{Root of YAML document is not a hash/map}: %s\n", err.Error())
	}

	switch v := raw.(type) {
	case map[string]interface{}:
		if len(v) == 0 {
			log.DEBUG("YAML doc is empty, creating empty hash/map")
		}
		return graft.NormalizeMap(v), nil
	case nil:
		log.DEBUG("YAML doc is null/empty, creating empty hash/map")
		return make(map[string]interface{}), nil
	case []interface{}:
		return nil, RootIsArrayError{msg: ansi.Sprintf("@R{Root of YAML document is not a hash/map}: root is an array\n")}
	default:
		return nil, ansi.Errorf("@R{Root of YAML document is not a hash/map}: found %T\n", raw)
	}
}

func loadYamlFile(file string) (YamlFile, error) {
	var target YamlFile
	if file == "-" {
		target = YamlFile{Reader: os.Stdin, Path: "-"}
	} else {
		// #nosec G304 - File path is from user-provided command line arguments which is expected behavior for the CLI tool
		f, err := os.Open(file)
		if err != nil {
			return YamlFile{}, ansi.Errorf("@R{Error reading file} @m{%s}: %s", file, err.Error())
		}
		target = YamlFile{Path: file, Reader: f}
	}
	return target, nil
}

func splitLoadYamlFile(file string) ([]YamlFile, error) {
	docs := []YamlFile{}

	yamlFile, err := loadYamlFile(file)
	if err != nil {
		return nil, err
	}

	fileData, err := readFile(&yamlFile)
	if err != nil {
		return nil, err
	}

	rawDocs := bytes.Split(fileData, []byte("\n---\n"))
	// strip off empty document created if the first three bytes of the file are the doc separator
	// keeps the indexing correct for when used with error messages
	if len(rawDocs[0]) == 0 {
		rawDocs = rawDocs[1:]
	}

	for i, docBytes := range rawDocs {
		buf := bytes.NewBuffer(docBytes)
		doc := YamlFile{Path: fmt.Sprintf("%s[%d]", yamlFile.Path, i), Reader: io.NopCloser(buf)}
		docs = append(docs, doc)
	}
	return docs, nil
}

func cmdMergeEval(options *mergeOpts) (map[string]interface{}, graft.Engine, error) {
	files := []YamlFile{}

	if len(options.Files) < 1 {
		stdinInfo, err := os.Stdin.Stat()
		if err != nil {
			return nil, nil, ansi.Errorf("@R{Error statting STDIN} - Bailing out: %s\n", err.Error())
		}

		if stdinInfo.Mode()&os.ModeCharDevice != 0 {
			return nil, nil, ansi.Errorf("@R{Error reading STDIN}: no data found. Did you forget to pipe data to STDIN, or specify yaml files to merge?")
		}

		options.Files = append(options.Files, "-")
	}

	for _, file := range options.Files {
		if options.MultiDoc {
			docs, err := splitLoadYamlFile(file)
			if err != nil {
				return nil, nil, err
			}
			files = append(files, docs...)
		} else {
			yamlFile, err := loadYamlFile(file)
			if err != nil {
				return nil, nil, err
			}
			files = append(files, yamlFile)
		}
	}

	result, engine, err := mergeAllDocs(files, options)
	if err != nil {
		return nil, nil, err
	}

	return result, engine, nil
}

func cmdFanEval(options *mergeOpts) ([]map[string]interface{}, error) {
	stdinInfo, err := os.Stdin.Stat()
	if err != nil {
		return nil, ansi.Errorf("@R{Error statting STDIN} - Bailing out: %s\n", err.Error())
	}
	if stdinInfo.Mode()&os.ModeCharDevice == 0 {
		options.Files = append(options.Files, "-")
	}

	if len(options.Files) == 0 {
		return nil, ansi.Errorf("@R{Missing Input:} You must specify at least a source document to graft fan. If no files are specified, STDIN is used. Using STDIN for source and target docs only works with -m.")
	}

	roots := []map[string]interface{}{}
	sourcePath := options.Files[0]
	options.Files = options.Files[1:]

	docs := []YamlFile{}
	source := YamlFile{}
	if options.MultiDoc {
		sourceDocs, sourceErr := splitLoadYamlFile(sourcePath)
		if sourceErr != nil {
			return nil, sourceErr
		}
		// only the first yaml document of the source will be treated as actual source, all others
		// will be treated as target documents
		source = sourceDocs[0]
		docs = append(sourceDocs[1:], docs...)
	} else {
		source, err = loadYamlFile(sourcePath)
		if err != nil {
			return nil, err
		}
	}

	for _, file := range options.Files {
		yamlDocs, yamlErr := splitLoadYamlFile(file)
		if yamlErr != nil {
			return nil, yamlErr
		}
		docs = append(docs, yamlDocs...)
	}

	sourceBytes, err := readFile(&source)
	if err != nil {
		return nil, err
	}

	if len(docs) < 1 {
		return nil, ansi.Errorf("@R{Missing Input:} You must specify at least one target document to graft fan. If no files are specified, STDIN is used. Using STDIN for source and target docs only works with -m.")
	}

	for _, doc := range docs {
		sourceBuffer := bytes.NewBuffer(sourceBytes)
		source = YamlFile{Path: source.Path, Reader: io.NopCloser(sourceBuffer)}
		result, _, err := mergeAllDocs([]YamlFile{source, doc}, options)
		if err != nil {
			return nil, err
		}
		roots = append(roots, result)
	}

	return roots, nil
}

func cmdJSONEval(options jsonOpts) ([]string, error) {
	stdinInfo, err := os.Stdin.Stat()
	if err != nil {
		return nil, ansi.Errorf("@R{Error statting STDIN} - Bailing out: %s\n", err.Error())
	}
	if stdinInfo.Mode()&os.ModeCharDevice == 0 {
		options.Files = append(options.Files, "-")
	}

	output, err := graft.JSONifyFiles(options.Files, options.Strict)
	if err != nil {
		return nil, err
	}

	return output, nil
}

type yamlVaultSecret struct {
	Key        string
	References []string
}

type byKey []yamlVaultSecret

type yamlVaultRefs struct {
	Secrets []yamlVaultSecret
}

func (refs byKey) Len() int           { return len(refs) }
func (refs byKey) Swap(i, j int)      { refs[i], refs[j] = refs[j], refs[i] }
func (refs byKey) Less(i, j int) bool { return refs[i].Key < refs[j].Key }

func formatVaultRefs(engine graft.Engine) string {
	refs := yamlVaultRefs{}
	vaultRefs := engine.GetOperatorState().GetVaultRefs()
	for secret, srcs := range vaultRefs {
		refs.Secrets = append(refs.Secrets, yamlVaultSecret{secret, srcs})
	}

	sort.Sort(byKey(refs.Secrets))
	for _, secret := range refs.Secrets {
		sort.Strings(secret.References)
	}

	output, err := graft.MarshalYAML(refs)
	if err != nil {
		panic(fmt.Sprintf("Could not marshal YAML for vault references: %+v", vaultRefs))
	}

	return string(output)
}

func readFile(file *YamlFile) ([]byte, error) {
	var data []byte
	var err error

	if file.Path == "-" {
		file.Path = "STDIN"
		stat, statErr := os.Stdin.Stat()
		if statErr != nil {
			return nil, ansi.Errorf("@R{Error statting STDIN} - Bailing out: %s\n", statErr.Error())
		}
		if stat.Mode()&os.ModeCharDevice == 0 {
			data, err = io.ReadAll(os.Stdin)
			if err != nil {
				return nil, ansi.Errorf("@R{Error reading file} @m{%s}: %s\n", file.Path, err.Error())
			}
		}
	} else {
		data, err = io.ReadAll(file.Reader)
		if err != nil {
			return nil, ansi.Errorf("@R{Error reading file} @m{%s}: %s\n", file.Path, err.Error())
		}
	}
	if len(data) == 0 && file.Path == "STDIN" {
		return nil, ansi.Errorf("@R{Error reading STDIN}: no data found. Did you forget to pipe data to STDIN, or specify yaml files to merge?")
	}

	return data, nil
}

//nolint:gocyclo // mergeAllDocs orchestrates complex document merging with multiple options
func mergeAllDocs(files []YamlFile, options *mergeOpts) (map[string]interface{}, graft.Engine, error) {
	// Create engine with settings from options (caller-provided first, then defaults)
	engineOpts := make([]graft.EngineOption, 0, len(options.EngineOpts)+3)
	engineOpts = append(engineOpts, options.EngineOpts...)
	engineOpts = append(engineOpts,
		graft.WithCache(true, 1000),
		graft.WithConcurrency(10),
	)

	// Set dataflow order if specified (default to alphabetical if not set)
	dataflowOrder := options.DataflowOrder
	if dataflowOrder == "" {
		dataflowOrder = "alphabetical"
	}
	engineOpts = append(engineOpts, graft.WithDataflowOrder(dataflowOrder))

	engine, err := graft.NewEngine(engineOpts...)
	if err != nil {
		return nil, nil, ansi.Errorf("@R{Failed to create graft engine}: %s", err.Error())
	}

	// Parse all documents
	docs := []graft.Document{}
	for _, file := range files {
		log.DEBUG("Processing file '%s'", file.Path)

		data, readErr := readFile(&file)
		if readErr != nil {
			return nil, nil, readErr
		}

		// Check if it's a go-patch document
		if options.EnableGoPatch {
			_, parseErr := parseYAML(data)
			if isArrayError(parseErr) {
				log.DEBUG("Detected root of document as an array. Attempting go-patch parsing")
				ops, patchErr := parseGoPatch(data)
				if patchErr != nil {
					return nil, nil, ansi.Errorf("@m{%s}: @R{%s}\n", file.Path, patchErr.Error())
				}
				// Create a go-patch document
				doc := graft.NewGoPatchDocument(ops)
				docs = append(docs, doc)
				continue
			}
		}

		// Parse as YAML
		doc, parseDocErr := engine.ParseYAML(data)
		if parseDocErr != nil {
			return nil, nil, ansi.Errorf("@m{%s}: @R{%s}\n", file.Path, parseDocErr.Error())
		}
		docs = append(docs, doc)
	}

	// Merge all documents
	mergeBuilder := engine.Merge(context.TODO(), docs...)

	// Apply merge options
	if options.FallbackAppend {
		mergeBuilder = mergeBuilder.WithArrayMergeStrategy(graft.AppendArrays)
	}

	if options.SkipEval {
		mergeBuilder = mergeBuilder.SkipEvaluation()
	}

	// Apply cherry-pick keys at the builder level
	if len(options.CherryPick) > 0 {
		mergeBuilder = mergeBuilder.WithCherryPick(options.CherryPick...)
	}

	// Apply prune keys at the builder level
	if len(options.Prune) > 0 {
		mergeBuilder = mergeBuilder.WithPrune(options.Prune...)
	}

	// Execute merge
	merged, err := mergeBuilder.Execute()
	if err != nil {
		// Check if this is a MultiError from the merger (Issue #172)
		if strings.Contains(err.Error(), "error(s) detected:") {
			return nil, nil, err
		}
		return nil, nil, ansi.Errorf("@R{Merge failed}: %s", err.Error())
	}

	// Get the raw data for backward compatibility
	// The CLI expects a map[string]interface{}
	data, ok := merged.GetData().(map[string]interface{})
	if !ok {
		return nil, nil, ansi.Errorf("@R{Merge result is not a map}")
	}
	return data, engine, nil
}

func diffFiles(paths []string) (output string, hasDifferences bool, err error) {
	if len(paths) != 2 {
		return "", false, ansi.Errorf("incorrect number of files given to diffFiles(); please file a bug report")
	}

	from, to, err := ytbx.LoadFiles(paths[0], paths[1])
	if err != nil {
		return "", false, err
	}

	report, err := dyff.CompareInputFiles(from, to)
	if err != nil {
		return "", false, err
	}

	reportWriter := &dyff.HumanReport{
		Report:            report,
		DoNotInspectCerts: false,
		NoTableStyle:      false,
		OmitHeader:        true,
	}

	var buf bytes.Buffer
	out := bufio.NewWriter(&buf)
	if err := reportWriter.WriteReport(out); err != nil {
		return "", false, fmt.Errorf("failed to write report: %w", err)
	}
	if err := out.Flush(); err != nil {
		return "", false, fmt.Errorf("failed to flush report: %w", err)
	}

	return buf.String(), len(report.Diffs) > 0, nil
}

type RootIsArrayError struct {
	msg string
}

func (r RootIsArrayError) Error() string {
	return r.msg
}
