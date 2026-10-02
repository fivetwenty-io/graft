# Diff & Comparison

Graft provides semantic comparison of YAML/JSON documents, in several
output formats, using a report that matches `spruce diff`.

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

### Default (Human Report)

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

This is graft's own spruce-compatible human-readable report. `graft diff`
with no flags prints the same bytes `spruce diff` prints, apart from the
differences listed under [Accepted differences from
spruce](#accepted-differences-from-spruce). Exit code `1` (differences
found).

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

Type changes (e.g. `8080` → `"8080"`) show up in the default report as
`± type change from int to string`, with both the old and new value
visible. `--changes` reports them as MODIFIED, since it has no separate
symbol for a type change.

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
± type change from int to string
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

## Input Parsing

`graft diff` reads its two inputs with graft's own YAML reader, which is
built on goccy/go-yaml and made to behave like the yaml.v3 reader spruce
uses. The two readers agree on almost every input. The subsections below
cover the places where they don't, so a diff that surprises you can be
traced to its cause.

### Template placeholders

An unquoted `{{...}}` placeholder in a value position reads as a plain
string. That matters for files such as Concourse pipeline templates, where
`a: {{x}}` against `a: {{y}}` reports a value change:

```
a
± value change
- {{x}}
+ {{y}}
```

spruce parses `{{x}}` as an empty nested map, so it reports no difference
between those two files, and it panics when the placeholders sit in a
simple list. `graft merge` reads the same placeholder as a string and
writes it back quoted, so `a: {{x}}` merges to `a: '{{x}}'`. `graft json`
and files pulled in with `(( load ))` read placeholders the same way, so a
Concourse file behaves the same wherever graft reads YAML.

The rewrite applies only in value positions. A placeholder used as a
mapping key, and a run of unbalanced braces such as `{{{{`, stay parse
errors.

### Parse errors

When an input does not parse, `graft diff` prints `unable to parse data
from <file>: yaml: ` and exits `2`, as spruce does, and then prints
goccy's message. The line number matches spruce's where libyaml and goccy
agree. For some errors it differs, namely unterminated quotes, unclosed
flow collections, bad indentation, and errors where spruce prints no line
at all. `TestParseErrorLineDivergences` in `internal/yamlnode` records
those cases, and the [Genesis compatibility
contract](../spruce/genesis-compat-contract.md#parse-errors) lists them
in a table.

### Accepted differences from spruce

These are the deliberate differences between `graft diff` and `spruce
diff`. Each one is pinned by a test, so a change to any of them is
caught.

graft accepts these inputs, and spruce rejects them:

- The `\/` escape in a double-quoted string.
- Raw control characters.
- A stream that holds only `...`.
- Content after `...` without a new `---`.
- A `%YAML 1.2` directive.

graft rejects these inputs, and spruce accepts them:

- Complex mapping keys.
- An empty tagged value followed by another key, such as `a: !!str` and
  then `b: 1`.
- `!!merge <<` used as a value.
- A `%TAG !!` directive.

`TestParseAcceptanceDivergences` pins the `%YAML`, empty tagged value,
`!!merge`, and `%TAG !!` cases.

One comment layout lands in a different place. When a comment block sits
between a key and its block collection, and a later line of the block
starts left of the key, graft keeps the earlier line as the key's foot
comment, where yaml.v3 gives it to the first item. In this document, graft
attaches `# c22` to `y0`, and yaml.v3 attaches it to the first item:

```yaml
a:
  y0:
  # c22
# c1
  - 1
```

Both tools give `# c1` to the first item.
`TestCommentsDedentedBlockDivergence` pins graft's placement.

Where spruce panics, graft exits `2` with an error message instead of a
stack trace. That covers an SGR `38` sequence without valid arguments
inside a value, a tag of exactly `!`, and a modification with a nil node.

TOML local dates and times print in the machine's local zone, as they do
in spruce. graft uses the offset of the process's local zone, read when
the file loads, as spruce does at startup.

graft reads a CRLF file exactly as it reads the same file with LF line
endings. spruce prints an extra blank line above a commented key in a CRLF
file, and graft does not.

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

`pkg/graft` exports `DiffDocuments(a, b Document, opts *DiffOptions)
(DiffResult, error)` for comparing two documents from Go code. It also
exports a lower-level `Diff(a, b interface{}) (Diffable, error)` helper
(spruce-inherited), which has no non-test callers anywhere in graft.

Neither one is part of the `diff` command's own code path. `graft diff`
builds on graft's internal comparison and report packages, so treat
`Diff` as an implementation detail, not a supported API.

## See Also

- [diff Command](cli/diff.md) - CLI reference
- [History Tracking](history-tracking.md) - Merge history
- [merge Command](cli/merge.md) - Merge with change tracking
