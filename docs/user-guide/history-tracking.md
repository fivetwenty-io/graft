# History Tracking

Graft tracks the complete history of how each value was derived during merge and evaluation.

## Overview

History tracking answers the question: "Where did this value come from?"

For each path in the final document, Graft can tell you:

- Which file(s) contributed to the value
- What the value was at each stage
- When operators were evaluated
- The final resolved value

## Enabling History

### CLI

```sh
graft merge --history base.yml overlay.yml secrets.yml
```

### Library

```go
result, _ := engine.Merge(ctx, base, overlay).
    TrackHistory().
    Execute()

history := result.History()
```

## History Output

### Full History

```sh
graft merge --history base.yml env.yml secrets.yml
```

**Output:**
```
Merge History:

database.host:
  [0] base.yml:12      → "localhost"
  [1] env.yml:5        → "db.prod.example.com"
  Final                → "db.prod.example.com"

database.password:
  [0] base.yml:13      → (( param "Required" ))
  [1] secrets.yml:3    → (( vault "secret/db:password" ))
  [2] <evaluated>      → "***REDACTED***"
  Final                → "***REDACTED***"

database.pool_size:
  [0] base.yml:14      → 10
  [1] env.yml:8        → (( calc * 5 ))
  [2] <evaluated>      → 50
  Final                → 50

server.timeout:
  [0] base.yml:20      → 30
  Final                → 30  (unchanged)
```

### Trace Specific Path

```sh
graft merge --trace-path database.password base.yml env.yml secrets.yml
```

**Output:**
```
database.password:
  [0] base.yml:13      → (( param "Required" ))
      Type: operator (param)
      Note: Required parameter marker

  [1] secrets.yml:3    → (( vault "secret/db:password" ))
      Type: operator (vault)
      Note: Overwrote param marker

  [2] <evaluated>      → "***REDACTED***"
      Type: evaluation
      Note: Vault operator resolved
      Backend: vault
      Path: secret/db:password
      Duration: 45ms

  Final                → "***REDACTED***"
```

### Show Changes

```sh
graft merge --show-changes base.yml env.yml
```

**Output:**
```
Merge Summary: 2 files → 45 keys (12 changed, 8 added, 2 removed)

database.host:
  ✗ base.yml:12        "localhost"
  ✓ env.yml:5          "db.prod.example.com"

database.pool_size:
  ✗ base.yml:14        10
  ○ env.yml:8          (( calc * 5 ))
  ✓ <evaluated>        50

meta.internal:
  ✗ base.yml:45        { debug: true }
  - <pruned>

api.key:
  + env.yml:15         "abc123"
```

**Legend:**

- ✓ Final value used
- ✗ Value overwritten
- ○ Intermediate value (unevaluated operator)
- \+ Added
- \- Removed/pruned

### Changes Only

```sh
graft merge --changes-only base.yml env.yml
```

**Output:**
```
Changed paths (12 of 45):
  database.host        "localhost" → "db.prod.example.com"
  database.pool_size   10 → 50
  server.timeout       30 → 60
  server.ssl           <none> → true
  ...
```

## History Phases

History entries are tagged with their phase:

| Phase | Description |
|-------|-------------|
| LOAD | Initial file loading |
| MERGE | Value merging from overlays |
| EVAL | Operator evaluation |
| POST | Post-processing (prune, etc.) |

```
database.url:
  [0] base.yml:15      LOAD   → (( concat "..." ))
  [1] <evaluated>      EVAL   → "postgres://localhost:5432/app"
  [2] <redacted>       POST   → "postgres://***:***@localhost:5432/app"
```

## History Entry Details

Each history entry contains:

| Field | Description |
|-------|-------------|
| Index | Order in history (0 = first) |
| Source | File name |
| Line | Line number in source |
| Phase | When change occurred |
| Operation | Type of change |
| OldValue | Previous value |
| NewValue | New value |
| Operator | If value is/was an operator |
| Evaluated | Result after evaluation |
| Timestamp | When it happened |

## Library API

### Access History

```go
result, _ := engine.Merge(ctx, docs...).
    TrackHistory().
    Execute()

history := result.History()

// All paths that have history
for _, path := range history.AllPaths() {
    fmt.Println(path)
}

// Only paths that changed
for _, path := range history.ChangedPaths() {
    fmt.Println(path)
}
```

### Path History

```go
// Get history for specific path
entries := history.ForPath("database.password")

for _, entry := range entries {
    fmt.Printf("[%d] %s:%d → %v\n",
        entry.Index,
        entry.Source,
        entry.Line,
        entry.NewValue)
}
```

### Query History

```go
// Query with filters
entries := history.Query(graft.HistoryFilter{
    Path:   "database.*",
    Phase:  graft.PhaseEval,
    Source: "secrets.yml",
})
```

### Timeline

```go
// Get all changes in order
timeline := history.Timeline()

for _, entry := range timeline {
    fmt.Printf("%s: %s changed from %v to %v\n",
        entry.Source,
        entry.Path,
        entry.OldValue,
        entry.NewValue)
}
```

### Export History

```go
// As JSON
jsonData, _ := history.ToJSON()

// As YAML
yamlData, _ := history.ToYAML()
```

## Practical Examples

### Debugging Merge Issues

```sh
# Why is my value wrong?
graft merge --trace-path database.connection_string \
    base.yml env.yml secrets.yml

# See all changes
graft merge --history base.yml env.yml secrets.yml | \
    grep "database\."
```

### Audit Trail

```sh
# Generate audit report
echo "# Configuration Audit"
echo "Generated: $(date)"
echo
echo "## Source Files"
echo "- base.yml"
echo "- production.yml"
echo "- secrets.yml"
echo
echo "## Change History"
graft merge --history base.yml production.yml secrets.yml
```

### CI/CD Verification

```sh
#!/bin/bash
# Verify expected values and their sources

OUTPUT=$(graft merge --history base.yml env.yml)

# Check database.host came from env.yml
if ! echo "$OUTPUT" | grep -q "database.host:.*env.yml"; then
    echo "ERROR: database.host should come from env.yml"
    exit 1
fi

echo "Configuration sources verified"
```

### Interactive Debugging

```sh
graft debug base.yml env.yml secrets.yml

graft> history database.password
database.password:
  [0] base.yml:13    → (( param "Required" ))
  [1] secrets.yml:3  → (( vault "secret/db:password" ))

graft> inspect database.password
(( vault "secret/db:password" ))  # Not yet evaluated

graft> eval database.password
Evaluating: (( vault "secret/db:password" ))
Result: "my-secret-password"
```

## Performance Considerations

History tracking adds overhead:

- Memory: Stores all intermediate values
- CPU: Additional processing per change

For production with large files, consider:

```sh
# Trace specific paths instead of full history
graft merge --trace-path critical.setting base.yml env.yml
```

In the library:

```go
// Enable only when needed
if debug {
    builder = builder.TrackHistory()
}
```

## Secret Redaction

Sensitive values are automatically redacted in history output:

```
database.password:
  [0] base.yml:13      → (( param "Required" ))
  [1] secrets.yml:3    → (( vault "secret/db:password" ))
  [2] <evaluated>      → "***REDACTED***"
```

Configure redaction patterns:

```go
engine, _ := graft.NewEngine(
    graft.WithHistoryRedaction([]string{
        "password",
        "secret",
        "key",
        "token",
    }),
)
```

## See Also

- [Diff & Comparison](diffing.md) - Comparing documents
- [debug Command](cli/debug.md) - Interactive debugging
- [merge Command](cli/merge.md) - Merge with history
