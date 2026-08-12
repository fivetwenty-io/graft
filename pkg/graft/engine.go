package graft

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/fivetwenty-io/graft/internal/cache"
	"github.com/fivetwenty-io/graft/internal/config"
	"github.com/fivetwenty-io/graft/internal/features"
	"github.com/fivetwenty-io/graft/internal/metrics"
	"github.com/fivetwenty-io/graft/internal/parallel"
	"github.com/fivetwenty-io/graft/log"
	"github.com/fivetwenty-io/graft/pkg/graft/interfaces"

	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// DefaultEngine is the default implementation of the Engine interface
// It provides all the core functionality needed by graft.
type DefaultEngine struct {
	// Configuration
	opts EngineOptions

	// Operator registry (clone of DefaultRegistry, plus engine-local overrides)
	registry       *UnifiedOperatorRegistry
	localOperators map[string]bool // tracks operators registered on this engine (not inherited)
	opMutex        sync.RWMutex

	// Vault state (refs tracking only; client and cache live in internal/backends/vault)
	vaultRefs  map[string][]string
	vaultMutex sync.RWMutex
	skipVault  bool

	// AWS state (client and cache live in internal/backends/aws)
	skipAws bool

	// NATS state
	skipNats bool

	// Static IPs state
	usedIPs map[string]string
	ipMutex sync.RWMutex

	// Prune state
	keysToPrune []string
	pruneMutex  sync.RWMutex

	// Sort state
	pathsToSort map[string]string
	sortMutex   sync.RWMutex

	// Warning suppression
	suppressWarnings bool

	// Metrics and monitoring
	metrics *EngineMetrics

	// Document memory for tracking history
	documentMemory *DocumentMemory

	// New infrastructure systems (all optional)

	// Features controls feature flags for the engine
	Features *features.FeatureFlags

	// Cache provides optional caching for operator results (nil if disabled)
	Cache cache.Cache

	// MetricsRegistry provides optional metrics collection (nil if disabled)
	MetricsRegistry *metrics.Registry

	// Config provides unified configuration management (nil uses defaults)
	Config *config.Config

	// Pool provides optional worker pool for parallel evaluation (nil if disabled)
	Pool *parallel.WorkerPool
}

// EngineMetrics tracks engine performance metrics.
type EngineMetrics struct {
	OperatorCalls map[string]int64
	CacheHits     int64
	CacheMisses   int64
	VaultCalls    int64
	AWSCalls      int64
}

// defaultEngineOpts returns default EngineOptions for NewDefaultEngine.
func defaultEngineOpts() EngineOptions {
	return EngineOptions{
		EnableCache:    true,
		CacheSize:      10000,
		EnableParallel: false,
		MaxConcurrency: 4,
		DataflowOrder:  "alphabetical",
	}
}

// NewDefaultEngine creates a new default engine with default configuration.
func NewDefaultEngine() *DefaultEngine {
	opts := defaultEngineOpts()
	e := newEngineFromOptions(&opts)
	return e
}

// RegisterOperator registers a custom operator in the engine's registry clone.
// Returns an error if the operator was already explicitly registered on this engine
// (i.e. not just inherited from DefaultRegistry). Custom operators can shadow
// built-in operators that were inherited from DefaultRegistry.
func (e *DefaultEngine) RegisterOperator(name string, op Operator) error {
	e.opMutex.Lock()
	defer e.opMutex.Unlock()

	// Fail if operator was already locally registered on this engine
	if e.localOperators[name] {
		return fmt.Errorf("operator %s already registered", name)
	}

	// Preserve existing metadata if available (from DefaultRegistry clone); override implementation.
	var entry *UnifiedOperatorEntry
	if existing, exists := e.registry.Get(name); exists {
		entryCopy := *existing
		entryCopy.Implementation = op
		entry = &entryCopy
	} else {
		entry = &UnifiedOperatorEntry{
			Name:           name,
			Precedence:     PrecedenceCall,
			MinArgs:        0,
			MaxArgs:        -1,
			Phase:          EvalPhase,
			Implementation: op,
		}
	}
	if err := e.registry.Register(entry); err != nil {
		return err
	}
	e.localOperators[name] = true
	return nil
}

