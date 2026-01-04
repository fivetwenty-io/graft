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
prod_host: (( awsparam production@"/app/db/host" ))
staging_host: (( awsparam staging@"/app/db/host" ))
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
| `AWS_ENDPOINT_URL` | Custom endpoint URL | - |

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
main_config: (( awsparam main-account@"/app/config" ))
shared_config: (( awsparam shared-account@"/shared/config" ))
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

### Batching

Multiple parameters are fetched efficiently:

```yaml
# These are batched into a single AWS API call
host: (( awsparam "/app/db/host" ))
port: (( awsparam "/app/db/port" ))
user: (( awsparam "/app/db/user" ))
```

### Caching

Parameters are cached during evaluation:

```yaml
# Same parameter, fetched only once
primary_host: (( awsparam "/app/db/host" ))
backup_host: (( awsparam "/app/db/host" ))  # cached
```

## See Also

- [AWS Secrets Manager](aws-secrets-manager.md) - For more complex secrets
- [Secrets Overview](index.md) - All backends
- [Configuration](../configuration.md) - Environment variables
