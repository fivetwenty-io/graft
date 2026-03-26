package graft

import (
	"context"
	"io"

	"github.com/fivetwenty-io/graft/internal/cache"
	"github.com/fivetwenty-io/graft/internal/config"
	"github.com/fivetwenty-io/graft/internal/features"
	"github.com/fivetwenty-io/graft/internal/metrics"
	"github.com/fivetwenty-io/graft/internal/parallel"
	"github.com/fivetwenty-io/graft/pkg/graft/interfaces"
)

// Document represents a YAML/JSON document in a more user-friendly format
// This abstraction provides type-safe access to the internal map[string]interface{} representation.
type Document interface {
	// Get retrieves a value at the given path (e.g., "meta.instance_groups.0.name")
	Get(path string) (interface{}, error)

	// GetString retrieves a string value at the given path
	GetString(path string) (string, error)

	// GetInt retrieves an integer value at the given path
	GetInt(path string) (int, error)

	// GetBool retrieves a boolean value at the given path
	GetBool(path string) (bool, error)

	// GetSlice retrieves a slice value at the given path
	GetSlice(path string) ([]interface{}, error)

	// GetMap retrieves a map value at the given path
	GetMap(path string) (map[string]interface{}, error)

	// Set sets a value at the given path
	Set(path string, value interface{}) error

	// Delete removes a value at the given path
	Delete(path string) error

	// Keys returns all top-level keys
	Keys() []string

	// ToYAML converts the document to YAML bytes
	ToYAML() ([]byte, error)

	// ToJSON converts the document to JSON bytes
	ToJSON() ([]byte, error)

	// RawData returns the underlying data structure
	RawData() interface{}

	// Clone creates a deep copy of the document
	Clone() Document

	// Prune removes a key from the document
	Prune(key string) Document

	// CherryPick creates a new document with only the specified keys
	CherryPick(keys ...string) Document

	// GetData returns the underlying data (for backward compatibility)
	GetData() interface{}

	// Additional type-safe getters
	GetInt64(path string) (int64, error)
	GetFloat64(path string) (float64, error)
	GetStringSlice(path string) ([]string, error)
	GetMapStringString(path string) (map[string]string, error)
}

// Engine is the enhanced interface for using graft as a library.
type Engine interface {
	// Document operations
	ParseYAML(data []byte) (Document, error)
	ParseJSON(data []byte) (Document, error)
	ParseFile(path string) (Document, error)
	ParseReader(reader io.Reader) (Document, error)

	// Merge operations with builder pattern options
	Merge(ctx context.Context, docs ...Document) MergeBuilder
	MergeFiles(ctx context.Context, paths ...string) MergeBuilder
	MergeReaders(ctx context.Context, readers ...io.Reader) MergeBuilder

	// Evaluate processes operators in a document
	Evaluate(ctx context.Context, doc Document) (Document, error)

	// Output operations
	ToYAML(doc Document) ([]byte, error)
	ToJSON(doc Document) ([]byte, error)
	ToJSONIndent(doc Document, indent string) ([]byte, error)

	// Operator management
	RegisterOperator(name string, op Operator) error
	UnregisterOperator(name string) error
	ListOperators() []string
	GetOperator(name string) (Operator, bool)

	// Configuration
	WithLogger(logger Logger) Engine
	WithVaultClient(client VaultClient) Engine
	WithAWSConfig(config AWSConfig) Engine

	// State access for operators
	GetOperatorState() OperatorState

	// Memory tracking
	GetMemoryTracker() interfaces.MemoryTracker
}

// OperatorState provides state access for operators during evaluation.
// Backend-specific clients and caches (Vault KV, AWS session, Secrets Manager,
// Parameter Store) are accessed through their respective backend packages
// (internal/backends/vault and internal/backends/aws) rather than through
// this interface.
type OperatorState interface {
	// Vault refs
	AddVaultRef(path string, keys []string)
	IsVaultSkipped() bool

	// Vault refs (for vaultinfo command)
	GetVaultRefs() map[string][]string
	ResetVaultRefs()

	// Skip setters (for REDACT mode)
	SetSkipVault(v bool)
	SetSkipAws(v bool)
	SetSkipNats(v bool)

	// AWS skip
	IsAWSSkipped() bool

	// NATS operations
	IsNATSSkipped() bool

	// Static IPs
	GetUsedIPs() map[string]string
	SetUsedIP(key, ip string)

	// Prune operations
	AddKeyToPrune(key string)
	GetKeysToPrune() []string
	ResetKeysToPrune()

	// Sort operations
	AddPathToSort(path, order string)
	GetPathsToSort() map[string]string
	ResetPathsToSort()

	// Reset operations (for cleanup between evaluations)
	ResetUsedIPs()

	// Warning suppression
	SuppressWarnings() bool
	SetSuppressWarnings(v bool)
}