// GetOperator retrieves an operator by name from the engine's registry clone.
func (e *DefaultEngine) GetOperator(name string) (Operator, bool) {
	e.opMutex.RLock()
	defer e.opMutex.RUnlock()

	return e.registry.GetImplementation(name)
}

// EngineContext interface methods for internal operator access

// AddVaultRef records a Vault reference for tracking.
func (e *DefaultEngine) AddVaultRef(path string, keys []string) {
	e.vaultMutex.Lock()
	defer e.vaultMutex.Unlock()

	if e.vaultRefs[path] == nil {
		e.vaultRefs[path] = []string{}
	}
	e.vaultRefs[path] = append(e.vaultRefs[path], keys...)
}

// IsVaultSkipped returns true if Vault operations should be skipped.
func (e *DefaultEngine) IsVaultSkipped() bool {
	return e.skipVault
}

// IsAWSSkipped returns true if AWS operations should be skipped.
func (e *DefaultEngine) IsAWSSkipped() bool {
	return e.skipAws
}

// GetUsedIPs returns a copy of the used IP addresses map.
func (e *DefaultEngine) GetUsedIPs() map[string]string {
	e.ipMutex.RLock()
	defer e.ipMutex.RUnlock()

	ips := make(map[string]string)
	for k, v := range e.usedIPs {
		ips[k] = v
	}
	return ips
}

// SetUsedIP records a used IP address.
func (e *DefaultEngine) SetUsedIP(key, ip string) {
	e.ipMutex.Lock()
	defer e.ipMutex.Unlock()
	e.usedIPs[key] = ip
}

// AddKeyToPrune adds a key to the list of keys to prune.
func (e *DefaultEngine) AddKeyToPrune(key string) {
	e.pruneMutex.Lock()
	defer e.pruneMutex.Unlock()
	e.keysToPrune = append(e.keysToPrune, key)
}

// GetKeysToPrune returns a copy of the keys to prune.
func (e *DefaultEngine) GetKeysToPrune() []string {
	e.pruneMutex.RLock()
	defer e.pruneMutex.RUnlock()

	keys := make([]string, len(e.keysToPrune))
	copy(keys, e.keysToPrune)
	return keys
}

// AddPathToSort adds a path to sort with the specified order.
func (e *DefaultEngine) AddPathToSort(path, order string) {
	e.sortMutex.Lock()
	defer e.sortMutex.Unlock()
	e.pathsToSort[path] = order
}

// GetPathsToSort returns a copy of the paths to sort.
func (e *DefaultEngine) GetPathsToSort() map[string]string {
	e.sortMutex.RLock()
	defer e.sortMutex.RUnlock()

	paths := make(map[string]string)
	for k, v := range e.pathsToSort {
		paths[k] = v
	}
	return paths
}

// GetVaultRefs returns a copy of the vault references map.
func (e *DefaultEngine) GetVaultRefs() map[string][]string {
	e.vaultMutex.RLock()
	defer e.vaultMutex.RUnlock()

	result := make(map[string][]string)
	for k, v := range e.vaultRefs {
		refs := make([]string, len(v))
		copy(refs, v)
		result[k] = refs
	}
	return result
}

// ResetVaultRefs clears the vault references map.
func (e *DefaultEngine) ResetVaultRefs() {
	e.vaultMutex.Lock()
	defer e.vaultMutex.Unlock()
	e.vaultRefs = make(map[string][]string)
}

// SetSkipVault sets whether vault operations should be skipped.
func (e *DefaultEngine) SetSkipVault(v bool) {
	e.skipVault = v
}

// SetSkipAws sets whether AWS operations should be skipped.
func (e *DefaultEngine) SetSkipAws(v bool) {
	e.skipAws = v
}

// SetSkipNats sets whether NATS operations should be skipped.
func (e *DefaultEngine) SetSkipNats(v bool) {
	e.skipNats = v
}

// IsNATSSkipped returns true if NATS operations should be skipped.
func (e *DefaultEngine) IsNATSSkipped() bool {
	return e.skipNats
}

// ResetKeysToPrune clears the keys to prune list.
func (e *DefaultEngine) ResetKeysToPrune() {
	e.pruneMutex.Lock()
	defer e.pruneMutex.Unlock()
	e.keysToPrune = nil
}

