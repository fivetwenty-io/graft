# Configuration Options

Graft uses the functional options pattern for flexible, extensible configuration. This guide covers all available options for configuring the Engine and its components.

## Option Pattern Overview

Options are functions that modify internal configuration. This pattern enables:

- Clean default values

- Extensibility without API changes

- Self-documenting configuration

- Compile-time safety

```go
// Basic usage
engine, err := graft.NewEngine(
    graft.WithCacheSize(1000),
    graft.WithHistoryTracking(true),
)

// Options can be stored and reused
opts := []graft.Option{
    graft.WithCacheSize(1000),
    graft.WithCacheTTL(5 * time.Minute),
}
engine, err := graft.NewEngine(opts...)
```

## Engine Options

### Cache Configuration

Control the internal caching system for parsed documents and external lookups.

```go
// Set maximum number of cached items
graft.WithCacheSize(size int) Option

// Set time-to-live for cached items
graft.WithCacheTTL(ttl time.Duration) Option

// Disable caching entirely
graft.WithCacheDisabled() Option
```

**Example:**

```go
engine, _ := graft.NewEngine(
    graft.WithCacheSize(500),
    graft.WithCacheTTL(1 * time.Minute),
)
```

**Defaults:**

| Option | Default Value |
|--------|---------------|
| CacheSize | 100 |
| CacheTTL | 5 minutes |

### Tracing and Debugging

Enable detailed logging for debugging merge operations.

```go
// Enable trace output to a writer
graft.WithTraceOutput(w io.Writer) Option

// Set trace level
graft.WithTraceLevel(level TraceLevel) Option

// Available trace levels
const (
    TraceLevelNone TraceLevel = iota
    TraceLevelError
    TraceLevelWarn
    TraceLevelInfo
    TraceLevelDebug
    TraceLevelTrace
)
```

**Example:**

```go
engine, _ := graft.NewEngine(
    graft.WithTraceOutput(os.Stderr),
    graft.WithTraceLevel(graft.TraceLevelDebug),
)
```

### History Tracking

Enable tracking of all changes during merge and evaluation.

```go
// Enable or disable history tracking globally
graft.WithHistoryTracking(enabled bool) Option

// Configure history storage limits
graft.WithHistoryConfig(config HistoryConfig) Option

type HistoryConfig struct {
    MaxEntriesPerPath int           // Max entries per path (0 = unlimited)
    RetentionPeriod   time.Duration // How long to keep entries (0 = forever)
    CompressValues    bool          // Compress stored values
}
```

**Example:**

```go
engine, _ := graft.NewEngine(
    graft.WithHistoryTracking(true),
    graft.WithHistoryConfig(graft.HistoryConfig{
        MaxEntriesPerPath: 100,
        RetentionPeriod:   24 * time.Hour,
        CompressValues:    true,
    }),
)
```

### Custom Operators

Register custom operators at engine creation time.

```go
// Register a single custom operator
graft.WithCustomOperator(name string, op Operator) Option

// Register multiple operators
graft.WithOperators(ops map[string]Operator) Option
```

**Example:**

```go
engine, _ := graft.NewEngine(
    graft.WithCustomOperator("env", graft.OperatorFunc(
        func(ctx graft.EvalContext, args []interface{}) (interface{}, error) {
            name := args[0].(string)
            return os.Getenv(name), nil
        },
    )),
)
```

### Post-Processors

Configure post-processing pipeline.

```go
// Add post-processors to the pipeline
graft.WithPostProcessors(processors ...PostProcessor) Option

// Enable built-in validators
graft.WithValidation(enabled bool) Option

// Enable analytics collection
graft.WithAnalytics(enabled bool) Option
```

**Example:**

```go
engine, _ := graft.NewEngine(
    graft.WithPostProcessors(
        &SchemaValidator{Schema: mySchema},
        &SecretDetector{Patterns: secretPatterns},
    ),
    graft.WithValidation(true),
)
```

## Pipeline Options

Configure parallel processing behavior.

```go
graft.WithPipeline(config PipelineConfig) Option

type PipelineConfig struct {
    // File processing
    FileParallelism     int  // Files processed in parallel (default: runtime.NumCPU())

    // Evaluation
    EvalParallelism     int  // Operators per wave (default: 16)

    // Sub-tree merging
    SubtreeParallelism  bool // Enable parallel sub-tree merge (default: true)
    SubtreeThreshold    int  // Min keys to parallelize (default: 100)

    // External calls
    ExternalParallelism int           // Max concurrent external calls (default: 32)
    BatchSize           int           // Requests per batch (default: 20)
    BatchTimeout        time.Duration // Max wait for batch (default: 100ms)
}
```

**Example:**

```go
engine, _ := graft.NewEngine(
    graft.WithPipeline(graft.PipelineConfig{
        FileParallelism:     8,
        EvalParallelism:     16,
        ExternalParallelism: 32,
        SubtreeParallelism:  true,
        SubtreeThreshold:    50,
        BatchSize:           25,
        BatchTimeout:        150 * time.Millisecond,
    }),
)
```

### Pipeline Presets

Pre-configured pipeline settings for common scenarios.

```go
// No parallelism - useful for debugging
graft.WithPipeline(graft.PipelineSequential)

// Low parallelism - resource constrained environments
graft.WithPipeline(graft.PipelineConservative)

// Balanced - default settings
graft.WithPipeline(graft.PipelineBalanced)

// Maximum parallelism - many external calls
graft.WithPipeline(graft.PipelineHighThroughput)

// Optimized for small, fast merges
graft.WithPipeline(graft.PipelineLowLatency)
```

**Preset Comparison:**

