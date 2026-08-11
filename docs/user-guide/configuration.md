# Configuration

Configure Graft behavior through environment variables and options.

## Environment Variables

### General

| Variable | Description | Default |
|----------|-------------|---------|
| `DEBUG` | Enable debug logging (any non-empty value except `false`/`0`) | `false` |
| `TRACE` | Enable trace logging (any non-empty value except `false`/`0`) | `false` |
| `NO_COLOR` | Disable colorized output (any non-empty value) | - |

There is no `GRAFT_COLOR`, `GRAFT_DEBUG`, or `GRAFT_TRACE` variable. Color
is controlled by the `--color` CLI flag (`on`/`off`/`auto`) and by
`NO_COLOR`/`TERM=dumb`; debug and trace logging are controlled by the
`-D`/`--debug` and `-T`/`--trace` flags or by the `DEBUG`/`TRACE`
environment variables above.

### Vault Configuration

| Variable | Description | Default |
|----------|-------------|---------|
| `VAULT_ADDR` | Vault server URL | - |
| `VAULT_TOKEN` | Authentication token | - |
| `VAULT_NAMESPACE` | Vault namespace | - |
| `VAULT_SKIP_VERIFY` | Skip TLS verification | `false` |

If `VAULT_ADDR` or `VAULT_TOKEN` is unset, graft falls back to
`~/.svtoken`, then to a bare token read from `~/.vault-token`.
`VAULT_CACERT`, `VAULT_CAPATH`, `VAULT_CLIENT_CERT`, `VAULT_CLIENT_KEY`,
and `VAULT_TLS_SERVER_NAME` are not honored: graft builds its own
`http.Client` with its own TLS configuration when constructing the Vault
API client, which does not pick up the HashiCorp SDK's environment-driven
TLS settings.

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
| `AWS_PROFILE` | Credentials profile | - |
| `AWS_ROLE` | Role ARN to assume | - |
| `AWS_ACCESS_KEY_ID` | Access key ID | - |
| `AWS_SECRET_ACCESS_KEY` | Secret access key | - |
| `AWS_SESSION_TOKEN` | Session token (STS) | - |
| `AWS_WEB_IDENTITY_TOKEN_FILE` | Web identity token file (with `AWS_ROLE_ARN`) | - |
| `AWS_CA_BUNDLE` | Path to a custom CA bundle | - |

`AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_SESSION_TOKEN`,
`AWS_WEB_IDENTITY_TOKEN_FILE`, `AWS_ROLE_ARN`, and `AWS_CA_BUNDLE` are read
by the AWS SDK's default credential chain, not by graft's own code; graft
itself reads only `AWS_REGION`, `AWS_PROFILE`, and `AWS_ROLE` directly.
There is no `AWS_ENDPOINT_URL`; the real, per-target-only equivalent is
`AWS_{TARGET}_ENDPOINT` below.

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
| `NATS_URL` | NATS server URL | `nats://127.0.0.1:4222` |
| `NATS_TOKEN` | Authentication token | - |
| `NATS_USER` | Username | - |
| `NATS_PASSWORD` | Password | - |
| `NATS_NKEY` | Path to an nkey seed file | - |
| `NATS_CREDS` | Path to a `.creds` file | - |
| `NATS_CERT_FILE` | TLS client certificate path | - |
| `NATS_KEY_FILE` | TLS client key path | - |
| `NATS_CA_FILE` | TLS CA certificate path | - |
| `NATS_TIMEOUT` | Connection timeout | `5s` |

When more than one auth variable is set, precedence (highest first) is
`NATS_CREDS`, `NATS_NKEY`, `NATS_TOKEN`, then `NATS_USER`/`NATS_PASSWORD`.

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
# NATS TLS
export NATS_TLS="true"
export NATS_CERT_FILE="/path/to/cert.pem"
export NATS_KEY_FILE="/path/to/key.pem"
export NATS_CA_FILE="/path/to/ca.pem"
```

Graft has no working TLS-material environment variables for Vault
(`VAULT_CACERT`/`VAULT_CLIENT_CERT`/`VAULT_CLIENT_KEY` are not honored;
see [Vault Configuration](#vault-configuration) above). Use `VAULT_ADDR`
with an `https://` URL and `VAULT_SKIP_VERIFY` for TLS verification
control instead.

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
