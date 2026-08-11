# Quick Start

Get up and running with Graft in 5 minutes.

## Prerequisites

- Graft installed ([Installation Guide](installation.md))
- A terminal/command prompt

## Step 1: Create Your First Configuration Files

Create a base configuration file:

```sh
cat > base.yml << 'EOF'
application:
  name: my-app
  version: 1.0.0

database:
  host: localhost
  port: 5432
  name: myapp_db
  pool_size: 10

server:
  host: 0.0.0.0
  port: 8080
  timeout: 30

features:
  - authentication
  - logging
EOF
```

Create an environment-specific overlay:

```sh
cat > production.yml << 'EOF'
database:
  host: db.prod.example.com
  pool_size: 50

server:
  port: 443
  timeout: 60
  ssl: true

features:
  - (( prepend ))
  - rate-limiting
  - monitoring
EOF
```

## Step 2: Merge the Files

Run Graft to merge the files:

```sh
graft merge base.yml production.yml
```

**Output:**

```yaml
application:
  name: my-app
  version: 1.0.0
database:
  host: db.prod.example.com
  name: myapp_db
  pool_size: 50
  port: 5432
features:
- rate-limiting
- monitoring
- authentication
- logging
server:
  host: 0.0.0.0
  port: 443
  ssl: true
  timeout: 60
```

Notice how:

- `database.host` was overwritten with the production value
- `database.pool_size` was overwritten
- `database.port` and `database.name` were preserved from base
- `features` array was prepended (production items first)
- `server.ssl` was added

Notice the shape of the output too. Graft always prints the whole merged
document, sorts map keys alphabetically at every level, and emits list items
at their parent key's indentation rather than indented beneath it.

## Step 3: Use Operators

Create a configuration with operators:

```sh
cat > config.yml << 'EOF'
meta:
  environment: production
  region: us-east-1

database:
  url: (( concat "postgres://" database.host ":" database.port "/" database.name ))
  host: db.prod.example.com
  port: 5432
  name: (( concat "app_" meta.environment ))

server:
  name: (( concat meta.region "-server" ))

computed:
  full_name: '(( concat "Application: " application.name " v" application.version ))'

application:
  name: my-app
  version: 2.0.0
EOF
```

`full_name` is wrapped in single quotes because its expression contains a
`: ` sequence, which YAML would otherwise read as a mapping key. The same
applies to any expression containing a ternary — `size: '(( large ? "8Gi" :
"2Gi" ))'` — and an expression must stay on one line, since a plain scalar
cannot span lines.

```sh
graft merge config.yml
```

**Output:**

```yaml
application:
  name: my-app
  version: 2.0.0
computed:
  full_name: "Application: my-app v2.0.0"
database:
  host: db.prod.example.com
  name: app_production
  port: 5432
  url: postgres://db.prod.example.com:5432/app_production
meta:
  environment: production
  region: us-east-1
server:
  name: us-east-1-server
```

## Step 4: Reference Values with Grab

Use `grab` to reference values from elsewhere:

```sh
cat > with-grab.yml << 'EOF'
defaults:
  timeout: 30
  retries: 3

services:
  api:
    timeout: (( grab defaults.timeout ))
    retries: (( grab defaults.retries ))
  worker:
    timeout: (( grab defaults.timeout * 2 ))
    retries: (( grab defaults.retries ))
EOF
```

```sh
graft merge with-grab.yml
```

**Output:**

```yaml
defaults:
  retries: 3
  timeout: 30
services:
  api:
    retries: 3
    timeout: 30
  worker:
    retries: 3
    timeout: 60
```

## Step 5: Use Conditionals

Create environment-aware configurations:

```sh
cat > conditional.yml << 'EOF'
environment: production

(( if environment == "production" ))
replicas: 5
resources:
  memory: 4Gi
  cpu: "2"
(( else ))
replicas: 1
resources:
  memory: 512Mi
  cpu: "0.5"
(( fi ))
EOF
```

```sh
graft merge conditional.yml
```

**Output:**

```yaml
environment: production
replicas: 5
resources:
  cpu: "2"
  memory: 4Gi
```

## Step 6: Compare Files with Diff

Compare two configurations. `graft diff` reports structural changes — paths
that were added, removed, or changed — rather than a line-by-line text diff,
and exits 1 when the documents differ:

```sh
graft diff base.yml production.yml
```

## Step 7: Convert to JSON

Graft reads YAML or JSON on either side of a merge, and `graft json`
converts the result:

```sh
graft merge base.yml production.yml | graft json
```

## What's Next?

Now that you've got the basics, explore more:

- [CLI Commands](../user-guide/cli/) - Full command reference
- [Operators](../user-guide/operators/) - All 47 operators
- [Array Merging](../user-guide/array-merging.md) - Advanced array operations
- [Secrets Management](../user-guide/secrets/) - Integrate with Vault, AWS, NATS
- [Examples](../examples/) - More practical examples

## Common Patterns

### Multi-file Merge

Merge multiple files in order:

```sh
graft merge base.yml env.yml secrets.yml overrides.yml
```

### Cherry-pick Specific Keys

Output only specific sections:

```sh
graft merge base.yml prod.yml --cherry-pick database --cherry-pick server
```

### Prune Keys from Output

Remove specific keys:

```sh
graft merge base.yml prod.yml --prune meta --prune internal
```

### Skip Operator Evaluation

See the raw operators without evaluation:

```sh
graft merge config.yml --skip-eval
```

### Select a List Entry by Field

Both flags accept a `field=value` predicate in place of a path segment,
written in dotted form:

```sh
graft merge --cherry-pick 'servers.name=primary' config.yml
graft merge --prune 'servers.name=replica' config.yml
```
