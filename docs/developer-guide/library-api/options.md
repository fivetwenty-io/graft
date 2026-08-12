# Configuration Options

Graft uses the functional options pattern to configure an `Engine`. This page covers every option `pkg/graft` actually exports, plus `Configure`, the runtime-reconfiguration entry point.

## Option Pattern Overview

```go
// Applied at construction
engine, err := graft.NewEngine(
    graft.WithCacheSize(1000),
    graft.WithCacheTTL(5 * time.Minute),
)

// Options can be stored and reused
opts := []graft.Option{
    graft.WithCacheSize(1000),
    graft.WithCacheTTL(5 * time.Minute),
}
engine, err := graft.NewEngine(opts...)
```

`graft.Option` is an alias for `graft.EngineOption` (`type Option = EngineOption`), not a distinct type — a `[]graft.Option` and a `[]graft.EngineOption` are interchangeable, and every function below that returns `EngineOption` also satisfies `Option`.

`NewEngine(opts...)` applies `opts` over the library's one documented default configuration: caching enabled with a 10000-entry cache, parallel evaluation disabled, 4 max concurrent workers, alphabetical dataflow order. `CreateDefaultEngine()` is `NewEngine()` with no options — a discoverable, explicitly-named "just give me a working engine" entry point.

## Cache Options

```go
func WithCache(enabled bool, size int) Option
func WithCacheSize(size int) Option
func WithCacheTTL(ttl time.Duration) Option
func WithCacheDisabled() Option
func WithCacheInstance(c cache.Cache) Option
```

- `WithCache(enabled, size)` sets both `EnableCache` and `CacheSize` together.
- `WithCacheSize(size)` sets only the cache's max entry count; it does not itself enable caching (see the engine's default, which is caching enabled). A non-positive `size` is ignored, leaving the current size unchanged.
- `WithCacheTTL(ttl)` sets a default time-to-live: an entry set after this option takes effect expires and is evicted `ttl` after it was written. A zero or negative `ttl` disables expiration (entries live until evicted for capacity reasons). Has no effect if caching ends up disabled.
- `WithCacheDisabled()` disables the engine's operator result cache; equivalent to `WithCache(false, 0)`.
- `WithCacheInstance(c)` supplies a caller-built `cache.Cache` implementation directly, bypassing the size/TTL knobs above for that cache. Supplying a non-nil instance also enables caching.

**Example:**

```go
engine, _ := graft.NewEngine(
    graft.WithCacheSize(500),
    graft.WithCacheTTL(1 * time.Minute),
)
```

**Note on feature flags:** `EnableCache` alone can still no-op if a `FeatureFlags` value passed via `WithFeatureFlags` (or `GRAFT_FEATURE_CACHE=false` in the environment) has caching disabled — supplying a pre-built `CacheInstance` bypasses that flag entirely, while the `EnableCache` boolean does not.

## Tracing and Debugging

```go
type TraceLevel int

const (
    TraceLevelNone TraceLevel = iota
    TraceLevelError // reserved; currently behaves like TraceLevelNone
    TraceLevelWarn  // reserved; currently behaves like TraceLevelNone
    TraceLevelInfo  // reserved; currently behaves like TraceLevelNone
    TraceLevelDebug
    TraceLevelTrace
)

func WithTraceOutput(w io.Writer) Option
func WithTraceLevel(level TraceLevel) Option
func WithDebugLogging(enabled bool) Option
```

