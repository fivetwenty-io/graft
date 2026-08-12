package graft

import (
	"context"
	"io"
	"time"

	"github.com/fivetwenty-io/graft/internal/cache"
	"github.com/fivetwenty-io/graft/internal/config"
	"github.com/fivetwenty-io/graft/internal/features"
	"github.com/fivetwenty-io/graft/internal/metrics"
	"github.com/fivetwenty-io/graft/internal/parallel"
	"github.com/fivetwenty-io/graft/pkg/graft/interfaces"
)

// Document represents a YAML/JSON document in a more user-friendly format
// This abstraction provides type-safe access to the internal map[string]interface{} representation.
//
// Document is not intended to be implemented outside this package. The
// two in-repo implementations (the map-backed default and the go-patch
// operation holder returned by NewGoPatchDocument) are exhaustive; new
// methods may be added to this interface in minor releases.
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

	// Prune removes one or more keys from the document, returning a new
	// document with those keys removed. A key that does not resolve is
	// silently skipped.
	Prune(keys ...string) Document

	// CherryPick creates a new document with only the specified keys
	CherryPick(keys ...string) Document

	// GetData returns the underlying data (for backward compatibility)
	GetData() interface{}

	// Additional type-safe getters
	GetInt64(path string) (int64, error)
	GetFloat64(path string) (float64, error)
	GetStringSlice(path string) ([]string, error)
	GetMapStringString(path string) (map[string]string, error)

	// Checked getters return the zero value on any failure (missing path
	// or wrong type) instead of an error. Use the Get*/GetString/GetInt/
	// etc. forms above when the distinction between "missing" and
	// "present but zero" matters.
	String(path string) string
	Int(path string) int
	Int64(path string) int64
	Float64(path string) float64
	Bool(path string) bool

	// Has reports whether path resolves to a value in the document.
	Has(path string) bool

	// Paths returns every leaf path in the document, in canonical dotted
	// form (no "$" prefix), sorted for stable output.
	Paths() []string

	// SortKeys returns a new Document with map keys sorted recursively.
	// See the *document implementation for what "sorted" guarantees.
	SortKeys() Document

	// ToJSONIndent converts the document to indented JSON bytes using
	// indent as the per-level indentation string. See also
	// Engine.ToJSONIndent, which additionally passes through evaluation.
	ToJSONIndent(indent string) ([]byte, error)
}

// Engine is the enhanced interface for using graft as a library.
//
// Engine is not intended to be implemented outside this package. NewEngine
// only ever returns *DefaultEngine; new methods may be added to this
// interface in minor releases.
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

	// Diff computes the differences between a and b using
	// DefaultDiffOptions(). See DiffDocuments (diff_changes.go) for the
	// underlying implementation.
	Diff(a, b Document) DiffResult
	// DiffWithOptions computes the differences between a and b using
	// opts (nil selects DefaultDiffOptions()).
	DiffWithOptions(a, b Document, opts *DiffOptions) DiffResult

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
//
// MergeBuilder is not intended to be implemented outside this package.
// Engine.Merge/MergeFiles/MergeReaders only ever return the package's own
// implementation; new methods may be added to this interface in minor
// releases.
type MergeBuilder interface {
	// Base sets the base document for the merge, replacing position 0 in
	// the builder's document list - including a document supplied via
	// engine.Merge(ctx, docs...)'s first argument. Calling Base more than
	// once on the same chain replaces the previous base rather than
	// accumulating; use Overlay/OverlayFile to add further documents. A
	// nil doc is not validated here: Execute() panics on it later, the
	// same pre-existing hazard as passing a nil Document to Engine.Merge
	// directly.
	Base(doc Document) MergeBuilder

	// Overlay appends one or more documents to be merged, in call order,
	// on top of the base and any earlier overlays (from Merge's own
	// arguments, prior Overlay calls, or prior OverlayFile calls). A nil
	// entry is not validated here; see Base's doc comment for the same
	// pre-existing nil hazard.
	Overlay(docs ...Document) MergeBuilder

	// OverlayFile loads each path via the engine's ParseFile (the same
	// extension-based YAML/JSON/go-patch auto-detection ParseFile
	// documents, including the "-" == STDIN convention) and appends the
	// resulting documents as overlays, in path order. A load failure does
	// not panic and is not returned directly: it is captured on the
	// returned builder and reported by Execute(), matching
	// Engine.MergeFiles/MergeReaders' error-carrying-builder convention.
	OverlayFile(paths ...string) MergeBuilder

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

	// CacheTTL sets a default time-to-live for entries in the engine's
	// operator result cache (see WithCacheTTL). Zero means no expiration.
	CacheTTL time.Duration

	// TraceOutput, when set (via WithTraceOutput), redirects graft's
	// DEBUG/TRACE output. See WithTraceOutput for the process-wide-sink
	// caveat.
	TraceOutput io.Writer

	// TraceLevel selects which of graft's DEBUG/TRACE output is produced
	// when set via WithTraceLevel. Read traceLevelSet, not the zero value
	// of TraceLevel, to tell "unset" apart from "explicitly set to
	// TraceLevelNone".
	TraceLevel TraceLevel

	// traceLevelSet is true once WithTraceLevel has been applied,
	// distinguishing "never configured" (leave process logging state
	// untouched) from "explicitly set to TraceLevelNone" (turn output
	// off). Unexported: only WithTraceLevel sets it.
	traceLevelSet bool

	// debugLoggingSet is true once WithDebugLogging has been applied,
	// distinguishing "never configured" from "explicitly set", the same
	// way traceLevelSet does for TraceLevel. Unexported: only
	// WithDebugLogging sets it.
	debugLoggingSet bool
}