// ResetPathsToSort clears the paths to sort map.
func (e *DefaultEngine) ResetPathsToSort() {
	e.sortMutex.Lock()
	defer e.sortMutex.Unlock()
	e.pathsToSort = make(map[string]string)
}

// ResetUsedIPs clears the used IPs map.
func (e *DefaultEngine) ResetUsedIPs() {
	e.ipMutex.Lock()
	defer e.ipMutex.Unlock()
	e.usedIPs = make(map[string]string)
}

// resetPerRunState clears the prune/sort/used-IP markers that accumulate
// during a single evaluate() run. Called via defer at the top of evaluate
// so it fires exactly once per run, after this run's own post-processing
// has already read the markers it needs.
func (e *DefaultEngine) resetPerRunState() {
	e.ResetKeysToPrune()
	e.ResetPathsToSort()
	e.ResetUsedIPs()
}

// SuppressWarnings returns whether warnings should be suppressed.
func (e *DefaultEngine) SuppressWarnings() bool {
	return e.suppressWarnings
}

// SetSuppressWarnings sets whether warnings should be suppressed.
func (e *DefaultEngine) SetSuppressWarnings(v bool) {
	e.suppressWarnings = v
}

// Internal methods

func (e *DefaultEngine) createEvaluator(t map[string]interface{}) *Evaluator {
	here, _ := tree.ParseCursor("$")
	ev := &Evaluator{
		Tree:          t,
		Deps:          map[string][]tree.Cursor{},
		Here:          here,
		engine:        e,
		DataflowOrder: e.opts.DataflowOrder,
	}

	// Set memory tracker if available
	if e.documentMemory != nil {
		ev.SetMemoryTracker(e.documentMemory)
	}

	return ev
}

// GetMemoryTracker returns the memory tracker interface.
func (e *DefaultEngine) GetMemoryTracker() interfaces.MemoryTracker {
	if e.documentMemory == nil {
		return nil
	}
	return e.documentMemory
}

