# Vault / OpenBao Integration

Integrate with HashiCorp Vault or OpenBao for secrets management.

## Basic Usage

```yaml
database:
  password: (( vault "secret/db:password" ))
```

The path format is: `path:key`

- `path` - Vault secret path
- `key` - Key within the secret data

## Syntax Variations

### Simple Path

```yaml
password: (( vault "secret/db:password" ))
```

### With Default Value

```yaml
password: (( vault "secret/db:password" || "default-value" ))
```

### Multiple Paths (Fallback)

```yaml
# Try paths in order, use first successful
password: (( vault "secret/v2/db:password; secret/v1/db:password" ))
```

### Dynamic Path

```yaml
env: production

password: (( vault (concat "secret/" env "/db:password") ))
```

### Path Segment From Another Vault Lookup

A vault path segment can come from a prior `(( vault ... ))` lookup
elsewhere in the tree. Graft's dependency ordering resolves `meta.path`
first, then composes it into the second lookup's path:

```yaml
meta:
  path: (( vault "secret/paths:root" ))

value: (( vault "secret/paths:" meta.path ))
```

If `secret/paths` holds `{root: "child", child: "s3kr1t"}`, `meta.path`
resolves to `child` and `value` resolves to `s3kr1t`. This works
regardless of which field is declared first in the document.

### With Target

```yaml
# Use specific Vault target
prod_pass: (( vault@prod "secret/db:password" ))
staging_pass: (( vault@staging "secret/db:password" ))
```

### Bypassing the Cache

Secrets are cached per target and path for the duration of a merge, so
repeated references produce one Vault request. To make a single call
skip that cache — never reading from it and never writing to it — add
the `:nocache` [expression modifier](../../reference/expression-modifiers.md)
to the operator name, before any target:

```yaml
otp: (( vault:nocache "secret/totp:code" ))
prod_otp: (( vault:nocache@prod "secret/totp:code" ))
```

## Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `VAULT_ADDR` | Vault server URL | - |
| `VAULT_TOKEN` | Authentication token | - |
| `VAULT_NAMESPACE` | Vault namespace | - |
| `VAULT_SKIP_VERIFY` | Skip TLS verification | `false` |
| `VAULT_CACERT` | CA certificate path | - |
| `VAULT_CLIENT_CERT` | Client certificate path | - |
| `VAULT_CLIENT_KEY` | Client key path | - |

### Per-Target Variables

For multi-target setups, prefix with target name:

```sh
# Default target
export VAULT_ADDR="https://vault.example.com"
export VAULT_TOKEN="s.default-token"

# Production target
export VAULT_PROD_ADDR="https://vault-prod.example.com"
export VAULT_PROD_TOKEN="s.prod-token"

# Staging target
export VAULT_STAGING_ADDR="https://vault-staging.example.com"
export VAULT_STAGING_TOKEN="s.staging-token"
```

### Library Configuration

```go
engine, _ := graft.NewEngine(
    graft.WithVault(graft.VaultConfig{
        Address:    "https://vault.example.com",
        Token:      os.Getenv("VAULT_TOKEN"),
        Namespace:  "prod",
        SkipVerify: false,
    }),
    graft.WithVaultTarget("staging", graft.VaultConfig{
        Address: "https://vault-staging.example.com",
        Token:   os.Getenv("VAULT_STAGING_TOKEN"),
    }),
)
```

## KV Secrets Engines

### KV Version 2 (Default)

For KV v2, Graft automatically handles the `data/` path component:

```yaml
# This works with both KV v1 and v2
password: (( vault "secret/db:password" ))

# For KV v2, Graft translates to: secret/data/db
```

### KV Version 1

Works the same way - Graft detects the version automatically.

### Explicit Path

If you need to specify the exact path:

```yaml
# Explicit v2 data path
password: (( vault "secret/data/db:password" ))
```

## Authentication Methods

### Token Authentication

```sh
export VAULT_TOKEN="s.xxxxxxxxxxxxxxxx"
```

### AppRole Authentication

Configure through environment:

```sh
export VAULT_ROLE_ID="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
export VAULT_SECRET_ID="xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx"
```

### Kubernetes Authentication

When running in Kubernetes:

```sh
export VAULT_AUTH_METHOD="kubernetes"
export VAULT_ROLE="my-app"
```

## Practical Examples

### Database Credentials

