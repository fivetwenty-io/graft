# diff Command

Compare two YAML/JSON files semantically and display differences.

## Usage

```sh
graft diff [flags] file1.yml file2.yml
```

## Flags

| Flag | Short | Description |
|------|-------|-------------|
| `--side-by-side` | `-y` | Side-by-side diff view |
| `--unified` | `-u` | Unified diff format (git-style) |
| `--changes` | | List all changes (original → new) |
| `--context` | | Lines of context in unified diff |
| `--width` | | Width for side-by-side view |
| `--no-color` | | Disable colorized output |
| `--quiet` | `-q` | Exit with status only, no output |

## Output Formats

### Default (Semantic Diff)

Shows a summary of differences:

```sh
graft diff base.yml modified.yml
```

**Output:**
```
2 keys differ between base.yml and modified.yml

database.host:
  - "localhost"
  + "db.prod.example.com"

database.timeout:
  - 30
  + 60
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
meta:                                 │
  version: "1.0"                      │
```

**Color coding:**

- Red/strikethrough: removed lines
- Green: added lines
- Yellow/cyan: modified lines
- Gray: unchanged context

### Unified (Git-Style)

Standard unified diff format:

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
@@ meta @@
-  version: "1.0"
```

### Change List

Detailed list of all changes:

```sh
graft diff --changes base.yml modified.yml
```

**Output:**
```
Changes (2 modified, 1 added, 1 removed):

  MODIFIED  database.host
            - "localhost"
            + "db.prod.example.com"

  MODIFIED  database.timeout
            - 30
            + 60

  ADDED     database.ssl
            + true

  REMOVED   meta.version
            - "1.0"
```

## Options

### Context Lines

Control context in unified diff:

```sh
graft diff --unified --context=5 base.yml modified.yml
```

### Output Width

Control side-by-side width:

```sh
graft diff --side-by-side --width=160 base.yml modified.yml
```

### Quiet Mode

Check for differences without output:

```sh
if graft diff --quiet base.yml modified.yml; then
  echo "Files are identical"
else
  echo "Files differ"
fi
```

## Semantic Comparison

Unlike text-based diff tools, Graft compares files semantically:

- Key order doesn't matter
- Equivalent values are equal (e.g., `true` vs `True`)
- Type-aware comparison (1 vs "1" are different)

### Example

**file1.yml:**
```yaml
database:
  host: localhost
  port: 5432
```

**file2.yml:**
```yaml
# Different order, same content
port: 5432
host: localhost
database:
```

```sh
graft diff file1.yml file2.yml
```

**Output:**
```
Files are semantically identical
```

## Use Cases

### Pre-Deploy Validation

```sh
# Compare current vs new config
graft diff current-config.yml new-config.yml

# Only deploy if different
if ! graft diff --quiet current.yml new.yml; then
  kubectl apply -f new.yml
fi
```

### Configuration Review

```sh
# Review environment differences
graft diff envs/staging.yml envs/production.yml --changes
```

### Merge Preview

```sh
# See what a merge would change
graft merge base.yml overlay.yml > merged.yml
graft diff base.yml merged.yml
```

### CI/CD Checks

```sh
# Fail if config changed unexpectedly
graft diff expected.yml actual.yml --quiet || exit 1
```

## Exit Codes

| Code | Description |
|------|-------------|
| 0 | Files are identical |
| 1 | Files differ |
| 2 | Error (file not found, parse error, etc.) |

## Examples

### Basic Comparison

```sh
# Simple diff
graft diff old-config.yml new-config.yml
```

### Detailed Review

```sh
# Side-by-side with full width
graft diff --side-by-side --width=200 before.yml after.yml
```

### Script Integration

```sh
#!/bin/bash
# Deploy only if config changed

CONFIG_CHANGED=$(graft diff --quiet current.yml new.yml; echo $?)

if [ "$CONFIG_CHANGED" -eq 1 ]; then
  echo "Configuration changed, deploying..."
  graft diff --changes current.yml new.yml
  cp new.yml current.yml
  ./deploy.sh
else
  echo "No changes, skipping deploy"
fi
```

### Compare Merged Results

```sh
# Compare merge results between environments
graft merge base.yml dev.yml > dev-merged.yml
graft merge base.yml prod.yml > prod-merged.yml
graft diff dev-merged.yml prod-merged.yml
```

## See Also

- [merge](merge.md) - Merge configurations
- [History Tracking](../history-tracking.md) - Track where values came from