//nolint:gocyclo // evaluation pipeline with multiple phases and post-processing is inherently complex
func (e *DefaultEngine) evaluate(ctx context.Context, ev *Evaluator) error {
	// Reset per-run prune/sort/used-IP markers on every exit path (success,
	// any phase error, context cancellation, sort failure) so a reused
	// engine never leaks one Merge().Execute() run's state into the next.
	// The legacy Evaluator.Run() does the equivalent reset for prune/sort
	// (evaluator.go ResetKeysToPrune/ResetPathsToSort) right after capturing
	// them for post-processing; this live path consumes GetKeysToPrune/
	// GetPathsToSort below but never resets, so a later unrelated Execute()
	// call on the same engine silently inherits and re-applies this run's
	// prune/sort markers. Deferring the reset (rather than resetting inline
	// after each Get) guarantees it fires exactly once per run regardless
	// of which return statement is hit, while leaving this run's own
	// post-processing below - which reads the markers before the deferred
	// reset runs - unaffected. usedIPs gets the same treatment: spruce's
	// StaticIPOperator.Setup() clears its package-level UsedIPs map on
	// every phase run (op_static_ips.go), but graft's Setup() is a no-op -
	// ResetUsedIPs is otherwise never called anywhere - so static_ips
	// claims leak across engine reuse the same way prune/sort markers do.
	defer e.resetPerRunState()

	// Check context cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Set the engine on the evaluator
	ev.engine = Engine(e)

	// A non-empty REDACT environment variable forces vault, AWS, and NATS
	// operators to return the literal string "REDACTED" instead of making a
	// backend call, matching spruce's evaluator.go REDACT semantics. This
	// check must live on the DefaultEngine.Evaluate path (not only on the
	// legacy Evaluator.Run path) because Evaluate is what the CLI merge
	// path (MergeBuilder) actually calls.
	if os.Getenv("REDACT") != "" {
		state := e.GetOperatorState()
		state.SetSkipVault(true)
		state.SetSkipAws(true)
		state.SetSkipNats(true)
	}

	// Record evaluation start time if metrics are enabled
	var startTime time.Time
	if e.IsFeatureEnabled(features.FeatureMetrics) && e.MetricsRegistry != nil {
		startTime = time.Now()
	}

	// Run evaluation phases. MergePhase errors are accumulated rather than
	// returned immediately, so ParamPhase and EvalPhase still run and their
	// errors are combined with any MergePhase errors into a single report -
	// this matches spruce's evaluator.go Run(), which appends MergePhase
	// errors to a running MultiError instead of short-circuiting on them.
	// ParamPhase errors still abort evaluation before EvalPhase runs and are
	// returned on their own (dropping any accumulated MergePhase errors),
	// matching spruce: once a required param is unresolved, downstream
	// EvalPhase operators can't be trusted to evaluate meaningfully, so
	// spruce doesn't bother combining prior merge errors into that report.
	var mergeErrs MultiError
	for _, phase := range []OperatorPhase{MergePhase, ParamPhase, EvalPhase} {
		// Check context cancellation before each phase
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		var phaseErr error
		if e.IsFeatureEnabled(features.FeatureParallelEvaluation) && e.Pool != nil {
			phaseErr = ev.RunPhaseParallel(phase)
		} else {
			phaseErr = ev.RunPhase(phase)
		}
		if phaseErr != nil {
			// Record failure metric if enabled
			if e.IsFeatureEnabled(features.FeatureMetrics) && e.MetricsRegistry != nil {
				phaseName := phaseToString(phase)
				counter := e.MetricsRegistry.GetOrCreateCounter("graft_evaluation_errors_total", metrics.Labels{"phase": phaseName})
				counter.Inc()
			}

			switch phase {
			case MergePhase:
				mergeErrs.Append(phaseErr)
				continue
			case ParamPhase:
				return phaseErr
			case EvalPhase:
				mergeErrs.Append(phaseErr)
				return mergeErrs
			default:
				mergeErrs.Append(phaseErr)
				return mergeErrs
			}
		}
	}

	if len(mergeErrs.Errors) > 0 {
		return mergeErrs
	}

	// Record successful evaluation metrics if enabled
	if e.IsFeatureEnabled(features.FeatureMetrics) && e.MetricsRegistry != nil {
		duration := time.Since(startTime)
		counter := e.MetricsRegistry.GetOrCreateCounter("graft_evaluations_total", nil)
		counter.Inc()
		// Record duration as gauge (simplified histogram)
		gauge := e.MetricsRegistry.GetOrCreateGauge("graft_evaluation_duration_seconds", nil)
		gauge.Set(duration.Seconds())
	}

	// Post-processing: apply operator-level pruning
	prunePaths := e.GetKeysToPrune()
	log.DEBUG("Engine: Found %d prune paths to process: %v", len(prunePaths), prunePaths)
	if len(prunePaths) > 0 {
		// Convert tree paths to Document paths and remove them
		doc := NewDocument(ev.Tree)
		for _, path := range prunePaths {
			// Remove the "$." prefix if present
			cleanPath := strings.TrimPrefix(path, "$.")
			log.DEBUG("Engine: Pruning path '%s' (cleaned: '%s')", path, cleanPath)
			doc = doc.Prune(cleanPath)
		}
		// Update the evaluator tree with the pruned document
		if pruned, ok := doc.RawData().(map[string]interface{}); ok {
			ev.Tree = pruned
		}
	}

	// Post-processing: apply sort operations
	sortPaths := e.GetPathsToSort()
	log.DEBUG("Engine: Found %d sort paths to process: %v", len(sortPaths), sortPaths)
	if len(sortPaths) > 0 {
		for path, sortKey := range sortPaths {
			// Remove the "$." prefix if present
			cleanPath := strings.TrimPrefix(path, "$.")
			log.DEBUG("Engine: Sorting path '%s' by key '%s'", cleanPath, sortKey)

			// Navigate to the list at the path
			value, err := getValueAtPath(ev.Tree, cleanPath)
			if err != nil {
				log.DEBUG("Engine: Failed to get value at path '%s': %v", cleanPath, err)
				continue
			}

			// Check if it's a list
			if list, ok := value.([]interface{}); ok {
				// Sort the list in place. A sort failure (e.g. a quoted
				// (( sort by "key" )) taken literally against maps keyed
				// by the unquoted key) is fatal in spruce, not a
				// best-effort skip, so it must fail evaluation here too.
				if err := SortList(cleanPath, list, sortKey); err != nil {
					log.DEBUG("Engine: Failed to sort list at path '%s': %v", cleanPath, err)
					// Wrap in MultiError so the CLI renders spruce's
					// "N error(s) detected:\n - $.path: msg" format
					// instead of the generic "Merge failed: ..." wrapper
					// used for raw, unaggregated errors.
					return MultiError{Errors: []error{err}}
				}
				log.DEBUG("Engine: Successfully sorted list at path '%s'", cleanPath)
			} else {
				log.DEBUG("Engine: Value at path '%s' is not a list, skipping sort", cleanPath)
			}
		}
	}

	return nil
}

