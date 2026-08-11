# Environment Variables

Complete reference of all environment variables supported by Graft.

## Graft Core

| Variable | Default | Description |
|----------|---------|-------------|
| `GRAFT_COLOR` | `auto` | Color output: `on`, `off`, `auto` |
| `GRAFT_DEBUG` | `false` | Enable debug logging |
| `GRAFT_TRACE` | `false` | Enable trace logging (verbose) |
| `GRAFT_CACHE_SIZE` | `100` | Maximum cache entries |
| `GRAFT_CACHE_TTL` | `5m` | Cache time-to-live |
| `GRAFT_MAX_LOOP_ITERATIONS` | `1000` | Iteration cap for `(( while ))` loops |

### Loop Iteration Cap

A `(( while ))` loop that exceeds the cap fails the run with
`while loop exceeded maximum iterations (<n>)` and exit code `2`. The
`--max-loop-iterations` flag overrides this variable when both are set.

```bash
GRAFT_MAX_LOOP_ITERATIONS=50 graft merge config.yml
```

### Color Output

```bash
# Force color
export GRAFT_COLOR=on
graft merge base.yml overlay.yml

# Disable color
export GRAFT_COLOR=off
graft diff before.yml after.yml

# Auto-detect (default)
export GRAFT_COLOR=auto
```

### Debug Logging

```bash
# Enable debug output
export GRAFT_DEBUG=true
graft merge base.yml overlay.yml

# Enable verbose trace output
export GRAFT_TRACE=true
graft merge base.yml overlay.yml
```

## HashiCorp Vault / OpenBao

### Default Target

| Variable | Default | Description |
|----------|---------|-------------|
| `VAULT_ADDR` | | Vault server address (required) |
| `VAULT_TOKEN` | | Authentication token (required) |
| `VAULT_NAMESPACE` | | Vault namespace (enterprise) |
| `VAULT_SKIP_VERIFY` | `false` | Skip TLS certificate verification |
| `VAULT_CACERT` | | Path to CA certificate |
| `VAULT_CAPATH` | | Path to directory of CA certificates |
| `VAULT_CLIENT_CERT` | | Path to client certificate |
| `VAULT_CLIENT_KEY` | | Path to client private key |
| `VAULT_TLS_SERVER_NAME` | | TLS server name for SNI |
| `VAULT_RATE_LIMIT` | `0` | Rate limit (requests per second, 0 = unlimited) |
| `VAULT_TIMEOUT` | `30s` | Request timeout |

### Named Targets

For multi-target configurations, use `VAULT_{TARGET}_*` format:

| Variable | Description |
|----------|-------------|
| `VAULT_{TARGET}_ADDR` | Target-specific server address |
| `VAULT_{TARGET}_TOKEN` | Target-specific token |
| `VAULT_{TARGET}_NAMESPACE` | Target-specific namespace |
| `VAULT_{TARGET}_SKIP_VERIFY` | Target-specific TLS skip |

**Example:**

```bash
# Default Vault
export VAULT_ADDR=https://vault.example.com
export VAULT_TOKEN=s.default-token

# Staging target
export VAULT_STAGING_ADDR=https://vault-staging.example.com
export VAULT_STAGING_TOKEN=s.staging-token

# Production target
export VAULT_PROD_ADDR=https://vault-prod.example.com
export VAULT_PROD_TOKEN=s.prod-token
export VAULT_PROD_NAMESPACE=production
```

**Usage in YAML:**

```yaml
default_secret: (( vault "secret/db:password" ))
staging_secret: (( vault@staging "secret/db:password" ))
prod_secret: (( vault@prod "secret/db:password" ))
```

### OpenBao

OpenBao uses the same environment variables as Vault. Configure a named target for OpenBao instances:

```bash
export VAULT_BAO_ADDR=https://openbao.example.com
export VAULT_BAO_TOKEN=s.bao-token
```

```yaml
bao_secret: (( vault@bao "secret/db:password" ))
```

