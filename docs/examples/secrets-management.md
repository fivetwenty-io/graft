# Secrets Management Examples

This guide demonstrates how to integrate Graft with external secrets backends including HashiCorp Vault, AWS Parameter Store, AWS Secrets Manager, and NATS JetStream.

## HashiCorp Vault Integration

### Basic Vault Usage

**Prerequisites:**

- Vault server running and accessible
- `VAULT_ADDR` and `VAULT_TOKEN` environment variables set

```sh
export VAULT_ADDR=https://vault.example.com
export VAULT_TOKEN=s.xxxxxxxxxxxxxxxxxxxxxxxx
```

**config.yml:**

```yaml
database:
  host: db.example.com
  port: 5432
  user: app_user
  password: (( vault "secret/database:password" ))

api:
  key: (( vault "secret/api:key" ))
  secret: (( vault "secret/api:secret" ))
```

```sh
graft merge config.yml
```

**Output:**

```yaml
api:
  key: ak_live_xxxxxxxxxxxxx
  secret: sk_live_xxxxxxxxxxxxx
database:
  host: db.example.com
  password: super-secret-password
  port: 5432
  user: app_user
```

### Vault with Multiple Keys

Retrieve multiple keys from a single secret path:

**config.yml:**

```yaml
# Single path, multiple keys
database:
  host: (( vault "secret/database:host" ))
  port: (( vault "secret/database:port" ))
  user: (( vault "secret/database:username" ))
  password: (( vault "secret/database:password" ))
```

### Dynamic Vault Paths

Construct Vault paths dynamically using other operators:

**config.yml:**

```yaml
meta:
  environment: production
  service: api

database:
  # Path: secret/production/api/database:password
  password: (( vault (concat "secret/" meta.environment "/" meta.service "/database:password") ))

cache:
  # Path: secret/production/api/redis:auth_token
  auth_token: (( vault (concat "secret/" meta.environment "/" meta.service "/redis:auth_token") ))
```

### Vault with Default Values

Provide fallback values for development or when Vault is unavailable:

**config.yml:**

```yaml
database:
  password: (( vault "secret/database:password" || "dev-password" ))

api:
  key: (( vault "secret/api:key" || "dev-api-key" ))
```

### Multi-Path Vault Fallback

Try multiple Vault paths in order:

**config.yml:**

```yaml
database:
  # Try v2 path first, fall back to v1 path
  password: (( vault "secret/v2/database:password; secret/v1/database:password" ))
```

### Multi-Target Vault

Access different Vault clusters using targets:

**Environment Setup:**

```sh
# Default Vault
export VAULT_ADDR=https://vault.example.com
export VAULT_TOKEN=s.default-token

# Production Vault
export VAULT_PROD_ADDR=https://vault-prod.example.com
export VAULT_PROD_TOKEN=s.prod-token

# Staging Vault
export VAULT_STAGING_ADDR=https://vault-staging.example.com
export VAULT_STAGING_TOKEN=s.staging-token
```

**config.yml:**

```yaml
# Use default Vault
dev_secret: (( vault "secret/dev:password" ))

# Use production Vault (target prefix: prod@)
prod_secret: (( vault@prod "secret/database:password" ))

# Use staging Vault (target prefix: staging@)
staging_secret: (( vault@staging "secret/database:password" ))
```

### OpenBao Support

OpenBao (Vault fork) uses the same operator with a different target:

```yaml
# Using OpenBao
secret: (( vault@openbao "secret/database:password" ))
```

**Environment:**

```sh
export VAULT_OPENBAO_ADDR=https://openbao.example.com
export VAULT_OPENBAO_TOKEN=s.openbao-token
```

### Vault Namespaces

For Vault Enterprise with namespaces:

```sh
export VAULT_NAMESPACE=admin/team-a
```

**config.yml:**

```yaml
# Accesses secret in admin/team-a namespace
team_secret: (( vault "secret/team:password" ))
```

## AWS Parameter Store

### Basic AWS Parameter Store Usage

**Prerequisites:**

- AWS credentials configured
- `AWS_REGION` set

```sh
export AWS_REGION=us-east-1
# Or use AWS_PROFILE, AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY
```

**config.yml:**

```yaml
database:
  host: (( awsparam "/myapp/production/database/host" ))
  port: (( awsparam "/myapp/production/database/port" ))
  password: (( awsparam "/myapp/production/database/password" ))
```

### JSON Value Extraction

Extract specific keys from JSON parameter values:

**Parameter value in AWS:**