// EngineOption is a functional option for configuring an engine.
type EngineOption func(*EngineOptions)

// WithLogger sets the logger the engine reports evaluation activity to.
// A nil logger disables reporting (the default: an engine constructed
// without WithLogger reports nothing).
func WithLogger(logger Logger) EngineOption {
	return func(opts *EngineOptions) {
		opts.Logger = logger
	}
}

// WithVaultClient sets the vault client for the engine.
//
// Deprecated: has no effect. Vault access is configured through
// environment variables read by internal/backends/vault today; a
// WithVault engine option carrying equivalent configuration is planned.
func WithVaultClient(client VaultClient) EngineOption {
	return func(opts *EngineOptions) {
		opts.VaultClient = client
	}
}

// WithAWSConfig sets the AWS configuration.
//
// Deprecated: has no effect. AWS access is configured through environment
// variables read by internal/backends/aws today; a WithAWS engine option
// carrying equivalent configuration is planned.
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
//
// Deprecated: has no effect. Vault access is configured through
// environment variables read by internal/backends/vault today; a
// WithVault engine option carrying equivalent configuration is planned.
func WithVaultConfig(address, token string) EngineOption {
	return func(opts *EngineOptions) {
		opts.VaultAddress = address
		opts.VaultToken = token
	}
}

// WithDebugLogging enables or disables graft's DEBUG output (log.DebugOn),
// the same knob the CLI's -d/--debug flag sets. See also WithTraceLevel,
// which offers the same control plus TRACE output; if both are applied to
// the same engine, WithTraceLevel wins. Like WithTraceOutput/
// WithTraceLevel, this reaches into process-global logging state (DEBUG is
// a package-level function, not per-engine) - see WithTraceOutput's doc
// comment for the full caveat.
func WithDebugLogging(enabled bool) EngineOption {
	return func(opts *EngineOptions) {
		opts.DebugLogging = enabled
		opts.debugLoggingSet = true
	}
}

// WithAWSRegion sets the AWS region.
//
// Deprecated: has no effect. AWS access is configured through environment
// variables read by internal/backends/aws today; a WithAWS engine option
// carrying equivalent configuration is planned.
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

// WithMemoryPools is a shorthand for enabling/disabling memory pools via
// feature flags.
//
// Deprecated: sets features.FeatureMemoryPools, but nothing in graft reads
// that flag today - there is no pooling implementation to gate. The flag
// is still set (and observable via Engine's feature-flag accessors) so
// this option keeps compiling and remains forward-compatible with a future
// pooling implementation.
func WithMemoryPools(enabled bool) EngineOption {
	return func(opts *EngineOptions) {
		if opts.FeatureFlags == nil {
			opts.FeatureFlags = features.DefaultFlags()
		}
		opts.FeatureFlags.Set(features.FeatureMemoryPools, enabled)
	}
}

// WithYAMLCompat sets YAML 1.1 backward-compatibility behavior (see
// YAMLCompat) used when parsing YAML with ParseYAML. A nil compat is
// ignored, leaving the engine's default (DefaultYAMLCompat(), which
// converts yes/no/on/off-style scalars to booleans) in effect.
func WithYAMLCompat(compat *YAMLCompat) EngineOption {
	return func(opts *EngineOptions) {
		if compat != nil {
			opts.YAMLCompat = compat
		}
	}
}

// WithVaultSkipTLS disables TLS verification for Vault connections.
//
// Deprecated: has no effect. Vault TLS verification is configured through
// environment variables read by internal/backends/vault today; a
// WithVault engine option carrying equivalent configuration is planned.
func WithVaultSkipTLS(skip bool) EngineOption {
	return func(opts *EngineOptions) {
		opts.VaultSkipTLS = skip
	}
}

// WithAWSProfile sets the AWS credentials profile.
//
// Deprecated: has no effect. The AWS credentials profile is configured
// through environment variables read by internal/backends/aws today; a
// WithAWS engine option carrying equivalent configuration is planned.
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

// WithMaxWorkers sets the maximum number of worker goroutines.
//
// Deprecated: use WithConcurrency instead; the two are functionally
// identical (both set only EngineOptions.MaxConcurrency).
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

// NewEngine creates a new engine instance with the given options, applied
// over the library's one documented default configuration
// (defaultEngineOpts: caching enabled with a 10000-entry cache, parallel
// evaluation disabled, 4 max concurrent workers, alphabetical dataflow
// order). Earlier versions of NewEngine used a second, different default
// set (1000-entry cache, 10 max workers); that drift is gone as of this
// version - NewEngine and NewDefaultEngine now start from the same
// defaults.
func NewEngine(options ...EngineOption) (Engine, error) {
	opts := defaultEngineOpts()

	for _, option := range options {
		option(&opts)
	}

	return createEngineFromOptions(&opts)
}

// CreateDefaultEngine creates an engine with the library's default
// configuration (see NewEngine). It is equivalent to NewEngine() with no
// options; it exists as a discoverable, explicitly-named entry point for
// callers who want "just give me a working engine" without needing to know
// that an empty options list already does that.
func CreateDefaultEngine() (Engine, error) {
	return NewEngine()
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
