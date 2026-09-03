# Diff & Comparison

Graft provides semantic comparison of YAML/JSON documents, in several
output formats, built on [dyff](https://github.com/homeport/dyff).

## Overview

Unlike text-based diff tools, graft compares documents **semantically**:

- Key order doesn't matter
- Equivalent values are equal (e.g. `true` and `True`)
- Type-aware comparison (`8080` and `"8080"` are different)
- Understands YAML/JSON structure

All examples below are real output, captured against:

```yaml
# base.yml
database:
  host: localhost
  port: 5432
  timeout: 30
meta:
  version: "1.0"
```

```yaml
# modified.yml
database:
  host: db.prod.example.com
  port: 5432
  timeout: 60
  ssl: true
```

## Diff Formats

### Default (dyff Human Report)

```sh
graft diff base.yml modified.yml
```

**Output:**
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

This is dyff's own human-readable report, unchanged — `graft diff` with no
flags delegates entirely to dyff's `HumanReport` renderer. Exit code `1`
(differences found).

### Change List

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

### Unified (Git-Style)

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

Hunks are grouped per top-level key (`@@ <key> @@`), each with its own
git-style unified diff body, rather than one hunk per contiguous line
range across the whole file. Context lines default to 3 (git's `-u`
default); control it with `--context`:

```sh
graft diff --unified --context=0 base.yml modified.yml
```

### Side-by-Side

```sh
graft diff --side-by-side base.yml modified.yml
```

**Output** (captured at `--width=70` for a narrower example; the default
total width is 80):
```
base.yml                          │ modified.yml
──────────────────────────────────┼──────────────────────────────────
database:                         │ database:
  host: localhost                 │   host: db.prod.example.com
  port: 5432                      │   port: 5432
  timeout: 30                     │   ssl: true
meta:                             │   timeout: 60
  version: "1.0"                  │ 
```

Rows are aligned by a line-level diff of each file's full YAML text (via
[`pmezard/go-difflib`](https://github.com/pmezard/go-difflib)'s
longest-matching-block (Ratcliff/Obershelp) implementation), so an
insertion/deletion shifts the alignment rather than comparing files
line-by-line positionally. `-y` is the short form.

Control the total width (both columns plus the separator) with `--width`:

```sh
graft diff --side-by-side --width=160 base.yml modified.yml
```

## Change Types

| Type | Symbol (default/`--changes`) | Description |
|------|--------|-------------|
| Added | `+` | Key exists only in the second file |
| Removed | `-` | Key exists only in the first file |
| Modified | `±` (default) / `MODIFIED` (`--changes`) | Value changed |

Type changes (e.g. `8080` → `"8080"`) show up as a MODIFIED/value change
with both the old and new value visible — there is no separate symbol for
"type changed" specifically; dyff reports it as a value modification.

## Color Coding

Output is colorized by default when writing to a terminal, following
graft's global `--color`/`--no-color` flags (see
[CLI Reference: Color flags](../reference/cli.md#color-flags)); `diff`
doesn't have flags of its own for this.

Disable color for one invocation:

```sh
graft diff --no-color base.yml modified.yml
```

`--color off` does the same thing:

```sh
graft diff --color off base.yml modified.yml
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
# No output, exit code 0: the two documents are semantically identical
```

### Value Equivalence

These are considered equal:

```yaml
# file1.yml
enabled: true
count: 42

# file2.yml
enabled: True      # Same as true
count: 42           # Same number
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
port
± value change
- 8080
+ "8080"
```

## Merge Change Tracking

`graft diff` compares two already-written files. To see what a *merge*
changed or would change, use `merge`'s own history/change flags — see
[History Tracking](history-tracking.md) for `--show-changes` and
`--changes-only` (these are `merge` flags, not `diff` flags; `graft diff`
has no `--show-changes`/`--changes-only` of its own).

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
graft diff --changes envs/dev.yml envs/prod.yml
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
| 0 | Files are semantically identical |
| 1 | Files differ |
| 2 | Error (file not found, parse error) |

`--quiet` suppresses all output but keeps these exit codes, for scripting:

```sh
if graft diff --quiet file1.yml file2.yml; then
  echo "No changes"
else
  echo "Files differ"
fi
```

## Flags Not Implemented

`--ignore-paths`/`--only-paths` (filtering the diff to exclude/include
specific paths) are not implemented. Combine `graft diff --changes` with
`grep`/`jq`-style post-filtering if you need this today.

## Library API

There is no public `graft.Diff`/`DiffResult`/`Change` library API. The
`pkg/graft` package does export a lower-level `Diff(a, b interface{})
(Diffable, error)` helper (spruce-inherited), but it has no non-test
callers anywhere in graft and is not part of the `diff` command's own code
path — `graft diff` builds directly on the `dyff`/`ytbx` packages, not on
`pkg/graft`'s `Diff`. Treat it as an implementation detail, not a
supported API.

## See Also

- [diff Command](cli/diff.md) - CLI reference
- [History Tracking](history-tracking.md) - Merge history
- [merge Command](cli/merge.md) - Merge with change tracking