// Implement Engine interface methods

// ParseYAML parses YAML data into a Document.
func (e *DefaultEngine) ParseYAML(data []byte) (Document, error) {
	// Handle empty data
	if len(data) == 0 {
		return nil, nil
	}

	// Expand (( if )), (( for )), (( while )), (( case )) control-flow
	// constructs into plain YAML before anything else touches the bytes.
	// A document with no control-flow markers is returned unchanged by
	// the expander, so this is a no-op for the overwhelming majority of
	// callers. See pkg/graft/controlflow_hook.go for why this is a
	// function-pointer hook rather than a direct import.
	if ControlFlowExpander != nil {
		expanded, cfErr := ControlFlowExpander(e, data)
		if cfErr != nil {
			return nil, NewParseError(fmt.Sprintf("control flow expansion failed: %s", cfErr.Error()), cfErr)
		}
		data = expanded
	}

	// Work around a goccy/go-yaml v1.19.2 parser bug where a bare "-"
	// sequence terminator followed by a sibling map key gets misparsed
	// into the sequence (see sanitizeBareSequenceTerminators).
	data = sanitizeBareSequenceTerminators(data)

	// Quote graft's <<<: inject keys for goccy/go-yaml compatibility
	data = QuoteInjectKeys(data)

	// First parse as generic interface to check document type. Quoted
	// YAML 1.1 boolean-lookalike scalars ("yes", 'On', "OFF", ...) are
	// tagged during this parse so the compat conversion below skips
	// them, matching spruce (quoting is an explicit request to keep the
	// value a string).
	genericResult, err := ParseYAML11CompatAware(data)
	if err != nil {
		// Fold goccy's own error text (it carries line/column detail, the
		// same information spruce's underlying library reports) into the
		// message instead of dropping it, so a human debugging kit YAML
		// gets a usable location, not just "failed to parse YAML".
		return nil, NewParseError(fmt.Sprintf("failed to parse YAML: %s", err.Error()), err)
	}

	if genericResult == nil {
		return nil, nil
	}

	// Check that root is a map/hash — yaml.v3 returns map[string]interface{}
	switch result := genericResult.(type) {
	case map[string]interface{}:
		// Apply YAML 1.1 boolean compatibility conversions (yes/no/on/off → bool)
		converted := DefaultYAMLCompat().ConvertMapValues(result)
		converted = UnprotectYAML11QuotedBools(converted).(map[string]interface{})
		return NewDocument(converted), nil
	case map[interface{}]interface{}:
		// yaml.v3 produces this when all root keys are non-strings
		converted := make(map[string]interface{}, len(result))
		for k, v := range result {
			converted[fmt.Sprintf("%v", k)] = v
		}
		final := DefaultYAMLCompat().ConvertMapValues(converted)
		final = UnprotectYAML11QuotedBools(final).(map[string]interface{})
		return NewDocument(final), nil
	default:
		// Return plain error for compatibility with tests
		return nil, fmt.Errorf("root of YAML document is not a hash/map")
	}
}

// ParseMultiDocYAML splits multi-document YAML (separated by "\n---\n") and
// parses each document. An empty leading document produced by a file that
// begins with the separator line is silently discarded.
func (e *DefaultEngine) ParseMultiDocYAML(data []byte) ([]Document, error) {
	rawDocs := bytes.Split(data, []byte("\n---\n"))

	// Strip empty leading doc if the file starts with a --- separator
	if len(rawDocs) > 0 && len(bytes.TrimSpace(rawDocs[0])) == 0 {
		rawDocs = rawDocs[1:]
	}

	docs := make([]Document, 0, len(rawDocs))
	for _, docBytes := range rawDocs {
		doc, err := e.ParseYAML(docBytes)
		if err != nil {
			return nil, err
		}
		if doc != nil {
			docs = append(docs, doc)
		}
	}
	return docs, nil
}

