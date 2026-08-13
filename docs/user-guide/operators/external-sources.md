# External Sources Operators

Operators for loading data from files and external sources.

## file

Read file contents as a string.

**Syntax:**
```yaml
value: (( file "path/to/file" ))
```

**Examples:**

### Read Text File

```yaml
readme: (( file "README.md" ))
# Contents of README.md as a string
```

### Read Configuration File

```yaml
# Load a certificate
ssl:
  cert: (( file "/etc/ssl/certs/server.crt" ))
  key: (( file "/etc/ssl/private/server.key" ))
```

### Read with Base64 Encoding

```yaml
# Load binary file and encode
binary_data: (( base64 (file "image.png") ))
```

### Kubernetes Secrets

```yaml
apiVersion: v1
kind: Secret
type: Opaque
data:
  tls.crt: (( base64 (file "certs/tls.crt") ))
  tls.key: (( base64 (file "certs/tls.key") ))
```

### Dynamic Path

```yaml
env: production

config_content: (( file (concat "configs/" env ".conf") ))
```

## load

Load and parse a YAML or JSON file, making its contents available as structured data.

**Syntax:**
```yaml
value: (( load "path/to/file.yml" ))
```

**Examples:**

### Load YAML File

```yaml
# external.yml
database:
  host: localhost
  port: 5432

# main.yml
external_config: (( load "external.yml" ))
db_host: (( grab external_config.database.host ))
```

### Load JSON File

```yaml
# config.json
# {"api": {"endpoint": "https://api.example.com", "timeout": 30}}

api_config: (( load "config.json" ))
api_url: (( grab api_config.api.endpoint ))
```

### Load and Merge

```yaml
# Load external config and grab specific values
external: (( load "shared-config.yml" ))

database:
  host: (( grab external.database.host ))
  port: (( grab external.database.port ))
  credentials: (( grab external.database.credentials ))
```

### Dynamic Loading

```yaml
environment: production

# Load environment-specific config
env_config: (( load (concat "envs/" environment ".yml") ))

settings:
  replicas: (( grab env_config.replicas ))
  resources: (( grab env_config.resources ))
```

### Load Multiple Files

```yaml
# Load several configuration files
base_config: (( load "configs/base.yml" ))
app_config: (( load "configs/app.yml" ))
secrets_config: (( load "configs/secrets.yml" ))

# Combine them
combined:
  base: (( grab base_config ))
  app: (( grab app_config ))
  secrets: (( grab secrets_config ))
```

## raw_env

Reads an environment variable as a raw, uncoerced string.

```yaml
# With PORT=8080 in the environment:
port: (( raw_env $PORT ))     # the string "8080"
grabbed: (( grab $PORT ))     # the integer 8080
```

Ordinary `$VAR` substitution parses the variable's value as YAML, so
`8080` becomes a number and `true` becomes a boolean. `raw_env` skips
that coercion and always returns the literal string. A set-but-empty
variable is a valid empty string; an unset variable is an error:

```
environment variable $PORT is not set
```

`raw_env` takes exactly one argument, and it must be an environment
variable reference. In a fallback chain, each `raw_env` side stays raw
while non-`raw_env` alternatives coerce normally:

```yaml
# Raw string from $PRIMARY, else raw string from $SECONDARY
host: (( raw_env $PRIMARY || raw_env $SECONDARY ))

# Raw string from $PORT, else the integer 8080
port: (( raw_env $PORT || 8080 ))
```

## Differences: file vs load

| Aspect | file | load |
|--------|------|------|
| Output | Raw string | Parsed data structure |
| Use case | Text content, certificates | Configuration files |
| Formats | Any text file | YAML, JSON |
| Access | Entire file as string | Navigate structure with grab |

### When to Use file

- Loading certificates or keys
- Including text content (README, templates)
- Binary files (with base64 encoding)
- Non-YAML/JSON configuration files

### When to Use load

- Loading structured configuration
- Sharing configuration between files
- Building modular configurations
- Loading JSON API responses

## Path Resolution

Paths can be:

- **Relative:** Resolved relative to the current file
- **Absolute:** Starting with `/`

```yaml
# Relative to current file
local: (( file "config.txt" ))

# Absolute path
system: (( file "/etc/myapp/config.txt" ))

# Dynamic path
env: dev
config: (( load (concat "./envs/" env ".yml") ))
```

## Error Handling

### File Not Found

```yaml
# This will error if file doesn't exist
required: (( file "must-exist.txt" ))

# Use with default for optional files
optional: (( file "might-not-exist.txt" || "" ))
```

### Parse Errors

```yaml
# load will error on invalid YAML/JSON
config: (( load "broken.yml" ))
# Error: failed to parse broken.yml: yaml: line 5: ...
```

## Practical Examples

### Modular Configuration

**Directory structure:**
```
config/
├── main.yml
├── database.yml
├── cache.yml
└── envs/
    ├── dev.yml
    └── prod.yml
```

**main.yml:**
```yaml
environment: (( grab env || "dev" ))

# Load components
database: (( load "database.yml" ))
cache: (( load "cache.yml" ))
env_overrides: (( load (concat "envs/" environment ".yml") ))

# Apply environment overrides
settings:
  db_host: (( grab env_overrides.database.host || grab database.host ))
  cache_url: (( grab env_overrides.cache.url || grab cache.url ))
```

### Kubernetes ConfigMap with Files

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: app-config
data:
  config.yml: (( file "app-config.yml" ))
  logging.yml: (( file "logging-config.yml" ))
  nginx.conf: (( file "nginx/default.conf" ))
```

### Certificate Bundle

```yaml
tls:
  certificate: (( file "certs/server.crt" ))
  private_key: (( file "certs/server.key" ))
  ca_bundle: (( file "certs/ca-bundle.crt" ))
```

### Shared Defaults

**defaults.yml:**
```yaml
timeouts:
  connect: 5
  read: 30
  write: 30

retry:
  attempts: 3
  backoff: exponential
```

**service.yml:**
```yaml
defaults: (( load "defaults.yml" ))

http_client:
  connect_timeout: (( grab defaults.timeouts.connect ))
  read_timeout: (( grab defaults.timeouts.read ))
  retry_attempts: (( grab defaults.retry.attempts ))
```

### Template Files

```yaml
# Load template and use as string
email_template: (( file "templates/welcome.html" ))

# Load data file
users: (( load "data/users.json" ))
```

## Security Considerations

- File paths are not sandboxed by default
- Be cautious with dynamic paths from untrusted input
- Consider using absolute paths in production
- Validate file contents when appropriate

## See Also

- [Data Manipulation](data-manipulation.md) - grab, concat
- [Secrets](../secrets/) - For sensitive data from secure stores
- [Operators Overview](index.md) - All operators
