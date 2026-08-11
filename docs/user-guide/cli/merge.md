# merge Command

Merge multiple YAML/JSON files into a single document.

## Usage

```sh
graft merge [flags] file1.yml file2.yml ... fileN.yml
```

Files are merged left to right. Values in later files override values in earlier files.

## Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--skip-eval` | | Don't evaluate operators |
| `--prune` | | Remove key from output (repeatable) |
| `--cherry-pick` | | Output only specific keys (repeatable) |
| `--fallback-append` | | Use append for array merges (default: inline) |
| `--go-patch` | | Treat file as go-patch format |
| `--multi-doc` | `-m` | Handle multi-document YAML |
| `--history` | | Show merge history for all paths |
| `--trace-path` | | Show history for specific path |
| `--show-changes` | | Show merge change tree |
| `--changes-only` | | Show only changed paths |
| `--interactive` | | Enter debugging REPL (equivalent to `graft debug`; no short form) |

## Basic Usage

### Simple Merge

```sh
graft merge base.yml overlay.yml
```

**base.yml:**
```yaml
database:
  host: localhost
  port: 5432
```

**overlay.yml:**
```yaml
database:
  host: db.prod.example.com
```

**Output:**
```yaml
database:
  host: db.prod.example.com
  port: 5432
```

### Multiple Files

```sh
graft merge base.yml env.yml secrets.yml overrides.yml
```

Files are processed in order. Each subsequent file overlays the result of previous merges.

## Controlling Output

### Prune Keys

Remove specific keys from output:

```sh
graft merge base.yml overlay.yml --prune meta --prune internal
```

**Example:**
```yaml
# Input
database:
  host: localhost
meta:
  author: admin
internal:
  debug: true

# Output (with --prune meta --prune internal)
database:
  host: localhost
```

### Cherry-pick Keys

Output only specific keys:

```sh
graft merge base.yml overlay.yml --cherry-pick database --cherry-pick server
```

**Example:**
```yaml
# Input (merged)
database:
  host: localhost
server:
  port: 8080
logging:
  level: debug
meta:
  version: 1.0

# Output (with --cherry-pick database --cherry-pick server)
database:
  host: localhost
server:
  port: 8080
```

## Operator Control

### Skip Evaluation

See raw operators without evaluation:

```sh
graft merge config.yml --skip-eval
```

**config.yml:**
```yaml
database:
  url: (( concat "postgres://" host ":" port ))
  host: localhost
  port: 5432
```

**With --skip-eval:**
```yaml
database:
  url: (( concat "postgres://" host ":" port ))
  host: localhost
  port: 5432
```

**Without --skip-eval:**
```yaml
database:
  url: postgres://localhost:5432
  host: localhost
  port: 5432
```

## History and Tracing

### Show Full History

See where every value came from:

```sh
graft merge --history base.yml env.yml secrets.yml
```

**Output:**
```
database.host:
  [0] base.yml:12      → "localhost"
  [1] env.yml:5        → "db.prod.example.com"
  Final                → "db.prod.example.com"

database.password:
  [0] base.yml:13      → (( param "Required" ))
  [1] secrets.yml:3    → (( vault "secret/db:password" ))
  [2] <evaluated>      → "***REDACTED***"
  Final                → "***REDACTED***"
```

### Trace Specific Path

Show history for one path only:

```sh
graft merge --trace-path database.host base.yml env.yml
```

**Output:**
```
database.host:
  [0] base.yml:12      → "localhost"
  [1] env.yml:5        → "db.prod.example.com"
  Final                → "db.prod.example.com"
```

### Show Changes

See the merge change tree:

```sh
graft merge --show-changes base.yml env.yml secrets.yml
```

**Output:**
```
Merge Summary: 3 files → 45 keys (12 changed, 8 added, 2 removed)

database.host:
  ✗ base.yml:12        "localhost"
  ✓ env.yml:5          "db.prod.example.com"

database.password:
  ✗ base.yml:13        (( param "Required" ))
  ○ secrets.yml:3      (( vault "secret/db:password" ))
  ✓ <evaluated>        "s3cr3t-p4ss"
```

**Legend:**

- ✓ Final value used
- ✗ Value overwritten
- ○ Intermediate value (operator before evaluation)
- \+ Added
- \- Removed/pruned

### Changes Only

Show only paths that changed:

```sh
graft merge --changes-only base.yml env.yml
```

## Array Merging

### Default Behavior (Inline)

By default, arrays merge by index:

```yaml
# base.yml
items:
  - first
  - second

# overlay.yml
items:
  - replaced

# Result
items:
  - replaced
  - second
```

### Fallback to Append

Use `--fallback-append` to append instead:

```sh
graft merge --fallback-append base.yml overlay.yml
```

```yaml
# Result with --fallback-append
items:
  - first
  - second
  - replaced
```

### Array Operators

Use array operators for precise control:

```yaml
items:
  - (( append ))
  - new-item
```

See [Array Merging](../array-merging.md) for full details.

## Special Formats

### Multi-Document YAML

Handle YAML files with multiple documents:

```sh
graft merge --multi-doc multi.yml overlay.yml
```

### Go-Patch Format

Apply go-patch operations:

```sh
graft merge base.yml --go-patch patch.yml
```

## Interactive Mode

Enter debugging REPL for step-by-step merge:

```sh
graft merge --interactive base.yml overlay.yml secrets.yml
```

See [debug](debug.md) for full REPL documentation.

## Examples

### Environment Build

```sh
# Build production config
graft merge \
  configs/base.yml \
  configs/environments/production.yml \
  configs/secrets/production.yml \
  > production-config.yml
```

### Kubernetes ConfigMap

```sh
# Generate and apply ConfigMap
graft merge base.yml env.yml | \
  kubectl create configmap app-config --from-file=config.yml=/dev/stdin
```

### Partial Output

```sh
# Get only database config as JSON
graft merge base.yml prod.yml \
  --cherry-pick database \
  | graft json
```

### Debugging Merge Issues

```sh
# See where a value came from
graft merge --trace-path database.connection_string \
  base.yml env.yml secrets.yml
```

## See Also

- [diff](diff.md) - Compare configurations
- [Array Merging](../array-merging.md) - Array merge strategies
- [Operators](../operators/) - All operators
- [debug](debug.md) - Interactive debugging
