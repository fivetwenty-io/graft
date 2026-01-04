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
    // Cache settings
    graft.WithCacheSize(1000),
    graft.WithCacheTTL(5 * time.Minute),

    // Tracing
    graft.WithTraceOutput(os.Stderr),

    // History
    graft.WithHistoryTracking(true),
)
```

### Pipeline Configuration

```go
engine, _ := graft.NewEngine(
    graft.WithPipeline(graft.PipelineConfig{
        // File parallelism
        FileParallelism:     8,

        // Evaluation parallelism
        EvalParallelism:     16,

        // Sub-tree parallelism
        SubtreeParallelism:  true,
        SubtreeThreshold:    100,

        // External calls
        ExternalParallelism: 32,
        BatchSize:           20,
        BatchTimeout:        100 * time.Millisecond,

        // Pool sizes
        VaultPoolSize:       10,
        AWSPoolSize:         10,
        NATSPoolSize:        10,
    }),
)
```

### Pipeline Presets

```go
// For debugging (sequential execution)
graft.WithPipeline(graft.PipelineSequential)

// For resource-constrained environments
graft.WithPipeline(graft.PipelineConservative)

// Default balanced settings
graft.WithPipeline(graft.PipelineBalanced)

// For many external calls
graft.WithPipeline(graft.PipelineHighThroughput)

// For small, fast merges
graft.WithPipeline(graft.PipelineLowLatency)
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

### Large Files

```go
// Increase parallelism for large files
graft.WithPipeline(graft.PipelineConfig{
    FileParallelism:   16,
    EvalParallelism:   32,
    SubtreeParallelism: true,
    SubtreeThreshold:  50,  // Lower threshold
})
```

### Many External Calls

```go
// Optimize for external call throughput
graft.WithPipeline(graft.PipelineConfig{
    ExternalParallelism: 64,
    BatchSize:           50,
    BatchTimeout:        50 * time.Millisecond,
    VaultPoolSize:       20,
    AWSPoolSize:         20,
})
```

### Memory Optimization

```go
// Reduce memory usage
graft.WithCacheSize(100),        // Smaller cache
graft.WithHistoryTracking(false), // Disable history
```

### Debugging

```go
// Maximum visibility
graft.WithPipeline(graft.PipelineSequential),
graft.WithTraceOutput(os.Stderr),
graft.WithHistoryTracking(true),
```

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
