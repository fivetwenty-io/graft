# Configuration

Configure Graft behavior through environment variables and options.

## Environment Variables

### General

| Variable | Description | Default |
|----------|-------------|---------|
| `GRAFT_COLOR` | Color output: `on`, `off`, `auto` | `auto` |
| `GRAFT_DEBUG` | Enable debug logging | `false` |
| `GRAFT_TRACE` | Enable trace logging | `false` |

### Vault Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `VAULT_ADDR` | Vault server URL | - |
| `VAULT_TOKEN` | Authentication token | - |
| `VAULT_NAMESPACE` | Vault namespace | - |
| `VAULT_SKIP_VERIFY` | Skip TLS verification | `false` |
| `VAULT_CACERT` | CA certificate path | - |
| `VAULT_CLIENT_CERT` | Client certificate path | - |
| `VAULT_CLIENT_KEY` | Client key path | - |

**Per-target Vault:**

```sh
# Production target
export VAULT_PROD_ADDR="https://vault-prod.example.com"
export VAULT_PROD_TOKEN="s.prod-token"
export VAULT_PROD_NAMESPACE="prod"

# Staging target
export VAULT_STAGING_ADDR="https://vault-staging.example.com"
export VAULT_STAGING_TOKEN="s.staging-token"
```

### AWS Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `AWS_REGION` | AWS region | - |
| `AWS_PROFILE` | Credentials profile | `default` |
| `AWS_ACCESS_KEY_ID` | Access key ID | - |
| `AWS_SECRET_ACCESS_KEY` | Secret access key | - |
| `AWS_SESSION_TOKEN` | Session token (STS) | - |
| `AWS_ENDPOINT_URL` | Custom endpoint URL | - |

**Per-target AWS:**

```sh
# Production target
export AWS_PRODUCTION_REGION="us-east-1"
export AWS_PRODUCTION_PROFILE="production"

# Staging target
export AWS_STAGING_REGION="us-west-2"
export AWS_STAGING_PROFILE="staging"
```

### NATS Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `NATS_URL` | NATS server URL | `nats://localhost:4222` |
| `NATS_TOKEN` | Authentication token | - |
| `NATS_USER` | Username | - |
| `NATS_PASSWORD` | Password | - |
| `NATS_CREDS` | Credentials file path | - |
| `NATS_TLS_CERT` | TLS certificate path | - |
| `NATS_TLS_KEY` | TLS key path | - |
| `NATS_TLS_CA` | TLS CA certificate path | - |

**Per-target NATS:**

```sh
# Production target
export NATS_PRODUCTION_URL="nats://nats-prod.example.com:4222"
export NATS_PRODUCTION_TOKEN="prod-token"
```

## CLI Options

### Global Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--debug` | `-D` | Enable debug logging |
| `--trace` | `-T` | Enable trace logging |
| `--color` | | Color output: `on`, `off`, `auto` |
| `--version` | `-v` | Show version |
| `--help` | `-h` | Show help |

### Merge Options

| Flag | Description |
|------|-------------|
| `--skip-eval` | Don't evaluate operators |
| `--prune KEY` | Remove key from output |
| `--cherry-pick KEY` | Output only specific keys |
| `--fallback-append` | Default array merge to append |
| `--go-patch` | Enable go-patch format |
| `--multi-doc` | Handle multi-document YAML |
| `--history` | Show merge history |
| `--trace-path PATH` | Show history for path |

## Library Configuration

### Engine Options

```go
engine, _ := graft.NewEngine(
    // Operator-result cache: enabled, up to 1000 entries.
    graft.WithCache(true, 1000),

    // Verbose operator-level logging.
    graft.WithDebugLogging(true),
)
```

### Parallel Evaluation

```go
engine, _ := graft.NewEngine(
    // Worker count ceiling; 0 (the default) auto-detects from
    // runtime.NumCPU(). This sizes the pool used for wave-level operator
    // concurrency only - file-level read/parse concurrency (one goroutine
    // per input file) is separate, does not draw from this pool, and is
    // unconditional. No request-batching configuration exists either way
    // (backend request deduplication is unconditional, see
    // ../architecture/parallelism.md).
    graft.WithMaxWorkers(16),

    // false for sequential evaluation (debugging, or reproducing spruce's
    // exact operator evaluation order).
    graft.WithParallel(true),
)
```

### Backend Configuration

