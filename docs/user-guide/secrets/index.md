# Secrets Management

Graft integrates with multiple secrets backends to securely fetch sensitive data during configuration generation.

## Supported Backends

| Backend | Operator | Description |
|---------|----------|-------------|
| [HashiCorp Vault](vault.md) | `vault` | Industry-standard secrets management |
| [OpenBao](vault.md) | `vault` | Open-source Vault fork |
| [AWS Parameter Store](aws-ssm.md) | `awsparam` | AWS Systems Manager Parameter Store |
| [AWS Secrets Manager](aws-secrets-manager.md) | `awssecret` | AWS managed secrets service |
| [NATS JetStream](nats.md) | `nats` | Distributed messaging KV store |

## Common Patterns

### Basic Secret Fetching

```yaml
database:
  password: (( vault "secret/db:password" ))
  api_key: (( awssecret "prod/api-key" ))
  config: (( awsparam "/app/config" ))
```

### With Default Values

```yaml
# Use default if secret unavailable (development)
password: (( vault "secret/db:password" || "dev-password" ))
```

### Dynamic Paths

```yaml
environment: production

# Build path dynamically
password: (( vault (concat "secret/" environment "/db:password") ))
```

### Multi-Target Support

```yaml
# Fetch from different backends/clusters
prod_secret: (( vault production@"secret/db:password" ))
staging_secret: (( vault staging@"secret/db:password" ))
```

## Configuration

Secrets backends are configured through environment variables:

### Vault

```sh
export VAULT_ADDR="https://vault.example.com"
export VAULT_TOKEN="s.xxxxx"
export VAULT_NAMESPACE="prod"
```

### AWS

```sh
export AWS_REGION="us-west-2"
export AWS_PROFILE="production"
```

### NATS

```sh
export NATS_URL="nats://localhost:4222"
export NATS_TOKEN="mytoken"
```

See individual backend pages for complete configuration options.

## Security Best Practices

### 1. Use Environment Variables

Never hardcode credentials in configuration files:

```yaml
# BAD - Don't do this
password: (( vault "secret/db:password" ))
vault_token: "s.hardcoded-token"  # NEVER!

# GOOD - Token from environment
password: (( vault "secret/db:password" ))
# VAULT_TOKEN is read from environment
```

### 2. Limit Access Scope

Use the minimum permissions needed:

```hcl
# Vault policy - read-only, specific paths
path "secret/data/myapp/*" {
  capabilities = ["read"]
}
```

### 3. Rotate Secrets

Use versioning and rotation:

```yaml
# Vault with specific version
password: (( vault "secret/db:password?version=2" ))

# AWS with stage
password: (( awssecret "db-creds?stage=AWSCURRENT" ))
```

### 4. Audit Secret Access

Use the `vaultinfo` command to audit:

```sh
graft vaultinfo config.yml
```

### 5. Separate Environments

Use different backends or targets per environment:

```yaml
# Production
password: (( vault production@"secret/db:password" ))

# Staging
password: (( vault staging@"secret/db:password" ))
```

## Error Handling

### Missing Secrets

```yaml
# Will error if secret doesn't exist
required: (( vault "secret/required:key" ))

# With fallback for optional secrets
optional: (( vault "secret/optional:key" || "default" ))
```

### Connection Failures

When a backend is unreachable, Graft provides clear error messages:

```
Error evaluating vault operator at config.yml:5
  database:
    password: (( vault "secret/db:password" ))
             ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
Error: failed to connect to Vault at https://vault.example.com
  - Check VAULT_ADDR is correct
  - Verify network connectivity
  - Ensure Vault is unsealed
```

## Performance

### Connection Pooling

Graft maintains connection pools to backends:

```go
// Configure pool size in library
graft.WithVault(graft.VaultConfig{
    PoolSize: 10,
})
```

### Request Batching

Multiple secrets from the same backend are batched:

```yaml
# These three requests are batched into one
db_user: (( vault "secret/db:username" ))
db_pass: (( vault "secret/db:password" ))
db_host: (( vault "secret/db:host" ))
```

### Caching

Results are cached during evaluation:

```yaml
# Same secret path referenced twice - fetched only once
primary: (( vault "secret/db:password" ))
backup: (( vault "secret/db:password" ))  # cached
```

## See Also

- [Vault Integration](vault.md) - Full Vault documentation
- [AWS Parameter Store](aws-ssm.md) - SSM Parameter Store
- [AWS Secrets Manager](aws-secrets-manager.md) - AWS Secrets Manager
- [NATS JetStream](nats.md) - NATS KV store
- [Configuration](../configuration.md) - Environment variables
