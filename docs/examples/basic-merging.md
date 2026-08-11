# Basic Merging Examples

This guide provides step-by-step examples of merging YAML files with Graft, from simple overlays to complex scenarios.

## Simple Key Override

The most basic merge operation overwrites values from earlier files with values from later files.

### Setup Files

**base.yml:**

```yaml
application:
  name: my-app
  version: 1.0.0
  debug: false

database:
  host: localhost
  port: 5432
  name: myapp_db
```

**overlay.yml:**

```yaml
application:
  version: 2.0.0
  debug: true

database:
  host: db.production.example.com
```

### Run Merge

```sh
graft merge base.yml overlay.yml
```

### Output

```yaml
application:
  debug: true
  name: my-app
  version: 2.0.0
database:
  host: db.production.example.com
  name: myapp_db
  port: 5432
```

### What Happened

- `application.version` was overwritten from `1.0.0` to `2.0.0`
- `application.debug` was overwritten from `false` to `true`
- `application.name` was preserved (not in overlay)
- `database.host` was overwritten
- `database.port` and `database.name` were preserved

## Deep Merge Behavior

Graft performs deep merging of nested maps, preserving unspecified keys at any level.

### Setup Files

**base.yml:**

```yaml
server:
  http:
    host: 0.0.0.0
    port: 8080
    timeout: 30
  tls:
    enabled: false
    cert_file: ""
    key_file: ""
  logging:
    level: info
    format: json
    output: stdout
```

**production.yml:**

```yaml
server:
  http:
    port: 443
    timeout: 60
  tls:
    enabled: true
    cert_file: /etc/ssl/server.crt
    key_file: /etc/ssl/server.key
```

### Run Merge

```sh
graft merge base.yml production.yml
```

### Output

```yaml
server:
  http:
    host: 0.0.0.0
    port: 443
    timeout: 60
  logging:
    format: json
    level: info
    output: stdout
  tls:
    cert_file: /etc/ssl/server.crt
    enabled: true
    key_file: /etc/ssl/server.key
```

### What Happened

- `server.http.host` preserved from base (not in overlay)
- `server.http.port` overwritten to `443`
- `server.http.timeout` overwritten to `60`
- `server.tls` keys merged (enabled, cert_file, key_file updated)
- `server.logging` entirely preserved (not in overlay)

## Array Merging with Operators

Arrays require explicit operators to control merge behavior.

### Default Array Behavior

An array whose entries are plain scalars, carrying no array-merge marker, is
replaced outright by the overlay's array. Entries past the end of the overlay
are not preserved.

**base.yml:**

```yaml
features:
  - authentication
  - logging
  - caching
```

**overlay.yml:**

```yaml
features:
  - auth-v2
```

```sh
graft merge base.yml overlay.yml
```

**Output:**

```yaml
features:
- auth-v2
```

Arrays whose entries are maps take a different default: they merge by the
identifier key (`name` unless `DEFAULT_ARRAY_MERGE_KEY` says otherwise), so
entries the overlay does not mention survive.

**base.yml:**

```yaml
users:
  - name: alice
    role: admin
  - name: bob
    role: user
```

**overlay.yml:**

```yaml
users:
  - name: alice
    role: owner
```

```sh
graft merge base.yml overlay.yml
```

**Output:**

```yaml
users:
- name: alice
  role: owner
- name: bob
  role: user
```

To merge scalar arrays by position instead of replacing, say so with the
`(( inline ))` marker:

**overlay.yml:**

```yaml
features:
  - (( inline ))
  - auth-v2
```

```sh
graft merge base.yml overlay.yml
```

**Output:**

```yaml
features:
- auth-v2
- logging
- caching
```

### Append Operator

Add new items to the end of an array.

**base.yml:**

```yaml
packages:
  - nginx
  - postgresql
```

**overlay.yml:**

```yaml
packages:
  - (( append ))
  - redis
  - memcached
```

```sh
graft merge base.yml overlay.yml
```

**Output:**

```yaml
packages:
- nginx
- postgresql
- redis
- memcached
```

### Prepend Operator

Add new items to the beginning of an array.

**base.yml:**

```yaml
middleware:
  - auth
  - logging
```

**overlay.yml:**

```yaml
middleware:
  - (( prepend ))
  - rate-limiter
  - cors
```