## AWS

### Default Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `AWS_REGION` | | AWS region (required) |
| `AWS_PROFILE` | `default` | AWS credentials profile |
| `AWS_ACCESS_KEY_ID` | | Static access key |
| `AWS_SECRET_ACCESS_KEY` | | Static secret key |
| `AWS_SESSION_TOKEN` | | Session token (for temporary credentials) |
| `AWS_ROLE_ARN` | | Role ARN for assume role |
| `AWS_WEB_IDENTITY_TOKEN_FILE` | | Web identity token file |
| `AWS_ENDPOINT_URL` | | Custom endpoint (for testing) |
| `AWS_CA_BUNDLE` | | Path to CA bundle |
| `AWS_TIMEOUT` | `30s` | Request timeout |

### Named Targets

| Variable | Description |
|----------|-------------|
| `AWS_{TARGET}_REGION` | Target-specific region |
| `AWS_{TARGET}_PROFILE` | Target-specific profile |
| `AWS_{TARGET}_ACCESS_KEY_ID` | Target-specific access key |
| `AWS_{TARGET}_SECRET_ACCESS_KEY` | Target-specific secret key |

**Example:**

```bash
# Default AWS
export AWS_REGION=us-west-2
export AWS_PROFILE=default

# Staging target
export AWS_STAGING_REGION=us-east-1
export AWS_STAGING_PROFILE=staging

# Production target
export AWS_PROD_REGION=eu-west-1
export AWS_PROD_PROFILE=production
```

**Usage in YAML:**

```yaml
default_param: (( awsparam "/app/db_host" ))
staging_param: (( awsparam@staging "/app/db_host" ))
prod_secret: (( awssecret@prod "db-credentials" ))
```

### AWS Parameter Store Specific

| Variable | Default | Description |
|----------|---------|-------------|
| `AWS_SSM_ENDPOINT` | | Custom SSM endpoint |
| `AWS_SSM_TIMEOUT` | `30s` | SSM request timeout |

### AWS Secrets Manager Specific

| Variable | Default | Description |
|----------|---------|-------------|
| `AWS_SECRETSMANAGER_ENDPOINT` | | Custom Secrets Manager endpoint |
| `AWS_SECRETSMANAGER_TIMEOUT` | `30s` | Secrets Manager request timeout |

## NATS

### Default Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `NATS_URL` | `nats://localhost:4222` | NATS server URL |
| `NATS_TOKEN` | | Authentication token |
| `NATS_USER` | | Username |
| `NATS_PASSWORD` | | Password |
| `NATS_NKEY` | | NKey seed |
| `NATS_CREDS` | | Path to credentials file |
| `NATS_TLS_CERT` | | Path to TLS certificate |
| `NATS_TLS_KEY` | | Path to TLS private key |
| `NATS_TLS_CA` | | Path to CA certificate |
| `NATS_TIMEOUT` | `10s` | Connection timeout |
| `NATS_RECONNECT` | `true` | Enable automatic reconnection |

### Named Targets

| Variable | Description |
|----------|-------------|
| `NATS_{TARGET}_URL` | Target-specific URL |
| `NATS_{TARGET}_TOKEN` | Target-specific token |
| `NATS_{TARGET}_CREDS` | Target-specific credentials |

**Example:**

```bash
# Default NATS
export NATS_URL=nats://nats.example.com:4222
export NATS_TOKEN=default-token

# Production target
export NATS_PROD_URL=nats://nats-prod.example.com:4222
export NATS_PROD_CREDS=/path/to/prod.creds
```

**Usage in YAML:**

```yaml
config: (( nats "kv:config/settings" ))
prod_config: (( nats@prod "kv:config/settings" ))
```

