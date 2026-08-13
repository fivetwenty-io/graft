# NATS JetStream

Fetch configuration from NATS JetStream KV and Object stores.

## Basic Usage

```yaml
config:
  settings: (( nats "kv:config/settings" ))
```

## Store Types

### KV Store

Key-Value store for configuration data:

```yaml
# Syntax: kv:bucket/key
config: (( nats "kv:config/app-settings" ))
```

### Object Store

Object store for larger files:

```yaml
# Syntax: obj:bucket/object
template: (( nats "obj:templates/email.html" ))
```

## Syntax Variations

### Simple Access

```yaml
# KV store
setting: (( nats "kv:config/key" ))

# Object store
file: (( nats "obj:assets/template.yml" ))
```

### With Default Value

```yaml
setting: (( nats "kv:config/key" || "default" ))
```

### With Target

```yaml
# Use specific NATS cluster
prod_config: (( nats@production "kv:config/settings" ))
staging_config: (( nats@staging "kv:config/settings" ))
```

### With Explicit URL

```yaml
config: (( nats "kv:config/settings" "nats://server:4222" ))
```

### Dynamic Path

```yaml
environment: production

config: (( nats (concat "kv:config/" environment "/settings") ))
```

### With Inline Configuration Map

The second argument accepts a map instead of a URL string, letting you set
connection, TLS, and auth options directly in the YAML document rather than
through environment variables:

```yaml
config: (( nats "kv:config/settings" {
  url: "nats://server:4222",
  timeout: "10s",
  token: "my-token",
} ))
```

## Configuration

### Inline Configuration Map Keys

| Key | Type | Overrides |
|-----|------|-----------|
| `url` | string | `NATS_URL` |
| `timeout` | duration string (e.g. `"10s"`) | `NATS_TIMEOUT` |
| `retries` | int | `NATS_RETRIES` |
| `retry_interval` | duration string | `NATS_RETRY_INTERVAL` |
| `retry_backoff` | float | `NATS_RETRY_BACKOFF` |
| `max_retry_interval` | duration string | `NATS_MAX_RETRY_INTERVAL` |
| `tls` | bool | `NATS_TLS` |
| `cert_file` | string | `NATS_CERT_FILE` |
| `key_file` | string | `NATS_KEY_FILE` |
| `ca_file` | string | `NATS_CA_FILE` |
| `insecure_skip_verify` | bool | `NATS_INSECURE_SKIP_VERIFY` |
| `cache_ttl` | duration string | `NATS_CACHE_TTL` |
| `streaming_threshold` | int | `NATS_STREAMING_THRESHOLD` |
| `audit_logging` | bool | `NATS_AUDIT_LOGGING` |
| `token` | string | `NATS_TOKEN` |
| `user` | string | `NATS_USER` |
| `password` | string | `NATS_PASSWORD` |
| `nkey_seed_file` | string | `NATS_NKEY` |
| `creds_file` | string | `NATS_CREDS` |

Every key is optional; unset keys keep the value the corresponding
environment variable (or its default) already resolved to. The auth
precedence rule (`creds_file` > `nkey_seed_file` > `token` >
`user`/`password`) applies to the resulting config the same way it applies
to environment variables. This map only applies to the default (no-target)
connection - named targets are configured entirely through
`NATS_{TARGET}_*` environment variables, not through this map.

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `NATS_URL` | NATS server URL | `nats://127.0.0.1:4222` (the `nats.go` client default) |
| `NATS_TOKEN` | Authentication token | - |
| `NATS_USER` | Username | - |
| `NATS_PASSWORD` | Password | - |
| `NATS_NKEY` | Path to an nkey seed file | - |
| `NATS_CREDS` | Path to a `.creds` file | - |
| `NATS_CERT_FILE` | TLS client certificate path | - |
| `NATS_KEY_FILE` | TLS client key path | - |
| `NATS_CA_FILE` | TLS CA certificate path | - |
| `NATS_TIMEOUT` | Connection timeout | `5s` |

When more than one auth variable is set, only one method is used: `NATS_CREDS`
wins over `NATS_NKEY`, which wins over `NATS_TOKEN`, which wins over
`NATS_USER`/`NATS_PASSWORD`. See the
[Environment Variables Reference](../../reference/environment-variables.md)
for the complete list, including target-prefixed forms and retry/TLS
tuning variables.

### Per-Target Variables

```sh
# Default target
export NATS_URL="nats://localhost:4222"
export NATS_TOKEN="default-token"

# Production target
export NATS_PRODUCTION_URL="nats://nats-prod.example.com:4222"
export NATS_PRODUCTION_TOKEN="prod-token"

# Staging target
export NATS_STAGING_URL="nats://nats-staging.example.com:4222"
export NATS_STAGING_TOKEN="staging-token"
```

### Library Configuration

There is no `graft.NATSConfig` type or `WithNATS`/`WithNATSTarget` engine
option. As a library, graft configures NATS the same way the CLI does: by
reading the environment variables above (`NATS_URL`, `NATS_TOKEN`, the
`NATS_{TARGET}_*` prefixed forms, and so on) at evaluation time. The only
NATS-related engine option is `graft.WithSkipNats(true)`, which makes every
`(( nats ... ))` operator return the literal string `"REDACTED"` instead of
making a backend call:

```go
engine, _ := graft.NewEngine(
    graft.WithSkipNats(true),
)
```

## KV Store Details

### Bucket and Key Structure