```json
{
  "host": "db.example.com",
  "port": 5432,
  "username": "admin"
}
```

**config.yml:**

```yaml
database:
  host: (( awsparam "/myapp/database?key=host" ))
  port: (( awsparam "/myapp/database?key=port" ))
  user: (( awsparam "/myapp/database?key=username" ))
```

### Dynamic Parameter Paths

**config.yml:**

```yaml
meta:
  app: myapp
  env: production

database:
  host: (( awsparam (concat "/" meta.app "/" meta.env "/database/host") ))
  password: (( awsparam (concat "/" meta.app "/" meta.env "/database/password") ))
```

### Multi-Target AWS

Access parameters in different AWS accounts or regions:

**Environment Setup:**

```sh
# Default AWS
export AWS_REGION=us-east-1
export AWS_PROFILE=default

# Production account
export AWS_PROD_REGION=us-west-2
export AWS_PROD_PROFILE=production

# EU account
export AWS_EU_REGION=eu-west-1
export AWS_EU_PROFILE=eu-production
```

**config.yml:**

```yaml
# Default account
default_param: (( awsparam "/app/config/key" ))

# Production account (target prefix: prod@)
prod_param: (( awsparam@prod "/app/config/key" ))

# EU account (target prefix: eu@)
eu_param: (( awsparam@eu "/app/config/key" ))
```

### AWS Parameter Store with Defaults

**config.yml:**

```yaml
database:
  host: (( awsparam "/myapp/database/host" || "localhost" ))
  port: (( awsparam "/myapp/database/port" || 5432 ))
```

## AWS Secrets Manager

### Basic Secrets Manager Usage

**config.yml:**

```yaml
database:
  credentials: (( awssecret "myapp/production/database" ))
```

If the secret is a JSON object, you get the entire object:

```yaml
database:
  credentials:
    username: admin
    password: secret123
    host: db.example.com
```

### JSON Key Extraction

**Secret in AWS Secrets Manager:**

```json
{
  "username": "admin",
  "password": "super-secret",
  "host": "db.example.com",
  "port": 5432
}
```

**config.yml:**

```yaml
database:
  user: (( awssecret "myapp/database?key=username" ))
  password: (( awssecret "myapp/database?key=password" ))
  host: (( awssecret "myapp/database?key=host" ))
  port: (( awssecret "myapp/database?key=port" ))
```

### Version and Stage Selection

**config.yml:**

```yaml
# Get current version
current_password: (( awssecret "myapp/database?key=password&stage=AWSCURRENT" ))

# Get previous version (for rotation)
previous_password: (( awssecret "myapp/database?key=password&stage=AWSPREVIOUS" ))

# Get specific version
versioned_secret: (( awssecret "myapp/database?key=password&version=abc123" ))
```

### Multi-Target Secrets Manager

**Environment Setup:**

```sh
export AWS_REGION=us-east-1

export AWS_PROD_REGION=us-west-2
export AWS_PROD_PROFILE=production
```

**config.yml:**

```yaml
# Default account
dev_secret: (( awssecret "myapp/dev/database" ))

# Production account
prod_secret: (( awssecret@prod "myapp/prod/database?key=password" ))
```

## NATS JetStream

### NATS KV Store

**Prerequisites:**

- NATS server with JetStream enabled
- KV bucket created

```sh
export NATS_URL=nats://localhost:4222
# Or for authenticated access:
export NATS_TOKEN=your-token
```

**config.yml:**

```yaml
# Access KV bucket "config", key "database"
database:
  config: (( nats "kv:config/database" ))

# Multiple keys from same bucket
app:
  setting1: (( nats "kv:config/setting1" ))
  setting2: (( nats "kv:config/setting2" ))
```

### NATS Object Store

Store and retrieve larger objects like configuration files:

**config.yml:**

```yaml
# Retrieve YAML file from object store
templates:
  deployment: (( load (nats "obj:templates/deployment.yml") ))
  service: (( load (nats "obj:templates/service.yml") ))
```

### NATS with Explicit URL

**config.yml:**

```yaml
# Specify NATS server directly
config: (( nats "kv:config/settings" "nats://nats.example.com:4222" ))
```

### Multi-Target NATS

**Environment Setup:**

```sh
# Default NATS
export NATS_URL=nats://localhost:4222

# Production cluster
export NATS_PROD_URL=nats://nats-prod.example.com:4222
export NATS_PROD_TOKEN=prod-token

# EU cluster
export NATS_EU_URL=nats://nats-eu.example.com:4222
export NATS_EU_TOKEN=eu-token
```

**config.yml:**