// ParseJSON parses JSON data into a Document.
func (e *DefaultEngine) ParseJSON(data []byte) (Document, error) {
	// Handle empty data
	if len(data) == 0 {
		return nil, nil
	}

	var result map[string]interface{}
	err := json.Unmarshal(data, &result)
	if err != nil {
		// See the matching comment in ParseYAML: fold the underlying
		// decode error into the message so its detail is not dropped.
		return nil, NewParseError(fmt.Sprintf("failed to parse JSON: %s", err.Error()), err)
	}

	if result == nil {
		return nil, nil
	}

	return NewDocument(result), nil
}

// ParseFile and ParseReader are implemented in parse_file.go.

// Merge creates a new merge builder for combining documents.
func (e *DefaultEngine) Merge(ctx context.Context, docs ...Document) MergeBuilder {
	if ctx == nil {
		ctx = context.Background()
	}

	return &mergeBuilderImpl{
		engine: e,
		ctx:    ctx,
		docs:   docs,
	}
}

// MergeFiles creates a merge builder for files.
func (e *DefaultEngine) MergeFiles(ctx context.Context, paths ...string) MergeBuilder {
	// Implementation will be added
	return nil
}

// MergeReaders creates a merge builder for readers.
func (e *DefaultEngine) MergeReaders(ctx context.Context, readers ...io.Reader) MergeBuilder {
	// Implementation will be added
	return nil
}

// Evaluate processes operators in a document.
func (e *DefaultEngine) Evaluate(ctx context.Context, doc Document) (Document, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	// Get the raw data
	data, ok := doc.RawData().(map[string]interface{})
	if !ok {
		return nil, fmt.Errorf("document data is not a map")
	}

	// Create evaluator
	ev := e.createEvaluator(data)

	// Extract cherry-pick paths from context if present
	if cherryPickPaths := GetCherryPickPaths(ctx); len(cherryPickPaths) > 0 {
		ev.CherryPickPaths = cherryPickPaths
		ev.Only = cherryPickPaths // Also set the original field for backward compatibility
	}

	// Extract calc-modification prior values recorded during merge, if any.
	if priorValues := GetPriorCalcValues(ctx); len(priorValues) > 0 {
		ev.PriorValues = priorValues
	}

	// Run evaluation
	err := e.evaluate(ctx, ev)
	if err != nil {
		return nil, err
	}

	// Return evaluated document
	return NewDocument(ev.Tree), nil
}

// ToYAML converts a document to YAML bytes.
func (e *DefaultEngine) ToYAML(doc Document) ([]byte, error) {
	// Implementation will be added
	return nil, fmt.Errorf("not implemented")
}

// ToJSON converts a document to JSON bytes.
func (e *DefaultEngine) ToJSON(doc Document) ([]byte, error) {
	// Implementation will be added
	return nil, fmt.Errorf("not implemented")
}

// ToJSONIndent converts a document to indented JSON bytes.
func (e *DefaultEngine) ToJSONIndent(doc Document, indent string) ([]byte, error) {
	// Implementation will be added
	return nil, fmt.Errorf("not implemented")
}

// UnregisterOperator removes an operator from the engine's registry clone.
// Note: this removes from the clone only, not from DefaultRegistry.
func (e *DefaultEngine) UnregisterOperator(name string) error {
	e.opMutex.Lock()
	defer e.opMutex.Unlock()

	e.registry.mu.Lock()
	delete(e.registry.operators, name)
	e.registry.mu.Unlock()
	delete(e.localOperators, name)
	return nil
}

// ListOperators returns all available operators from the engine's registry clone.
func (e *DefaultEngine) ListOperators() []string {
	e.opMutex.RLock()
	defer e.opMutex.RUnlock()

	return e.registry.ListOperators()
}

// WithLogger sets a new logger (returns new engine instance).
func (e *DefaultEngine) WithLogger(logger Logger) Engine {
	// For now, return self as logging is not implemented yet
	return e
}

// WithVaultClient sets a new vault client (returns new engine instance).
func (e *DefaultEngine) WithVaultClient(client VaultClient) Engine {
	// For now, return self as custom vault client is not implemented yet
	return e
}

// WithAWSConfig sets AWS configuration (returns new engine instance).
func (e *DefaultEngine) WithAWSConfig(_ AWSConfig) Engine {
	// For now, return self as AWS config is not fully implemented yet
	return e
}