```sh
graft merge base.yml overlay.yml
```

**Output:**

```yaml
middleware:
- rate-limiter
- cors
- auth
- logging
```

### Replace Operator

Replace the entire array.

**base.yml:**

```yaml
allowed_hosts:
  - localhost
  - 127.0.0.1
```

**overlay.yml:**

```yaml
allowed_hosts:
  - (( replace ))
  - api.example.com
  - www.example.com
```

```sh
graft merge base.yml overlay.yml
```

**Output:**

```yaml
allowed_hosts:
- api.example.com
- www.example.com
```

### Merge by Key

Merge arrays of objects by a key field.

**base.yml:**

```yaml
users:
  - name: alice
    role: admin
    active: true
  - name: bob
    role: user
    active: true
```

**overlay.yml:**

```yaml
users:
  - (( merge on name ))
  - name: alice
    active: false
  - name: charlie
    role: viewer
    active: true
```

```sh
graft merge base.yml overlay.yml
```

**Output:**

```yaml
users:
- active: false
  name: alice
  role: admin
- active: true
  name: bob
  role: user
- active: true
  name: charlie
  role: viewer
```

### What Happened

- Alice's `active` field was updated, `role` preserved
- Bob was preserved unchanged
- Charlie was added

## Using Grab to Reference Values

The `grab` operator references values from elsewhere in the document.

### Basic Grab

**config.yml:**

```yaml
defaults:
  timeout: 30
  retries: 3

database:
  timeout: (( grab defaults.timeout ))
  retries: (( grab defaults.retries ))

cache:
  timeout: (( grab defaults.timeout ))
  retries: (( grab defaults.retries ))
```

```sh
graft merge config.yml
```

**Output:**

```yaml
cache:
  retries: 3
  timeout: 30
database:
  retries: 3
  timeout: 30
defaults:
  retries: 3
  timeout: 30
```

### Grab with Arithmetic

**config.yml:**

```yaml
base:
  timeout: 30
  memory: 512

services:
  api:
    timeout: (( grab base.timeout ))
    memory: (( grab base.memory ))
  worker:
    timeout: (( grab base.timeout * 2 ))
    memory: (( grab base.memory * 4 ))
```

```sh
graft merge config.yml
```

**Output:**

```yaml
base:
  memory: 512
  timeout: 30
services:
  api:
    memory: 512
    timeout: 30
  worker:
    memory: 2048
    timeout: 60
```

### Grab from Arrays

**config.yml:**

```yaml
regions:
  - name: us-east
    endpoint: api.us-east.example.com
  - name: eu-west
    endpoint: api.eu-west.example.com

primary:
  region: (( grab regions[0].name ))
  endpoint: (( grab regions[0].endpoint ))

secondary:
  region: (( grab regions[name=eu-west].name ))
  endpoint: (( grab regions[name=eu-west].endpoint ))
```

```sh
graft merge config.yml
```

**Output:**

```yaml
primary:
  endpoint: api.us-east.example.com
  region: us-east
regions:
- endpoint: api.us-east.example.com
  name: us-east
- endpoint: api.eu-west.example.com
  name: eu-west
secondary:
  endpoint: api.eu-west.example.com
  region: eu-west
```

## Computed Values with Concat

Build strings from multiple parts.

### Basic Concatenation

**config.yml:**

```yaml
database:
  host: db.example.com
  port: 5432
  name: myapp
  user: admin
  url: (( concat "postgres://" database.user "@" database.host ":" database.port "/" database.name ))
```

```sh
graft merge config.yml
```

**Output:**

```yaml
database:
  host: db.example.com
  name: myapp
  port: 5432
  url: postgres://admin@db.example.com:5432/myapp
  user: admin
```

### Long Concat Expressions

A `(( ... ))` expression has to fit on one YAML line. Splitting one across
several lines is a YAML parse error, not a graft error:

```
config.yml: parse_error: failed to parse YAML: [8:3] non-map value is specified
   5 |   full_name: (( concat
   6 |     meta.app "-"
   7 |     meta.env
>  8 |   ))
         ^
```

Keep the expression on one line; when it gets unwieldy, compute the pieces into
their own keys and concatenate those.

**config.yml:**

```yaml
meta:
  app: my-service
  env: production
  version: 2.5.0

labels:
  full_name: (( concat meta.app "-" meta.env "-" meta.version ))
```

