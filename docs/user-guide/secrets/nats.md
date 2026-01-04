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
prod_config: (( nats production@"kv:config/settings" ))
staging_config: (( nats staging@"kv:config/settings" ))
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

## Configuration

### Environment Variables

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

```go
engine, _ := graft.NewEngine(
    graft.WithNATS(graft.NATSConfig{
        URL:     "nats://localhost:4222",
        Token:   os.Getenv("NATS_TOKEN"),
        TLSCert: "/path/to/cert.pem",
        TLSKey:  "/path/to/key.pem",
    }),
    graft.WithNATSTarget("production", graft.NATSConfig{
        URL:   "nats://nats-prod.example.com:4222",
        Token: os.Getenv("NATS_PRODUCTION_TOKEN"),
    }),
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
    config: (( nats us-east@"kv:config/settings" ))
  eu-west:
    config: (( nats eu-west@"kv:config/settings" ))
```

### Dynamic Configuration Updates

NATS KV supports watching for changes (in the library):

```go
// Watch for configuration changes
engine.WatchKV("config/settings", func(value string) {
    // Handle configuration update
})
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

### Credentials File

```sh
export NATS_URL="nats://localhost:4222"
export NATS_CREDS="/path/to/user.creds"
```

### TLS Client Certificates

```sh
export NATS_URL="nats://localhost:4222"
export NATS_TLS_CERT="/path/to/client-cert.pem"
export NATS_TLS_KEY="/path/to/client-key.pem"
export NATS_TLS_CA="/path/to/ca.pem"
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

Connections are pooled and reused:

```go
graft.WithNATS(graft.NATSConfig{
    PoolSize: 10,  // Connection pool size
})
```

### Batching

Multiple KV requests are batched:

```yaml
# Efficiently batched
key1: (( nats "kv:config/key1" ))
key2: (( nats "kv:config/key2" ))
key3: (( nats "kv:config/key3" ))
```

### Caching

Values are cached during evaluation:

```yaml
# Same key, fetched only once
primary: (( nats "kv:config/setting" ))
backup: (( nats "kv:config/setting" ))  # cached
```

## NATS vs Other Backends

| Feature | NATS | Vault | AWS |
|---------|------|-------|-----|
| Latency | Very low | Low | Medium |
| Consistency | Eventual | Strong | Strong |
| Complexity | Simple | Complex | Medium |
| Self-hosted | Yes | Yes | No |
| Watch/Subscribe | Yes | No | Limited |

**Use NATS for:**

- Low-latency configuration
- Real-time config updates
- Self-hosted infrastructure
- Simple key-value storage

## See Also

- [Secrets Overview](index.md) - All backends
- [Vault Integration](vault.md) - Alternative for secrets
- [Configuration](../configuration.md) - Environment variables