// ArrayMergeStrategy defines how arrays are merged.
type ArrayMergeStrategy int

const (
	// InlineArrays is the default - arrays are merged inline by index.
	InlineArrays ArrayMergeStrategy = iota
	// AppendArrays appends arrays instead of merging inline.
	AppendArrays
	// ReplaceArrays replaces the entire array.
	ReplaceArrays
	// PrependArrays prepends new array elements.
	PrependArrays
)

// MergeBuilder provides a fluent interface for merge operations.
type MergeBuilder interface {
	// WithPrune specifies keys to remove from the final output
	WithPrune(keys ...string) MergeBuilder

	// WithCherryPick specifies keys to keep in the final output (all others removed)
	WithCherryPick(keys ...string) MergeBuilder

	// WithArrayMergeStrategy sets how arrays are merged
	WithArrayMergeStrategy(strategy ArrayMergeStrategy) MergeBuilder

	// SkipEvaluation skips operator evaluation after merging
	SkipEvaluation() MergeBuilder

	// EnableGoPatch enables go-patch format parsing
	EnableGoPatch() MergeBuilder

	// FallbackAppend uses append instead of inline for arrays by default
	FallbackAppend() MergeBuilder

	// Execute performs the merge operation
	Execute() (Document, error)
}

// EngineOptions configures a new engine instance using functional options.
type EngineOptions struct {
	Logger               Logger
	VaultClient          VaultClient
	AWSConfig            *AWSConfig
	EnableCache          bool
	CacheSize            int
	MaxConcurrency       int
	EnableMetrics        bool
	CustomOperators      map[string]Operator
	VaultAddress         string
	VaultToken           string
	DebugLogging         bool
	AWSRegion            string
	DataflowOrder        string // "alphabetical" (default) or "insertion"
	EnableMemoryTracking bool   // Enable document memory tracking

	// New infrastructure system options

	// FeatureFlags provides feature flag configuration (nil uses defaults)
	FeatureFlags *features.FeatureFlags

	// CacheInstance provides a custom cache implementation (nil uses default if caching enabled)
	CacheInstance cache.Cache

	// MetricsRegistry provides a custom metrics registry (nil uses default if metrics enabled)
	MetricsRegistry *metrics.Registry

	// ConfigInstance provides unified configuration (nil uses defaults)
	ConfigInstance *config.Config

	// WorkerPool provides a custom worker pool for parallel evaluation (nil uses default if parallel enabled)
	WorkerPool *parallel.WorkerPool

	// EnableParallel enables parallel evaluation (shorthand for feature flag)
	EnableParallel bool

	// YAMLCompat controls YAML 1.1 backward compatibility behavior (nil uses defaults)
	YAMLCompat *YAMLCompat

	// VaultSkipTLS disables TLS verification for Vault connections.
	VaultSkipTLS bool

	// AWSProfile sets the AWS credentials profile to use.
	AWSProfile string

	// MemoryConfig configures document memory tracking behavior.
	MemoryConfig *MemoryConfig

	// Skip flags for external service operators
	SkipVault bool
	SkipAws   bool
	SkipNats  bool
}

// EngineOption is a functional option for configuring an engine.
type EngineOption func(*EngineOptions)

// WithLogger sets the logger for the engine.
func WithLogger(logger Logger) EngineOption {
	return func(opts *EngineOptions) {
		opts.Logger = logger
	}
}

// WithVaultClient sets the vault client for the engine.
func WithVaultClient(client VaultClient) EngineOption {
	return func(opts *EngineOptions) {
		opts.VaultClient = client
	}
}