// UpdateOptions replaces the engine options and re-applies skip flags.
func (e *DefaultEngine) UpdateOptions(opts EngineOptions) {
	e.opts = opts
	e.skipVault = opts.SkipVault
	e.skipAws = opts.SkipAws
}

// GetOperatorState returns the operator state interface.
func (e *DefaultEngine) GetOperatorState() OperatorState {
	// The engine itself implements OperatorState
	return e
}

// GetDocumentMemory returns the document memory tracker.
func (e *DefaultEngine) GetDocumentMemory() *DocumentMemory {
	return e.documentMemory
}

// EnableMemoryTracking enables document memory tracking.
func (e *DefaultEngine) EnableMemoryTracking() {
	if e.documentMemory == nil {
		// Create with default config if not already created
		var memConfig MemoryConfig
		if e.opts.MemoryConfig != nil {
			memConfig = *e.opts.MemoryConfig
		}
		memConfig.Enabled = true
		e.documentMemory = NewDocumentMemory(memConfig)
	} else {
		e.documentMemory.Enable()
	}
}

// DisableMemoryTracking disables document memory tracking.
func (e *DefaultEngine) DisableMemoryTracking() {
	if e.documentMemory != nil {
		e.documentMemory.Disable()
	}
}

// GetMemoryStats returns memory tracking statistics.
func (e *DefaultEngine) GetMemoryStats() map[string]interface{} {
	if e.documentMemory == nil {
		return map[string]interface{}{
			"enabled": false,
		}
	}
	return e.documentMemory.GetMemoryStats()
}

// ClearMemoryHistory clears all tracked history.
func (e *DefaultEngine) ClearMemoryHistory() {
	if e.documentMemory != nil {
		e.documentMemory.Clear()
	}
}

// GetNodeHistory returns the history for a specific path.
func (e *DefaultEngine) GetNodeHistory(path string) (*NodeHistory, error) {
	if e.documentMemory == nil {
		return nil, fmt.Errorf("memory tracking is not enabled")
	}
	return e.documentMemory.GetHistory(path)
}

// QueryMemoryHistory queries the history with filters.
func (e *DefaultEngine) QueryMemoryHistory(filter HistoryFilter) []ChangeEvent {
	if e.documentMemory == nil {
		return []ChangeEvent{}
	}
	return e.documentMemory.Query(filter)
}

// Infrastructure system accessors

// GetFeatureFlags returns the feature flags for this engine.
func (e *DefaultEngine) GetFeatureFlags() *features.FeatureFlags {
	return e.Features
}

// SetFeatureFlags sets the feature flags for this engine.
func (e *DefaultEngine) SetFeatureFlags(ff *features.FeatureFlags) {
	e.Features = ff
}

// IsFeatureEnabled checks if a feature flag is enabled.
func (e *DefaultEngine) IsFeatureEnabled(flag string) bool {
	if e.Features == nil {
		return false
	}
	return e.Features.IsEnabled(flag)
}

// GetCache returns the cache for this engine (may be nil).
func (e *DefaultEngine) GetCache() cache.Cache {
	return e.Cache
}

// SetCache sets the cache for this engine.
func (e *DefaultEngine) SetCache(c cache.Cache) {
	e.Cache = c
}

// GetMetricsRegistry returns the metrics registry for this engine (may be nil).
func (e *DefaultEngine) GetMetricsRegistry() *metrics.Registry {
	return e.MetricsRegistry
}

// SetMetricsRegistry sets the metrics registry for this engine.
func (e *DefaultEngine) SetMetricsRegistry(r *metrics.Registry) {
	e.MetricsRegistry = r
}

// GetConfig returns the configuration for this engine (may be nil).
func (e *DefaultEngine) GetConfig() *config.Config {
	return e.Config
}

// SetConfig sets the configuration for this engine.
func (e *DefaultEngine) SetConfig(cfg *config.Config) {
	e.Config = cfg
}

// GetWorkerPool returns the worker pool for this engine (may be nil).
func (e *DefaultEngine) GetWorkerPool() *parallel.WorkerPool {
	return e.Pool
}

// SetWorkerPool sets the worker pool for this engine.
func (e *DefaultEngine) SetWorkerPool(pool *parallel.WorkerPool) {
	e.Pool = pool
}

