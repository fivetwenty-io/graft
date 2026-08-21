# Environment Variables

Complete reference of the environment variables graft reads, verified
against the code that reads them. Every variable below is named exactly
as graft (or, where noted, an underlying SDK) reads it — no aspirational
or planned variables are listed.

## Graft Core

| Variable | Default | Description |
|----------|---------|-------------|
| `DEBUG` | `false` | Enable debug logging. Any non-empty value except `false` or `0` enables it. |
| `TRACE` | `false` | Enable trace (verbose) logging. Same truthiness rule as `DEBUG`. |
| `NO_COLOR` | - | Disable colorized output. Any non-empty value disables color. |
| `TERM` | - | `TERM=dumb` also disables colorized output. |
| `REDACT` | - | Any non-empty value makes `vault`, `awsparam`/`awssecret`, and `nats` operators return the literal string `"REDACTED"` instead of making a backend call. Always wins over `merge --skip-vault`/`--skip-aws`/`--skip-nats` when both are set — those flags alone defer the expression instead of redacting it; see [CLI Reference: graft merge](cli.md#graft-merge). |
| `DEFAULT_ARRAY_MERGE_KEY` | `name` | The identifier key used to match array-of-maps entries across documents during a merge. |
| `GRAFT_FILE_BASE_PATH` | - | Base path prepended to relative paths passed to `(( file ))` and `(( load ))`. Checked before `SPRUCE_FILE_BASE_PATH`. |
| `SPRUCE_FILE_BASE_PATH` | - | Fallback for `GRAFT_FILE_BASE_PATH`, used when it is unset, so spruce-configured environments keep working unchanged. |
| `GRAFT_MAX_LOOP_ITERATIONS` | `1000` | Iteration cap for `(( while ))` loops. The `--max-loop-iterations` CLI flag overrides this variable when both are set. |

There is no `GRAFT_COLOR`, `GRAFT_DEBUG`, or `GRAFT_TRACE` variable. Color
is controlled by the `--color`/`--no-color` CLI flags together with
`NO_COLOR`/`TERM` above: an explicit `--color`/`--no-color` wins over
both variables, but with neither flag given, `NO_COLOR`/`TERM=dumb`
disable color even on a terminal (see
[docs/reference/cli.md](cli.md#color-flags) for the full precedence
order). Debug and trace logging are controlled by the `-D`/`--debug` and
`-T`/`--trace` flags or by `DEBUG`/`TRACE`.

### Loop Iteration Cap

A `(( while ))` loop that exceeds the cap fails the run with
`while loop exceeded maximum iterations (<n>)`.

```bash
GRAFT_MAX_LOOP_ITERATIONS=50 graft merge config.yml
```

### Debug and Trace Logging

```bash
# Enable debug output
export DEBUG=true
graft merge base.yml overlay.yml

# Enable verbose trace output
export TRACE=true
graft merge base.yml overlay.yml
```

### Array Merge Key

```bash
# Match array-of-maps entries by "id" instead of the default "name"
export DEFAULT_ARRAY_MERGE_KEY=id
graft merge base.yml overlay.yml
```

## Unified Configuration System (`GRAFT_*`)

Sixteen `GRAFT_*` variables (`internal/config/env.go`) and six
`GRAFT_FEATURE_*` variables (`internal/features/env.go`) feed graft's
unified `Config` and feature-flag systems. They are validated on every
invocation, but not every field is wired into what `graft merge`/`fan`/
`vaultinfo` actually do yet — see
[Configuration Reference](config.md#which-settings-actually-affect-a-merge)
for the field-by-field breakdown.

| Variable | Overrides | Default |
|---|---|---|
| `GRAFT_ENGINE_STRICT_MODE` | `engine.strict_mode` | `false` |
| `GRAFT_ENGINE_MAX_RECURSION` | `engine.max_recursion` | `100` |
| `GRAFT_ENGINE_TIMEOUT` | `engine.timeout` | `30s` |
| `GRAFT_CACHE_ENABLED` | `cache.enabled` | `true` |
| `GRAFT_CACHE_MAX_SIZE` | `cache.max_size` | `10000` |
| `GRAFT_CACHE_TTL` | `cache.ttl` | `5m` |
| `GRAFT_CACHE_L2_ENABLED` | `cache.l2_enabled` | `false` |
| `GRAFT_CACHE_L2_PATH` | `cache.l2_path` | `""` (OS user cache dir) |
| `GRAFT_PARALLEL_ENABLED` | `parallel.enabled` | `true` |
| `GRAFT_PARALLEL_MIN_WORKERS` | `parallel.min_workers` | `1` |
| `GRAFT_PARALLEL_MAX_WORKERS` | `parallel.max_workers` | `0` (auto-detect) |
| `GRAFT_METRICS_ENABLED` | `metrics.enabled` | `false` |
| `GRAFT_METRICS_FORMAT` | `metrics.format` | `prometheus` |
| `GRAFT_METRICS_ENDPOINT` | `metrics.endpoint` | `/metrics` |
| `GRAFT_LOGGING_LEVEL` | `logging.level` | `info` |
| `GRAFT_LOGGING_FORMAT` | `logging.format` | `text` |
| `GRAFT_FEATURE_PARALLEL` | `parallel_evaluation` feature flag | disabled |
| `GRAFT_FEATURE_CACHE` | `caching` feature flag | enabled |
| `GRAFT_FEATURE_METRICS` | `metrics` feature flag | disabled |
| `GRAFT_FEATURE_DEBUG` | `debug_logging` feature flag | disabled |
| `GRAFT_FEATURE_STRICT_TYPES` | `strict_type_checking` feature flag | disabled |
| `GRAFT_FEATURE_POOLS` | `memory_pools` feature flag | enabled |

Of these, only `GRAFT_PARALLEL_ENABLED`/`_MIN_WORKERS`/`_MAX_WORKERS` and
`GRAFT_FEATURE_CACHE` change observable CLI behavior today; the rest are
parsed and validated but not yet consulted by `merge`/`fan`/`vaultinfo`.
See [Configuration Reference](config.md) for the full explanation,
accepted values, and validation rules, and for the config-file precedence
this section participates in.

## HashiCorp Vault / OpenBao

### Default Target

| Variable | Default | Description |
|----------|---------|-------------|
| `VAULT_ADDR` | | Vault server address (required, unless the `~/.svtoken` or `~/.vault-token` fallback below applies) |
| `VAULT_TOKEN` | | Authentication token (required, unless a fallback applies) |
| `VAULT_NAMESPACE` | | Vault namespace (enterprise) |
| `VAULT_SKIP_VERIFY` | `false` | Skip TLS certificate verification |

If `VAULT_ADDR` or `VAULT_TOKEN` is unset, graft reads `~/.svtoken` (a
YAML file with `vault`, `token`, `namespace`, and `skip_verify` keys) as a
fallback for all four. If a token is still unset after that, graft reads
a bare token from `~/.vault-token`.

`VAULT_CACERT`, `VAULT_CAPATH`, `VAULT_CLIENT_CERT`, `VAULT_CLIENT_KEY`,
`VAULT_TLS_SERVER_NAME`, and `VAULT_RATE_LIMIT` are **not honored** by
graft, despite being variables the underlying `hashicorp/vault/api` SDK
recognizes: graft builds its own `http.Client` with its own TLS
configuration and passes it to `api.NewClient`, which only falls back to
the SDK's environment-derived HTTP client when the caller doesn't supply
one — and graft always does. Use `VAULT_ADDR` with an `https://` URL and
`VAULT_SKIP_VERIFY` for TLS control instead.

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

| Variable | Default | Description | Read by |
|----------|---------|-------------|---------|
| `AWS_REGION` | | AWS region | graft |
| `AWS_PROFILE` | | AWS credentials profile | graft |
| `AWS_ROLE` | | Role ARN to assume via STS `AssumeRole` | graft |
| `AWS_ACCESS_KEY_ID` | | Static access key | AWS SDK default credential chain |
| `AWS_SECRET_ACCESS_KEY` | | Static secret key | AWS SDK default credential chain |
| `AWS_SESSION_TOKEN` | | Session token (temporary credentials) | AWS SDK default credential chain |
| `AWS_WEB_IDENTITY_TOKEN_FILE` | | Web identity token file (with `AWS_ROLE_ARN`) | AWS SDK default credential chain |
| `AWS_ROLE_ARN` | | Role ARN for the SDK's web-identity (OIDC/IRSA) provider — distinct from graft's own `AWS_ROLE` above | AWS SDK default credential chain |
| `AWS_CA_BUNDLE` | | Path to a custom CA bundle | AWS SDK default credential chain |

The "Read by" column matters: `AWS_REGION`, `AWS_PROFILE`, and `AWS_ROLE`
are read directly by graft's own code
(`pkg/graft/operators/op_aws.go`). The rest are read by the
`aws-sdk-go` session's default credential chain, which graft enables via
`session.SharedConfigState: session.SharedConfigEnable` but never reads
itself — they work because the SDK honors them, not because graft has
explicit support for each one. There is no `AWS_ENDPOINT_URL` or
`AWS_TIMEOUT`; the real, per-target-only equivalents are
`AWS_{TARGET}_ENDPOINT` and `AWS_{TARGET}_HTTP_TIMEOUT` below.

### Named Targets

| Variable | Description |
|----------|-------------|
| `AWS_{TARGET}_REGION` | Target-specific region |
| `AWS_{TARGET}_PROFILE` | Target-specific profile |
| `AWS_{TARGET}_ROLE` | Target-specific role ARN to assume |
| `AWS_{TARGET}_ACCESS_KEY_ID` | Target-specific access key |
| `AWS_{TARGET}_SECRET_ACCESS_KEY` | Target-specific secret key |
| `AWS_{TARGET}_SESSION_TOKEN` | Target-specific session token |
| `AWS_{TARGET}_ENDPOINT` | Target-specific custom endpoint (for testing or non-AWS-compatible stores) |
| `AWS_{TARGET}_S3_FORCE_PATH_STYLE` | Force S3 path-style addressing (`true`/`false`) |
| `AWS_{TARGET}_DISABLE_SSL` | Disable SSL for the target (`true`/`false`) |
| `AWS_{TARGET}_MAX_RETRIES` | Maximum SDK retry attempts (default `3`) |
| `AWS_{TARGET}_HTTP_TIMEOUT` | HTTP request timeout (default `30s`) |
| `AWS_{TARGET}_CACHE_TTL` | Target cache time-to-live (default `5m`) |
| `AWS_{TARGET}_ASSUME_ROLE_DURATION` | Assumed-role session duration (default `1h`) |
| `AWS_{TARGET}_EXTERNAL_ID` | External ID for role assumption |
| `AWS_{TARGET}_SESSION_NAME` | Role session name (default `graft-{target}`) |
| `AWS_{TARGET}_MFA_SERIAL` | MFA device serial number |
| `AWS_{TARGET}_AUDIT_LOGGING` | Enable audit logging for the target (`true`/`false`) |

Unlike the default configuration, every one of these target-prefixed
variables is read directly by graft (`internal/backends/aws/client.go`),
not by the SDK's own environment resolution.

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

There are no AWS Parameter Store-specific or Secrets Manager-specific
environment variables (no `AWS_SSM_*` or `AWS_SECRETSMANAGER_*`); both
operators share the AWS configuration above.

## NATS

### Default Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `NATS_URL` | `nats://127.0.0.1:4222` (the `nats.go` client default) | NATS server URL |
| `NATS_TIMEOUT` | `5s` | Connection timeout |
| `NATS_RETRIES` | `3` | Maximum connection retry attempts |
| `NATS_RETRY_INTERVAL` | `1s` | Initial delay between retries |
| `NATS_RETRY_BACKOFF` | `2.0` | Multiplier applied to the retry interval after each attempt |
| `NATS_MAX_RETRY_INTERVAL` | `30s` | Ceiling for the backed-off retry interval |
| `NATS_TLS` | `false` | Enable TLS |
| `NATS_CERT_FILE` | | TLS client certificate path |
| `NATS_KEY_FILE` | | TLS client key path |
| `NATS_CA_FILE` | | TLS CA certificate path |
| `NATS_INSECURE_SKIP_VERIFY` | `false` | Skip TLS certificate verification |
| `NATS_TOKEN` | | Authentication token |
| `NATS_USER` | | Username |
| `NATS_PASSWORD` | | Password |
| `NATS_NKEY` | | Path to an nkey seed file |
| `NATS_CREDS` | | Path to a `.creds` file |
| `NATS_CACHE_TTL` | `5m` | KV/Object fetch result cache time-to-live |
| `NATS_STREAMING_THRESHOLD` | `10485760` (10MB) | Object size above which fetches stream instead of loading fully into memory |
| `NATS_AUDIT_LOGGING` | `false` | Log every KV/Object access at debug level |

When more than one auth variable is set, only one method is used, in this
order (highest precedence first): `NATS_CREDS`, `NATS_NKEY`, `NATS_TOKEN`,
then `NATS_USER`/`NATS_PASSWORD`. An invalid or unreadable nkey seed file,
or a TLS client certificate/key pair that fails to load, is a hard
configuration error — graft never silently falls back to an
unauthenticated or unencrypted connection.

There is no `NATS_RECONNECT` variable; reconnection is controlled by
`NATS_RETRIES`/`NATS_RETRY_INTERVAL`/`NATS_RETRY_BACKOFF`/
`NATS_MAX_RETRY_INTERVAL` above.

### Named Targets

Every variable in the table above has a `NATS_{TARGET}_*` form (for
example `NATS_{TARGET}_URL`, `NATS_{TARGET}_TOKEN`,
`NATS_{TARGET}_CERT_FILE`, `NATS_{TARGET}_NKEY`), read from
`internal/backends/nats/client.go`. Unlike the default configuration,
`NATS_{TARGET}_URL` is required — there is no fallback to the `nats.go`
client default for a named target.

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

## Parallel Evaluation

| Variable | Default | Description |
|----------|---------|-------------|
| `GRAFT_PARALLEL_ENABLED` | `true` | Enable parallel evaluation |
| `GRAFT_PARALLEL_MIN_WORKERS` | `1` | Minimum worker goroutines |
| `GRAFT_PARALLEL_MAX_WORKERS` | `0` (auto-detect from `NumCPU`) | Maximum worker goroutines |

These variables size the worker pool used for wave-level operator
concurrency only. File-level read/parse concurrency is separate: it spawns
one goroutine per input file, does not draw from the worker pool, and is
unconditional — neither variable above affects it. There are also no
request-batching variables: backend request deduplication (coalescing
concurrent identical Vault/AWS/NATS lookups) is unconditional and
unconfigurable. See
[Parallel Execution Model](../architecture/parallelism.md) for the full
model.

**Example:**

```bash
# Explicit worker ceiling.
export GRAFT_PARALLEL_MAX_WORKERS=16

# Sequential evaluation (debugging, or reproducing spruce's exact
# operator evaluation order).
export GRAFT_PARALLEL_ENABLED=false
```

## Example Configuration

### Development

```bash
# Development environment
export DEBUG=true

# Local Vault (dev mode)
export VAULT_ADDR=http://localhost:8200
export VAULT_TOKEN=root

# LocalStack for AWS
export AWS_REGION=us-east-1
export AWS_ACCESS_KEY_ID=test
export AWS_SECRET_ACCESS_KEY=test
# AWS_ENDPOINT_URL is not read by graft or the SDK's default chain; use a
# named target's AWS_{TARGET}_ENDPOINT instead if you need a custom
# endpoint for LocalStack.

# Local NATS
export NATS_URL=nats://localhost:4222
```

### CI/CD Pipeline

```bash
# CI configuration
export GRAFT_CACHE_TTL=0

# Vault (from CI secrets)
export VAULT_ADDR=$CI_VAULT_ADDR
export VAULT_TOKEN=$CI_VAULT_TOKEN

# AWS (using CI role via OIDC web identity)
export AWS_REGION=$AWS_DEFAULT_REGION
export AWS_WEB_IDENTITY_TOKEN_FILE=/var/run/secrets/token
export AWS_ROLE_ARN=$CI_AWS_ROLE_ARN
```

### Production

```bash
# Production configuration
export GRAFT_CACHE_MAX_SIZE=1000
export GRAFT_CACHE_TTL=5m

# Vault
export VAULT_ADDR=https://vault.example.com
export VAULT_TOKEN=$VAULT_TOKEN
export VAULT_NAMESPACE=production

# AWS with profile
export AWS_REGION=us-west-2
export AWS_PROFILE=production

# NATS with TLS and a credentials file
export NATS_URL=nats://nats.example.com:4222
export NATS_CREDS=/etc/nats/production.creds
export NATS_TLS=true
export NATS_CA_FILE=/etc/ssl/nats-ca.pem
```

## Precedence

Graft resolves each `GRAFT_*` configuration setting independently, in this
order, from highest to lowest priority:

1. Environment variable (`GRAFT_*`)

2. Configuration file (loaded via `--config` or discovered by search)

3. Built-in default

There is no setting-specific CLI flag tier above environment variables;
`--config <path>` only selects which file participates in the
configuration-file tier. Backend credentials (`VAULT_*`, `AWS_*`,
`NATS_*`) and the per-command variables at the top of this page
(`DEBUG`, `TRACE`, `REDACT`, and so on) are outside the `Config`
system and aren't affected by a configuration file at all — they're
read directly from the environment every time.

If `--config` is omitted, graft searches these locations in order and
loads the first file found:

1. `./graft.yaml` (current directory)

2. `$HOME/.graft/config.yaml` (user config directory)

3. `/etc/graft/config.yaml` (system config, Unix-like systems only)

## See Also

- [Configuration Reference](config.md) - The `GRAFT_*` config system, config file format, and which settings actually affect a merge

- [CLI Quick Reference](cli-quick-reference.md) - Command flags

- [Secrets Management](../user-guide/secrets/index.md) - Backend configuration

- [Configuration](../user-guide/configuration.md) - Config file options
