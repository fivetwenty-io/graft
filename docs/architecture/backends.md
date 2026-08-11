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

```go
type VaultConfig struct {
    Address    string        // Vault server URL
    Token      string        // Authentication token
    Namespace  string        // Enterprise namespace
    SkipVerify bool          // Skip TLS verification
    Timeout    time.Duration // Request timeout
    MaxRetries int           // Retry count
    RetryDelay time.Duration // Delay between retries
}
```

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

```go
type VaultClientPool struct {
    clients map[string]*vaultapi.Client
    configs map[string]*VaultConfig
    mu      sync.RWMutex
}

func NewVaultClientPool() *VaultClientPool {
    return &VaultClientPool{
        clients: make(map[string]*vaultapi.Client),
        configs: make(map[string]*VaultConfig),
    }
}

func (p *VaultClientPool) GetClient(target string) (*vaultapi.Client, error) {
    p.mu.RLock()
    if client, ok := p.clients[target]; ok {
        p.mu.RUnlock()
        return client, nil
    }
    p.mu.RUnlock()

    p.mu.Lock()
    defer p.mu.Unlock()

    // Double-check after acquiring write lock
    if client, ok := p.clients[target]; ok {
        return client, nil
    }

    // Create new client
    config := p.getConfig(target)
    client, err := p.createClient(config)
    if err != nil {
        return nil, err
    }

    p.clients[target] = client
    return client, nil
}

func (p *VaultClientPool) getConfig(target string) *VaultConfig {
    // Check for target-specific config
    if cfg, ok := p.configs[target]; ok {
        return cfg
    }

    // Build from environment
    prefix := ""
    if target != "" && target != "default" {
        prefix = strings.ToUpper(target) + "_"
    }

    return &VaultConfig{
        Address:    getEnv(prefix+"VAULT_ADDR", "https://127.0.0.1:8200"),
        Token:      getEnv(prefix+"VAULT_TOKEN", ""),
        Namespace:  getEnv(prefix+"VAULT_NAMESPACE", ""),
        SkipVerify: getEnvBool(prefix+"VAULT_SKIP_VERIFY", false),
        Timeout:    30 * time.Second,
    }
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
    CacheTTL           time.Duration
    AssumeRoleDuration time.Duration
    AuditLogging       bool
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

### Cache Interface

```go
type BackendCache interface {
    Get(key string) (interface{}, bool)
    Set(key string, value interface{}, ttl time.Duration)
    Delete(key string)
    Clear()
}
```

### LRU Cache Implementation

```go
type LRUCache struct {
    capacity int
    items    map[string]*cacheItem
    order    *list.List
    mu       sync.RWMutex
}

type cacheItem struct {
    key     string
    value   interface{}
    expires time.Time
    element *list.Element
}

func NewLRUCache(capacity int) *LRUCache {
    return &LRUCache{
        capacity: capacity,
        items:    make(map[string]*cacheItem),
        order:    list.New(),
    }
}

func (c *LRUCache) Get(key string) (interface{}, bool) {
    c.mu.RLock()
    item, ok := c.items[key]
    if !ok {
        c.mu.RUnlock()
        return nil, false
    }

    if time.Now().After(item.expires) {
        c.mu.RUnlock()
        c.Delete(key)
        return nil, false
    }
    c.mu.RUnlock()

    // Move to front (most recently used)
    c.mu.Lock()
    c.order.MoveToFront(item.element)
    c.mu.Unlock()

    return item.value, true
}

func (c *LRUCache) Set(key string, value interface{}, ttl time.Duration) {
    c.mu.Lock()
    defer c.mu.Unlock()

    // Update existing
    if item, ok := c.items[key]; ok {
        item.value = value
        item.expires = time.Now().Add(ttl)
        c.order.MoveToFront(item.element)
        return
    }

    // Evict if at capacity
    if len(c.items) >= c.capacity {
        oldest := c.order.Back()
        if oldest != nil {
            oldItem := oldest.Value.(*cacheItem)
            delete(c.items, oldItem.key)
            c.order.Remove(oldest)
        }
    }

    // Add new
    item := &cacheItem{
        key:     key,
        value:   value,
        expires: time.Now().Add(ttl),
    }
    item.element = c.order.PushFront(item)
    c.items[key] = item
}
```

### Multi-Tier Caching

```go
type CacheManager struct {
    // L1: In-memory LRU
    l1 *LRUCache

    // L2: External cache (optional)
    l2 ExternalCache

    // TTL configuration
    defaultTTL time.Duration
    secretTTL  time.Duration
}