// newEngineFromOptions builds a *DefaultEngine directly from *EngineOptions.
// It does not validate options; callers that need validation should call
// createEngineFromOptions instead.
func newEngineFromOptions(opts *EngineOptions) *DefaultEngine {
	e := &DefaultEngine{
		opts:           *opts,
		registry:       DefaultRegistry.Clone(),
		localOperators: make(map[string]bool),
		vaultRefs:      make(map[string][]string),
		usedIPs:        make(map[string]string),
		pathsToSort:    make(map[string]string),
		skipVault:      opts.SkipVault,
		skipAws:        opts.SkipAws,
		skipNats:       opts.SkipNats,
		metrics: &EngineMetrics{
			OperatorCalls: make(map[string]int64),
		},
	}

	// Initialize document memory if a config was provided and enabled
	if opts.MemoryConfig != nil && opts.MemoryConfig.Enabled {
		e.documentMemory = NewDocumentMemory(*opts.MemoryConfig)
	}

	return e
}

// createEngineFromOptions creates an engine from EngineOptions (used by NewEngine in api.go).
//
//nolint:gocyclo // engine configuration requires handling many optional settings
func createEngineFromOptions(opts *EngineOptions) (Engine, error) {
	// Validate options
	if opts.MaxConcurrency < 0 {
		return nil, NewConfigurationError("concurrency must be non-negative")
	}

	// Build the engine directly from opts
	engine := newEngineFromOptions(opts)

	// Register custom operators if any
	if opts.CustomOperators != nil {
		for name, op := range opts.CustomOperators {
			if err := engine.RegisterOperator(name, op); err != nil {
				return nil, err
			}
		}
	}

	// Enable memory tracking if requested (without a MemoryConfig)
	if opts.EnableMemoryTracking {
		engine.EnableMemoryTracking()
	}

	// Apply new infrastructure systems if provided

	// Set feature flags (use provided or create defaults)
	if opts.FeatureFlags != nil {
		engine.Features = opts.FeatureFlags
	} else {
		engine.Features = features.DefaultFlags()
	}

	// Apply caching options
	if opts.CacheInstance != nil {
		engine.Cache = opts.CacheInstance
	} else if opts.EnableCache && engine.IsFeatureEnabled(features.FeatureCaching) {
		// Create default cache if caching is enabled
		engine.Cache = cache.NewCache(cache.WithMaxSize(opts.CacheSize))
	}

	// Apply metrics registry
	if opts.MetricsRegistry != nil {
		engine.MetricsRegistry = opts.MetricsRegistry
	} else if opts.EnableMetrics && engine.IsFeatureEnabled(features.FeatureMetrics) {
		engine.MetricsRegistry = metrics.NewRegistry()
	}

	// Apply config
	if opts.ConfigInstance != nil {
		engine.Config = opts.ConfigInstance
	}

	// Apply worker pool
	if opts.WorkerPool != nil {
		engine.Pool = opts.WorkerPool
	} else if opts.EnableParallel && engine.IsFeatureEnabled(features.FeatureParallelEvaluation) {
		// Create default worker pool if parallel evaluation is enabled.
		// minWorkers comes from the resolved ConfigInstance's
		// Parallel.MinWorkers when one was supplied (WithConfigInstance),
		// instead of always hardcoding 1, so a config file/env override of
		// GRAFT_PARALLEL_MIN_WORKERS has an observable effect on pool
		// construction.
		minWorkers := 1
		if opts.ConfigInstance != nil && opts.ConfigInstance.Parallel.MinWorkers > 0 {
			minWorkers = opts.ConfigInstance.Parallel.MinWorkers
		}
		pool, err := parallel.NewPool(minWorkers, opts.MaxConcurrency)
		if err != nil {
			log.DEBUG("Warning: Failed to create worker pool: %v", err)
		} else {
			engine.Pool = pool
		}
	}

	return engine, nil
}

// phaseToString converts an OperatorPhase to a string for metrics labels.
func phaseToString(phase OperatorPhase) string {
	switch phase {
	case MergePhase:
		return "merge"
	case EvalPhase:
		return "eval"
	case ParamPhase:
		return "param"
	default:
		return valueTypeUnknown
	}
}
