# AWS Secrets Manager

Fetch secrets from AWS Secrets Manager.

## Basic Usage

```yaml
database:
  credentials: (( awssecret "production/db-credentials" ))
```

## Syntax Variations

### Simple Secret

```yaml
api_key: (( awssecret "production/api-key" ))
```

### With Default Value

```yaml
api_key: (( awssecret "production/api-key" || "default-key" ))
```

### JSON Key Extraction

Extract a specific key from a JSON secret:

```yaml
# Secret value: {"username": "admin", "password": "secret123"}
db_user: (( awssecret "production/db?key=username" ))
db_pass: (( awssecret "production/db?key=password" ))
```

### Nested JSON Access

```yaml
# Secret: {"database": {"primary": {"host": "db1.example.com"}}}
host: (( awssecret "production/config?key=database.primary.host" ))
```

### With Version/Stage

```yaml
# Specific version
password: (( awssecret "production/db?version=abc123" ))

# Specific stage
current_pass: (( awssecret "production/db?stage=AWSCURRENT" ))
previous_pass: (( awssecret "production/db?stage=AWSPREVIOUS" ))
```

### With Target

```yaml
prod_secret: (( awssecret@production "db-credentials" ))
staging_secret: (( awssecret@staging "db-credentials" ))
```

### Dynamic Path

```yaml
environment: production

secret: (( awssecret (concat environment "/db-credentials") ))
```

## Configuration

### Environment Variables

| Variable | Description | Default |
|----------|-------------|---------|
| `AWS_REGION` | AWS region | - |
| `AWS_PROFILE` | AWS credentials profile | `default` |
| `AWS_ACCESS_KEY_ID` | Access key ID | - |
| `AWS_SECRET_ACCESS_KEY` | Secret access key | - |
| `AWS_SESSION_TOKEN` | Session token (STS) | - |

### Per-Target Variables

```sh
# Default target
export AWS_REGION="us-west-2"
export AWS_PROFILE="default"

# Production target
export AWS_PRODUCTION_REGION="us-east-1"
export AWS_PRODUCTION_PROFILE="production"
```

### Library Configuration

```go
engine, _ := graft.NewEngine(
    graft.WithAWS(graft.AWSConfig{
        Region:  "us-west-2",
        Profile: "default",
    }),
    graft.WithAWSTarget("production", graft.AWSConfig{
        Region:  "us-east-1",
        Profile: "production",
    }),
)
```

## Secret Types

### Key/Value Secrets

Most common - JSON key/value pairs:

```yaml
# Secret in AWS: {"username": "admin", "password": "secret123"}

# Get entire secret as object
credentials: (( awssecret "db-credentials" ))

# Extract specific keys
username: (( awssecret "db-credentials?key=username" ))
password: (( awssecret "db-credentials?key=password" ))
```

### Plaintext Secrets

Simple string values:

```yaml
# Secret in AWS: "sk_live_abc123..."
stripe_key: (( awssecret "stripe-api-key" ))
```

### Binary Secrets

Binary secrets are returned as base64-encoded strings:

```yaml
# Binary secret
certificate: (( awssecret "tls-certificate" ))
# Result: base64-encoded string

# Decode if needed
cert_decoded: (( base64-decode (awssecret "tls-certificate") ))
```

## Practical Examples

### Database Credentials

```yaml
database:
  host: db.example.com
  port: 5432
  username: (( awssecret "prod/db?key=username" ))
  password: (( awssecret "prod/db?key=password" ))
  connection_string: (( concat "postgres://" (awssecret "prod/db?key=username") ":" (awssecret "prod/db?key=password") "@" (grab database.host) ":" (grab database.port) ))
```

### API Keys

```yaml
apis:
  stripe:
    secret_key: (( awssecret "stripe-credentials?key=secret_key" ))
    publishable_key: (( awssecret "stripe-credentials?key=publishable_key" ))
  twilio:
    account_sid: (( awssecret "twilio-credentials?key=account_sid" ))
    auth_token: (( awssecret "twilio-credentials?key=auth_token" ))
```

### Kubernetes Secrets

```yaml
apiVersion: v1
kind: Secret
type: Opaque
data:
  username: (( base64 (awssecret "db-creds?key=username") ))
  password: (( base64 (awssecret "db-creds?key=password") ))
```

### Multi-Environment Configuration

```yaml
environment: (( grab env || "development" ))

secrets:
  db_password: (( awssecret (concat environment "/db?key=password") ))
  api_key: (( awssecret (concat environment "/api?key=key") ))
```

### Secret Rotation

Secrets Manager supports automatic rotation. Access different versions:

```yaml
# Current active version
current_password: (( awssecret "db-creds?stage=AWSCURRENT&key=password" ))

# Previous version (for rotation grace period)
previous_password: (( awssecret "db-creds?stage=AWSPREVIOUS&key=password" ))
```

### Role Assumption and MFA

Set `AWS_{TARGET}_ROLE` (or, for the default target, `AWS_ROLE`) to have graft call `sts:AssumeRole` before reading secrets for that target:

```bash
export AWS_PROD_ROLE="arn:aws:iam::123456789012:role/SecretsReader"
```

When the role itself requires MFA, also set `AWS_{TARGET}_MFA_SERIAL` (or `AWS_MFA_SERIAL` for the default target):

```bash
export AWS_PROD_MFA_SERIAL="arn:aws:iam::123456789012:mfa/alice"
```

graft resolves the current MFA code in this order: `AWS_{TARGET}_MFA_TOKEN` (or `AWS_MFA_TOKEN`) if set, an interactive prompt on stderr if stdin is a terminal, or a clear error naming the variable to set. Set the token variable non-interactively — CI, scripts, and anything piping a document into `graft merge` — since none of those have a terminal to prompt on:

```bash
export AWS_PROD_MFA_TOKEN="123456"
```

## IAM Permissions

Required IAM permissions:

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "secretsmanager:GetSecretValue",
                "secretsmanager:DescribeSecret"
            ],
            "Resource": "arn:aws:secretsmanager:*:*:secret:production/*"
        },
        {
            "Effect": "Allow",
            "Action": "kms:Decrypt",
            "Resource": "arn:aws:kms:*:*:key/your-kms-key-id"
        }
    ]
}
```

## Query Parameters

| Parameter | Description | Example |
|-----------|-------------|---------|
| `key` | Extract JSON key | `?key=password` |
| `version` | Specific version ID | `?version=abc123` |
| `stage` | Version stage | `?stage=AWSCURRENT` |

Combine multiple:

```yaml
value: (( awssecret "secret?key=password&stage=AWSCURRENT" ))
```

## Error Handling

### Secret Not Found

```
Error: secret not found: production/missing
  - Verify the secret exists in Secrets Manager
  - Check the secret name is correct
```

### Permission Denied

```
Error: access denied for secret: production/db
  - Verify IAM permissions
  - Check the secret's resource policy
```

### Key Not Found in JSON

```
Error: key "missing" not found in secret production/db
  - Verify the key exists in the JSON
  - Check for typos in the key name
```

### Using Defaults

```yaml
# Handle missing secrets gracefully
optional: (( awssecret "optional-secret" || "default-value" ))
```

## Secrets Manager vs Parameter Store

| Feature | Secrets Manager | Parameter Store |
|---------|-----------------|-----------------|
| Rotation | Built-in | Manual |
| Pricing | Per-secret | Free tier available |
| Size limit | 64KB | 4KB/8KB |
| Versioning | Automatic | Manual |
| Binary | Supported | Not supported |
| JSON parsing | Built-in | Manual |

**Use Secrets Manager for:**

- Secrets requiring rotation
- Larger secrets (>4KB)
- Binary data
- When you need versioning

**Use Parameter Store for:**

- Simple configuration values
- Cost optimization
- Hierarchical parameter organization

## Performance

### Same-Secret Requests Collapse to One Fetch

Graft does not batch requests for *different* secrets into fewer AWS API calls - each distinct secret name is still its own `GetSecretValue` call. But graft caches by secret name, not by secret-plus-subkey, so extracting several `?key=` subkeys of one JSON-valued secret only fetches that secret once:

```yaml
# db-creds is one JSON secret with username/password/host keys - one
# GetSecretValue call for db-creds serves all three lines below.
username: (( awssecret "db-creds?key=username" ))
password: (( awssecret "db-creds?key=password" ))
host: (( awssecret "db-creds?key=host" ))
```

If these referenced three different secret names instead of three subkeys of `db-creds`, each would be its own AWS API call.

### Caching

Secrets are cached per target and secret name during evaluation, and concurrent references to the identical (target, secret) under parallel evaluation are coalesced into a single request rather than each firing its own:

```yaml
# Same secret, fetched only once
primary: (( awssecret "api-key" ))
backup: (( awssecret "api-key" ))  # cached
```

A single call can opt out of this cache — neither reading from it nor
writing to it — with the `:nocache`
[expression modifier](../../reference/expression-modifiers.md):
`(( awssecret:nocache "api-key" ))`.

## See Also

- [AWS Parameter Store](aws-ssm.md) - Simpler parameter storage
- [Secrets Overview](index.md) - All backends
- [Configuration](../configuration.md) - Environment variables