```
bucket/
├── key1
├── key2
├── path/
│   ├── subkey1
│   └── subkey2
```

Access with full path:

```yaml
value: (( nats "kv:bucket/path/subkey1" ))
```

### Value Types

KV values are returned as strings. Parse as needed:

```yaml
# String value
name: (( nats "kv:config/app-name" ))

# JSON value - parse it
config_json: (( nats "kv:config/settings" ))
# Use load operator to parse if needed

# Number (as string)
port_str: (( nats "kv:config/port" ))
port: (( calc port_str + 0 ))  # Convert to number
```

## Object Store Details

### Storing Files

Objects can contain any data - config files, templates, certificates:

```yaml
# YAML config file
config: (( load (nats "obj:configs/app.yml") ))

# Template file
template: (( nats "obj:templates/email.html" ))

# Certificate
cert: (( nats "obj:certs/server.crt" ))
```

### Larger Files

Object store is suitable for larger files (>1MB):

```yaml
# Large configuration bundle
bundle: (( nats "obj:bundles/full-config.tar.gz" ))
```

## Practical Examples

### Application Configuration

```yaml
app:
  name: (( nats "kv:config/app-name" || "my-app" ))
  version: (( nats "kv:config/app-version" || "1.0.0" ))
  settings: (( nats "kv:config/settings" ))
```

### Feature Flags

```yaml
features:
  new_ui: (( nats "kv:features/new-ui" || "false" ))
  beta_api: (( nats "kv:features/beta-api" || "false" ))
  dark_mode: (( nats "kv:features/dark-mode" || "true" ))
```

### Shared Templates

```yaml
# Load email templates from object store
email:
  welcome_template: (( nats "obj:templates/welcome.html" ))
  reset_password: (( nats "obj:templates/reset-password.html" ))
```

### Multi-Environment

```yaml
environment: production

config:
  db_host: (( nats (concat "kv:" environment "/db/host") ))
  api_key: (( nats (concat "kv:" environment "/api/key") ))
```

### Multi-Cluster Deployment

```yaml
# Fetch from different NATS clusters
clusters:
  us-east:
    config: (( nats@us-east "kv:config/settings" ))
  eu-west:
    config: (( nats@eu-west "kv:config/settings" ))
```

## Authentication

### Token Authentication

```sh
export NATS_URL="nats://localhost:4222"
export NATS_TOKEN="your-token"
```

### User/Password Authentication

```sh
export NATS_URL="nats://localhost:4222"
export NATS_USER="myuser"
export NATS_PASSWORD="mypassword"
```

### Nkey Authentication

```sh
export NATS_URL="nats://localhost:4222"
export NATS_NKEY="/path/to/user.nk"
```

### Credentials File

```sh
export NATS_URL="nats://localhost:4222"
export NATS_CREDS="/path/to/user.creds"
```

### TLS Client Certificates

```sh
export NATS_URL="nats://localhost:4222"
export NATS_TLS="true"
export NATS_CERT_FILE="/path/to/client-cert.pem"
export NATS_KEY_FILE="/path/to/client-key.pem"
export NATS_CA_FILE="/path/to/ca.pem"
```

## Error Handling

### Bucket/Key Not Found

```
Error: key not found: kv:config/missing
  - Verify the bucket and key exist
  - Check the path is correct
```

### Connection Failed

```
Error: failed to connect to NATS at nats://localhost:4222
  - Check NATS_URL is correct
  - Verify NATS server is running
  - Check network connectivity
```

### Authentication Failed

```
Error: authentication failed for NATS
  - Verify NATS_TOKEN or credentials
  - Check user permissions
```

### Using Defaults

```yaml
# Handle missing keys gracefully
optional: (( nats "kv:config/optional" || "default" ))
```

## Performance

### Connection Pooling

Connections are pooled and reused automatically, one connection per target
(the default, no-target connection has its own pool keyed by URL). Pool
size isn't configurable: idle connections are closed automatically after
5 minutes, checked every minute. There is no environment variable or
engine option to change either interval.

### No Cross-Key Batching

Each distinct KV or Object path is its own JetStream request - graft does not aggregate different keys into one call:

```yaml
# Three distinct keys - three separate JetStream requests, even under
# parallel evaluation (where they run concurrently rather than batched).
key1: (( nats "kv:config/key1" ))
key2: (( nats "kv:config/key2" ))
key3: (( nats "kv:config/key3" ))
```

### Caching

Values are cached per target and path during evaluation, and concurrent references to the identical (target, path) under parallel evaluation are coalesced into a single request rather than each firing its own:

```yaml
# Same key, fetched only once
primary: (( nats "kv:config/setting" ))
backup: (( nats "kv:config/setting" ))  # cached
```

A single call can opt out of this cache — neither reading from it nor
writing to it — with the `:nocache`
[expression modifier](../../reference/expression-modifiers.md):
`(( nats:nocache "kv:config/setting" ))`.

## NATS vs Other Backends

| Feature | NATS | Vault | AWS |
|---------|------|-------|-----|
| Latency | Very low | Low | Medium |
| Consistency | Eventual | Strong | Strong |
| Complexity | Simple | Complex | Medium |
| Self-hosted | Yes | Yes | No |

**Use NATS for:**

- Low-latency configuration
- Real-time config updates
- Self-hosted infrastructure
- Simple key-value storage

## See Also

- [Secrets Overview](index.md) - All backends
- [Vault Integration](vault.md) - Alternative for secrets
- [Configuration](../configuration.md) - Environment variables