```yaml
database:
  host: db.example.com
  port: 5432
  username: (( vault "secret/db:username" ))
  password: (( vault "secret/db:password" ))
  connection_string: (( concat "postgres://" (vault "secret/db:username") ":" (vault "secret/db:password") "@" (grab database.host) ":" (grab database.port) ))
```

### API Keys

```yaml
apis:
  stripe:
    secret_key: (( vault "secret/apis/stripe:secret_key" ))
    publishable_key: (( vault "secret/apis/stripe:publishable_key" ))
  sendgrid:
    api_key: (( vault "secret/apis/sendgrid:api_key" ))
```

### TLS Certificates

```yaml
tls:
  certificate: (( vault "secret/tls/server:certificate" ))
  private_key: (( vault "secret/tls/server:private_key" ))
  ca_bundle: (( vault "secret/tls/ca:bundle" ))
```

### Per-Environment Secrets

```yaml
environment: (( grab env || "development" ))

secrets:
  db_password: (( vault (concat "secret/" environment "/db:password") ))
  api_key: (( vault (concat "secret/" environment "/api:key") ))
```

### Multi-Cluster Deployment

```yaml
clusters:
  us-east:
    db_password: (( vault@us-east "secret/db:password" ))
  eu-west:
    db_password: (( vault@eu-west "secret/db:password" ))
```

## OpenBao Support

OpenBao is an open-source fork of Vault. Graft supports OpenBao using the same `vault` operator:

```sh
# OpenBao uses the same environment variables
export VAULT_ADDR="https://openbao.example.com"
export VAULT_TOKEN="s.openbao-token"
```

```yaml
password: (( vault "secret/db:password" ))
```

Or explicitly specify OpenBao target:

```yaml
password: (( vault@openbao "secret/db:password" ))
```

## Listing Vault References

Use `vaultinfo` to see all Vault references without evaluating them:

```sh
graft vaultinfo config.yml
```

**Output:**
```yaml
secrets:
- key: secret/api:key
  references:
  - api.key
- key: secret/db:password
  references:
  - database.password
- key: secret/db:username
  references:
  - database.username
```

Add `--json` for the same data as JSON, or `--paths-only` for just the
sorted list of secret keys (`docs/user-guide/cli/vaultinfo.md` has the
full flag reference).

`vaultinfo` runs offline and never contacts Vault by default, so it
cannot resolve a path segment built from another `(( vault ... ))`
lookup (see "Path Segment From Another Vault Lookup" above) to its real
value. Instead of silently reporting the corrupted, literal path
`secret/paths:REDACTED`, it renders a symbolic reference to the lookup
it came from:

```yaml
secrets:
- key: secret/paths:<secret/paths:root>
  references:
  - value
- key: secret/paths:root
  references:
  - meta.path
```

`<secret/paths:root>` means "whatever `secret/paths:root` resolves to
at merge time" — it is not a literal path to look up in Vault. Pass
`--resolve` to have `vaultinfo` perform real Vault lookups instead of
skipping them (requires a reachable Vault), which reports the concrete
composed path instead:

```yaml
secrets:
- key: secret/paths:child
  references:
  - value
- key: secret/paths:root
  references:
  - meta.path
```

The merge-time evaluation itself (`graft merge`, not `vaultinfo`) has
always resolved this correctly and is unaffected either way; only
`vaultinfo`'s offline reporting was affected.

## Error Handling

### Permission Denied

```
Error: permission denied for path "secret/data/db"
  - Verify your Vault token has read access
  - Check the Vault policy for this path
```

### Path Not Found

```
Error: secret not found at "secret/data/db"
  - Verify the path exists in Vault
  - Check for typos in the path
```

### Connection Failed

```
Error: failed to connect to Vault at https://vault.example.com
  - Check VAULT_ADDR is correct
  - Verify network connectivity
  - Ensure Vault is unsealed
```

### Using Defaults for Resilience

```yaml
# Fallback for development or when Vault is unavailable
password: (( vault "secret/db:password" || "dev-password" ))
```

## Best Practices

1. **Use namespaces** to organize secrets by environment
2. **Rotate tokens** regularly
3. **Use AppRole** for applications instead of static tokens
4. **Audit access** with `graft vaultinfo`
5. **Use defaults** judiciously - only for development

## See Also

- [Secrets Overview](index.md) - All secrets backends
- [AWS Parameter Store](aws-ssm.md) - Alternative backend
- [vaultinfo Command](../cli/vaultinfo.md) - Audit Vault references
