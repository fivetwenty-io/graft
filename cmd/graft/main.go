package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"

	"github.com/cppforlife/go-patch/patch"
	"github.com/gonvenience/ytbx"
	"github.com/homeport/dyff/pkg/dyff"
	"github.com/mattn/go-isatty"

	"github.com/fivetwenty-io/graft/internal/utils/ansi"

	"github.com/fivetwenty-io/graft/log"
	"github.com/fivetwenty-io/graft/pkg/graft"
	_ "github.com/fivetwenty-io/graft/pkg/graft/operators" // Register operators

	// Use geofffranks forks to persist the fix in https://github.com/go-yaml/yaml/pull/133/commits
	// Also https://github.com/go-yaml/yaml/pull/195
	"github.com/geofffranks/simpleyaml"
	"github.com/geofffranks/yaml"
	"github.com/voxelbrain/goptions"
)

// Version holds the Current version of graft.
var Version = "(development)"

var printStdOutf = func(format string, args ...interface{}) {
	_, _ = fmt.Fprintf(os.Stdout, format, args...)
}

var getopts = func(o interface{}) {
	err := goptions.Parse(o)
	if err != nil {
		usage()
	}
}

var exit = func(code int) {
	os.Exit(code)
}

var usage = func() {
	goptions.PrintHelp()
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
	Strict bool               `goptions:"--strict, description='Refuse to convert non-string keys to strings'"`
	Help   bool               `goptions:"--help, -h"`
	Files  goptions.Remainder `goptions:"description='Files to convert to JSON'"`
}

type mergeOpts struct {
	SkipEval       bool               `goptions:"--skip-eval, description='Do not evaluate graft logic after merging docs'"`
	Prune          []string           `goptions:"--prune, description='Specify keys to prune from final output (may be specified more than once)'"`
	CherryPick     []string           `goptions:"--cherry-pick, description='The opposite of prune, specify keys to cherry-pick from final output (may be specified more than once)'"`
	FallbackAppend bool               `goptions:"--fallback-append, description='Default merge normally tries to key merge, then inline. This flag says do an append instead of an inline.'"`
	EnableGoPatch  bool               `goptions:"--go-patch, description='Enable the use of go-patch when parsing files to be merged'"`
	MultiDoc       bool               `goptions:"--multi-doc, -m, description='Treat multi-doc yaml as multiple files.'"`
	DataflowOrder  string             `goptions:"--dataflow-order, description='Order of operations in dataflow output: alphabetical (default) or insertion'"`
	Help           bool               `goptions:"--help, -h"`
	Files          goptions.Remainder `goptions:"description='List of files to merge. To read STDIN, specify a filename of \\'-\\'.'"`
}

type cliOptions struct {
	Debug   bool   `goptions:"-D, --debug, description='Enable debugging'"`
	Trace   bool   `goptions:"-T, --trace, description='Enable trace mode debugging (very verbose)'"`
	Version bool   `goptions:"-v, --version, description='Display version information'"`
	Color   string `goptions:"--color, description='Control color output (on/off/auto, default: auto)'"`
	Action  goptions.Verbs
	Merge   mergeOpts `goptions:"merge"`
	Fan     mergeOpts `goptions:"fan"`
	JSON    jsonOpts  `goptions:"json"`
	Diff    struct {
		Files goptions.Remainder `goptions:"description='Show the semantic differences between two YAML files'"`
	} `goptions:"diff"`
	VaultInfo struct {
		EnableGoPatch bool               `goptions:"--go-patch, description='Enable the use of go-patch when parsing files to be merged'"`
		Files         goptions.Remainder `goptions:"description='List vault references in the given files'"`
	} `goptions:"vaultinfo"`
}