// WithAWSConfig sets the AWS configuration.
func WithAWSConfig(cfg *AWSConfig) EngineOption {
	return func(opts *EngineOptions) {
		opts.AWSConfig = cfg
	}
}

// WithCache enables caching with the specified size.
func WithCache(enabled bool, size int) EngineOption {
	return func(opts *EngineOptions) {
		opts.EnableCache = enabled
		opts.CacheSize = size
	}
}

// WithConcurrency sets the maximum number of concurrent operations.
func WithConcurrency(maxConcur int) EngineOption {
	return func(opts *EngineOptions) {
		opts.MaxConcurrency = maxConcur
	}
}

// WithMetrics enables metrics collection.
func WithMetrics(enabled bool) EngineOption {
	return func(opts *EngineOptions) {
		opts.EnableMetrics = enabled
	}
}

// WithCustomOperator registers a custom operator.
func WithCustomOperator(name string, op Operator) EngineOption {
	return func(opts *EngineOptions) {
		if opts.CustomOperators == nil {
			opts.CustomOperators = make(map[string]Operator)
		}
		opts.CustomOperators[name] = op
	}
}

// WithVaultConfig configures vault settings.
func WithVaultConfig(address, token string) EngineOption {
	return func(opts *EngineOptions) {
		opts.VaultAddress = address
		opts.VaultToken = token
	}
}

// WithDebugLogging enables debug logging.
func WithDebugLogging(enabled bool) EngineOption {
	return func(opts *EngineOptions) {
		opts.DebugLogging = enabled
	}
}

// WithAWSRegion sets the AWS region.
func WithAWSRegion(region string) EngineOption {
	return func(opts *EngineOptions) {
		opts.AWSRegion = region
	}
}

// WithDataflowOrder sets the order of operations in dataflow output
// Valid values are "alphabetical" (default) or "insertion".
func WithDataflowOrder(order string) EngineOption {
	return func(opts *EngineOptions) {
		opts.DataflowOrder = order
	}
}

// New infrastructure system options

// WithFeatureFlags sets the feature flags for the engine.
func WithFeatureFlags(ff *features.FeatureFlags) EngineOption {
	return func(opts *EngineOptions) {
		opts.FeatureFlags = ff
	}
}

// WithCacheInstance sets a custom cache implementation for the engine.
func WithCacheInstance(c cache.Cache) EngineOption {
	return func(opts *EngineOptions) {
		opts.CacheInstance = c
		opts.EnableCache = c != nil
	}
}

// WithMetricsRegistry sets a custom metrics registry for the engine.
func WithMetricsRegistry(r *metrics.Registry) EngineOption {
	return func(opts *EngineOptions) {
		opts.MetricsRegistry = r
		opts.EnableMetrics = r != nil
	}
}

// WithConfigInstance sets the unified configuration for the engine.
func WithConfigInstance(cfg *config.Config) EngineOption {
	return func(opts *EngineOptions) {
		opts.ConfigInstance = cfg
	}
}

// WithWorkerPool sets a custom worker pool for parallel evaluation.
func WithWorkerPool(pool *parallel.WorkerPool) EngineOption {
	return func(opts *EngineOptions) {
		opts.WorkerPool = pool
		opts.EnableParallel = pool != nil
	}
}

// WithCaching is a shorthand for enabling/disabling caching via feature flags.
func WithCaching(enabled bool) EngineOption {
	return func(opts *EngineOptions) {
		opts.EnableCache = enabled
		if opts.FeatureFlags == nil {
			opts.FeatureFlags = features.DefaultFlags()
		}
		opts.FeatureFlags.Set(features.FeatureCaching, enabled)
	}
}

// WithParallel is a shorthand for enabling/disabling parallel evaluation via feature flags.
func WithParallel(enabled bool) EngineOption {
	return func(opts *EngineOptions) {
		opts.EnableParallel = enabled
		if opts.FeatureFlags == nil {
			opts.FeatureFlags = features.DefaultFlags()
		}
		opts.FeatureFlags.Set(features.FeatureParallelEvaluation, enabled)
	}
}

// WithMemoryPools is a shorthand for enabling/disabling memory pools via feature flags.
func WithMemoryPools(enabled bool) EngineOption {
	return func(opts *EngineOptions) {
		if opts.FeatureFlags == nil {
			opts.FeatureFlags = features.DefaultFlags()
		}
		opts.FeatureFlags.Set(features.FeatureMemoryPools, enabled)
	}
}

