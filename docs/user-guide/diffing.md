# Diff & Comparison

Graft provides rich, semantic comparison of YAML/JSON documents with multiple output formats.

## Overview

Unlike text-based diff tools, Graft compares documents **semantically**:

- Key order doesn't matter
- Equivalent values are equal
- Type-aware comparison
- Understands YAML/JSON structure

## Diff Formats

### Default (Summary)

```sh
graft diff base.yml modified.yml
```

**Output:**
```
3 differences between base.yml and modified.yml

database.host:
  - "localhost"
  + "db.prod.example.com"

database.timeout:
  - 30
  + 60

database.ssl:
  + true (added)
```

### Side-by-Side

Compare files in two columns:

```sh
graft diff --side-by-side base.yml modified.yml
```

**Output:**
```
base.yml                              │ modified.yml
──────────────────────────────────────┼──────────────────────────────────────
database:                             │ database:
  host: "localhost"                   │   host: "db.prod.example.com"
  port: 5432                          │   port: 5432
  timeout: 30                         │   timeout: 60
                                      │   ssl: true
```

Control width:

```sh
graft diff --side-by-side --width=160 base.yml modified.yml
```

### Unified (Git-Style)

Standard patch format:

```sh
graft diff --unified base.yml modified.yml
```

**Output:**
```diff
--- base.yml
+++ modified.yml
@@ database @@
-  host: "localhost"
+  host: "db.prod.example.com"
   port: 5432
-  timeout: 30
+  timeout: 60
+  ssl: true
```

With context lines:

```sh
graft diff --unified --context=5 base.yml modified.yml
```

### Change List

Detailed list with old and new values:

```sh
graft diff --changes base.yml modified.yml
```

**Output:**
```
Changes (2 modified, 1 added, 0 removed):

  MODIFIED  database.host
            - "localhost"
            + "db.prod.example.com"

  MODIFIED  database.timeout
            - 30
            + 60

  ADDED     database.ssl
            + true
```

## Change Types

| Type | Symbol | Description |
|------|--------|-------------|
| Added | `+` | Key exists only in second file |
| Removed | `-` | Key exists only in first file |
| Modified | `~` | Value changed |
| Type Changed | `!` | Type changed (e.g., string → number) |

## Color Coding

Output is colorized by default when writing to a terminal:

| Element | Color |
|---------|-------|
| Added | Green |
| Removed | Red |
| Modified | Yellow |
| Unchanged | Gray |
| Path | Cyan |

Disable color:

```sh
graft diff --no-color base.yml modified.yml
```

Force color (e.g., for CI):

```sh
graft diff --color=on base.yml modified.yml | less -R
```

## Semantic Comparison

### Key Order Independence

```yaml
# file1.yml
database:
  host: localhost
  port: 5432

# file2.yml (same content, different order)
database:
  port: 5432
  host: localhost
```

```sh
graft diff file1.yml file2.yml
# Output: Files are semantically identical
```

### Value Equivalence

These are considered equal:

```yaml
# file1.yml
enabled: true
count: 42

# file2.yml
enabled: True      # Same as true
count: 42          # Same number
```

### Type Awareness

These are considered different:

```yaml
# file1.yml
port: 8080        # integer

# file2.yml
port: "8080"      # string
```

```
MODIFIED  port
  - 8080 (int)
  + "8080" (string)
```

## Merge Change Tracking

### Show Changes from Merge

```sh
graft merge --show-changes base.yml overlay.yml
```

**Output:**
```
Merge Summary: 2 files → 45 keys (5 changed, 2 added, 1 removed)

database.host:
  ✗ base.yml:12        "localhost"
  ✓ overlay.yml:5      "db.prod.example.com"

database.pool_size:
  ✗ base.yml:14        10
  ✓ overlay.yml:7      50

api.key:
  + overlay.yml:10     "abc123" (added)

meta.internal:
  - base.yml:20        (removed)
```

**Legend:**

- ✓ Final value used
- ✗ Value overwritten
- \+ Added
- \- Removed

### Changes Only

Show only paths that changed:

```sh
graft merge --changes-only base.yml overlay.yml
```

**Output:**
```
Changed paths (5 of 45):
  database.host        "localhost" → "db.prod.example.com"
  database.pool_size   10 → 50
  database.ssl         <none> → true
  api.key              <none> → "abc123"
  meta.internal        {...} → <removed>
```

## Practical Examples

### Pre-Deploy Validation

```sh
# Compare current vs proposed config
graft diff production-current.yml production-new.yml

# Only deploy if different
if ! graft diff --quiet current.yml new.yml; then
  echo "Changes detected:"
  graft diff --changes current.yml new.yml
  kubectl apply -f new.yml
fi
```

### Environment Comparison

```sh
# Compare dev and production
graft diff envs/dev.yml envs/prod.yml --changes
```

### Merge Preview

```sh
# See what merge would change
graft merge base.yml overlay.yml > merged.yml
graft diff base.yml merged.yml
```

### CI/CD Checks

```sh
#!/bin/bash
# Fail if config changed unexpectedly

graft merge base.yml env.yml > actual.yml

if ! graft diff --quiet expected.yml actual.yml; then
  echo "Configuration mismatch!"
  graft diff --side-by-side expected.yml actual.yml
  exit 1
fi
```

### Configuration Audit

```sh
# Generate change report
echo "# Configuration Changes"
echo "Generated: $(date)"
echo
graft diff --changes old-config.yml new-config.yml
```

## Exit Codes

| Code | Description |
|------|-------------|
| 0 | Files are identical |
| 1 | Files differ |
| 2 | Error (file not found, parse error) |

Use in scripts:

```sh
if graft diff --quiet file1.yml file2.yml; then
  echo "No changes"
else
  echo "Files differ"
fi
```

## Library API

```go
engine, _ := graft.NewEngine()

doc1, _ := engine.ParseFile("before.yml")
doc2, _ := engine.ParseFile("after.yml")

diff := engine.Diff(doc1, doc2)

// Check for changes
if !diff.HasChanges() {
    fmt.Println("No changes")
    return
}

// Iterate changes
for _, change := range diff.Changes() {
    switch change.Type {
    case graft.ChangeAdded:
        fmt.Printf("+ %s: %v\n", change.Path, change.NewValue)
    case graft.ChangeRemoved:
        fmt.Printf("- %s: %v\n", change.Path, change.OldValue)
    case graft.ChangeModified:
        fmt.Printf("~ %s: %v → %v\n", change.Path,
            change.OldValue, change.NewValue)
    }
}

// Output formatted
diff.WriteSideBySide(os.Stdout, &graft.DiffOptions{
    Color: true,
    Width: 120,
})
```

## See Also

- [diff Command](cli/diff.md) - CLI reference
- [History Tracking](history-tracking.md) - Merge history
- [merge Command](cli/merge.md) - Merge with change tracking
