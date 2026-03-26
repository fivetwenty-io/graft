package graft

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
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

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/secretsmanager/secretsmanageriface"
	"github.com/aws/aws-sdk-go/service/ssm/ssmiface"
	vaultkv "github.com/cloudfoundry-community/vaultkv"
	"gopkg.in/yaml.v3"

	"github.com/fivetwenty-io/graft/pkg/graft/tree"
)

// DefaultEngine is the default implementation of the Engine interface
// It provides all the core functionality needed by graft.
type DefaultEngine struct {
	// Configuration
	config EngineConfig

	// Operator registry (clone of DefaultRegistry, plus engine-local overrides)
	registry       *UnifiedOperatorRegistry
	localOperators map[string]bool // tracks operators registered on this engine (not inherited)
	opMutex        sync.RWMutex

	// Vault state
	vaultKV          *vaultkv.KV
	vaultSecretCache map[string]map[string]interface{}
	vaultRefs        map[string][]string
	vaultMutex       sync.RWMutex
	skipVault        bool

	// AWS state
	awsSession           *session.Session
	secretsManagerClient secretsmanageriface.SecretsManagerAPI
	parameterstoreClient ssmiface.SSMAPI
	awsSecretsCache      map[string]string
	awsParamsCache       map[string]string
	awsMutex             sync.RWMutex
	skipAws              bool

	// Static IPs state
	usedIPs map[string]string
	ipMutex sync.RWMutex

	// Prune state
	keysToPrune []string
	pruneMutex  sync.RWMutex

	// Sort state
	pathsToSort map[string]string
	sortMutex   sync.RWMutex

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

// EngineConfig holds configuration for the engine.
type EngineConfig struct {
	// Vault configuration
	VaultAddr    string
	VaultToken   string
	VaultSkipTLS bool
	SkipVault    bool

	// AWS configuration
	AWSRegion  string
	AWSProfile string
	SkipAWS    bool

	// Performance configuration
	EnableCaching  bool
	CacheSize      int
	EnableParallel bool
	MaxWorkers     int

	// Dataflow configuration
	DataflowOrder string // "alphabetical" (default) or "insertion"

	// Memory tracking configuration
	MemoryConfig MemoryConfig
}

// EngineMetrics tracks engine performance metrics.
type EngineMetrics struct {
	OperatorCalls map[string]int64
	CacheHits     int64
	CacheMisses   int64
	VaultCalls    int64
	AWSCalls      int64
}

// NewDefaultEngine creates a new default engine with default configuration.
func NewDefaultEngine() *DefaultEngine {
	return NewDefaultEngineWithConfig(DefaultEngineConfig())
}

// NewDefaultEngineWithConfig creates a new default engine with custom configuration.
//
//nolint:gocritic // hugeParam: config is passed by value for ownership semantics
func NewDefaultEngineWithConfig(config EngineConfig) *DefaultEngine {
	e := &DefaultEngine{
		config:           config,
		registry:         DefaultRegistry.Clone(),
		localOperators:   make(map[string]bool),
		vaultSecretCache: make(map[string]map[string]interface{}),
		vaultRefs:        make(map[string][]string),
		awsSecretsCache:  make(map[string]string),
		awsParamsCache:   make(map[string]string),
		usedIPs:          make(map[string]string),
		pathsToSort:      make(map[string]string),
		skipVault:        config.SkipVault,
		skipAws:          config.SkipAWS,
		metrics: &EngineMetrics{
			OperatorCalls: make(map[string]int64),
		},
	}

	// Initialize document memory if enabled
	if config.MemoryConfig.Enabled {
		e.documentMemory = NewDocumentMemory(config.MemoryConfig)
	}

	// Initialize vault if configured
	if !config.SkipVault && config.VaultAddr != "" {
		e.initializeVault()
	}

	// Initialize AWS if configured
	if !config.SkipAWS && config.AWSRegion != "" {
		e.initializeAWS()
	}

	return e
}

// DefaultEngineConfig returns default engine configuration.
func DefaultEngineConfig() EngineConfig {
	return EngineConfig{
		EnableCaching:  true,
		CacheSize:      10000,
		EnableParallel: false,
		MaxWorkers:     4,
		DataflowOrder:  "alphabetical", // Default to alphabetical ordering
		MemoryConfig: MemoryConfig{
			Enabled:            false, // Disabled by default
			MaxVersionsPerNode: 100,
			MaxTotalVersions:   10000,
			MaxMemoryMB:        100,
			CompressAfter:      24 * time.Hour,
			CleanupInterval:    15 * time.Minute,
			TrackMergePhase:    true,
			TrackEvalPhase:     true,
			EnableCompression:  true,
			CompressThreshold:  10,
		},
	}
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

// GetVaultClient returns the Vault client for secret operations.
func (e *DefaultEngine) GetVaultClient() *vaultkv.KV {
	e.vaultMutex.RLock()
	defer e.vaultMutex.RUnlock()
	return e.vaultKV
}

// GetVaultCache returns a copy of the cached Vault secrets.
func (e *DefaultEngine) GetVaultCache() map[string]map[string]interface{} {
	// Return a copy to avoid concurrent modification
	e.vaultMutex.RLock()
	defer e.vaultMutex.RUnlock()

	result := make(map[string]map[string]interface{})
	for k, v := range e.vaultSecretCache {
		result[k] = v
	}
	return result
}

// SetVaultCache caches Vault secret data for a path.
func (e *DefaultEngine) SetVaultCache(path string, data map[string]interface{}) {
	e.vaultMutex.Lock()
	defer e.vaultMutex.Unlock()
	e.vaultSecretCache[path] = data
}

// AddVaultRef records a Vault reference for tracking.
func (e *DefaultEngine) AddVaultRef(path string, keys []string) {
	e.vaultMutex.Lock()
	defer e.vaultMutex.Unlock()

	// Update internal vault refs
	if e.vaultRefs[path] == nil {
		e.vaultRefs[path] = []string{}
	}
	e.vaultRefs[path] = append(e.vaultRefs[path], keys...)

	// Also update global VaultRefs for backward compatibility with vaultinfo command
	if SkipVault || e.skipVault {
		if VaultRefs[path] == nil {
			VaultRefs[path] = []string{}
		}
		VaultRefs[path] = append(VaultRefs[path], keys...)
	}
}

// IsVaultSkipped returns true if Vault operations should be skipped.
func (e *DefaultEngine) IsVaultSkipped() bool {
	// Check both the engine's skipVault and the global SkipVault for backward compatibility
	return e.skipVault || SkipVault
}

// GetAWSSession returns the AWS session for API calls.
func (e *DefaultEngine) GetAWSSession() *session.Session {
	e.awsMutex.RLock()
	defer e.awsMutex.RUnlock()
	return e.awsSession
}

// GetSecretsManagerClient returns the AWS Secrets Manager client.
func (e *DefaultEngine) GetSecretsManagerClient() secretsmanageriface.SecretsManagerAPI {
	e.awsMutex.RLock()
	defer e.awsMutex.RUnlock()
	return e.secretsManagerClient
}

// GetParameterStoreClient returns the AWS Parameter Store client.
func (e *DefaultEngine) GetParameterStoreClient() ssmiface.SSMAPI {
	e.awsMutex.RLock()
	defer e.awsMutex.RUnlock()
	return e.parameterstoreClient
}

// GetAWSSecretsCache returns a copy of the cached AWS secrets.
func (e *DefaultEngine) GetAWSSecretsCache() map[string]string {
	e.awsMutex.RLock()
	defer e.awsMutex.RUnlock()

	result := make(map[string]string)
	for k, v := range e.awsSecretsCache {
		result[k] = v
	}
	return result
}

// SetAWSSecretCache caches an AWS secret value.
func (e *DefaultEngine) SetAWSSecretCache(key, value string) {
	e.awsMutex.Lock()
	defer e.awsMutex.Unlock()
	e.awsSecretsCache[key] = value
}

// GetAWSParamsCache returns a copy of the cached AWS parameters.
func (e *DefaultEngine) GetAWSParamsCache() map[string]string {
	e.awsMutex.RLock()
	defer e.awsMutex.RUnlock()

	result := make(map[string]string)
	for k, v := range e.awsParamsCache {
		result[k] = v
	}
	return result
}

// SetAWSParamCache caches an AWS parameter value.
func (e *DefaultEngine) SetAWSParamCache(key, value string) {
	e.awsMutex.Lock()
	defer e.awsMutex.Unlock()
	e.awsParamsCache[key] = value
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

// Internal methods

func (e *DefaultEngine) initializeVault() {
	// Initialize vault connection
	// Implementation will set up e.vaultKV
}

func (e *DefaultEngine) initializeAWS() {
	// Initialize AWS clients
	// Implementation will set up AWS session and clients
}

func (e *DefaultEngine) createEvaluator(t map[string]interface{}) *Evaluator {
	here, _ := tree.ParseCursor("$")
	ev := &Evaluator{
		Tree:          t,
		Deps:          map[string][]tree.Cursor{},
		Here:          here,
		engine:        e,
		DataflowOrder: e.config.DataflowOrder,
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
	// Check context cancellation
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}

	// Set the engine on the evaluator
	ev.engine = Engine(e)

	// Record evaluation start time if metrics are enabled
	var startTime time.Time
	if e.IsFeatureEnabled(features.FeatureMetrics) && e.MetricsRegistry != nil {
		startTime = time.Now()
	}

	// Run evaluation phases
	for _, phase := range []OperatorPhase{MergePhase, ParamPhase, EvalPhase} {
		// Check context cancellation before each phase
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if err := ev.RunPhase(phase); err != nil {
			// Record failure metric if enabled
			if e.IsFeatureEnabled(features.FeatureMetrics) && e.MetricsRegistry != nil {
				phaseName := phaseToString(phase)
				counter := e.MetricsRegistry.GetOrCreateCounter("graft_evaluation_errors_total", metrics.Labels{"phase": phaseName})
				counter.Inc()
			}
			return err
		}
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
				// Sort the list in place
				err := SortList(cleanPath, list, sortKey)
				if err != nil {
					log.DEBUG("Engine: Failed to sort list at path '%s': %v", cleanPath, err)
					// Don't fail the whole evaluation, just log the error
					continue
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

	// First parse as generic interface to check document type
	var genericResult interface{}
	err := yaml.Unmarshal(data, &genericResult)
	if err != nil {
		return nil, NewParseError("failed to parse YAML", err)
	}

	if genericResult == nil {
		return nil, nil
	}

	// Check that root is a map/hash — yaml.v3 returns map[string]interface{}
	switch result := genericResult.(type) {
	case map[string]interface{}:
		// Apply YAML 1.1 boolean compatibility conversions (yes/no/on/off → bool)
		converted := DefaultYAMLCompat().ConvertMapValues(result)
		return NewDocument(converted), nil
	default:
		// Return plain error for compatibility with tests
		return nil, fmt.Errorf("root of YAML document is not a hash/map")
	}
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
		return nil, NewParseError("failed to parse JSON", err)
	}

	if result == nil {
		return nil, nil
	}

	return NewDocument(result), nil
}

// ParseFile parses a file into a Document.
func (e *DefaultEngine) ParseFile(path string) (Document, error) {
	// Implementation will be added
	return nil, fmt.Errorf("not implemented")
}

// ParseReader parses data from a reader into a Document.
func (e *DefaultEngine) ParseReader(reader io.Reader) (Document, error) {
	// Implementation will be added
	return nil, fmt.Errorf("not implemented")
}

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

// UpdateConfig updates the engine configuration.
func (e *DefaultEngine) UpdateConfig(cfg EngineConfig) {
	e.config = cfg
	e.skipVault = cfg.SkipVault
	e.skipAws = cfg.SkipAWS
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
		memConfig := e.config.MemoryConfig
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

// createEngineFromOptions creates an engine from EngineOptions (used by api.go).
//
//nolint:gocyclo // engine configuration requires handling many optional settings
func createEngineFromOptions(opts *EngineOptions) (Engine, error) {
	// Validate options
	if opts.MaxConcurrency < 0 {
		return nil, NewConfigurationError("concurrency must be non-negative")
	}

	// Create engine config from options
	engineCfg := EngineConfig{
		VaultAddr:      opts.VaultAddress,
		VaultToken:     opts.VaultToken,
		AWSRegion:      opts.AWSRegion,
		EnableCaching:  opts.EnableCache,
		CacheSize:      opts.CacheSize,
		EnableParallel: opts.MaxConcurrency > 1,
		MaxWorkers:     opts.MaxConcurrency,
		DataflowOrder:  opts.DataflowOrder,
	}

	// Create the engine
	engine := NewDefaultEngineWithConfig(engineCfg)

	// Register custom operators if any
	if opts.CustomOperators != nil {
		for name, op := range opts.CustomOperators {
			if err := engine.RegisterOperator(name, op); err != nil {
				return nil, err
			}
		}
	}

	// Enable memory tracking if requested
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
		// Create default worker pool if parallel evaluation is enabled
		pool, err := parallel.NewPool(1, opts.MaxConcurrency)
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