// WithYAMLCompat sets YAML compatibility options.
func WithYAMLCompat(compat *YAMLCompat) EngineOption {
	return func(opts *EngineOptions) {
		opts.YAMLCompat = compat
	}
}

// WithVaultSkipTLS disables TLS verification for Vault connections.
func WithVaultSkipTLS(skip bool) EngineOption {
	return func(opts *EngineOptions) {
		opts.VaultSkipTLS = skip
	}
}

// WithAWSProfile sets the AWS credentials profile.
func WithAWSProfile(profile string) EngineOption {
	return func(opts *EngineOptions) {
		opts.AWSProfile = profile
	}
}

// WithMemoryConfig sets the document memory tracking configuration.
func WithMemoryConfig(cfg MemoryConfig) EngineOption {
	return func(opts *EngineOptions) {
		opts.MemoryConfig = &cfg
	}
}

// WithMaxWorkers sets the maximum number of worker goroutines (alias for WithConcurrency).
func WithMaxWorkers(n int) EngineOption {
	return func(opts *EngineOptions) {
		opts.MaxConcurrency = n
	}
}

// WithSkipVault configures the engine to skip vault operations.
func WithSkipVault(skip bool) EngineOption {
	return func(opts *EngineOptions) {
		opts.SkipVault = skip
	}
}

// WithSkipAws configures the engine to skip AWS operations.
func WithSkipAws(skip bool) EngineOption {
	return func(opts *EngineOptions) {
		opts.SkipAws = skip
	}
}

// WithSkipNats configures the engine to skip NATS operations.
func WithSkipNats(skip bool) EngineOption {
	return func(opts *EngineOptions) {
		opts.SkipNats = skip
	}
}

// Logger interface for structured logging.
type Logger interface {
	Debug(msg string, fields ...interface{})
	Info(msg string, fields ...interface{})
	Warn(msg string, fields ...interface{})
	Error(msg string, fields ...interface{})
}

// VaultClient interface for vault operations.
type VaultClient interface {
	Get(path string) (map[string]interface{}, error)
	List(path string) ([]string, error)
	Put(path string, data map[string]interface{}) error
}

// AWSConfig holds AWS-specific configuration.
type AWSConfig struct {
	Region   string
	Profile  string
	Role     string
	SkipAuth bool
	Endpoint string // For testing with localstack
}

// NewEngine creates a new engine instance with the given options.
func NewEngine(options ...EngineOption) (Engine, error) {
	opts := &EngineOptions{
		EnableCache:    true,
		CacheSize:      1000,
		MaxConcurrency: 10,
		EnableMetrics:  false,
	}

	for _, option := range options {
		option(opts)
	}

	return createEngineFromOptions(opts)
}

// CreateDefaultEngine creates an engine with sensible defaults.
func CreateDefaultEngine() (Engine, error) {
	return NewEngine(
		WithCache(true, 1000),
		WithConcurrency(10),
	)
}

// TODO: Implement convenience functions after Engine implementation is complete

// QuickMerge is a convenience function for simple merge operations
// func QuickMerge(yamlSources ...string) ([]byte, error) {
// 	engine, err := DefaultEngine()
// 	if err != nil {
// 		return nil, err
// 	}
//
// 	var docs []Document
// 	for _, source := range yamlSources {
// 		doc, err := engine.ParseYAML([]byte(source))
// 		if err != nil {
// 			return nil, NewParseError("failed to parse YAML", err)
// 		}
// 		docs = append(docs, doc)
// 	}
//
// 	result, err := engine.Merge(context.Background(), docs...).Execute()
// 	if err != nil {
// 		return nil, err
// 	}
//
// 	return engine.ToYAML(result)
// }

// QuickMergeFiles is a convenience function for merging files
//
//	func QuickMergeFiles(paths ...string) ([]byte, error) {
//		engine, err := DefaultEngine()
//		if err != nil {
//			return nil, err
//		}
//
//		result, err := engine.MergeFiles(context.Background(), paths...).Execute()
//		if err != nil {
//			return nil, err
//		}
//
//		return engine.ToYAML(result)
//	}
//