```go
// Vault
graft.WithVault(graft.VaultConfig{
    Address:    "https://vault.example.com",
    Token:      os.Getenv("VAULT_TOKEN"),
    Namespace:  "prod",
    SkipVerify: false,
    PoolSize:   10,
    Timeout:    30 * time.Second,
})

// Additional Vault targets
graft.WithVaultTarget("staging", graft.VaultConfig{
    Address: "https://vault-staging.example.com",
    Token:   os.Getenv("VAULT_STAGING_TOKEN"),
})

// AWS
graft.WithAWS(graft.AWSConfig{
    Region:  "us-west-2",
    Profile: "production",
})

// NATS
graft.WithNATS(graft.NATSConfig{
    URL:     "nats://localhost:4222",
    Token:   os.Getenv("NATS_TOKEN"),
    TLSCert: "/path/to/cert.pem",
    TLSKey:  "/path/to/key.pem",
})
```

### Post-Processing

```go
graft.WithPostProcessors(
    graft.SchemaValidator(mySchema),
    graft.SecretDetector(patterns),
    &MyCustomPostProcessor{},
)
```

## Configuration Files

Graft doesn't use configuration files by default. All configuration is through environment variables, CLI flags, or library options.

For project-specific settings, create a wrapper script:

```sh
#!/bin/bash
# graft-project.sh

export VAULT_ADDR="https://vault.example.com"
export AWS_REGION="us-west-2"

graft "$@"
```

Or use a Makefile:

```makefile
VAULT_ADDR := https://vault.example.com
AWS_REGION := us-west-2

.PHONY: config
config:
    VAULT_ADDR=$(VAULT_ADDR) AWS_REGION=$(AWS_REGION) \
    graft merge base.yml $(ENV).yml
```

## Configuration Precedence

Configuration sources are checked in order:

1. CLI flags (highest priority)
2. Environment variables
3. Library options
4. Defaults (lowest priority)

## Performance Tuning

Graft's actual tuning surface is smaller than it may look: parallel
evaluation (worker count and file-level read/parse concurrency) and the
operator-result cache. There is no request-batching configuration —
backend request deduplication (see
[Parallel Execution Model](../architecture/parallelism.md#level-3-backend-request-dedup))
is unconditional and has no tunable knobs.

### Worker Count

```go
// Explicit worker ceiling (0, the default, auto-detects from runtime.NumCPU()).
graft.WithMaxWorkers(16)

// Equivalent CLI/config form:
// GRAFT_PARALLEL_MAX_WORKERS=16 graft merge base.yml overlay.yml
```

`WithMaxWorkers` is an alias for `WithConcurrency`. This value sizes the
worker pool used for wave-level operator concurrency only. File-level
read/parse concurrency is separate: it spawns one goroutine per input
file, does not draw from the worker pool, and is unconditional — this
option does not affect it.

### Disabling Parallelism

```go
// Sequential evaluation, for debugging or reproducing spruce's exact
// operator evaluation order.
graft.WithParallel(false)

// Equivalent CLI/config form:
// GRAFT_PARALLEL_ENABLED=false graft merge base.yml overlay.yml
```

### Cache Size

```go
// Operator-result cache: enabled, holding up to 1000 entries.
graft.WithCache(true, 1000)
```

This is the general operator-result cache (`internal/cache`), separate
from each backend's own secret/parameter/KV cache
(`vault.SecretCache`, the AWS `ClientPool`'s per-target caches, and
`natsbackend.Cache`), which are unconditional and not sized via engine
options.

### Debugging

```go
// Verbose operator-level logging.
graft.WithDebugLogging(true)
```

No benchmark numbers are published here — see
[Parallel Execution Model](../architecture/parallelism.md#when-parallel-evaluation-helps)
for why, and how to measure your own workload.

## Security Configuration

### TLS Settings

```sh
# Vault TLS
export VAULT_CACERT="/path/to/ca.pem"
export VAULT_CLIENT_CERT="/path/to/client-cert.pem"
export VAULT_CLIENT_KEY="/path/to/client-key.pem"

# NATS TLS
export NATS_TLS_CERT="/path/to/cert.pem"
export NATS_TLS_KEY="/path/to/key.pem"
export NATS_TLS_CA="/path/to/ca.pem"
```

### Secret Redaction

```go
graft.WithHistoryRedaction([]string{
    "password",
    "secret",
    "key",
    "token",
    "credential",
})
```

## See Also

- [Secrets Management](secrets/) - Backend configuration
- [merge Command](cli/merge.md) - CLI options
- [Developer Guide](../developer-guide/) - Library usage
