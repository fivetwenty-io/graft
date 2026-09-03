# AWS Parameter Store (SSM)

Fetch parameters from AWS Systems Manager Parameter Store.

## Basic Usage

```yaml
database:
  host: (( awsparam "/app/production/db/host" ))
```

## Syntax Variations

### Simple Path

```yaml
value: (( awsparam "/app/config/key" ))
```

### With Default Value

```yaml
value: (( awsparam "/app/config/key" || "default" ))
```

### JSON Key Extraction

Extract a specific key from a JSON-formatted parameter:

```yaml
# Parameter value: {"host": "localhost", "port": 5432}
db_host: (( awsparam "/app/config?key=host" ))     # "localhost"
db_port: (( awsparam "/app/config?key=port" ))     # 5432
```

### Nested JSON Access

```yaml
# Parameter value: {"database": {"primary": {"host": "db1.example.com"}}}
host: (( awsparam "/app/config?key=database.primary.host" ))
```

### With Target

```yaml
# Use specific AWS profile/region
prod_host: (( awsparam@production "/app/db/host" ))
staging_host: (( awsparam@staging "/app/db/host" ))
```

### Dynamic Path

```yaml
environment: production

host: (( awsparam (concat "/app/" environment "/db/host") ))
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

# Staging target
export AWS_STAGING_REGION="us-west-2"
export AWS_STAGING_PROFILE="staging"
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

## Parameter Types

### String Parameters

```yaml
# Simple string parameter
hostname: (( awsparam "/app/hostname" ))
```

### SecureString Parameters

SecureStrings are automatically decrypted:

```yaml
# Encrypted with KMS
password: (( awsparam "/app/db/password" ))
```

### StringList Parameters

Comma-separated lists are returned as arrays:

```yaml
# Parameter value: "server1,server2,server3"
servers: (( awsparam "/app/servers" ))
# Result: ["server1", "server2", "server3"]
```

## Practical Examples

### Database Configuration

```yaml
database:
  host: (( awsparam "/app/prod/db/host" ))
  port: (( awsparam "/app/prod/db/port" || "5432" ))
  username: (( awsparam "/app/prod/db/username" ))
  password: (( awsparam "/app/prod/db/password" ))
```

### Application Configuration

```yaml
app:
  environment: production
  log_level: (( awsparam "/app/prod/log_level" || "info" ))
  features:
    new_ui: (( awsparam "/app/prod/features/new_ui" ))
```

### JSON Configuration Object

Store complex config as JSON parameter:

```yaml
# Parameter /app/config contains:
# {
#   "database": {"host": "db.example.com", "port": 5432},
#   "cache": {"host": "cache.example.com", "ttl": 300}
# }

config: (( awsparam "/app/config" ))

# Or extract specific values:
database:
  host: (( awsparam "/app/config?key=database.host" ))
  port: (( awsparam "/app/config?key=database.port" ))
cache:
  host: (( awsparam "/app/config?key=cache.host" ))
  ttl: (( awsparam "/app/config?key=cache.ttl" ))
```

### Multi-Environment Setup

```yaml
environment: (( grab env || "development" ))

config:
  db_host: (( awsparam (concat "/app/" environment "/db/host") ))
  api_key: (( awsparam (concat "/app/" environment "/api/key") ))