// checkForCycles detects circular references in the data structure.
func checkForCycles(root interface{}, maxDepth int) error {
	visited := make(map[uintptr]bool)

	var check func(o interface{}, depth int) error
	check = func(o interface{}, depth int) error {
		if depth == 0 {
			return ansi.Errorf("@*{Hit max recursion depth. You seem to have a self-referencing dataset}")
		}

		switch v := o.(type) {
		case map[string]interface{}:
			// Check if we've seen this map before (circular reference)
			ptr := reflect.ValueOf(v).Pointer()
			if visited[ptr] {
				return ansi.Errorf("@*{Hit max recursion depth. You seem to have a self-referencing dataset}")
			}
			visited[ptr] = true

			for _, val := range v {
				if err := check(val, depth-1); err != nil {
					return err
				}
			}

			delete(visited, ptr) // Remove after visiting children

		case []interface{}:
			// Check if we've seen this slice before (circular reference)
			ptr := reflect.ValueOf(v).Pointer()
			if visited[ptr] {
				return ansi.Errorf("@*{Hit max recursion depth. You seem to have a self-referencing dataset}")
			}
			visited[ptr] = true

			for _, val := range v {
				if err := check(val, depth-1); err != nil {
					return err
				}
			}

			delete(visited, ptr) // Remove after visiting children
		}

		return nil
	}

	return check(root, maxDepth)
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
	tree, err := cmdMergeEval(opts)
	if err != nil {
		log.PrintStdErrf("%s\n", err.Error())
		return 2
	}

	log.TRACE("Converting the following data back to YML:")
	log.TRACE("%#v", tree)

	if cycleErr := checkForCycles(tree, 4096); cycleErr != nil {
		log.PrintStdErrf("%s\n", cycleErr.Error())
		return 2
	}

	merged, err := yaml.Marshal(tree)
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

		if err := checkForCycles(tree, 4096); err != nil {
			log.PrintStdErrf("%s\n", err.Error())
			return 2
		}

		merged, err := yaml.Marshal(tree)
		if err != nil {
			log.PrintStdErrf("Unable to convert merged result back to YAML: %s\nData:\n%#v", err.Error(), tree)
			return 2
		}

		printStdOutf("---\n%s\n", string(merged))
	}
	return 0
}

