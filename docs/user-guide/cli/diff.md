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
| `--no-color` | | Disable colorized output, overriding the global `--color` |
| `--quiet` | `-q` | Exit with status only, no output |

## Output Formats

### Default (dyff Report)

With no format flag, `diff` prints [dyff](https://github.com/homeport/dyff)'s
own human-readable report:

```sh
graft diff base.yml modified.yml
```

**Output** (with `base.yml`/`modified.yml` as shown under
[Change Types](#change-types) below):
```

(root level)
- one map entry removed:
meta:
  version: 1.0

database
+ one map entry added:
ssl: true

database.host
± value change
- localhost
+ db.prod.example.com

database.timeout
± value change
- 30
+ 60


```

dyff orders its own report by path depth (root-level entries first, then
each nested path), and brackets it with a leading and a trailing blank
line.

`--side-by-side`, `--unified`, and `--changes` (below) render the same
underlying comparison differently; they don't add information dyff's own
report is missing.

### Side-by-Side

Compare both files' full content in two columns, aligned by a line-level
diff of their rendered YAML text:

```sh
graft diff --side-by-side base.yml modified.yml
```

**Output:**
```
base.yml                               │ modified.yml
───────────────────────────────────────┼───────────────────────────────────────
database:                              │ database:
  host: localhost                      │   host: db.prod.example.com
  port: 5432                           │   port: 5432
  timeout: 30                          │   ssl: true
meta:                                  │   timeout: 60
  version: "1.0"                       │ 
```

Because this aligns raw *lines*, not keys, a run of several changed/added
lines in a row (here, `timeout`'s value changing and `ssl` being added)
lines up as one block rather than each key getting its own row — nothing
is lost, but don't expect strict per-key alignment on rows that changed.

**Color coding:**

- Red: removed-only rows
- Green: added-only rows
- Yellow: modified rows (present, changed, on both sides)
- Uncolored: unchanged context rows

### Unified (Git-Style)

Git-style diff, grouped by top-level key (`@@ <key> @@` headers, not
numeric line ranges):

```sh
graft diff --unified base.yml modified.yml
```

**Output:**
```diff
--- base.yml
+++ modified.yml
@@ database @@
-  host: localhost
+  host: db.prod.example.com
   port: 5432
-  timeout: 30
+  ssl: true
+  timeout: 60
@@ meta @@
-  version: "1.0"
```

Only top-level keys with at least one change get a hunk; unchanged keys
(here, nothing else at the root) are omitted entirely, not shown as
context. `--context <n>` controls how many unchanged lines surround each
change within a hunk (default `3`).

### Change List

Detailed list of all changes, grouped by kind (modified, then added, then
removed) and sorted by path within each group:

```sh
graft diff --changes base.yml modified.yml
```

**Output:**
```
Changes (2 modified, 1 added, 1 removed):

  MODIFIED  database.host
            - localhost
            + db.prod.example.com

  MODIFIED  database.timeout
            - 30
            + 60

  ADDED     database.ssl
            + true

  REMOVED   meta
            - version: "1.0"
```

A wholly removed/added multi-key subtree (here, all of `meta`) is one
entry at the subtree's own root, not flattened to each of its leaves —
the same convention `graft diff`'s default report and `merge
--show-changes` use for an entirely new/removed section.

## Choosing One Rendering

`--side-by-side`, `--unified`, and `--changes` are mutually exclusive.
Passing more than one is refused rather than silently resolved in favor
of whichever graft checks first:

```sh
graft diff --unified --side-by-side base.yml modified.yml
```

```
--side-by-side, --unified, and --changes are mutually exclusive; pick one
```

## Color

Color is decided in two stages, and the per-command flag wins.

The global `--color` flag takes `on`, `off`, or `auto`, and defaults to
`auto`. Under `auto`, graft colors its output only when standard output
is a terminal, so a diff piped into a file or another program comes out
as plain text without your asking for it.

`--color=on` forces color on even when the destination is not a
terminal. Use it when piping into a pager that understands escape
sequences:

```sh
graft --color=on diff --changes base.yml modified.yml | less -R
```

`diff`'s own `--no-color` flag overrides whatever `--color` asked for, so
`graft --color=on diff --changes --no-color` prints no escape sequences
at all. Reach for it when a script sets `--color=on` globally and one
command inside it needs clean text.

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

Identical (semantically) files produce no output and exit `0` — dyff has
nothing to report, so there is no diff-specific "identical" message.

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

- [Inspecting a Merge](../../examples/inspecting-a-merge.md) - A walkthrough that uses all four renderings on a real configuration

- [merge](merge.md) - Merge configurations
- [History Tracking](../history-tracking.md) - Track where values came from