```

### Cross-Account Access

```yaml
# Access parameters in different AWS accounts
main_config: (( awsparam@main-account "/app/config" ))
shared_config: (( awsparam@shared-account "/shared/config" ))
```

### Role Assumption and MFA

Set `AWS_{TARGET}_ROLE` (for the default target, `AWS_DEFAULT_ROLE`) to have graft call `sts:AssumeRole` before reading parameters for that target. The plain `AWS_ROLE` spelling is read only when none of `AWS_DEFAULT_REGION`, `AWS_DEFAULT_PROFILE`, `AWS_DEFAULT_ROLE`, or `AWS_DEFAULT_ACCESS_KEY_ID` is set; once any of them is, the default target is configured from the `AWS_DEFAULT_*` family alone and `AWS_ROLE` is ignored:

```bash
export AWS_MAIN_ACCOUNT_ROLE="arn:aws:iam::123456789012:role/ParameterReader"
```

When the role itself requires MFA, also set `AWS_{TARGET}_MFA_SERIAL` (for the default target, either `AWS_DEFAULT_MFA_SERIAL` or the plain `AWS_MFA_SERIAL`):

```bash
export AWS_MAIN_ACCOUNT_MFA_SERIAL="arn:aws:iam::123456789012:mfa/alice"
```

graft resolves the current MFA code in this order: `AWS_{TARGET}_MFA_TOKEN` (or `AWS_MFA_TOKEN`) if set, an interactive prompt on stderr if stdin is a terminal, or a clear error naming the variable to set. Set the token variable for non-interactive runs (CI, scripts, and anything piping a document into `graft merge`), since none of those have a terminal to prompt on:

```bash
export AWS_MAIN_ACCOUNT_MFA_TOKEN="123456"
```

A token from the environment is used exactly once, since a one-time code cannot be replayed. If a profile's own `mfa_serial` in `~/.aws/config` and the target's `AWS_{TARGET}_ROLE` both need a code in the same run, the second `AssumeRole` has no token left; run graft interactively so it can prompt for each.

## IAM Permissions

Required IAM permissions:

```json
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Effect": "Allow",
            "Action": [
                "ssm:GetParameter",
                "ssm:GetParameters",
                "ssm:GetParametersByPath"
            ],
            "Resource": "arn:aws:ssm:*:*:parameter/app/*"
        },
        {
            "Effect": "Allow",
            "Action": "kms:Decrypt",
            "Resource": "arn:aws:kms:*:*:key/your-kms-key-id"
        }
    ]
}
```

## Path Hierarchy

Parameter Store uses path hierarchy:

```
/app/
├── production/
│   ├── db/
│   │   ├── host
│   │   ├── port
│   │   └── password
│   └── api/
│       └── key
└── staging/
    ├── db/
    │   ├── host
    │   └── password
    └── api/
        └── key
```

Access with full paths:

```yaml
db_host: (( awsparam "/app/production/db/host" ))
```

## Error Handling

### Parameter Not Found

```
Error: parameter not found: /app/config/missing
  - Verify the parameter exists in Parameter Store
  - Check the path is correct
```

### Permission Denied

```
Error: access denied for parameter: /app/secret
  - Verify IAM permissions
  - Check the parameter's resource policy
```

### Using Defaults

```yaml
# Handle missing parameters gracefully
optional: (( awsparam "/app/optional" || "default-value" ))
```

## Performance

### No Cross-Parameter Batching

Each distinct parameter path is its own `GetParameter` API call - graft does not group different parameter names into AWS's batch `GetParameters` call (which supports up to ten names per request):

```yaml
# Three distinct paths - three separate AWS API calls, even under
# parallel evaluation (where they run concurrently rather than batched).
host: (( awsparam "/app/db/host" ))
port: (( awsparam "/app/db/port" ))
user: (( awsparam "/app/db/user" ))
```

If a single parameter stores a JSON blob and you extract multiple keys from it with `?key=`, those calls *do* collapse to one fetch - see Caching below.

### Caching

Parameters are cached per target and path during evaluation. Referencing the same parameter path twice - including through different `?key=` subkey extractions of one JSON-valued parameter - fetches it only once, and concurrent references to the identical (target, path) under parallel evaluation are coalesced into a single request rather than each firing its own:

```yaml
# Same parameter, fetched only once
primary_host: (( awsparam "/app/db/host" ))
backup_host: (( awsparam "/app/db/host" ))  # cached

# Same parameter path, different subkeys of its JSON value - still one fetch
cache_host: (( awsparam "/app/config?key=cache.host" ))
cache_ttl: (( awsparam "/app/config?key=cache.ttl" ))
```

A single call can opt out of this cache — neither reading from it nor
writing to it — with the `:nocache`
[expression modifier](../../reference/expression-modifiers.md):
`(( awsparam:nocache "/app/db/host" ))`.

## See Also

- [AWS Secrets Manager](aws-secrets-manager.md) - For more complex secrets
- [Secrets Overview](index.md) - All backends
- [Configuration](../configuration.md) - Environment variables
