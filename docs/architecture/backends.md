# Backend Architecture

Graft supports multiple external backends for secrets management and configuration storage. Each backend is designed with connection pooling, deduplication of concurrent identical requests, and multi-target support.

## Overview

### Supported Backends

| Backend | Operator | Purpose |
|---------|----------|---------|
| Vault / OpenBao | `vault` | Secrets management |
| AWS SSM | `awsparam` | Parameter Store |
| AWS Secrets Manager | `awssecret` | Secrets storage |
| NATS JetStream | `nats` | KV and Object store |

### Common Features

All backends share these design characteristics:

- **Multi-Target Support**

  Use `operator@target` syntax for multiple environments

- **Connection Pooling**

  Reuse connections per target

- **Request Deduplication**

  Concurrent identical requests (same target, same path) are coalesced into one backend call

- **Caching**

  Configurable TTL cache

- **Fallback Values**

  Use `|| "default"` syntax

## Custom Backend Registry (Extension Point)

Everything below this section describes graft's own, always-on resolution
path through `internal/backends`. Separately, `pkg/graft` exposes a
`Backend` extension point that lets a library consumer plug a *different*
secret/parameter source into the same four operators — this section covers
that design; [Custom Backends](../developer-guide/custom-backends.md) is
the how-to (minimal implementation, registration, testing) and
[Backend Configuration Options](../developer-guide/library-api/options.md#backend-configuration-options)
covers the two built-in `Backend` implementations (`WithVault`/`WithAWS`)
that also go through this same registry.

### Why a registry, not a direct dependency

`internal/backends/{vault,aws,nats}` import `pkg/graft` (for `graft.Engine`
and debug logging), so `pkg/graft` cannot import them back to construct
adapters wrapping them without an import cycle. Rather than restructure
that dependency direction, the registry inverts it: `pkg/graft` defines the
`Backend` interface and a name-keyed registry
(`Engine.RegisterBackend`/`GetBackend`/`ListBackends`/`UnregisterBackend`,
or `WithBackend` at construction), and each of the `vault`/`awsparam`/
`awssecret`/`nats` operators (`pkg/graft/operators`, which already imports
both `pkg/graft` and `internal/backends`) consults the registry first,
falling back to its existing `internal/backends` call unchanged when
nothing is registered under its own name. The observable result is
identical to "the registry started out seeded with a pass-through adapter
over `internal/backends`, which the caller never overrode" — but with a
simpler dependency graph and no such adapter actually written.

### Feature flag

The registry is consulted only when `features.FeatureBackendRegistry` is
enabled on the engine — off by default, so an engine that never touches
this extension point behaves byte-identical to one built before the
registry existed. Toggle it with `graft.WithBackendRegistry(true)` (the
only way to reach the flag from outside this module) or the
`GRAFT_FEATURE_BACKEND_REGISTRY=true` environment variable, read by the
`graft` CLI at startup. With the flag on and nothing registered under a
given operator's name, behavior is still unchanged: that operator falls
back to `internal/backends` exactly as if the flag were off.

### The `Backend` interface

```go
type Backend interface {
    Name() string
    Get(ctx context.Context, path string) (interface{}, error)
    GetBatch(ctx context.Context, paths []string) (map[string]interface{}, error)
    Health(ctx context.Context) error
    Close() error
}

// TargetedBackend is an optional capability (checked with a type
// assertion) for backends that support "@target" call syntax.
type TargetedBackend interface {
    Backend
    GetWithTarget(ctx context.Context, target, path string) (interface{}, error)
}
```

A missing path is reported via `errors.Is(err, graft.ErrBackendNotFound)`,
not a distinct error type. `GetBatch` exists on the interface because a
batch API is a reasonable thing for a secret-store client to offer, but no
graft operator calls it — `vault`/`awsparam`/`awssecret`/`nats` each
resolve one path per operator call, and there is no batching call site
anywhere in graft to design a real batched fetch against. Implementations
that don't need real batching use `graft.SequentialGetBatch` (loops `Get`
once per path); see [Custom Backends: Batching](../developer-guide/custom-backends.md#batching).

### Generic wrapping, not backend-specific logic

`RegisterBackend` wraps whatever is registered with the registry's own
cache/retry/audit-logging behavior (`WithBackendCache`/`WithBackendRetry`/
`WithAuditLogger`), rather than requiring each `Backend` implementation to
build that itself:

```go
type RetryConfig struct {
    MaxAttempts     int
    InitialInterval time.Duration
    MaxInterval     time.Duration
    Multiplier      float64
    RetryableErrors func(error) bool
}

type BackendCache interface {
    Get(key string) (interface{}, bool)
    Set(key string, value interface{}, ttl time.Duration)
    Delete(key string)
    Clear()
}

type AuditLogger interface {
    LogAccess(ctx context.Context, backend, path string, success bool, err error)
}
```

All three wrap only `Get`/`GetWithTarget` — not `GetBatch`, for the same
reason `GetBatch` itself is unwired above — and only backends actually
registered in the registry; they never touch `internal/backends`' own,
separate behavior (its response caches described per-backend below, or
NATS's native `Config.AuditLogging` trail). Graft ships no default
`BackendCache` implementation and no cross-backend cache administration
(stats, invalidate-by-prefix, clear-all, sometimes called a
`CacheManager` elsewhere) — only this one per-backend interface, supplied
by the caller. See
[Generic retry, caching, and audit logging](../developer-guide/custom-backends.md#generic-retry-caching-and-audit-logging)
for the full walkthrough and defaulting rules (e.g. `Multiplier <= 0`
means constant delay, not exponential backoff).

### Errors

A non-not-found failure from a registered backend is wrapped as a
`*graft.GraftError{Type: graft.ExternalError}` carrying a
`*graft.BackendError` as its `Cause`:

```go
type BackendError struct {
    Backend string
    Target  string
    Path    string
    Message string
    Cause   error
}
```

`*graft.BackendError` is never the outermost error an operator returns —
only reachable via `errors.As`. It has no `Operation`/`Retriable`/
`RetryCount` fields; retry eligibility is decided by `RetryConfig`, not
carried on the error.

## Vault / OpenBao

### Operator Syntax

```yaml
# Basic access
password: (( vault "secret/db:password" ))

# With target (different Vault instance)
prod_pass: (( vault@prod "secret/db:password" ))
staging_pass: (( vault@staging "secret/db:password" ))

# Multiple paths with fallback
password: (( vault "secret/v2:pass; secret/v1:pass" ))

# With default value
password: (( vault "secret/db:password" || "default" ))

# Dynamic path construction
password: (( vault (concat "secret/" env ":password") ))
```

### Configuration

There is no `VaultConfig` type on the built-in (non-registry) path, and no `Timeout`/`MaxRetries`/`RetryDelay` fields anywhere in it. The real per-target config type is `vault.Target` (`internal/backends/vault/config.go`), built from environment variables by `ClientPool.getTargetConfig` — the same "there is no `X` type, real type is `Y`" shape as the AWS/NATS sections below:

```go
// internal/backends/vault/config.go
type Target struct {
    URL        string `yaml:"url"`
    Token      string `yaml:"token"`
    Namespace  string `yaml:"namespace"`
    SkipVerify bool   `yaml:"skip_verify"`
}
```

Separately, `pkg/graft.VaultConfig` (`backend_vault.go:26-51`) is a different, real, public type — but it belongs to the custom backend registry's `WithVault`/`WithVaultTarget` engine options (see [Custom Backend Registry](#custom-backend-registry-extension-point) above and [Backend Configuration Options](../developer-guide/library-api/options.md#backend-configuration-options)), not to this always-on path. Its fields are `Address, Token, Namespace, SkipVerify, Timeout, PoolSize` — `Token`/`Namespace`/`SkipVerify` match `Target`'s names, `Address` is `Target.URL` under a different name, and `Timeout`/`PoolSize` have no `Target` equivalent at all. It is read only when a `WithVault` call registers a backend under `"vault"` with the registry feature flag on; this section's environment variables and `vault.Target` govern every other case.

Environment variables:

- `VAULT_ADDR` - Vault server URL

- `VAULT_TOKEN` - Authentication token

- `VAULT_NAMESPACE` - Enterprise namespace

- `VAULT_SKIP_VERIFY` - Skip TLS verification

Per-target environment variables:

- `VAULT_{TARGET}_ADDR`

- `VAULT_{TARGET}_TOKEN`

- etc.

### Connection Pool

There is no `VaultClientPool`/`NewVaultClientPool` type — no such name exists anywhere in the Go source. The real type is `vault.ClientPool` (`internal/backends/vault/client.go`), a `sync.RWMutex`-guarded map of per-target `VaultReader`s, matching the `aws.ClientPool`/`nats.ClientPool` shape documented below:

```go
// internal/backends/vault/client.go (abridged)
type ClientPool struct {
    mu      sync.RWMutex
    clients map[string]VaultReader
    configs map[string]*Target
}

var DefaultPool = &ClientPool{
    clients: make(map[string]VaultReader),
    configs: make(map[string]*Target),
}

func (vcp *ClientPool) GetClient(targetName string, engine graft.Engine) (VaultReader, error) {
    vcp.mu.RLock()
    if client, exists := vcp.clients[targetName]; exists {
        vcp.mu.RUnlock()
        return client, nil
    }
    vcp.mu.RUnlock()

    config, err := vcp.getTargetConfig(targetName, engine)
    if err != nil {
        return nil, fmt.Errorf("vault target '%s' not found: %w", targetName, err)
    }

    client, err := CreateClientFromConfig(config)
    if err != nil {
        return nil, fmt.Errorf("failed to create vault client for target '%s': %w", targetName, err)
    }

    vcp.mu.Lock()
    if existing, exists := vcp.clients[targetName]; exists {
        vcp.mu.Unlock()
        return existing, nil
    }
    vcp.clients[targetName] = client
    vcp.configs[targetName] = config
    vcp.mu.Unlock()

    return client, nil
}
```

### Request Deduplication

Graft does not batch distinct Vault requests into fewer round trips — there
is no `VaultBatcher`, no request queue, and no batch-size/timeout
configuration. What it does is deduplicate: when a wave's concurrent
operators (see [Parallel Execution Model](parallelism.md#level-2-wave-based-operator-evaluation))
resolve to the *same* cache key (`target + path`), only the first caller
triggers a real Vault request — every other concurrent caller for that
exact key waits on and shares its result, via a `singleflight`-based group
(`internal/backends/reqdedup`). A document with ten different secret paths
still makes ten Vault requests; only references to the *same* path
collapse to one.

```go
// internal/backends/vault/cache.go (abridged)
func (c *secretCache) GetOrFetch(path string, fetch func() (map[string]interface{}, error)) (map[string]interface{}, error) {
    if v, ok := c.Get(path); ok {
        return v, nil
    }
    v, err := c.group.Do(path, fetch) // coalesces concurrent identical-key callers
    if err != nil {
        return nil, err
    }
    c.Set(path, v)
    return v, nil
}
```

The vault client cache has no TTL — once a path is fetched, the cached
value is reused for the rest of the process's evaluation runs until
explicitly reset (`vault.SecretCache.Reset()`), not expired on a timer.
The same `GetOrFetch`/dedup pattern backs `awsparam`/`awssecret`
(`internal/backends/aws/cache.go`) and `nats`
(`internal/backends/nats/cached_fetch.go`) — NATS's cache is the one
exception with a real TTL (`Config.CacheTTL`), inherited from its
pre-existing `TTLCache`.

## AWS Parameter Store

### Operator Syntax

```yaml
# Basic parameter
db_host: (( awsparam "/app/prod/db_host" ))

# With JSON key extraction
db_port: (( awsparam "/app/config?key=database.port" ))

# With target (different AWS account/region)
db_host: (( awsparam@staging "/app/db_host" ))

# With default
db_host: (( awsparam "/app/db_host" || "localhost" ))
```

### Configuration

The real per-target config type is `aws.Target` (`internal/backends/aws/config.go`), built from environment variables by `ClientPool.GetTargetConfig` - there is no `AWSConfig` type, and no `Endpoint`/`AccessKey`/`SecretKey` fields under those names (the real fields are `Endpoint`, `AccessKeyID`, `SecretAccessKey`, plus role-assumption, retry, and cache-TTL fields not documented here):

```go
// internal/backends/aws/config.go (abridged)
type Target struct {
    Region             string
    Profile            string
    Role               string
    AccessKeyID        string
    SecretAccessKey    string
    SessionToken       string
    Endpoint           string
    MaxRetries         int
    HTTPTimeout        time.Duration
    AssumeRoleDuration time.Duration
    // ...
}
```

Environment variables:

- `AWS_REGION` - AWS region

- `AWS_PROFILE` - AWS profile name

- `AWS_ACCESS_KEY_ID` - Access key ID

- `AWS_SECRET_ACCESS_KEY` - Secret access key

Per-target environment variables:

- `AWS_{TARGET}_REGION`

- `AWS_{TARGET}_PROFILE`

- etc.

### Client Pool

The real type is `aws.ClientPool` (`internal/backends/aws/client.go`), a `sync.RWMutex`-guarded map of per-target sessions - not a type named `AWSSessionPool`:

```go
// internal/backends/aws/client.go (abridged)
type ClientPool struct {
    mu       sync.RWMutex
    sessions map[string]*session.Session
    configs  map[string]*Target
    // secretsManagerClients, parameterStoreClients, secretsCache, paramsCache omitted
}

func (acp *ClientPool) GetSession(targetName string) (*session.Session, error) {
    acp.mu.RLock()
    if sess, exists := acp.sessions[targetName]; exists {
        acp.mu.RUnlock()
        return sess, nil
    }
    acp.mu.RUnlock()

    config, err := acp.GetTargetConfig(targetName)
    if err != nil {
        return nil, fmt.Errorf("AWS target '%s' not found: %w", targetName, err)
    }

    sess, err := acp.CreateSessionFromConfig(config)
    if err != nil {
        return nil, fmt.Errorf("failed to create AWS session for target '%s': %w", targetName, err)
    }

    acp.mu.Lock()
    acp.sessions[targetName] = sess
    acp.configs[targetName] = config
    acp.mu.Unlock()

    return sess, nil
}
```

### Parameter Store

There is no `SSMClient` type. `awsparam` resolution lives in `pkg/graft/operators/op_aws.go`'s `AwsOperator.getAwsParam`, which fetches through `ClientPool.GetOrFetchParam` - the cache-check-then-dedup-then-fetch-then-cache sequence described in [Request Deduplication](#request-deduplication) above:

```go
// pkg/graft/operators/op_aws.go (abridged)
func (o AwsOperator) getAwsParam(awsSession *session.Session, cacheTarget, param string) (string, error) {
    return awsbackend.DefaultPool.GetOrFetchParam(cacheTarget, param, func() (string, error) {
        client := ssm.New(awsSession)
        input := &ssm.GetParameterInput{
            Name:           awsSDK.String(param),
            WithDecryption: awsSDK.Bool(true),
        }
        output, err := client.GetParameter(input)
        if err != nil {
            return "", err
        }
        return awsSDK.StringValue(output.Parameter.Value), nil
    })
}
```

## AWS Secrets Manager

### Operator Syntax

```yaml
# Basic secret
api_key: (( awssecret "prod/api-key" ))

# With JSON key extraction
db_pass: (( awssecret "prod/db?key=password" ))

# With version/stage
db_pass: (( awssecret "prod/db?key=password&stage=AWSCURRENT" ))

# With target
api_key: (( awssecret@prod "api-credentials" ))
```

### Secrets Manager

There is no `SecretsManagerClient` type. `awssecret` resolution is `AwsOperator.getAwsSecret` (`pkg/graft/operators/op_aws.go`), fetching through `ClientPool.GetOrFetchSecret` - the same cache-dedup-fetch-cache pattern as `awsparam`, keyed by `stage`/`version` query parameters when given:

```go
// pkg/graft/operators/op_aws.go (abridged)
func (o AwsOperator) getAwsSecret(awsSession *session.Session, cacheTarget, secret string, params url.Values) (string, error) {
    return awsbackend.DefaultPool.GetOrFetchSecret(cacheTarget, secret, func() (string, error) {
        client := secretsmanager.New(awsSession)
        input := &secretsmanager.GetSecretValueInput{
            SecretId: awsSDK.String(secret),
        }
        if params.Get("stage") != "" {
            input.VersionStage = awsSDK.String(params.Get("stage"))
        } else if params.Get("version") != "" {
            input.VersionId = awsSDK.String(params.Get("version"))
        }
        output, err := client.GetSecretValue(input)
        if err != nil {
            return "", err
        }
        return awsSDK.StringValue(output.SecretString), nil
    })
}
```

The `?key=` subkey extraction (JSON key lookup within a secret's value) and default-value (`||`) handling happen in the operator's caller, not inside this fetch function.

## NATS JetStream

### Operator Syntax

```yaml
# KV store access
config: (( nats "kv:bucket/key" ))

# Object store access
template: (( nats "obj:assets/template.yml" ))

# With target
config: (( nats@synadia "kv:config/settings" ))

# With explicit URL
config: (( nats "kv:bucket/key" "nats://server:4222" ))
```

### Configuration

There is no `NATSConfig` type. The real per-target type is `nats.Target`
(`internal/backends/nats/config.go`), and the connection-level type built
from it is `nats.Config`; both name the TLS fields `CertFile`/`KeyFile`/
`CAFile`, not `TLSCert`/`TLSKey`/`TLSCA`:

```go
// internal/backends/nats/config.go (abridged)
type Target struct {
    URL      string
    Timeout  time.Duration
    TLS      bool
    CertFile string
    KeyFile  string
    CAFile   string
    CacheTTL time.Duration
    // Retries, RetryInterval, RetryBackoff, MaxRetryInterval,
    // InsecureSkipVerify, StreamingThreshold, AuditLogging omitted

    // Auth: at most one method wins, in this order (highest first):
    // CredsFile, NkeySeedFile, Token, User/Password.
    Token        string
    User         string
    Password     string
    NkeySeedFile string
    CredsFile    string
}
```

The environment variable names for these fields are the target-prefixed
`NATS_{TARGET}_CERT_FILE`/`NATS_{TARGET}_KEY_FILE`/`NATS_{TARGET}_CA_FILE`
(TLS) and `NATS_{TARGET}_TOKEN`/`NATS_{TARGET}_USER`/
`NATS_{TARGET}_PASSWORD`/`NATS_{TARGET}_NKEY`/`NATS_{TARGET}_CREDS` (auth);
the default (no-target) connection reads the same names without the
`{TARGET}_` segment (`NATS_CERT_FILE`, `NATS_TOKEN`, etc.). Auth-option
construction (`BuildConnectionOptions` in `internal/backends/nats/client.go`)
fails fast with an error rather than silently falling back to an anonymous
connection when a configured nkey seed file or TLS client certificate
can't be loaded. See
[Environment Variables Reference](../reference/environment-variables.md)
for the full, verified table.

### Client Pool

There is no `NATSConnectionPool` type. The real type is `nats.ClientPool`
(`internal/backends/nats/client.go`), a `sync.RWMutex`-guarded map of
per-target pooled connections. Its cache-hit path takes the write lock,
not a read lock: it mutates the shared `*PooledConnection`'s
`RefCount`/`LastUsed` fields, and `RLock` only guarantees the map itself
isn't concurrently written, not that other `RLock` holders can't race on
values reached through it:

```go
// internal/backends/nats/client.go (abridged)
type ClientPool struct {
    mu          sync.RWMutex
    connections map[string]*PooledConnection
    configs     map[string]*Target
}

func (ncp *ClientPool) GetConnection(targetName string) (*PooledConnection, error) {
    ncp.mu.Lock()
    if conn, exists := ncp.connections[targetName]; exists {
        conn.RefCount++
        conn.LastUsed = time.Now()
        ncp.mu.Unlock()
        return conn, nil
    }
    ncp.mu.Unlock()

    config, err := ncp.GetTargetConfig(targetName)
    if err != nil {
        return nil, fmt.Errorf("NATS target '%s' not found: %w", targetName, err)
    }

    conn, err := CreateConnectionFromConfig(&Config{URL: config.URL /* ... */})
    if err != nil {
        return nil, fmt.Errorf("failed to create NATS connection for target '%s': %w", targetName, err)
    }

    pooledConn := &PooledConnection{Conn: conn, LastUsed: time.Now(), RefCount: 1}
    js, err := jetstream.New(conn)
    if err != nil {
        conn.Close()
        return nil, fmt.Errorf("failed to create JetStream context for target '%s': %w", targetName, err)
    }
    pooledConn.JS = js

    ncp.mu.Lock()
    ncp.connections[targetName] = pooledConn
    ncp.configs[targetName] = config
    ncp.mu.Unlock()

    return pooledConn, nil
}
```

### KV and Object Fetch

There is no `NATSClient` type. `nats` resolution is
`pkg/graft/operators/op_nats.go`, which parses the `kv:`/`obj:` path
prefix and calls the target-namespaced, dedup-and-cache-wrapped entry
points in `internal/backends/nats/cached_fetch.go`
(`FetchFromKVCached`/`FetchFromObjectCached` - see
[Request Deduplication](#request-deduplication) above), which in turn call
the real JetStream reads in `FetchFromKV`/`FetchFromObject`
(`internal/backends/nats/client.go`).

## Caching Strategy

Two independent caches can be in play for the same `(( vault ... ))` call,
depending on whether the custom backend registry (above) has anything
registered under `"vault"`:

- **The built-in path's own per-backend cache** — described per backend
  above (Vault's [Request Deduplication](#request-deduplication) section,
  AWS's `awsbackend.DefaultPool`, NATS's `cached_fetch.go`). No TTL for
  Vault/AWS; a real TTL (`Config.CacheTTL`, default 5 minutes) for NATS.
  See [Cache TTL](#cache-ttl) below.
- **The registry's optional `BackendCache` wrapper**
  (`WithBackendCache(name, c)`), which only applies to a backend actually
  registered in the registry, with a fixed `graft.DefaultBackendCacheTTL`
  (5 minutes) — see [Generic wrapping, not backend-specific logic](#generic-wrapping-not-backend-specific-logic)
  above. Graft ships no default `BackendCache` implementation and no
  cross-backend cache administration (stats, invalidate-by-prefix,
  clear-all across every registered backend at once) — a caller that wants
  that holds onto the `BackendCache` instances it hands to
  `WithBackendCache` itself and administers them directly.

The two never both apply to the same call: a registered custom backend
bypasses `internal/backends` entirely (see
[Why a registry, not a direct dependency](#why-a-registry-not-a-direct-dependency)
above), so its cache behavior, if any, comes only from `WithBackendCache`.

## Error Handling

`errors.As(err, &backendErr)` against a `*graft.BackendError` (defined
above under [Errors](#errors)) is the shape to check for a registry-mediated
backend failure. Errors from the built-in (non-registry) path — a
misconfigured Vault token, an AWS credentials failure, an unreachable NATS
server — surface as whatever `internal/backends`/the AWS/Vault/NATS SDKs
themselves return, wrapped by the calling operator in a `*graft.GraftError`
with `Type: ExternalError` (`graft.NewExternalError`); they are not
`*graft.BackendError`, which exists only for the registry.

Retry behavior for a registry-registered backend is `WithBackendRetry`'s
`RetryConfig` (documented above under
[Generic wrapping, not backend-specific logic](#generic-wrapping-not-backend-specific-logic)).
The built-in path's own retry behavior is backend-specific and pre-existing:
the `hashicorp/vault/api` client's built-in 5xx retry (2 retries by
default) for Vault, the AWS SDK's own retry policy for `awsparam`/
`awssecret`, and `nats.CreateConnectionWithRetry` for NATS connection
setup — none of it goes through `graft.RetryConfig`.

## Security Considerations

### TLS

`graft.TLSConfig` (`CertFile`, `KeyFile`, `CAFile`, `SkipVerify`,
`ServerName`) is a reusable parameter bag for `Backend` implementations
that need to establish their own TLS connections — graft's registry and
operators never read it; it exists only so custom backends don't each
invent an equivalent struct. The built-in Vault/AWS/NATS clients have their
own, separate TLS configuration, already covered in their sections above
(Vault's `VAULT_SKIP_VERIFY`, NATS's `CertFile`/`KeyFile`/`CAFile` on
`nats.Target`).

### Audit Logging

`graft.AuditLogger` (defined above under
[Generic wrapping, not backend-specific logic](#generic-wrapping-not-backend-specific-logic))
is the one audit mechanism the registry provides:
`LogAccess(ctx, backend, path string, success bool, err error)`, called
once per `Get`/`GetWithTarget`, cache hit or miss, for a backend registered
in the registry. It is unrelated to `internal/backends/nats.Config.AuditLogging`,
NATS's own, separate, pre-existing audit trail for the built-in (non-registry)
path — the two can be used together without conflict, since one covers
registry-mediated backends and the other covers NATS's built-in client
regardless of the registry.

## Performance Considerations

### Connection Pooling

There is no per-backend connection pool size setting. Each backend's
`ClientPool` (`vault.ClientPool`, `aws.ClientPool`, `nats.ClientPool`)
creates one client/session/connection per distinct target the first time
it's needed and reuses it for the rest of the run — pool "size" is
however many distinct targets a document references, and it is not
configurable. Concurrency across targets is bounded by the shared worker
pool (`GRAFT_PARALLEL_MAX_WORKERS`, see
[Parallel Execution Model](parallelism.md)), not by a per-backend limit.

### No Batching

Graft does not batch requests for any backend. AWS's SSM `GetParameters`
(up to 10 distinct parameter names per call) and Secrets Manager's
`BatchGetSecretValue` are real AWS APIs that could reduce request count
further for the AWS backends specifically, but graft does not use them
today — each `awsparam`/`awssecret` call issues its own `GetParameter`/
`GetSecretValue` request. Vault's KV API and the NATS KV/Object APIs graft
uses have no equivalent multi-key batch read to fall back on. See
[Request Deduplication](#request-deduplication) above for what graft does
do: coalesce concurrent *identical* requests, which is a different thing
than batching *distinct* ones.

### Cache TTL

Vault and AWS caches have no TTL — a fetched value is reused for the rest
of the process's evaluation until explicitly reset, not expired on a
timer. NATS's cache does have a TTL (`Config.CacheTTL`, default 5
minutes), inherited from its pre-existing `TTLCache`. There is no
per-data-type TTL recommendation to make since only one of the three
backends' caches has a TTL to tune in the first place.