```sh
graft merge config.yml
```

**Output:**

```yaml
labels:
  full_name: my-service-production-2.5.0
meta:
  app: my-service
  env: production
  version: 2.5.0
```

## Default Values with ||

Provide fallback values when a reference might not exist.

**config.yml:**

```yaml
environment: production

settings:
  log_level: (( grab custom.log_level || "info" ))
  timeout: (( grab custom.timeout || 30 ))
  debug: (( grab debug_mode || false ))
```

```sh
graft merge config.yml
```

**Output:**

```yaml
environment: production
settings:
  debug: false
  log_level: info
  timeout: 30
```

Since `custom.log_level`, `custom.timeout`, and `debug_mode` do not exist, the defaults are used.

### With Overlay Providing Values

**overlay.yml:**

```yaml
custom:
  log_level: debug
  timeout: 60
```

```sh
graft merge config.yml overlay.yml
```

**Output:**

```yaml
custom:
  log_level: debug
  timeout: 60
environment: production
settings:
  debug: false
  log_level: debug
  timeout: 60
```

## Multi-File Merging

Merge multiple files in sequence.

### Setup Files

**base.yml:**

```yaml
app:
  name: my-service
  version: 1.0.0

database:
  host: localhost
  port: 5432

server:
  port: 8080
```

**env.yml:**

```yaml
database:
  host: db.staging.example.com

server:
  port: 443
```

**secrets.yml:**

```yaml
database:
  password: (( grab $DATABASE_PASSWORD || "default-password" ))

api:
  key: (( grab $API_KEY || "dev-key" ))
```

**overrides.yml:**

```yaml
app:
  debug: true
```

### Run Merge

```sh
graft merge base.yml env.yml secrets.yml overrides.yml
```

### Output

```yaml
api:
  key: dev-key
app:
  debug: true
  name: my-service
  version: 1.0.0
database:
  host: db.staging.example.com
  password: default-password
  port: 5432
server:
  port: 443
```

### See the History

```sh
graft merge --history base.yml env.yml secrets.yml overrides.yml
```

This shows where each value originated.

## Pruning Keys

Remove keys from the final output.

**config.yml:**

```yaml
meta:
  internal_id: abc123
  author: devteam

database:
  host: localhost
  port: 5432

_templates:
  base_url: http://localhost
```

```sh
graft merge config.yml --prune meta --prune _templates
```

**Output:**

```yaml
database:
  host: localhost
  port: 5432
```

### Using Prune Operator

**config.yml:**

```yaml
meta:
  internal: (( prune ))
  public: visible

database:
  host: localhost
  debug_connection: (( prune ))
```

```sh
graft merge config.yml
```

**Output:**

```yaml
database:
  host: localhost
meta:
  public: visible
```

## Cherry-Picking Keys

Output only specific keys.

**config.yml:**

```yaml
database:
  host: localhost
  port: 5432

server:
  host: 0.0.0.0
  port: 8080

cache:
  host: localhost
  port: 6379

logging:
  level: info
```

```sh
graft merge config.yml --cherry-pick database --cherry-pick server
```

**Output:**

```yaml
database:
  host: localhost
  port: 5432
server:
  host: 0.0.0.0
  port: 8080
```

## Injecting Map Contents

Inject all keys from a map into the parent level.

**config.yml:**

```yaml
defaults:
  timeout: 30
  retries: 3
  buffer_size: 1024

database:
  <<<: (( inject defaults ))
  host: localhost
  port: 5432

cache:
  <<<: (( inject defaults ))
  host: localhost
  port: 6379
```

```sh
graft merge config.yml
```

**Output:**

```yaml
cache:
  buffer_size: 1024
  host: localhost
  port: 6379
  retries: 3
  timeout: 30
database:
  buffer_size: 1024
  host: localhost
  port: 5432
  retries: 3
  timeout: 30
defaults:
  buffer_size: 1024
  retries: 3
  timeout: 30
```

## See Also

- [Conditional Configurations](conditional-configs.md) - Using if/else, loops
- [Multi-Environment Setups](multi-environment.md) - Dev/staging/prod management
- [Array Merging Guide](../user-guide/array-merging.md) - Complete array documentation
- [Operator Reference](../reference/operator-quick-reference.md) - All operators