func (m *CacheManager) Get(key string) (interface{}, bool) {
    // Check L1
    if value, ok := m.l1.Get(key); ok {
        return value, true
    }

    // Check L2
    if m.l2 != nil {
        if value, ok := m.l2.Get(key); ok {
            // Promote to L1
            m.l1.Set(key, value, m.defaultTTL)
            return value, true
        }
    }

    return nil, false
}

func (m *CacheManager) Set(key string, value interface{}, isSecret bool) {
    ttl := m.defaultTTL
    if isSecret {
        ttl = m.secretTTL
    }

    m.l1.Set(key, value, ttl)

    if m.l2 != nil && !isSecret {
        m.l2.Set(key, value, ttl)
    }
}
```

## Error Handling

### Backend Errors

```go
type BackendError struct {
    Backend    string
    Target     string
    Operation  string
    Path       string
    Cause      error
    RetryCount int
    Retriable  bool
}

func (e *BackendError) Error() string {
    return fmt.Sprintf("%s[%s] %s %s: %v",
        e.Backend, e.Target, e.Operation, e.Path, e.Cause)
}

func (e *BackendError) Unwrap() error {
    return e.Cause
}
```

### Retry Logic

```go
type RetryConfig struct {
    MaxRetries   int
    InitialDelay time.Duration
    MaxDelay     time.Duration
}

func withRetry(ctx context.Context, config RetryConfig, fn func() error) error {
    var lastErr error

    for attempt := 0; attempt <= config.MaxRetries; attempt++ {
        err := fn()
        if err == nil {
            return nil
        }

        lastErr = err

        // Check if retriable
        if !isRetriable(err) {
            return err
        }

        // Check context
        select {
        case <-ctx.Done():
            return ctx.Err()
        default:
        }

        // Wait before retry with exponential backoff
        delay := config.InitialDelay * time.Duration(1<<attempt)
        if delay > config.MaxDelay {
            delay = config.MaxDelay
        }

        time.Sleep(delay)
    }

    return lastErr
}

func isRetriable(err error) bool {
    // Network errors are retriable
    var netErr net.Error
    if errors.As(err, &netErr) {
        return netErr.Temporary()
    }

    // Rate limit errors are retriable
    if strings.Contains(err.Error(), "rate limit") {
        return true
    }

    return false
}
```

## Security Considerations

### TLS Configuration

```go
type TLSConfig struct {
    Enabled    bool
    SkipVerify bool   // Only for development
    CertFile   string
    KeyFile    string
    CAFile     string
    MinVersion uint16
}

func (c *TLSConfig) Build() (*tls.Config, error) {
    if !c.Enabled {
        return nil, nil
    }

    config := &tls.Config{
        InsecureSkipVerify: c.SkipVerify,
        MinVersion:         c.MinVersion,
    }

    if c.MinVersion == 0 {
        config.MinVersion = tls.VersionTLS12
    }

    // Load client certificate
    if c.CertFile != "" && c.KeyFile != "" {
        cert, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
        if err != nil {
            return nil, err
        }
        config.Certificates = []tls.Certificate{cert}
    }

    // Load CA certificate
    if c.CAFile != "" {
        caCert, err := os.ReadFile(c.CAFile)
        if err != nil {
            return nil, err
        }
        config.RootCAs = x509.NewCertPool()
        config.RootCAs.AppendCertsFromPEM(caCert)
    }

    return config, nil
}
```

### Audit Logging

```go
type AuditEvent struct {
    Timestamp time.Time
    Operation string
    Backend   string
    Target    string
    Path      string
    User      string
    Success   bool
    Error     string
    Duration  time.Duration
}

type AuditLogger interface {
    Log(event AuditEvent)
}

// Log backend operations
func (c *VaultClient) Read(path string) (interface{}, error) {
    start := time.Now()

    value, err := c.client.Logical().Read(path)

    c.audit.Log(AuditEvent{
        Timestamp: start,
        Operation: "read",
        Backend:   "vault",
        Target:    c.target,
        Path:      path,
        Success:   err == nil,
        Error:     errString(err),
        Duration:  time.Since(start),
    })

    return value, err
}
```

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