The underlying `github.com/fivetwenty-io/graft/log` package only distinguishes two real output levels — debug and trace — so `TraceLevelError`/`TraceLevelWarn`/`TraceLevelInfo` are accepted for forward compatibility but currently behave identically to `TraceLevelNone` (both DEBUG and TRACE output disabled). `TraceLevelDebug` enables DEBUG output (matching the CLI's `-d`/`--debug` flag); `TraceLevelTrace` enables both DEBUG and TRACE output (matching `-t`/`--trace`).

`WithDebugLogging(enabled)` is the same DEBUG on/off knob as `WithTraceLevel`, without the trace half; if both are applied to the same engine, `WithTraceLevel` wins.

**Process-wide sink, not per-engine:** `DEBUG`/`TRACE` are package-level functions (`pkg/graft` and `pkg/graft/operators` both funnel into the same `log` package sink), not routed per `Engine` instance. `WithTraceOutput`/`WithTraceLevel`/`WithDebugLogging` therefore affect every `DEBUG`/`TRACE` call in the process, not only calls made through the engine that configured them. If a process constructs more than one engine with these options, the last one applied (at construction, or via `Configure`) wins process-wide. A `nil` writer passed to `WithTraceOutput` is a no-op, leaving any previously configured destination (or the `os.Stderr` default) unchanged.

**Example:**

```go
engine, _ := graft.NewEngine(
    graft.WithTraceOutput(os.Stderr),
    graft.WithTraceLevel(graft.TraceLevelDebug),
)
```

## Custom Operators

```go
func WithCustomOperator(name string, op Operator) Option
func WithOperators(ops map[string]Operator) Option
```

`WithCustomOperator` registers a single operator. `WithOperators` registers a set at once, merging into any operators already configured (via an earlier `WithCustomOperator`/`WithOperators` call) rather than replacing the set — each entry behaves exactly like a `WithCustomOperator(name, op)` call. A custom operator is visible under `name` during evaluation on this engine, shadowing a built-in operator of the same name if one exists.

**Example:**

```go
engine, _ := graft.NewEngine(
    graft.WithOperators(map[string]graft.Operator{
        "env":       envOperator{},
        "timestamp": timestampOperator{},
    }),
)
```

## Post-Processors

```go
func WithPostProcessors(procs ...PostProcessor) Option
```

Registers `procs` to run, in phase-then-priority order, after evaluation, pruning, and cherry-picking finish on every merge the engine executes. `MergeBuilder.WithPostProcessors` adds further processors for one merge chain only; both sets combine rather than one replacing the other. Calling `WithPostProcessors` more than once, at either level, appends rather than replaces. See [Custom Post-Processors](../custom-post-processors.md) for the `PostProcessor` interface, the built-in constructors (`NewPruner`, `NewCherryPicker`, `NewKeySorter`, `NewSecurityRedactor`), and ordering rules.

**Example:**

```go
engine, _ := graft.NewEngine(
    graft.WithPostProcessors(
        graft.NewSecurityRedactor([]string{"password", "secret"}, ""),
    ),
)
```

## History Tracking

```go
func WithHistoryTracking(enabled bool) Option
func WithHistoryConfig(config HistoryConfig) Option
```

`WithHistoryTracking(true)` enables document-memory tracking at
construction, using any `MemoryConfig` separately supplied via
`WithMemoryConfig`/`WithHistoryConfig`, or a zero-value one otherwise.
`WithHistoryTracking(false)` is a genuine no-op, not a way to turn
tracking back off - it never calls `DisableMemoryTracking`, so it cannot
undo a `WithMemoryConfig`/`WithHistoryConfig` call elsewhere in the same
`NewEngine(...)` call. `WithHistoryConfig` is a smaller, documented-field
view onto `MemoryConfig` (`MaxEntriesPerPath`, `RetentionPeriod`,
`CompressValues`) that also enables tracking; `WithMemoryConfig` remains
available directly for anything `HistoryConfig` does not expose.

Tracking is off by default and costs nothing when off. Enabling it lets
a merge's resulting `Document.History()` report the changes
`DocumentMemory` recorded during merge and evaluation - see
[History Interface](history-api.md) for the full `History`/
`HistoryEntry`/`HistoryConfig` surface, what is and is not recorded, and
`MergeBuilder.TrackHistory()`, the per-merge-chain alternative to
enabling tracking at the engine level.

**Example:**

```go
engine, _ := graft.NewEngine(graft.WithHistoryTracking(true))

// Or with per-path/compression limits:
engine, _ := graft.NewEngine(graft.WithHistoryConfig(graft.HistoryConfig{
    MaxEntriesPerPath: 20,
    CompressValues:    true,
}))
```

## Other Engine Options

| Option | Effect |
|--------|--------|
| `WithConcurrency(n int)` | Sets `MaxConcurrency` for parallel evaluation |
| `WithMetrics(enabled bool)` | Enables metrics collection |
| `WithMetricsRegistry(r *metrics.Registry)` | Supplies a custom metrics registry; enables metrics |
| `WithFeatureFlags(ff *features.FeatureFlags)` | Sets the engine's feature flag set |
| `WithConfigInstance(cfg *config.Config)` | Sets the unified configuration instance |
| `WithWorkerPool(pool *parallel.WorkerPool)` | Supplies a custom worker pool; enables parallel evaluation |
| `WithCaching(enabled bool)` | Shorthand: sets `EnableCache` and the `FeatureCaching` flag together |
| `WithParallel(enabled bool)` | Shorthand: sets `EnableParallel` and the `FeatureParallelEvaluation` flag together |
| `WithMemoryConfig(cfg MemoryConfig)` | Configures document memory tracking behavior directly (the full 11-field `MemoryConfig`, vs. `WithHistoryConfig`'s smaller surface above) |
| `WithDataflowOrder(order string)` | `"alphabetical"` (default) or `"insertion"` for dataflow output ordering |
| `WithSkipVault(skip bool)` | Skips Vault-backed operator lookups |
| `WithSkipAws(skip bool)` | Skips AWS-backed operator lookups |
| `WithSkipNats(skip bool)` | Skips NATS-backed operator lookups |
| `WithLogger(logger Logger)` | Sets the logger the engine reports evaluation activity to via `Debug()` calls; a `nil` logger (the default) reports nothing |
| `WithYAMLCompat(compat *YAMLCompat)` | Sets YAML 1.1 backward-compatibility behavior used by `ParseYAML`; a `nil` compat is ignored, leaving the default (`yes`/`no`/`on`/`off`-style scalars convert to booleans) in effect |

## Backend Configuration Options

```go
type VaultConfig struct {
    Address    string
    Token      string
    Namespace  string
    SkipVerify bool
    Timeout    time.Duration
    PoolSize   int
}
func WithVault(config VaultConfig) Option
func WithVaultTarget(name string, config VaultConfig) Option

func WithAWS(config AWSConfig) Option
func WithAWSTarget(name string, config AWSConfig) Option
```

`WithVault`/`WithVaultTarget` and `WithAWS`/`WithAWSTarget` register real, working `Backend` implementations — a Vault KV reader built directly on `github.com/hashicorp/vault/api`, and an SSM/Secrets Manager reader built directly on `github.com/aws/aws-sdk-go` — under the names the `vault`, `awsparam`, and `awssecret` operators look up (`WithAWS` registers both `"awsparam"` and `"awssecret"` from one call, since one AWS session configuration serves both AWS operators). They are **not** adapters over `internal/backends/vault`/`internal/backends/aws`: that package imports `pkg/graft`, so `pkg/graft` cannot import it back without a cycle, so these are separate, from-scratch implementations. Concretely this means they do not share the built-in path's process-global client pool, its response cache, or (for Vault) its `.vault-token`/`.svtoken`-file fallback; each builds one client per configured target, lazily, on first use.

Registering a backend this way has no effect on evaluation until `WithBackendRegistry(true)` is also set (or a supplied `*features.FeatureFlags` already enables `FeatureBackendRegistry`) — see [Custom Backends](../custom-backends.md). Without it, the `vault`/`awsparam`/`awssecret` operators keep using the environment-configured `internal/backends` path exactly as before, byte-identical to an engine that never called `WithVault`/`WithAWS` at all.

```go
srv := httptest.NewServer(vaultKVHandler) // stand-in for a real Vault server

engine, err := graft.NewEngine(
    graft.WithBackendRegistry(true),
    graft.WithVault(graft.VaultConfig{Address: srv.URL, Token: "s.xxxx"}),
    graft.WithVaultTarget("staging", graft.VaultConfig{Address: "https://vault-staging.example.com", Token: "s.yyyy"}),
)
```

`(( vault "secret/db:password" ))` then reads through the `WithVault` configuration; `(( vault@staging "secret/db:password" ))` reads through the matching `WithVaultTarget("staging", ...)` configuration. A `@target` with no matching `WithVaultTarget`/`WithAWSTarget` call is a configuration error (distinct from a missing secret), not a silent fallback to the default target. Calling `WithVault`/`WithAWS` more than once, or combining either with `WithVaultTarget`/`WithAWSTarget` for the same target, applies to the same underlying backend: the last call for a given target wins, matching `WithBackend`'s own "last registration for a name wins" rule.

`VaultConfig.PoolSize` and `AWSConfig.PoolSize` (an addition to the existing `AWSConfig` struct — `Region`, `Profile`, `Role`, `SkipAuth`, `Endpoint`, plus `AccessKeyID`/`SecretAccessKey`/`SessionToken`/`PoolSize`) set the underlying HTTP transport's `MaxIdleConnsPerHost`/`MaxIdleConns`; non-positive leaves Go's `http.Transport` zero-value default (2 per host) in effect. Retry and caching beyond what the underlying SDKs already do on their own are available by layering `WithBackendRetry("vault", ...)`/`WithBackendCache("vault", ...)` (and the `"awsparam"`/`"awssecret"` equivalents) on top, described in [Custom Backends](../custom-backends.md) — `WithVault`/`WithAWS` deliberately do not duplicate that.

Not carried over from `internal/backends/aws.Target`: `S3ForcePathStyle`, `MaxRetries`, `HTTPTimeout`, MFA'd role assumption (`AssumeRoleDuration`/`ExternalID`/`SessionName`/`MfaSerial`), `CacheTTL`, `AuditLogging` (the last is available generically via `WithAuditLogger`, described in [Custom Backends](../custom-backends.md), instead). A bare `AWSConfig.Role` (no MFA) does work, via `sts:AssumeRole`. A key/secret ID reaches `Get`/`GetWithTarget` already stripped of its `?stage=...&key=...` query suffix by the operator, so stage/version secret selection is unavailable to a `WithAWS`-registered backend; it always fetches the latest/default version.

`WithNATS`/`WithNATSTarget` are not implemented in this release (see [Cut from this page](#cut-from-this-page)).

## Deprecated Options

These options compile and construct an engine without error, but have no effect. Each is superseded by real configuration described in its doc comment.

| Option | Deprecated because | Configure instead via |
|--------|---------------------|------------------------|
| `WithMaxWorkers(n int)` | Functionally identical to `WithConcurrency` (both set only `MaxConcurrency`) | `WithConcurrency(n)` |
| `WithVaultClient(client VaultClient)` | The `VaultClient` interface has no implementation anywhere in this module, and nothing would call it if one existed | `WithVault(VaultConfig{...})` |
| `WithVaultConfig(address, token string)` | `EngineOptions.VaultAddress`/`VaultToken` are never read | `WithVault(VaultConfig{Address: address, Token: token})` |
| `WithVaultSkipTLS(skip bool)` | `EngineOptions.VaultSkipTLS` is never read | `WithVault(VaultConfig{SkipVerify: skip, ...})` |
| `WithAWSConfig(cfg *AWSConfig)` | `EngineOptions.AWSConfig` is never read | `WithAWS(AWSConfig{...})` (same struct type, but actually used) |
| `WithAWSRegion(region string)` | `EngineOptions.AWSRegion` is never read | `WithAWS(AWSConfig{Region: region})` |
| `WithAWSProfile(profile string)` | `EngineOptions.AWSProfile` is never read | `WithAWS(AWSConfig{Profile: profile})` |
| `WithMemoryPools(enabled bool)` | Sets a feature flag nothing reads — no pooling implementation exists to gate | N/A |

**Note:** the `Engine` interface separately has three no-op methods with related names — `WithLogger(logger) Engine`, `WithVaultClient(client) Engine`, `WithAWSConfig(cfg) Engine` — that return the receiver unchanged. These are distinct symbols from the `EngineOption`-returning functions of similar names above (`graft.WithLogger(logger) Option` is functional; `Engine.WithLogger(logger) Engine` is not).

## Configure: runtime reconfiguration

```go
func (e *DefaultEngine) Configure(opts ...Option) error
```

`Configure` is a method on the concrete `*DefaultEngine`, not part of the `Engine` interface — call it directly on the value `NewEngine` returns, or type-assert an `Engine` to `*DefaultEngine` first.

It applies `opts` as an incremental change over the engine's current configuration: a copy of the engine's existing options with `opts` applied on top, so any field `opts` doesn't touch keeps its current value. It validates the result fully before changing anything: an invalid `MaxConcurrency` (negative) returns an error without touching the engine's configuration, and so does one or more invalid pending custom-operator or custom-backend registrations (an empty name, a nil `Operator`/`Backend`, or — for backends only — a `Backend` stored under a map key that disagrees with its own `Name()`) — for the first invalid registration in sorted-name order, deterministically. Only once validation passes does it:

- re-derive the engine's cache from the resulting configuration (rebuilding it with the new size/TTL, or removing it, as `EnableCache`/`CacheSize`/`CacheTTL`/`CacheInstance` dictate — a rebuild discards the previous cache's contents and closes the outgoing cache; a call that changes none of those fields, nor the `FeatureCaching` flag that gates `EnableCache`, skips the rebuild entirely),
- re-apply any `WithTraceOutput`/`WithTraceLevel`/`WithDebugLogging` change (subject to the same process-wide-sink caveat described above),
- register every pending custom operator (`WithOperators`/`WithCustomOperator`) not already registered on this engine, in the same sorted-name order used for validation,
- keep the vault/AWS/NATS skip flags in sync with the resulting configuration,
- and apply `WithBackendRegistry`/`WithBackend`/`WithBackendRetry`/`WithBackendCache`/`WithAuditLogger` (and, transitively, `WithVault`/`WithVaultTarget`/`WithAWS`/`WithAWSTarget`, which register through `WithBackend`) exactly as `NewEngine` does at construction: the feature flag first, then retry/cache/audit-logger configuration, then every pending backend registration in sorted-name order — see [Backend Configuration Options](#backend-configuration-options).

Because pending operators and backends are validated up front, registration is expected to always succeed; if it somehow does not, the rest of the configuration applied by that call has already taken effect — registration is the one step `Configure` cannot roll back.

```go
engine, _ := graft.NewEngine()

err := engine.(*graft.DefaultEngine).Configure(
    graft.WithCacheSize(2000),
    graft.WithTraceLevel(graft.TraceLevelDebug),
)
```

`UpdateOptions(opts EngineOptions)` is the older, non-incremental sibling: it replaces the engine's options wholesale, so any field not set on `opts` reverts to its zero value — including fields the engine was originally constructed with. Prefer `Configure` unless a full reset is what you want.

## Cut from this page

`WithValidation(enabled bool)` and `WithAnalytics(enabled bool)` do not exist anywhere in `pkg/graft` and are not planned; they described no defined semantic. Pipeline-parallelism options (`WithPipeline`/`PipelineConfig`) and NATS connection options (`WithNATS`/`WithNATSTarget`/`NATSConfig`) are not implemented in this release; they are not documented here until they ship. `WithVault`/`WithVaultTarget`/`WithAWS`/`WithAWSTarget` **are** implemented - see [Backend Configuration Options](#backend-configuration-options) above. History tracking (`WithHistoryTracking`/`WithHistoryConfig`) is implemented - see the [History Tracking](#history-tracking) section above and [History Interface](history-api.md).

## Related Documentation

- [Engine Interface](engine.md) - Core engine operations

- [Document Interface](document.md) - Document handling

- [MergeBuilder API](merge-builder.md) - Merge configuration

- [Custom Operators](../custom-operators.md) - Creating operators