```yaml
# Default NATS
local_config: (( nats "kv:config/local" ))

# Production NATS
prod_config: (( nats@prod "kv:config/production" ))

# EU NATS
eu_config: (( nats@eu "kv:config/europe" ))
```

## Combined Secrets Example

A real-world configuration using multiple secrets backends:

**config.yml:**

```yaml
meta:
  app: my-service
  environment: (( grab $ENVIRONMENT || "development" ))

# Database credentials from Vault
database:
  host: db.example.com
  port: 5432
  user: (( vault (concat "secret/" meta.environment "/database:username") || "dev_user" ))
  password: (( vault (concat "secret/" meta.environment "/database:password") || "dev_password" ))

# API keys from AWS Secrets Manager
external_services:
  stripe:
    api_key: (( awssecret (concat meta.app "/" meta.environment "/stripe?key=api_key") || "sk_test_xxx" ))
    webhook_secret: (( awssecret (concat meta.app "/" meta.environment "/stripe?key=webhook_secret") || "whsec_xxx" ))

  sendgrid:
    api_key: (( awssecret (concat meta.app "/" meta.environment "/sendgrid?key=api_key") || "SG.xxx" ))

# Feature flags from NATS KV
features:
  config: (( nats (concat "kv:features/" meta.environment) || "{}" ))

# TLS certificates from AWS Parameter Store
tls:
  certificate: (( awsparam (concat "/" meta.app "/" meta.environment "/tls/cert") || "" ))
  private_key: (( awsparam (concat "/" meta.app "/" meta.environment "/tls/key") || "" ))
```

## Listing Vault References

Use `graft vaultinfo` to find all Vault references in your configuration:

```sh
graft vaultinfo config.yml
```

**Output:**

```
Vault references found:

  secret/production/database:username
    - config.yml:8

  secret/production/database:password
    - config.yml:9

  secret/production/api:key
    - config.yml:14

Total: 3 references
```

## Secrets Batching

Graft automatically batches requests to secrets backends for performance. When you have multiple secrets from the same backend, they are fetched efficiently:

**config.yml:**

```yaml
# These 5 Vault calls are batched into fewer requests
credentials:
  db_user: (( vault "secret/db:username" ))
  db_pass: (( vault "secret/db:password" ))
  api_key: (( vault "secret/api:key" ))
  api_secret: (( vault "secret/api:secret" ))
  jwt_secret: (( vault "secret/jwt:secret" ))
```

## Error Handling

### Missing Secrets with Defaults

```yaml
# Won't fail if secret is missing
password: (( vault "secret/maybe-missing:password" || "default-password" ))
```

### Required Secrets

```yaml
# Will fail with clear error if secret is missing
password: (( vault "secret/required:password" ))
```

**Error Output:**

```
Error at config.yml:5:12
  password: (( vault "secret/required:password" ))
            ^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^^
Failed to retrieve secret: secret/required:password
Vault error: secret not found

Hint: Ensure the secret exists and you have read access.
```

### Conditional Secrets

```yaml
meta:
  use_vault: true

database:
  (( if meta.use_vault ))
  password: (( vault "secret/database:password" ))
  (( else ))
  password: local-dev-password
  (( fi ))
```

## Security Best Practices

### Never Commit Actual Secrets

Configuration files should contain only operator expressions, never actual secret values:

```yaml
# GOOD - operator expression
password: (( vault "secret/db:password" ))

# BAD - never commit actual secrets
password: "actual-secret-value"
```

### Use Environment-Specific Paths

```yaml
meta:
  env: (( grab $ENVIRONMENT ))

# Secrets are isolated by environment
database:
  password: (( vault (concat "secret/" meta.env "/database:password") ))
```

### Audit Secret Access

Enable history tracking to see which secrets were accessed:

```sh
graft merge --history config.yml
```

### Redact Secrets in Output

When debugging, secrets are automatically redacted in history output:

```
database.password:
  [0] config.yml:5    → (( vault "secret/db:password" ))
  [1] <evaluated>     → "***REDACTED***"
```

## See Also

- [Vault Documentation](../user-guide/secrets/vault.md) - Complete Vault guide
- [AWS SSM Documentation](../user-guide/secrets/aws-ssm.md) - Parameter Store guide
- [AWS Secrets Manager Documentation](../user-guide/secrets/aws-secrets-manager.md) - Secrets Manager guide
- [NATS Documentation](../user-guide/secrets/nats.md) - NATS JetStream guide
- [Environment Variables Reference](../reference/environment-variables.md) - All backend environment variables