## Pipeline Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `GRAFT_FILE_PARALLELISM` | `NumCPU` | Files processed in parallel |
| `GRAFT_EVAL_PARALLELISM` | `16` | Operators per eval wave |
| `GRAFT_EXTERNAL_PARALLELISM` | `32` | Max concurrent external calls |
| `GRAFT_SUBTREE_PARALLELISM` | `true` | Enable sub-tree parallel merge |
| `GRAFT_SUBTREE_THRESHOLD` | `100` | Min keys to parallelize |
| `GRAFT_BATCH_SIZE` | `20` | Requests per batch |
| `GRAFT_BATCH_TIMEOUT` | `100ms` | Max wait for batch |

**Example:**

```bash
# High-throughput configuration
export GRAFT_EXTERNAL_PARALLELISM=64
export GRAFT_BATCH_SIZE=50

# Sequential processing (debugging)
export GRAFT_FILE_PARALLELISM=1
export GRAFT_EVAL_PARALLELISM=1
export GRAFT_SUBTREE_PARALLELISM=false
```

## History and Tracing

| Variable | Default | Description |
|----------|---------|-------------|
| `GRAFT_HISTORY_ENABLED` | `false` | Enable history tracking by default |
| `GRAFT_HISTORY_MAX_ENTRIES` | `0` | Max entries per path (0 = unlimited) |
| `GRAFT_HISTORY_RETENTION` | `0` | History retention period (0 = forever) |

## Security

| Variable | Default | Description |
|----------|---------|-------------|
| `GRAFT_REDACT_SECRETS` | `true` | Redact secrets in error messages |
| `GRAFT_SECRET_PATTERNS` | `password,secret,key,token` | Patterns to redact |

## File Handling

| Variable | Default | Description |
|----------|---------|-------------|
| `GRAFT_MAX_FILE_SIZE` | `10MB` | Maximum input file size |
| `GRAFT_TEMP_DIR` | System temp | Temporary file directory |

## Example Configuration

### Development

```bash
# Development environment
export GRAFT_COLOR=on
export GRAFT_DEBUG=true

# Local Vault (dev mode)
export VAULT_ADDR=http://localhost:8200
export VAULT_TOKEN=root

# LocalStack for AWS
export AWS_REGION=us-east-1
export AWS_ENDPOINT_URL=http://localhost:4566
export AWS_ACCESS_KEY_ID=test
export AWS_SECRET_ACCESS_KEY=test

# Local NATS
export NATS_URL=nats://localhost:4222
```

### CI/CD Pipeline

```bash
# CI configuration
export GRAFT_COLOR=off
export GRAFT_CACHE_TTL=0

# Vault (from CI secrets)
export VAULT_ADDR=$CI_VAULT_ADDR
export VAULT_TOKEN=$CI_VAULT_TOKEN

# AWS (using CI role)
export AWS_REGION=$AWS_DEFAULT_REGION
export AWS_WEB_IDENTITY_TOKEN_FILE=/var/run/secrets/token
export AWS_ROLE_ARN=$CI_AWS_ROLE_ARN
```

### Production

```bash
# Production configuration
export GRAFT_REDACT_SECRETS=true
export GRAFT_CACHE_SIZE=1000
export GRAFT_CACHE_TTL=5m

# Vault with TLS
export VAULT_ADDR=https://vault.example.com
export VAULT_TOKEN=$VAULT_TOKEN
export VAULT_NAMESPACE=production
export VAULT_CACERT=/etc/ssl/vault-ca.pem

# AWS with profile
export AWS_REGION=us-west-2
export AWS_PROFILE=production

# NATS with TLS
export NATS_URL=nats://nats.example.com:4222
export NATS_CREDS=/etc/nats/credentials
export NATS_TLS_CA=/etc/ssl/nats-ca.pem
```

## Precedence

Configuration is resolved in this order (highest to lowest):

1. Command-line flags

2. Environment variables

3. Configuration file (`~/.graft/config.yml`)

4. Default values

## See Also

- [CLI Quick Reference](cli-quick-reference.md) - Command flags

- [Secrets Management](../user-guide/secrets/index.md) - Backend configuration

- [Configuration](../user-guide/configuration.md) - Config file options