func handleVaultInfo(options *cliOptions) int {
	graft.VaultRefs = map[string][]string{}
	graft.SkipVault = true
	options.Merge.Files = options.VaultInfo.Files
	options.Merge.EnableGoPatch = options.VaultInfo.EnableGoPatch
	_, err := cmdMergeEval(&options.Merge)
	if err != nil {
		log.PrintStdErrf("%s\n", err.Error())
		return 2
	}

	printStdOutf("%s\n", formatVaultRefs())
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

func main() {
	var options cliOptions
	getopts(&options)

	if envFlag("DEBUG") || options.Debug {
		log.DebugOn = true
	}

	if envFlag("TRACE") || options.Trace {
		log.TraceOn = true
		log.DebugOn = true
	}

	if options.JSON.Help || options.Merge.Help || options.Fan.Help {
		usage()
		return
	}

	if options.Version {
		printStdOutf("%s - Version %s\n", os.Args[0], Version)
		exit(0)
		return
	}

	colorEnabled, colorValid := handleColorFlag(options.Color)
	if !colorValid {
		exit(1)
		return
	}
	ansi.Color(colorEnabled)

	var exitCode int
	switch options.Action {
	case "merge":
		exitCode = handleMerge(&options.Merge)
	case "fan":
		exitCode = handleFan(&options.Fan)
	case "vaultinfo":
		exitCode = handleVaultInfo(&options)
	case "json":
		exitCode = handleJSON(options.JSON)
	case "diff":
		exitCode = handleDiff(options.Diff.Files, options.Color)
	default:
		usage()
		return
	}
	exit(exitCode)
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

// convertLegacyMap converts map[interface{}]interface{} returned by simpleyaml
// to map[string]interface{} for use throughout graft. This is a temporary bridge
// until simpleyaml is replaced by yaml.v3 in Task 1.9.
func convertLegacyMap(m map[interface{}]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(m))
	for k, v := range m {
		result[fmt.Sprintf("%v", k)] = convertLegacyValue(v)
	}
	return result
}

func convertLegacyValue(v interface{}) interface{} {
	switch val := v.(type) {
	case map[interface{}]interface{}:
		return convertLegacyMap(val)
	case []interface{}:
		result := make([]interface{}, len(val))
		for i, elem := range val {
			result[i] = convertLegacyValue(elem)
		}
		return result
	default:
		return v
	}
}

func parseYAML(data []byte) (map[string]interface{}, error) {
	y, err := simpleyaml.NewYaml(data)
	if err != nil {
		return nil, err
	}

	if emptyY, emptyErr := simpleyaml.NewYaml([]byte{}); emptyErr == nil && *y == *emptyY {
		log.DEBUG("YAML doc is empty, creating empty hash/map")
		return make(map[string]interface{}), nil
	}

	rawDoc, err := y.Map()

	if err != nil {
		if _, arrayErr := y.Array(); arrayErr == nil {
			return nil, RootIsArrayError{msg: ansi.Sprintf("@R{Root of YAML document is not a hash/map}: %s\n", err)}
		}
		return nil, ansi.Errorf("@R{Root of YAML document is not a hash/map}: %s\n", err.Error())
	}

	doc := convertLegacyMap(rawDoc)
	return doc, nil
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

func cmdMergeEval(options *mergeOpts) (map[string]interface{}, error) {
	files := []YamlFile{}

	if len(options.Files) < 1 {
		stdinInfo, err := os.Stdin.Stat()
		if err != nil {
			return nil, ansi.Errorf("@R{Error statting STDIN} - Bailing out: %s\n", err.Error())
		}

		if stdinInfo.Mode()&os.ModeCharDevice != 0 {
			return nil, ansi.Errorf("@R{Error reading STDIN}: no data found. Did you forget to pipe data to STDIN, or specify yaml files to merge?")
		}

		options.Files = append(options.Files, "-")
	}

	for _, file := range options.Files {
		if options.MultiDoc {
			docs, err := splitLoadYamlFile(file)
			if err != nil {
				return nil, err
			}
			files = append(files, docs...)
		} else {
			yamlFile, err := loadYamlFile(file)
			if err != nil {
				return nil, err
			}
			files = append(files, yamlFile)
		}
	}

	result, err := mergeAllDocs(files, options)
	if err != nil {
		return nil, err
	}

	return result, nil
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
		result, err := mergeAllDocs([]YamlFile{source, doc}, options)
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

func formatVaultRefs() string {
	refs := yamlVaultRefs{}
	for secret, srcs := range graft.VaultRefs {
		refs.Secrets = append(refs.Secrets, yamlVaultSecret{secret, srcs})
	}

	sort.Sort(byKey(refs.Secrets))
	for _, secret := range refs.Secrets {
		sort.Strings(secret.References)
	}

	output, err := yaml.Marshal(refs)
	if err != nil {
		panic(fmt.Sprintf("Could not marshal YAML for vault references: %+v", graft.VaultRefs))
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
func mergeAllDocs(files []YamlFile, options *mergeOpts) (map[string]interface{}, error) {
	// Create engine with settings from options
	engineOpts := []graft.EngineOption{
		graft.WithCache(true, 1000),
		graft.WithConcurrency(10),
	}

	// Set dataflow order if specified (default to alphabetical if not set)
	dataflowOrder := options.DataflowOrder
	if dataflowOrder == "" {
		dataflowOrder = "alphabetical"
	}
	engineOpts = append(engineOpts, graft.WithDataflowOrder(dataflowOrder))

	engine, err := graft.NewEngine(engineOpts...)
	if err != nil {
		return nil, ansi.Errorf("@R{Failed to create graft engine}: %s", err.Error())
	}

	// Parse all documents
	docs := []graft.Document{}
	for _, file := range files {
		log.DEBUG("Processing file '%s'", file.Path)

		data, readErr := readFile(&file)
		if readErr != nil {
			return nil, readErr
		}

		// Check if it's a go-patch document
		if options.EnableGoPatch {
			_, parseErr := parseYAML(data)
			if isArrayError(parseErr) {
				log.DEBUG("Detected root of document as an array. Attempting go-patch parsing")
				ops, patchErr := parseGoPatch(data)
				if patchErr != nil {
					return nil, ansi.Errorf("@m{%s}: @R{%s}\n", file.Path, patchErr.Error())
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
			return nil, ansi.Errorf("@m{%s}: @R{%s}\n", file.Path, parseDocErr.Error())
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
			return nil, err
		}
		return nil, ansi.Errorf("@R{Merge failed}: %s", err.Error())
	}

	// Get the raw data for backward compatibility
	// The CLI expects a map[string]interface{}
	data, ok := merged.GetData().(map[string]interface{})
	if !ok {
		return nil, ansi.Errorf("@R{Merge result is not a map}")
	}
	return data, nil
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