| Preset | FileParallelism | EvalParallelism | ExternalParallelism | SubtreeParallelism |
|--------|-----------------|-----------------|---------------------|-------------------|
| Sequential | 1 | 1 | 1 | false |
| Conservative | 2 | 4 | 8 | false |
| Balanced | NumCPU | 16 | 32 | true |
| HighThroughput | NumCPU | 32 | 64 | true |
| LowLatency | NumCPU | 8 | 16 | false |

## Backend Options

### Vault / OpenBao

Configure HashiCorp Vault or OpenBao connections.

```go
// Configure default Vault connection
graft.WithVault(config VaultConfig) Option

// Configure named Vault target
graft.WithVaultTarget(name string, config VaultConfig) Option

type VaultConfig struct {
    Address    string        // Vault server address
    Token      string        // Authentication token
    Namespace  string        // Vault namespace (enterprise)
    SkipVerify bool          // Skip TLS verification
    Timeout    time.Duration // Request timeout
    PoolSize   int           // Connection pool size
}
```

**Example:**

```go
engine, _ := graft.NewEngine(
    // Default Vault
    graft.WithVault(graft.VaultConfig{
        Address:   "https://vault.example.com",
        Token:     os.Getenv("VAULT_TOKEN"),
        Namespace: "prod",
        Timeout:   30 * time.Second,
        PoolSize:  10,
    }),
    // Named target for staging
    graft.WithVaultTarget("staging", graft.VaultConfig{
        Address: "https://vault-staging.example.com",
        Token:   os.Getenv("VAULT_STAGING_TOKEN"),
    }),
)
```

**Usage in YAML:**

```yaml
prod_secret: (( vault "secret/db:password" ))
staging_secret: (( vault staging@"secret/db:password" ))
```

### AWS

Configure AWS services (Parameter Store, Secrets Manager).

```go
// Configure default AWS connection
graft.WithAWS(config AWSConfig) Option

// Configure named AWS target
graft.WithAWSTarget(name string, config AWSConfig) Option

type AWSConfig struct {
    Region    string // AWS region
    Profile   string // AWS profile name
    Endpoint  string // Custom endpoint (for testing)
    AccessKey string // Static access key (optional)
    SecretKey string // Static secret key (optional)
    PoolSize  int    // Connection pool size
}
```

**Example:**

```go
engine, _ := graft.NewEngine(
    graft.WithAWS(graft.AWSConfig{
        Region:  "us-west-2",
        Profile: "production",
    }),
    graft.WithAWSTarget("staging", graft.AWSConfig{
        Region:  "us-east-1",
        Profile: "staging",
    }),
)
```

**Usage in YAML:**

```yaml
db_host: (( awsparam "/app/prod/db_host" ))
api_key: (( awssecret "prod/api-credentials" ))
staging_host: (( awsparam staging@"/app/db_host" ))
```

### NATS

Configure NATS JetStream connections.

```go
// Configure default NATS connection
graft.WithNATS(config NATSConfig) Option

// Configure named NATS target
graft.WithNATSTarget(name string, config NATSConfig) Option

type NATSConfig struct {
    URL      string        // NATS server URL
    Token    string        // Authentication token
    TLSCert  string        // TLS certificate path
    TLSKey   string        // TLS key path
    Timeout  time.Duration // Request timeout
    PoolSize int           // Connection pool size
}
```

**Example:**

```go
engine, _ := graft.NewEngine(
    graft.WithNATS(graft.NATSConfig{
        URL:     "nats://nats.example.com:4222",
        Token:   os.Getenv("NATS_TOKEN"),
        Timeout: 10 * time.Second,
    }),
)
```

**Usage in YAML:**

```yaml
config: (( nats "kv:config/settings" ))
template: (( nats "obj:assets/template.yml" ))
```

## Applying Options at Runtime

Options can be applied after engine creation using `Configure()`.

```go
engine, _ := graft.NewEngine()

// Reconfigure at runtime
err := engine.Configure(
    graft.WithCacheSize(2000),
    graft.WithHistoryTracking(true),
)
```

**Note:** Not all options can be changed at runtime. Backend configurations and pipeline settings typically require a new engine instance.

## Complete Example

```go
package main

import (
    "os"
    "time"

    "github.com/fivetwenty-io/graft/pkg/graft"
)

func main() {
    engine, err := graft.NewEngine(
        // Caching
        graft.WithCacheSize(1000),
        graft.WithCacheTTL(5 * time.Minute),

        // Tracing
        graft.WithTraceOutput(os.Stderr),
        graft.WithTraceLevel(graft.TraceLevelInfo),

        // History
        graft.WithHistoryTracking(true),

        // Pipeline
        graft.WithPipeline(graft.PipelineBalanced),

        // Vault
        graft.WithVault(graft.VaultConfig{
            Address: os.Getenv("VAULT_ADDR"),
            Token:   os.Getenv("VAULT_TOKEN"),
        }),

        // AWS
        graft.WithAWS(graft.AWSConfig{
            Region:  "us-west-2",
            Profile: "default",
        }),

        // Custom operator
        graft.WithCustomOperator("timestamp", graft.OperatorFunc(
            func(ctx graft.EvalContext, args []interface{}) (interface{}, error) {
                return time.Now().Unix(), nil
            },
        )),
    )
    if err != nil {
        panic(err)
    }

    // Use engine...
}
```

## Related Documentation

- [Engine Interface](engine.md) - Core engine operations

- [Document Interface](document.md) - Document handling

- [MergeBuilder API](merge-builder.md) - Merge configuration

- [Custom Operators](../custom-operators.md) - Creating operators

- [Custom Backends](../custom-backends.md) - Creating backends
